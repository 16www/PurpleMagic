"""Exercise the shipped GM console against the isolated Hub publication API."""
import argparse
import base64
import json
from pathlib import Path
from urllib.parse import urlsplit
from playwright.sync_api import sync_playwright, expect

def run(receipt, output):
    config=json.loads(receipt.read_text());target=config['url'];public=config['publicUrl']
    for endpoint in [target,public]:
        assert urlsplit(endpoint).scheme=='http' and urlsplit(endpoint).hostname=='127.0.0.1'
    output.mkdir(parents=True,exist_ok=True);errors=[];calls=[]
    try:
        with sync_playwright() as p:
            browser=p.chromium.launch(headless=True)
            context=browser.new_context(viewport={'width':1440,'height':1000})
            def watch(page):
                page.on('pageerror',lambda e:errors.append(str(e)))
                page.on('request',lambda r:calls.append((r.method,urlsplit(r.url).path)) if '/api/' in r.url else None)
            page=context.new_page();watch(page)
            page.goto(target+'/admin/');page.locator('#token').fill('test-admin-token-that-is-long-and-isolated');page.locator('#login-form button').click()
            expect(page.locator('#workspace')).to_be_visible()
            page.goto(target+'/gm/console/#gateway')
            expect(page.get_by_role('heading',name='登录器公告',exact=True)).to_be_visible()
            expect(page.get_by_role('button',name='发布公告',exact=True)).to_be_enabled()
            def published():
                response=context.request.get(public+'/launcher/v2/bootstrap.json');assert response.ok
                return json.loads(base64.b64decode(response.json()['Payload']))
            def write_count():
                return calls.count(('PUT','/admin/api/news'))
            initial=published();before_preview=write_count()
            page.get_by_label('公告标题',exact=True).fill('新版本 <b>公告</b>')
            page.get_by_label('公告分类',exact=True).select_option('更新')
            page.get_by_label('显示日期',exact=True).fill('10 / 15')
            page.get_by_label('公告正文',exact=True).fill('第一行\n第二行 <script>原文</script>')
            page.get_by_role('button',name='预览',exact=True).click()
            expect(page.get_by_role('dialog')).to_contain_text('<script>原文</script>')
            assert page.locator('#dialog script,#dialog b').count()==0
            assert write_count()==before_preview,'preview submitted the announcement form'
            assert published()==initial,'preview changed the signed publication'
            page.get_by_role('button',name='关闭',exact=True).click()
            page.get_by_role('button',name='发布公告',exact=True).click()
            expect(page.get_by_role('status')).to_contain_text('公告已发布')
            expect(page.locator('.news-entry')).to_have_count(2)
            page.get_by_role('button',name='发布公告',exact=True).click()
            expect(page.get_by_role('status')).to_contain_text('公告已发布')
            expect(page.locator('.news-entry')).to_have_count(2)
            assert published()['news'][1]['title']=='新版本 <b>公告</b>'
            # A second window cannot replace a newer whole-list publication.
            second=context.new_page();watch(second);second.goto(target+'/gm/console/#gateway')
            expect(second.locator('.news-entry')).to_have_count(2)
            second.locator('.news-entry').nth(1).click()
            page.get_by_label('公告标题',exact=True).fill('新版公告已修订')
            page.get_by_role('button',name='发布公告',exact=True).click()
            expect(page.get_by_role('status')).to_contain_text('公告已发布')
            second.get_by_label('公告标题',exact=True).fill('旧窗口内容')
            second.get_by_role('button',name='发布公告',exact=True).click()
            expect(second.get_by_role('alert')).to_contain_text('其他窗口')
            expect(second.get_by_role('button',name='发布公告',exact=True)).to_be_disabled()
            expect(second.get_by_label('公告标题',exact=True)).to_have_value('旧窗口内容')
            assert published()['news'][1]['title']=='新版公告已修订'
            second.get_by_role('button',name='重新载入',exact=True).click()
            second.get_by_role('dialog').get_by_role('button',name='重新载入',exact=True).click()
            expect(second.locator('.news-entry').nth(1)).to_contain_text('新版公告已修订')
            second.close()
            page.get_by_role('button',name='上移',exact=True).click()
            expect(page.locator('.news-entry').first).to_contain_text('新版公告已修订')
            assert published()['news'][0]['title']=='新版公告已修订'
            page.screenshot(path=str(output/'announcements-light.png'),full_page=True)
            page.locator('#theme-open').click();page.locator('[data-action=preset][data-value=ember-dusk]').click();page.locator('[data-action=mode][data-value=dark]').click();page.keyboard.press('Escape');page.wait_for_timeout(300)
            page.screenshot(path=str(output/'announcements-ember.png'),full_page=True)
            page.set_viewport_size({'width':390,'height':844});page.screenshot(path=str(output/'announcements-mobile.png'),full_page=True)
            assert page.evaluate('document.documentElement.scrollWidth <= innerWidth + 1')
            page.set_viewport_size({'width':1440,'height':1000})
            before_cancel=write_count();before_cancel_publication=published()
            page.get_by_label('公告标题',exact=True).fill('尚未发布的编辑')
            page.get_by_role('button',name='删除',exact=True).click()
            page.get_by_role('dialog').get_by_role('button',name='取消',exact=True).click()
            assert write_count()==before_cancel,'delete confirmation submitted the edited announcement'
            assert published()==before_cancel_publication,'cancel changed the signed publication'
            assert len(published()['news'])==2
            page.get_by_role('button',name='删除',exact=True).click();page.get_by_role('button',name='删除并发布',exact=True).click()
            expect(page.locator('.news-entry')).to_have_count(1);assert len(published()['news'])==1
            page.clock.install();before=len(calls);page.clock.run_for(60000);page.wait_for_timeout(80);assert len(calls)==before
            page.get_by_role('button',name='退出',exact=True).click();expect(page).to_have_url(target+'/admin/');expect(page.locator('#login')).to_be_visible()
            assert not errors,errors
            assert not any('/api/news' in path and path!='/admin/api/news' for _,path in calls)
            browser.close()
        (output/'result.json').write_text(json.dumps({'passed':True,'calls':calls,'errors':errors,'checks':['existing Hub API','preview escaping','create/edit/replay','multi-window conflict','reorder','cancel/remove','signed public bootstrap readback','responsive warm theme','idle no polling','logout']},ensure_ascii=False,indent=2),encoding='utf-8')
        print('PASS real Hub + shipped GM announcement editor: publish/readback, conflict, ordering, delete, preview, mobile, theme and idle')
    finally:
        Path(str(receipt)+'.done').write_text('done',encoding='utf-8')

if __name__=='__main__':
    parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('receipt',type=Path);parser.add_argument('output',type=Path);args=parser.parse_args();run(args.receipt,args.output)
