"""Exercise online mail snapshots against the disposable gateway preview."""
import json
from pathlib import Path
from playwright.sync_api import sync_playwright,expect

ROOT=Path(__file__).resolve().parents[1]
STATE=ROOT/'runtime/preview/gateway-live'
OUT=ROOT/'runtime/verification/gateway-online-mail'


def main():
    OUT.mkdir(parents=True,exist_ok=True)
    ready=json.loads((STATE/'ready.json').read_text(encoding='utf-8'))
    errors,requests=[],[]
    with sync_playwright() as p:
        browser=p.chromium.launch(headless=True)
        context=browser.new_context(ignore_https_errors=True,viewport={'width':1440,'height':1000})
        page=context.new_page()
        page.on('pageerror',lambda e:errors.append(str(e)))
        page.on('request',lambda r:requests.append(r.url) if '/api/' in r.url else None)
        page.goto(ready['url'])
        page.get_by_label('导入登录 Key').set_input_files(str(STATE/'login-key.json'))
        page.get_by_role('button',name='登录',exact=True).click()
        expect(page.get_by_role('button',name='打开流光',exact=True)).to_be_visible()
        base=ready['url'].split('/console/')[0]+'/api/v1/'

        def get(path):
            r=context.request.get(base+path)
            assert r.status==200,r.text()
            return r.json()['data']

        before={n:len(get(f'characters/{n}/mailbox')) for n in (10,11,20,30,40,41)}
        page.evaluate("location.hash='mail'")
        form=page.locator('#op-compose')
        form.get_by_role('button',name='在线角色',exact=True).click()
        expect(form.get_by_label('账号 ID',exact=True)).to_have_count(0)
        form.get_by_label('邮件标题',exact=True).fill('在线活动奖励')
        form.get_by_label('金币',exact=True).fill('500')
        page.screenshot(path=str(OUT/'online-compose.png'),full_page=True)
        lost=[]

        def drop(route):
            response=route.fetch()
            assert response.status==200,response.text()
            lost.append(response.json()['data'])
            route.abort()

        page.route('**/api/v1/mail/online-preview',drop)
        form.get_by_role('button',name='预览邮件',exact=True).click()
        expect(page.get_by_role('alert')).to_be_visible()
        assert len(lost)==1
        page.unroute('**/api/v1/mail/online-preview',drop)
        page.reload()
        expect(form.get_by_label('金币',exact=True)).to_have_value('500')
        expect(form.get_by_label('账号 ID',exact=True)).to_have_count(0)
        form.get_by_role('button',name='预览邮件',exact=True).click()
        detail=page.get_by_role('region',name='任务详情',exact=True)
        expect(detail).to_contain_text('预览时在线角色')
        expect(detail).to_contain_text('在线名单时间')
        expect(detail).to_contain_text('流光·剑影')
        expect(detail).to_contain_text('流光·逐光')
        recovered=get('tasks/'+lost[0]['id'])
        assert recovered==lost[0],'retry resampled online roles'
        for n,count in before.items():assert len(get(f'characters/{n}/mailbox'))==count
        page.locator('#theme-open').click()
        page.locator('[data-action=preset][data-value=ember-dusk]').click()
        page.locator('[data-action=mode][data-value=dark]').click()
        page.keyboard.press('Escape')
        page.set_viewport_size({'width':390,'height':844})
        detail.scroll_into_view_if_needed()
        page.wait_for_timeout(250)
        page.screenshot(path=str(OUT/'online-task-mobile.png'),full_page=True)
        assert page.evaluate('document.documentElement.scrollWidth<=innerWidth+1')
        detail.get_by_role('button',name='确认发放',exact=True).click()
        page.locator('#dialog').get_by_role('button',name='确认并发放',exact=True).click()
        expect(detail).to_contain_text('成功 2 · 失败 0')
        for n,count in before.items():assert len(get(f'characters/{n}/mailbox'))==count+(1 if n in (40,41) else 0)
        page.evaluate("location.hash='mail'")
        form.get_by_role('button',name='新建在线发放',exact=True).click()
        form.get_by_role('button',name='预览邮件',exact=True).click()
        expect(detail).to_contain_text('待发 2 · 成功 0')
        online=[v for v in get('tasks') if v.get('recipientSource')]
        assert len(online)==2 and len({v['recipientSource']['snapshotId'] for v in online})==2
        page.wait_for_timeout(300)
        start=len(requests)
        page.wait_for_timeout(1600)
        assert len(requests)==start,'idle polling'
        assert not errors,errors
        browser.close()
    (OUT/'browser-result.json').write_text(json.dumps({'checks':['online-scope','fixed-members-and-time','lost-preview-recovery','offline-roles-excluded','confirmed-delivery','explicit-new-snapshot','mobile-theme','idle-zero-requests'],'errors':errors},ensure_ascii=False,indent=2),encoding='utf-8')
    print('PASS online mail browser checks')


if __name__=='__main__':main()
