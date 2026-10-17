"""Exercise inventory editing and deletion with the disposable production gateway."""
import json
from pathlib import Path
from playwright.sync_api import sync_playwright, expect

ROOT=Path(__file__).resolve().parents[1]
STATE=ROOT/'runtime/preview/gateway-live'
OUT=ROOT/'runtime/verification/gateway-inventory'


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

        def open_character(name='晨雾·剑影'):
            page.get_by_role('button',name='打开晨雾',exact=True).click()
            page.get_by_role('button',name='查看'+name,exact=True).click()
            page.get_by_role('tab',name='背包与仓库',exact=True).click()

        open_character()
        dialog=page.locator('#dialog')
        editor=dialog.get_by_role('region',name='物品操作',exact=True)
        expect(dialog.locator('[data-inventory-select]')).to_have_count(20)
        dialog.get_by_role('button',name='编辑槽位 9',exact=True).click()
        editor.get_by_label('强化等级',exact=True).fill('12')
        editor.get_by_label('品级',exact=True).select_option('0')
        editor.get_by_label('物品操作原因',exact=True).fill('装备属性修复')
        editor.get_by_role('button',name='预览物品变更',exact=True).click()
        confirm=editor.get_by_label('物品变更确认',exact=True)
        expect(confirm).to_be_visible()
        expect(confirm).to_contain_text('强化')
        predicted=confirm.inner_text()
        confirm.scroll_into_view_if_needed()
        page.screenshot(path=str(OUT/'inventory-configure-confirm.png'),full_page=True)
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
        page.reload();open_character()
        editor.get_by_role('button',name='重试并取得物品回执',exact=True).click()
        expect(editor.get_by_role('status')).to_contain_text('配置已完成，共 1 格')
        page.unroute('**/inventory-operations',lose_response)
        dialog.get_by_role('tab',name='物品操作记录',exact=True).click()
        expect(dialog.locator('tbody tr')).to_have_count(1)
        dialog.get_by_role('button',name='查看物品回执',exact=True).click()
        expect(dialog.get_by_label('所选记录详情')).to_contain_text('装备属性修复')
        recorded=dialog.get_by_label('所选记录详情').locator('.inventory-changes').inner_text()
        assert recorded in predicted, 'random preview changed on commit'
        dialog.get_by_role('tab',name='背包与仓库',exact=True).click()
        dialog.get_by_role('button',name='编辑槽位 9',exact=True).click()
        editor.get_by_label('强化等级',exact=True).fill('32')
        editor.get_by_label('物品操作原因',exact=True).fill('边界检查')
        editor.get_by_role('button',name='预览物品变更',exact=True).click()
        assert not confirm.is_visible(), 'HTML range validation missing'
        editor.get_by_label('强化等级',exact=True).fill('13')
        editor.get_by_role('button',name='预览物品变更',exact=True).click()
        expect(confirm).to_be_visible()
        editor.get_by_label('强化等级',exact=True).fill('14')
        expect(confirm).not_to_be_visible()
        editor.get_by_role('button',name='清空物品选择',exact=True).click()
        dialog.locator('[data-inventory-select="10"]').check()
        dialog.locator('[data-inventory-select="11"]').check()
        editor.get_by_role('button',name='删除所选',exact=True).click()
        editor.get_by_label('物品操作原因',exact=True).fill('清理示例物品')
        editor.get_by_role('button',name='预览物品变更',exact=True).click()
        expect(confirm.locator('.inventory-change')).to_have_count(2)
        editor.get_by_role('button',name='确认物品提交',exact=True).click()
        expect(editor.get_by_role('status')).to_contain_text('删除已完成，共 2 格')
        expect(dialog.locator('[data-inventory-select="10"]')).to_have_count(0)
        expect(dialog.locator('[data-inventory-select="11"]')).to_have_count(0)
        dialog.get_by_label('物品容器').select_option('consumables')
        dialog.get_by_role('button',name='编辑槽位 65',exact=True).click()
        editor.get_by_label('物品数量',exact=True).fill('1')
        editor.get_by_label('物品操作原因',exact=True).fill('调整补给数量')
        editor.get_by_role('button',name='预览物品变更',exact=True).click()
        expect(confirm).to_contain_text('12 → 1')
        editor.get_by_role('button',name='确认物品提交',exact=True).click()
        expect(editor.get_by_role('status')).to_contain_text('配置已完成，共 1 格')
        dialog.get_by_role('tab',name='物品操作记录',exact=True).click()
        expect(dialog.locator('tbody tr')).to_have_count(3)
        dialog.get_by_role('button',name='查看物品回执',exact=True).first.click()
        page.screenshot(path=str(OUT/'inventory-ledger.png'),full_page=True)
        dialog.get_by_role('tab',name='任务操作记录',exact=True).click()
        expect(dialog).to_contain_text('暂无记录')
        dialog.get_by_role('tab',name='背包与仓库',exact=True).click()
        dialog.get_by_label('物品容器').select_option('equipment')
        editor.get_by_role('button',name='选择本页物品',exact=True).click()
        expect(editor).to_contain_text('已选 20 格')
        dialog.get_by_role('button',name='下一页',exact=True).click()
        expect(editor).to_contain_text('已选 0 格')
        page.wait_for_timeout(300);baseline=len(requests);page.wait_for_timeout(1400)
        assert len(requests)==baseline,'idle polling'
        page.keyboard.press('Escape');open_character('晨雾·逐光')
        dialog.get_by_role('tab',name='物品操作记录',exact=True).click()
        expect(dialog).to_contain_text('暂无记录')
        page.keyboard.press('Escape')
        page.locator('#theme-open').click()
        page.locator('[data-action=preset][data-value=ember-dusk]').click()
        page.locator('[data-action=mode][data-value=dark]').click()
        page.keyboard.press('Escape');open_character()
        dialog.get_by_role('button',name='编辑槽位 9',exact=True).click()
        editor.get_by_label('品级',exact=True).select_option('1')
        editor.get_by_label('物品操作原因',exact=True).fill('外观检查')
        editor.get_by_role('button',name='预览物品变更',exact=True).click()
        expect(confirm).to_be_visible();confirm.scroll_into_view_if_needed()
        page.screenshot(path=str(OUT/'inventory-dark.png'),full_page=True)
        page.set_viewport_size({'width':390,'height':844})
        confirm.scroll_into_view_if_needed()
        page.screenshot(path=str(OUT/'inventory-mobile.png'),full_page=True)
        assert page.evaluate('document.documentElement.scrollWidth<=innerWidth+1')
        assert dialog.evaluate('(d)=>d.scrollWidth<=d.clientWidth+1')
        assert not errors,errors
        browser.close()
    (OUT/'browser-result.json').write_text(json.dumps({'checks':['configure','random-preview-fixed','lost-response-reload','request-id-recovery','quantity','batch-delete','ledger-isolation','character-isolation','paging-selection','input-invalidates-preview','no-idle-poll','dark-theme','mobile'],'errors':errors},ensure_ascii=False,indent=2),encoding='utf-8')
    print('PASS gateway inventory browser checks')


if __name__=='__main__':main()
