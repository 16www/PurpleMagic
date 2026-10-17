"""Verify mailbox deletions against the disposable gateway preview."""
import json
from pathlib import Path
from playwright.sync_api import sync_playwright, expect

ROOT=Path(__file__).resolve().parents[1]
STATE=ROOT/'runtime/preview/gateway-live'
OUT=ROOT/'runtime/verification/gateway-mailbox'


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
        panel=dialog.get_by_role('region',name='邮箱删除操作')
        base=ready['url'].split('/console/')[0]+'/api/v1/'

        def get(path):
            res=context.request.get(base+path)
            assert res.status==200,res.text()
            return res.json()['data']

        def open_character(account='晨雾',name='晨雾·逐光'):
            page.get_by_role('button',name='打开'+account,exact=True).click()
            page.get_by_role('button',name='查看'+name,exact=True).click()
            dialog.get_by_role('tab',name='邮箱',exact=True).click()
            expect(panel).to_be_visible()

        def preview(reason):
            panel.get_by_label('邮件删除原因').fill(reason)
            panel.get_by_role('button',name='预览邮件删除',exact=True).click()

        def confirm():
            panel.get_by_role('button',name='确认删除邮件',exact=True).click()

        original=get('characters/10/mailbox')
        gold=get('characters/11/gold')
        open_character()
        expect(dialog.locator('[data-mail-select]')).to_have_count(4)
        dialog.locator('[data-mail-delete="preview-gift"]').click()
        preview('撤回活动奖励')
        expect(panel.get_by_role('alert')).to_contain_text('未领取资产')
        assert len(get('characters/11/mailbox'))==4
        panel.get_by_label('同时丢弃未领取资产').check()
        preview('撤回活动奖励')
        expect(panel.get_by_label('邮件删除确认')).to_contain_text('50000')
        expect(panel.get_by_label('邮件删除确认')).to_contain_text('490009735')
        panel.get_by_label('邮件删除确认').scroll_into_view_if_needed()
        page.screenshot(path=str(OUT/'mailbox-confirm.png'),full_page=True)
        assert len(get('characters/11/mailbox'))==4
        lost=[]

        def lose_response(route):
            if route.request.method!='POST' or lost:
                route.continue_();return
            response=route.fetch()
            assert response.status==200,response.text()
            lost.append(True);route.abort('connectionreset')

        page.route('**/mailbox-operations',lose_response)
        confirm()
        expect(panel.get_by_role('button',name='重试并取得邮件回执')).to_be_enabled()
        assert lost
        page.reload();open_character()
        expect(dialog.locator('[data-mail-select]')).to_have_count(3)
        panel.get_by_role('button',name='重试并取得邮件回执').click()
        expect(panel.get_by_role('status')).to_contain_text('已删除 1 封')
        assert len(get('characters/11/mailbox-operations'))==1
        page.unroute('**/mailbox-operations',lose_response)
        panel.get_by_role('button',name='选择本页已领取',exact=True).click()
        expect(dialog.locator('[data-mail-select]:checked')).to_have_count(1)
        dialog.locator('[data-mail-select="preview-letter"]').check()
        preview('清理补给与通知')
        expect(panel.get_by_label('邮件删除确认')).to_contain_text('活动通知')
        confirm()
        expect(panel.get_by_role('status')).to_contain_text('已删除 2 封')
        expect(dialog.locator('[data-mail-select]')).to_have_count(1)
        dialog.get_by_role('tab',name='邮件删除记录',exact=True).click()
        expect(dialog.locator('[data-mail-receipt]')).to_have_count(2)
        dialog.locator('[data-mail-receipt="0"]').click()
        expect(dialog.get_by_label('所选记录详情')).to_contain_text('清理补给与通知')
        page.screenshot(path=str(OUT/'mailbox-ledger.png'),full_page=True)
        dialog.get_by_role('tab',name='邮箱',exact=True).click()
        expect(dialog.locator('[data-mail-select]')).to_have_count(1)
        dialog.locator('[data-mail-delete="preview-extra"]').click();preview('旧删除预览')
        expect(panel.get_by_label('邮件删除确认')).to_contain_text('旧删除预览')
        # Another administrator commits first; the displayed preview then conflicts.
        headers={'X-CSRF-Token':get('capabilities')['csrfToken']}
        body={'accountId':'preview:1','items':[{'mailId':'preview-extra'}],'reason':'另一管理员清理'}
        plan=context.request.post(base+'characters/11/mailbox-operations/preview',data=body,headers=headers)
        assert plan.status==200,plan.text()
        body.update({k:plan.json()['data'][k] for k in ('items','planToken')})
        headers['Idempotency-Key']='browser-mailbox-conflict-001'
        response=context.request.post(base+'characters/11/mailbox-operations',data=body,headers=headers)
        assert response.status==200,response.text()
        confirm();expect(panel.get_by_role('alert')).to_contain_text('已删除')
        dialog.get_by_role('button',name='刷新记录',exact=True).click()
        expect(dialog.locator('[data-mail-select]')).to_have_count(0)
        assert get('characters/10/mailbox')==original
        assert get('characters/11/gold')==gold
        dialog.get_by_role('button',name='关闭',exact=True).click()
        open_character('远山','远山·剑影')
        expect(dialog.locator('[data-mail-select]')).to_have_count(4)
        assert get('characters/20/mailbox-operations')==[]
        page.wait_for_timeout(300);baseline=len(requests);page.wait_for_timeout(1600)
        assert len(requests)==baseline,'idle requests'
        page.screenshot(path=str(OUT/'mailbox-desktop.png'),full_page=True)
        dialog.get_by_role('button',name='关闭',exact=True).click()
        page.locator('#theme-open').click()
        page.locator('[data-action=preset][data-value=ember-dusk]').click()
        page.locator('[data-action=mode][data-value=dark]').click()
        page.keyboard.press('Escape');open_character('远山','远山·剑影')
        expect(dialog.locator('[data-mail-select]')).to_have_count(4)
        page.screenshot(path=str(OUT/'mailbox-dark.png'),full_page=True)
        page.set_viewport_size({'width':390,'height':844})
        dialog.locator('[data-mail-delete="preview-gift"]').click()
        panel.get_by_label('同时丢弃未领取资产').check()
        preview('活动奖励撤回预览')
        expect(panel.get_by_label('邮件删除确认')).to_contain_text('50000')
        panel.get_by_label('邮件删除确认').scroll_into_view_if_needed()
        page.screenshot(path=str(OUT/'mailbox-mobile.png'),full_page=True)
        assert page.evaluate('document.documentElement.scrollWidth<=innerWidth+1')
        assert not errors,errors
        browser.close()
    (OUT/'result.json').write_text(json.dumps({'checks':['real-gateway','single-delete','unclaimed-confirmation','preview-zero-write','mixed-batch','lost-response-reload-replay','receipt-detail','stale-preview','character-account-isolation','no-idle-poll','themes','mobile'],'errors':errors},ensure_ascii=False,indent=2),encoding='utf-8')
    print('PASS gateway mailbox browser checks')


if __name__=='__main__':main()
