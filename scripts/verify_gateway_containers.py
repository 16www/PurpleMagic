"""Verify container upgrades, recovery and account/character isolation in the live preview."""
import json
from pathlib import Path
from playwright.sync_api import sync_playwright, expect

ROOT=Path(__file__).resolve().parents[1]
STATE=ROOT/'runtime/preview/gateway-live'
OUT=ROOT/'runtime/verification/gateway-containers'


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
        expect(page.get_by_label('管理密码')).not_to_have_value('')
        page.get_by_role('button',name='登录',exact=True).click()
        dialog=page.locator('#dialog')
        panel=dialog.get_by_role('region',name='角色容量')
        base=ready['url'].split('/console/')[0]+'/api/v1/'

        def get(path):
            res=context.request.get(base+path)
            assert res.status==200,res.text()
            return res.json()['data']

        def open_character(account='晨雾',name='晨雾·剑影'):
            page.get_by_role('button',name='打开'+account,exact=True).click()
            page.get_by_role('button',name='查看'+name,exact=True).click()
            dialog.get_by_role('tab',name='容量管理',exact=True).click()

        def preview(reason):
            panel.get_by_label('扩容原因',exact=True).fill(reason)
            panel.get_by_role('button',name='预览扩容',exact=True).click()
            expect(panel.get_by_label('扩容确认')).to_contain_text(reason)

        def current(main,avatar,cargo):
            for id,value in [('main',main),('avatars',avatar),('warehouse',cargo)]:
                expect(panel.locator('[data-capacity-current="'+id+'"]').first).to_have_text(str(value))

        open_character()
        current(0,0,8)
        inventory=get('characters/10/inventory?container=equipment&limit=100')
        currency=get('accounts/preview:1/currency')['balance']
        panel.get_by_label('主背包扩展',exact=True).select_option('8')
        panel.get_by_label('装扮栏扩展',exact=True).select_option('7')
        panel.get_by_label('个人仓库',exact=True).select_option('24')
        preview('活动扩容')
        current(0,0,8)
        page.screenshot(path=str(OUT/'containers-confirm.png'),full_page=True)
        lost=[]

        def lose_response(route):
            if route.request.method!='POST' or lost:
                route.continue_();return
            response=route.fetch()
            assert response.status==200,response.text()
            lost.append(True);route.abort('connectionreset')

        page.route('**/container-operations',lose_response)
        panel.get_by_role('button',name='确认扩容提交',exact=True).click()
        expect(panel.get_by_role('button',name='重试扩容并取得回执',exact=True)).to_be_enabled()
        assert lost
        page.reload();open_character();current(8,7,24)
        panel.get_by_role('button',name='重试扩容并取得回执',exact=True).click()
        expect(panel.get_by_role('status')).to_contain_text('共 3 项')
        expect(panel.locator('tbody tr')).to_have_count(1)
        page.unroute('**/container-operations',lose_response)
        assert get('characters/10/inventory?container=equipment&limit=100')==inventory
        assert get('accounts/preview:1/currency')['balance']==currency
        panel.get_by_role('button',name='全部扩至最大',exact=True).click()
        preview('全部容量补齐')
        expect(panel.get_by_label('扩容确认')).to_contain_text('200')
        panel.get_by_role('button',name='确认扩容提交',exact=True).click()
        expect(panel.get_by_role('status')).to_contain_text('共 3 项')
        current(24,56,200)
        for name in ['主背包扩展','装扮栏扩展','个人仓库']:
            expect(panel.get_by_label(name,exact=True)).to_be_disabled()
        expect(panel.locator('tbody tr')).to_have_count(2)
        dialog.get_by_role('button',name='关闭',exact=True).click()
        open_character('晨雾','晨雾·逐光');current(0,0,8)
        expect(panel.locator('tbody')).to_contain_text('暂无 GM')
        # Another administrator changes capacity after this browser's preview.
        panel.get_by_label('个人仓库',exact=True).select_option('24');preview('过时扩容预览')
        snap=get('characters/11/containers');headers={'X-CSRF-Token':get('capabilities')['csrfToken']}
        body={'accountId':snap['accountId'],'targets':{'warehouse':40},'revision':snap['revision'],'reason':'另一管理员扩容'}
        plan=context.request.post(base+'characters/11/container-operations/preview',data=body,headers=headers)
        assert plan.status==200,plan.text()
        body['planToken']=plan.json()['data']['planToken'];headers['Idempotency-Key']='browser-container-conflict-001'
        result=context.request.post(base+'characters/11/container-operations',data=body,headers=headers)
        assert result.status==200,result.text()
        panel.get_by_role('button',name='确认扩容提交',exact=True).click()
        expect(panel.get_by_role('alert')).to_contain_text('已变更')
        panel.get_by_role('button',name='刷新容量与流水',exact=True).click();current(0,0,40)
        dialog.get_by_role('button',name='关闭',exact=True).click()
        open_character('远山','远山·剑影');current(0,0,8)
        expect(panel.locator('tbody')).to_contain_text('暂无 GM')
        dialog.get_by_role('button',name='关闭',exact=True).click()
        open_character('星河','星河·逐光')
        expect(panel).to_contain_text('角色尚未创建容器存档')
        expect(panel.get_by_role('button',name='预览扩容',exact=True)).to_be_disabled()
        dialog.get_by_role('button',name='关闭',exact=True).click()
        open_character();current(24,56,200)
        page.wait_for_timeout(300);baseline=len(requests);page.wait_for_timeout(1600)
        assert len(requests)==baseline,'idle requests'
        page.screenshot(path=str(OUT/'containers-desktop.png'),full_page=True)
        dialog.get_by_role('button',name='关闭',exact=True).click()
        page.locator('#theme-open').click()
        page.locator('[data-action=preset][data-value=ember-dusk]').click()
        page.locator('[data-action=mode][data-value=dark]').click()
        page.keyboard.press('Escape');open_character();current(24,56,200)
        page.screenshot(path=str(OUT/'containers-dark.png'),full_page=True)
        page.set_viewport_size({'width':390,'height':844})
        page.screenshot(path=str(OUT/'containers-mobile.png'),full_page=True)
        assert page.evaluate('document.documentElement.scrollWidth<=innerWidth+1')
        assert not errors,errors
        browser.close()
    (OUT/'result.json').write_text(json.dumps({'checks':['real-gateway','three-container-atomic-upgrade','preview-zero-write','lost-response-reload-replay','maximum-stages','inventory-currency-preserved','same-account-and-cross-account-isolation','missing-state','stale-preview','ledger','no-idle-poll','dark-theme','mobile'],'errors':errors},ensure_ascii=False,indent=2),encoding='utf-8')
    print('PASS gateway container browser checks')


if __name__=='__main__':main()
