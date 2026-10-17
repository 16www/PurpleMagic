"""Check task operations through the production gateway and disposable preview."""
import json
from pathlib import Path
from playwright.sync_api import sync_playwright, expect

ROOT = Path(__file__).resolve().parents[1]
STATE = ROOT/'runtime/preview/gateway-live'
OUT = ROOT/'runtime/verification/gateway-quests'


def main():
    OUT.mkdir(parents=True, exist_ok=True)
    ready = json.loads((STATE/'ready.json').read_text(encoding='utf-8'))
    errors, requests = [], []
    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        context = browser.new_context(ignore_https_errors=True, viewport={'width':1440, 'height':1000})
        page = context.new_page()
        page.on('pageerror', lambda e: errors.append(str(e)))
        page.on('request', lambda r: requests.append(r.url) if '/api/' in r.url else None)
        page.goto(ready['url'])
        page.get_by_label('导入登录 Key').set_input_files(str(STATE/'login-key.json'))
        expect(page.get_by_label('管理密码')).not_to_have_value('')
        page.get_by_role('button', name='登录', exact=True).click()

        def open_character(name='晨雾·剑影', account='晨雾'):
            page.get_by_role('button', name='打开'+account, exact=True).click()
            page.get_by_role('button', name='查看'+name, exact=True).click()
            page.get_by_role('tab', name='游戏任务', exact=True).click()

        open_character()
        dialog = page.locator('#dialog')
        operations = dialog.get_by_role('region', name='任务操作', exact=True)
        expect(dialog.locator('[data-quest-select]')).to_have_count(5)
        quest_id = dialog.locator('[data-quest-select]').first.get_attribute('data-quest-select')

        def preview(action, reason):
            dialog.locator('[data-quest-select="'+quest_id+'"]').check()
            operations.get_by_label('任务操作类型', exact=True).select_option(action)
            operations.get_by_label('任务操作原因', exact=True).fill(reason)
            operations.get_by_role('button', name='预览任务变更', exact=True).click()
            expect(operations.get_by_label('任务变更确认', exact=True)).to_be_visible()

        preview('ready', '修复任务目标进度')
        operations.get_by_role('button', name='确认任务提交', exact=True).click()
        expect(operations.get_by_role('status')).to_contain_text('标记可交已完成')
        preview('complete', '补齐任务归档')
        expect(operations.get_by_label('任务变更确认')).to_contain_text('已完成')
        operations.get_by_label('任务变更确认').scroll_into_view_if_needed()
        page.screenshot(path=str(OUT/'quests-confirm.png'), full_page=True)
        lost = []

        def lose_response(route):
            if route.request.method != 'POST' or lost:
                route.continue_()
                return
            response = route.fetch()
            assert response.status == 200, response.text()
            lost.append(True)
            route.abort('connectionreset')

        page.route('**/quest-operations', lose_response)
        operations.get_by_role('button', name='确认任务提交', exact=True).click()
        expect(operations.get_by_role('button', name='重试并取得任务回执', exact=True)).to_be_enabled()
        assert lost
        page.reload()
        open_character()
        operations.get_by_role('button', name='重试并取得任务回执', exact=True).click()
        expect(operations.get_by_role('status')).to_contain_text('标记完成（不发奖励）已完成')
        page.unroute('**/quest-operations', lose_response)
        dialog.get_by_role('tab', name='任务操作记录', exact=True).click()
        expect(dialog.locator('tbody tr')).to_have_count(2)
        dialog.get_by_role('button', name='查看回执', exact=True).first.click()
        expect(dialog.get_by_label('所选记录详情')).to_contain_text('补齐任务归档')
        page.screenshot(path=str(OUT/'quests-ledger.png'), full_page=True)
        dialog.get_by_role('tab', name='游戏任务', exact=True).click()
        dialog.get_by_label('游戏任务状态').select_option('completed')
        expect(dialog.locator('[data-quest-select="'+quest_id+'"]')).to_be_visible()
        preview('reset', '恢复任务以重新推进')
        operations.get_by_role('button', name='确认任务提交', exact=True).click()
        expect(operations.get_by_role('status')).to_contain_text('恢复未完成 / 重置链路已完成')
        dialog.get_by_role('tab', name='任务库', exact=True).click()
        expect(dialog.locator('[data-quest-select]')).to_have_count(20)
        operations.get_by_role('button', name='选择本页', exact=True).click()
        expect(operations).to_contain_text('已选 20 项')
        dialog.get_by_role('button', name='下一页', exact=True).click()
        expect(operations).to_contain_text('已选 0 项')
        dialog.get_by_role('tab', name='任务操作记录', exact=True).click()
        expect(dialog.locator('tbody tr')).to_have_count(3)
        page.wait_for_timeout(300)
        baseline = len(requests)
        page.wait_for_timeout(1400)
        assert len(requests) == baseline, 'idle polling'
        page.keyboard.press('Escape')
        open_character('晨雾·逐光')
        dialog.get_by_role('tab', name='任务操作记录', exact=True).click()
        expect(dialog).to_contain_text('暂无记录')
        page.keyboard.press('Escape')
        page.locator('#theme-open').click()
        page.locator('[data-action=preset][data-value=ember-dusk]').click()
        page.locator('[data-action=mode][data-value=dark]').click()
        page.keyboard.press('Escape')
        open_character()
        dialog.get_by_role('tab', name='任务库', exact=True).click()
        expect(dialog.locator('[data-quest-select]')).to_have_count(20)
        dialog.locator('[data-quest-select]').first.check()
        operations.get_by_label('任务操作原因').fill('界面检查')
        operations.get_by_role('button', name='预览任务变更', exact=True).click()
        expect(operations.get_by_label('任务变更确认')).to_be_visible()
        page.screenshot(path=str(OUT/'quests-dark.png'), full_page=True)
        page.set_viewport_size({'width':390, 'height':844})
        operations.scroll_into_view_if_needed()
        page.screenshot(path=str(OUT/'quests-mobile.png'), full_page=True)
        assert page.evaluate('document.documentElement.scrollWidth <= innerWidth+1')
        assert dialog.evaluate('(d)=>d.scrollWidth<=d.clientWidth+1'), 'dialog overflows'
        assert not errors, errors
        browser.close()
    (OUT/'browser-result.json').write_text(json.dumps({'checks':['ready','complete-no-rewards','reset','preview','persistent-request-recovery','ledger','character-isolation','selection-page-reset','no-idle-poll','dark-theme','mobile'], 'errors':errors, 'runtime':'authenticated offline fixture for six disposable characters'}, ensure_ascii=False, indent=2), encoding='utf-8')
    print('PASS gateway quest browser checks')


if __name__ == '__main__':
    main()
