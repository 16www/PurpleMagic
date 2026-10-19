"""Verify the new GM workflow only against a disposable loopback HTTP/DB fixture."""
import argparse
import json
import os
import time
from pathlib import Path
from urllib.parse import urlsplit
from playwright.sync_api import sync_playwright, expect

def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--url',required=True)
    p.add_argument('--item',required=True)
    p.add_argument('--output-dir',type=Path,default=Path(os.environ.get('DNF90GM_FEEDBACK_BROWSER_OUTPUT',str(Path(__file__).resolve().parents[1]/'runtime/tests/feedback-browser'))))
    args=p.parse_args()
    external=os.environ.get('DNF90GM_EXTERNAL_FEEDBACK')=='1'
    if urlsplit(args.url).hostname!='127.0.0.1' or urlsplit(args.url).scheme!='http': p.error('Disposable loopback fixture required')
    out=args.output_dir;out.mkdir(parents=True,exist_ok=True)
    errors,calls=[],[]
    with sync_playwright() as pw:
        browser=pw.chromium.launch(headless=True)
        context=browser.new_context(viewport={'width':1440,'height':1000})
        page=context.new_page()
        page.on('pageerror',lambda e:errors.append(str(e)))
        wire=[]
        def record_response(response):
            if '/api/v1/tasks/' in response.url or '/api/v1/capabilities' in response.url:
                try: wire.append({'url':response.url,'status':response.status,'body':response.json()})
                except Exception: pass
                (out/'wire.json').write_text(json.dumps(wire,ensure_ascii=False,indent=2),encoding='utf-8')
        page.on('response',record_response)
        page.on('request',lambda r:calls.append({'method':r.method,'url':r.url}) if '/api/' in r.url else None)
        def get(path):
            r=context.request.get(args.url+'/api/v1/'+path)
            assert r.status==200,r.text()
            return r.json()['data']
        def mails(cid,title):
            return [m for m in get(f'characters/{cid}/mailbox?limit=100') if m['title']==title]
        page.goto(args.url+'/console/#players')
        page.get_by_label('管理密码',exact=True).fill('fixture')
        page.get_by_role('button',name='登录',exact=True).click()
        page.locator('[data-account="wallet:1"]').click()
        page.locator('[data-character="10"]').click()
        workspace=page.get_by_role('region',name='玩家工作区')
        expect(workspace.get_by_role('tab')).to_have_count(8)
        expect(page.locator('#dialog')).not_to_be_visible()
        workspace.get_by_role('tab',name='金币与点券',exact=True).click()
        workspace.get_by_role('button',name='账号点券',exact=True).click()
        expect(workspace.locator('#currency-form')).to_be_visible()
        page.get_by_role('button',name='点券与流水',exact=True).click()
        expect(page.locator('#currency-form')).to_have_count(1)
        expect(workspace.locator('#currency-form')).not_to_be_visible()
        page.get_by_role('button',name='角色列表',exact=True).click()
        page.locator('[data-character="10"]').click()
        workspace.get_by_role('tab',name='金币与点券',exact=True).click()
        workspace.get_by_role('button',name='账号点券',exact=True).click()
        expect(page.locator('#currency-form')).to_have_count(1)
        page.screenshot(path=str(out/'workspace-desktop.png'),full_page=True)
        page.locator('[data-account="wallet:2"]').click()
        expect(workspace).to_contain_text('选择角色')
        expect(workspace.locator('#currency-form')).not_to_be_visible()
        page.locator('[data-character="12"]').click()
        expect(workspace).to_contain_text('角色12')
        expect(workspace).not_to_contain_text('角色10')
        page.locator('[data-account="wallet:1"]').click()
        page.locator('[data-character="10"]').click()
        page.set_viewport_size({'width':390,'height':844})
        page.locator('[data-character="11"]').click()
        expect(page.locator('.picker-collapsed')).not_to_be_visible()
        expect(workspace).to_contain_text('角色11')
        page.screenshot(path=str(out/'workspace-mobile.png'),full_page=True)
        assert page.evaluate('document.documentElement.scrollWidth<=innerWidth+1')
        page.emulate_media(color_scheme='dark')
        expect(page.locator('html')).to_have_attribute('data-theme','dark')
        page.screenshot(path=str(out/'workspace-mobile-dark.png'),full_page=True)
        page.set_viewport_size({'width':1440,'height':1000})
        page.screenshot(path=str(out/'workspace-desktop-dark.png'),full_page=True)
        page.emulate_media(color_scheme='light')
        page.get_by_role('button',name='展开选人区',exact=True).click()
        page.locator('[data-pick="account"][data-id="wallet:1"]').check()
        page.locator('[data-pick="character"][data-id="10"]').check()
        page.get_by_role('button',name='批量发送道具',exact=True).click()
        form=page.locator('#op-compose')
        expect(form).to_contain_text('1 个账号（全部现有角色）、1 个角色')
        title='一次确认批量道具'
        form.get_by_label('邮件标题',exact=True).fill(title)
        form.get_by_label('物品数量',exact=True).fill('1')
        form.get_by_label('物品 ID',exact=True).fill(args.item)
        form.get_by_label('物品 ID',exact=True).blur()
        expect(form.get_by_label('装备品级',exact=True)).to_be_visible()
        targets=get('accounts/wallet%3A1/characters')
        # An incomplete confirmation roster must never leave Send enabled.
        page.route('**/api/v1/tasks/*/confirmation',lambda route:route.abort(),times=1)
        form.get_by_role('button',name='发送道具',exact=True).click()
        dialog=page.locator('#dialog')
        expect(dialog.get_by_role('alert')).to_be_visible()
        expect(dialog.get_by_role('button',name='确认并发放',exact=True)).to_be_disabled()
        dialog.get_by_role('button',name='取消',exact=True).last.click()
        start=len(calls)
        form.get_by_role('button',name='发送道具',exact=True).click()
        dialog=page.locator('#dialog')
        expect(dialog).to_be_visible()
        expect(dialog).to_contain_text(f'最终收件人 {len(targets)} 个')
        for c in targets: expect(dialog).to_contain_text(c['name'])
        assert not any('/run' in c['url'] for c in calls[start:])
        page.screenshot(path=str(out/'batch-confirmation.png'),full_page=True)
        dialog.get_by_role('button',name='确认并发放',exact=True).click()
        expect(page.get_by_role('status').filter(has_text=f'已投递 {len(targets)} / {len(targets)}')).to_be_visible(timeout=20000)
        assert len([c for c in calls[start:] if c['url'].endswith('/schedule' if external else '/run')])==(1 if external else 2)
        for c in targets: assert len(mails(c['characterId'],title))==1,c
        assert not mails('12',title)
        page.reload()
        form.get_by_role('button',name='发送道具',exact=True).click()
        expect(dialog).to_contain_text('0 个待发角色')
        dialog.get_by_role('button',name='取消',exact=True).last.click()
        for c in targets: assert len(mails(c['characterId'],title))==1
        # The first committed batch response is lost; reusing the draft resumes
        # the fixed job and its receipts instead of creating duplicate mail.
        lost_title='响应丢失恢复'
        form.get_by_label('邮件标题',exact=True).fill(lost_title)
        def lose_response(route):
            response=route.fetch()
            assert response.status==200,response.text()
            route.abort()
        page.route('**/api/v1/tasks/*/'+('schedule' if external else 'run'),lose_response,times=1)
        form.get_by_role('button',name='发送道具',exact=True).click()
        dialog.get_by_role('button',name='确认并发放',exact=True).click()
        expect(page.get_by_role('alert')).to_be_visible(timeout=20000)
        if external:
            page.goto('about:blank')
            deadline=time.monotonic()+20
            while time.monotonic()<deadline and not all(len(mails(c['characterId'],lost_title))==1 for c in targets): time.sleep(.2)
            page.goto(args.url+'/console/#mail')
        else:
            page.reload()
            form.get_by_role('button',name='发送道具',exact=True).click()
            expect(dialog).to_contain_text(f'{len(targets)-10} 个待发角色')
            dialog.get_by_role('button',name='确认并发放',exact=True).click()
            expect(page.get_by_role('status').filter(has_text=f'已投递 {len(targets)} / {len(targets)}')).to_be_visible(timeout=20000)
        for c in targets: assert len(mails(c['characterId'],lost_title))==1,c
        page.get_by_role('button',name='全部角色福利',exact=True).click()
        form.get_by_label('邮件标题',exact=True).fill('自动福利无需发放按钮')
        form.get_by_label('金币',exact=True).fill('7')
        form.get_by_role('button',name='保存规则',exact=True).click()
        row=page.get_by_role('row').filter(has_text='自动福利无需发放按钮')
        expect(row).to_have_count(1)
        start=len(calls)
        row.get_by_role('button',name='启用',exact=True).click()
        dialog.get_by_role('button',name='确认启用',exact=True).click()
        expect(row).to_contain_text('现有及未来角色自动发放')
        page.reload()
        deadline=time.monotonic()+20
        while time.monotonic()<deadline and not mails('14','自动福利无需发放按钮'):
            time.sleep(.1)
        assert not any('/preview' in c['url'] or c['url'].endswith('/run') for c in calls[start:])
        assert len(mails('14','自动福利无需发放按钮'))==1,get('characters/14/mailbox?limit=100')
        page.screenshot(path=str(out/'automatic-benefit.png'),full_page=True)
        assert not errors,errors
        (out/'result.json').write_text(json.dumps({'ok':True,'recipientCount':len(targets),'consoleErrors':errors,'calls':calls},ensure_ascii=False,indent=2),encoding='utf-8')
        browser.close()
    print('GM feedback browser: PASS')

if __name__=='__main__':main()
