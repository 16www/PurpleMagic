"""Exercise account-owned inventory through the isolated gateway preview."""
import json
from pathlib import Path
from playwright.sync_api import sync_playwright, expect

ROOT=Path(__file__).resolve().parents[1]
STATE=ROOT/'runtime/preview/gateway-live'
OUT=ROOT/'runtime/verification/gateway-account-inventory'


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

        def open_character(name='晨雾·剑影',account='晨雾',container='account-cargo'):
            page.get_by_role('button',name='打开'+account,exact=True).click()
            page.get_by_role('button',name='查看'+name,exact=True).click()
            page.get_by_role('tab',name='背包与仓库',exact=True).click()
            page.get_by_label('物品容器').select_option(container)

        open_character()
        dialog=page.locator('#dialog')
        editor=dialog.get_by_role('region',name='物品操作',exact=True)
        expect(editor).to_contain_text('该账号所有角色')
        dialog.get_by_role('button',name='编辑槽位 0',exact=True).click()
        editor.get_by_label('品级',exact=True).select_option('1')
        editor.get_by_label('物品操作原因',exact=True).fill('账号金库装备修复')
        editor.get_by_role('button',name='预览物品变更',exact=True).click()
        confirm=editor.get_by_label('物品变更确认',exact=True)
        expect(confirm).to_be_visible()
        confirm.scroll_into_view_if_needed()
        page.screenshot(path=str(OUT/'account-cargo-confirm.png'),full_page=True)
        editor.get_by_role('button',name='确认物品提交',exact=True).click()
        expect(editor.get_by_role('status')).to_contain_text('配置已完成，共 1 格')
        dialog.get_by_label('物品容器').select_option('shared')
        dialog.get_by_role('button',name='编辑槽位 354',exact=True).click()
        expect(editor.get_by_label('品级',exact=True)).to_have_count(0)
        editor.get_by_label('物品数量',exact=True).fill('2000')
        editor.get_by_label('物品操作原因',exact=True).fill('共享晶块调整')
        editor.get_by_role('button',name='预览物品变更',exact=True).click()
        expect(confirm).to_contain_text('1200 → 2000')
        lost=[]

        def lose_response(route):
            if route.request.method!='POST' or lost:
                route.continue_();return
            response=route.fetch()
            assert response.status==200,response.text()
            lost.append(True);route.abort('connectionreset')

        page.route('**/inventory-operations',lose_response)
        editor.get_by_role('button',name='确认物品提交',exact=True).click()
        expect(editor.get_by_role('button',name='重试并取得物品回执',exact=True)).to_be_enabled()
        assert lost
        page.reload();open_character(container='shared')
        editor.get_by_role('button',name='重试并取得物品回执',exact=True).click()
        expect(editor.get_by_role('status')).to_contain_text('配置已完成，共 1 格')
        page.unroute('**/inventory-operations',lose_response)
        dialog.get_by_role('tab',name='账号物品记录',exact=True).click()
        expect(dialog.locator('tbody tr')).to_have_count(2)
        dialog.get_by_role('button',name='查看物品回执',exact=True).first.click()
        expect(dialog.get_by_label('所选记录详情')).to_contain_text('共享晶块调整')
        page.screenshot(path=str(OUT/'account-inventory-ledger.png'),full_page=True)
        page.keyboard.press('Escape');open_character('晨雾·逐光',container='shared')
        expect(dialog.locator('tbody tr').first).to_contain_text('2000')
        dialog.get_by_role('tab',name='账号物品记录',exact=True).click()
        expect(dialog.locator('tbody tr')).to_have_count(2)
        dialog.get_by_role('tab',name='背包与仓库',exact=True).click()
        dialog.get_by_label('物品容器').select_option('account-cargo')
        dialog.locator('[data-inventory-select="0"]').check()
        editor.get_by_role('button',name='删除所选',exact=True).click()
        editor.get_by_label('物品操作原因',exact=True).fill('另一角色清理账号金库')
        editor.get_by_role('button',name='预览物品变更',exact=True).click()
        expect(confirm).to_contain_text('删除整格')
        editor.get_by_role('button',name='确认物品提交',exact=True).click()
        expect(editor.get_by_role('status')).to_contain_text('删除已完成，共 1 格')
        page.keyboard.press('Escape');open_character()
        expect(dialog).to_contain_text('暂无记录')
        dialog.get_by_role('tab',name='账号物品记录',exact=True).click()
        expect(dialog.locator('tbody tr')).to_have_count(3)
        dialog.get_by_role('tab',name='物品操作记录',exact=True).click()
        expect(dialog).to_contain_text('暂无记录')
        page.keyboard.press('Escape');open_character('远山·剑影','远山')
        dialog.get_by_role('tab',name='账号物品记录',exact=True).click()
        expect(dialog).to_contain_text('暂无记录')
        page.keyboard.press('Escape')
        page.locator('#theme-open').click()
        page.locator('[data-action=preset][data-value=ember-dusk]').click()
        page.locator('[data-action=mode][data-value=dark]').click()
        page.keyboard.press('Escape');open_character(container='shared')
        dialog.get_by_role('button',name='编辑槽位 354',exact=True).click()
        editor.get_by_label('物品数量',exact=True).fill('2500')
        editor.get_by_label('物品操作原因',exact=True).fill('账号资产预览')
        editor.get_by_role('button',name='预览物品变更',exact=True).click()
        expect(confirm).to_be_visible()
        page.set_viewport_size({'width':390,'height':844})
        confirm.scroll_into_view_if_needed()
        page.screenshot(path=str(OUT/'account-inventory-mobile.png'),full_page=True)
        assert page.evaluate('document.documentElement.scrollWidth<=innerWidth+1')
        assert dialog.evaluate('(d)=>d.scrollWidth<=d.clientWidth+1')
        page.wait_for_timeout(300);baseline=len(requests);page.wait_for_timeout(1200)
        assert len(requests)==baseline,'idle polling'
        assert not errors,errors
        browser.close()
    (OUT/'browser-result.json').write_text(json.dumps({'checks':['account-cargo-configure','shared-count','lost-response-reload','same-account-role-visibility','account-ledger','other-account-isolation','character-ledger-isolation','account-cargo-delete','dark-mobile','no-idle-poll'],'errors':errors},ensure_ascii=False,indent=2),encoding='utf-8')
    print('PASS gateway account inventory browser checks')


if __name__=='__main__':main()
