"""Exercise worn equipment edits and deletion in the disposable gateway preview."""
import json
from pathlib import Path
from playwright.sync_api import sync_playwright, expect

ROOT = Path(__file__).resolve().parents[1]
STATE = ROOT / 'runtime/preview/gateway-live'
OUT = ROOT / 'runtime/verification/gateway-equipped'


def main():
    OUT.mkdir(parents=True, exist_ok=True)
    ready = json.loads((STATE / 'ready.json').read_text(encoding='utf-8'))
    origin = ready['url'].split('/console/')[0]
    errors, requests, receipts = [], [], []
    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        context = browser.new_context(ignore_https_errors=True, viewport={'width': 1440, 'height': 1000})
        page = context.new_page()
        page.on('pageerror', lambda e: errors.append(str(e)))
        page.on('request', lambda r: requests.append(r.url) if '/api/' in r.url else None)
        page.goto(ready['url'])
        page.get_by_label('导入登录 Key').set_input_files(str(STATE / 'login-key.json'))
        expect(page.get_by_label('管理密码')).not_to_have_value('')
        page.get_by_role('button', name='登录', exact=True).click()
        dialog = page.locator('#dialog')
        editor = dialog.get_by_role('region', name='物品操作', exact=True)
        confirm = editor.get_by_label('物品变更确认', exact=True)

        def open_character(name='晨雾·剑影'):
            page.get_by_role('button', name='打开晨雾', exact=True).click()
            page.get_by_role('button', name='查看' + name, exact=True).click()
            dialog.get_by_role('tab', name='背包与仓库', exact=True).click()
            dialog.get_by_label('物品容器').select_option('equipped')

        def get(path):
            response = context.request.get(origin + '/api/v1/' + path)
            assert response.status == 200, response.text()
            return response.json()['data']

        def submit():
            with page.expect_response(lambda r: r.url.endswith('/inventory-operations') and r.request.method == 'POST') as captured:
                editor.get_by_role('button', name='确认物品提交', exact=True).click()
            response = captured.value
            assert response.status == 200, response.text()
            receipts.append(response.json()['data'])
            return response.request

        open_character()
        expect(dialog.locator('tbody tr')).to_have_count(1)
        bag_before = get('characters/10/inventory?container=equipment&limit=100')
        ledger_count = len(get('characters/10/inventory-operations?limit=20'))
        dialog.get_by_role('button', name='编辑槽位 10', exact=True).click()
        editor.get_by_label('品级', exact=True).select_option('1')
        editor.get_by_label('强化等级', exact=True).fill('11')
        expect(editor.get_by_label('物品数量', exact=True)).to_have_count(0)
        editor.get_by_label('物品操作原因', exact=True).fill('穿戴装备属性修复')
        editor.get_by_role('button', name='预览物品变更', exact=True).click()
        expect(confirm).to_contain_text('12 → 11')
        confirm.scroll_into_view_if_needed()
        page.screenshot(path=str(OUT / 'equipped-confirm.png'), full_page=True)
        original = submit()
        expect(editor.get_by_role('status')).to_contain_text('配置已完成，共 1 格')
        assert receipts[0]['ownerScope'] == 'character'
        assert receipts[0]['changes'][0]['after']['attributes']['reinforce'] == 11
        replay = context.request.post(original.url, data=original.post_data,
                                      headers={k: v for k, v in original.all_headers().items()
                                               if k in ('content-type', 'x-csrf-token', 'idempotency-key')})
        assert replay.status == 200 and replay.json()['data'] == receipts[0]
        page.reload()
        open_character()
        assert get('characters/10/inventory/equipped/10')['attributes']['reinforce'] == 11
        assert get('characters/10/inventory?container=equipment&limit=100') == bag_before
        dialog.get_by_role('tab', name='物品操作记录', exact=True).click()
        expect(dialog.locator('tbody tr')).to_have_count(ledger_count + 1)
        dialog.get_by_role('button', name='查看物品回执', exact=True).first.click()
        expect(dialog.get_by_label('所选记录详情')).to_contain_text('穿戴装备属性修复')
        page.keyboard.press('Escape')
        page.locator('#theme-open').click()
        page.locator('[data-action=preset][data-value=ember-dusk]').click()
        page.locator('[data-action=mode][data-value=dark]').click()
        page.keyboard.press('Escape')
        open_character()
        editor.get_by_role('button', name='选择本页物品', exact=True).click()
        editor.get_by_role('button', name='删除所选', exact=True).click()
        editor.get_by_label('物品操作原因', exact=True).fill('清理穿戴装备')
        editor.get_by_role('button', name='预览物品变更', exact=True).click()
        expect(confirm).to_contain_text('删除整格')
        page.set_viewport_size({'width': 390, 'height': 844})
        confirm.scroll_into_view_if_needed()
        page.screenshot(path=str(OUT / 'equipped-delete-mobile.png'), full_page=True)
        assert page.evaluate('document.documentElement.scrollWidth <= innerWidth + 1')
        assert dialog.evaluate('(d) => d.scrollWidth <= d.clientWidth + 1')
        submit()
        expect(editor.get_by_role('status')).to_contain_text('删除已完成，共 1 格')
        expect(dialog).to_contain_text('暂无记录')
        assert get('characters/10/inventory?container=equipment&limit=100') == bag_before
        dialog.get_by_role('tab', name='物品操作记录', exact=True).click()
        expect(dialog.locator('tbody tr')).to_have_count(ledger_count + 2)
        page.keyboard.press('Escape')
        open_character('晨雾·逐光')
        expect(dialog).to_contain_text('暂无记录')
        dialog.get_by_role('tab', name='物品操作记录', exact=True).click()
        expect(dialog).to_contain_text('暂无记录')
        page.wait_for_timeout(300)
        baseline = len(requests)
        page.wait_for_timeout(1200)
        assert len(requests) == baseline, 'idle polling'
        assert not errors, errors
        browser.close()
    (OUT / 'browser-result.json').write_text(json.dumps({
        'checks': ['equipped-configure', 'fixed-one-item', 'receipt-replay', 'reload-readback', 'bag-preserved',
                   'character-ledger', 'equipped-delete', 'other-role-isolation', 'dark-mobile', 'no-idle-poll'],
        'receipts': receipts, 'errors': errors}, ensure_ascii=False, indent=2), encoding='utf-8')
    print('PASS gateway equipped browser checks')


if __name__ == '__main__':
    main()
