"""Exercise zero-balance shared resources through the isolated gateway preview."""
import json
from pathlib import Path
from urllib.parse import quote
from playwright.sync_api import sync_playwright, expect

ROOT = Path(__file__).resolve().parents[1]
STATE = ROOT / 'runtime/preview/gateway-live'
OUT = ROOT / 'runtime/verification/gateway-shared-resources'


def main():
    OUT.mkdir(parents=True, exist_ok=True)
    ready = json.loads((STATE / 'ready.json').read_text(encoding='utf-8'))
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
            dialog.get_by_label('物品容器').select_option('shared')
            expect(dialog.locator('tbody tr')).to_have_count(12)

        def set_count(slot, before, after, reason):
            dialog.get_by_role('button', name=f'编辑槽位 {slot}', exact=True).click()
            quantity = editor.get_by_label('物品数量', exact=True)
            expect(quantity).to_have_attribute('min', '0')
            quantity.fill(str(after))
            editor.get_by_label('物品操作原因', exact=True).fill(reason)
            editor.get_by_role('button', name='预览物品变更', exact=True).click()
            expect(confirm).to_contain_text(f'{before} → {after}')
            expect(confirm).not_to_contain_text('耐久')
            expect(confirm).not_to_contain_text('封装')
            confirm.scroll_into_view_if_needed()
            if not receipts:
                page.screenshot(path=str(OUT / 'shared-create-confirm.png'), full_page=True)
            with page.expect_response(lambda r: r.url.endswith('/inventory-operations') and r.request.method == 'POST') as captured:
                editor.get_by_role('button', name='确认物品提交', exact=True).click()
            response = captured.value
            assert response.status == 200, response.text()
            receipt = response.json()['data']
            receipts.append(receipt)
            change = receipt['changes'][0]
            assert change['before']['count'] == str(before), change
            assert change['after']['count'] == str(after), change
            assert change['after']['present'] == (after > 0), change
            expect(editor.get_by_role('status')).to_contain_text('配置已完成，共 1 格')
            expect(dialog.locator('tbody tr')).to_have_count(12)
            return response.request

        open_character()
        # Read the actual owner and ledger so this check also works on a fresh preview.
        info = context.request.get(ready['url'].split('/console/')[0] + '/api/v1/characters/10/inventory?container=shared')
        assert info.status == 200, info.text()
        owner = quote(info.json()['data']['accountId'], safe='')
        ledger_url = ready['url'].split('/console/')[0] + f'/api/v1/accounts/{owner}/inventory-operations?limit=20'
        ledger = context.request.get(ledger_url)
        assert ledger.status == 200, ledger.text()
        initial_records = len(ledger.json()['data'])
        original = set_count(355, 0, 125, '补入白色晶块')
        assert receipts[-1]['changes'][0]['before']['present'] is False
        set_count(355, 125, 0, '清空白色晶块')
        # Replaying the original successful request after clearing must not grant again.
        replay = context.request.post(original.url, data=original.post_data,
                                      headers={k: v for k, v in original.all_headers().items()
                                               if k in ('content-type', 'x-csrf-token', 'idempotency-key')})
        assert replay.status == 200, replay.text()
        assert replay.json()['data'] == receipts[0]
        set_count(355, 0, 42, '再次补入白色晶块')
        set_count(360, 0, 8, '补入灵魂')
        page.keyboard.press('Escape')
        open_character('晨雾·逐光')
        for slot, count in ((355, '42'), (360, '8')):
            response = context.request.get(ready['url'].split('/console/')[0] + f'/api/v1/characters/11/inventory/shared/{slot}')
            assert response.status == 200, response.text()
            item = response.json()['data']
            assert item['count'] == count and item['present'] is True, item
        dialog.get_by_role('tab', name='账号物品记录', exact=True).click()
        expect(dialog.locator('tbody tr')).to_have_count(initial_records + 4)
        page.keyboard.press('Escape')
        page.locator('#theme-open').click()
        page.locator('[data-action=preset][data-value=ember-dusk]').click()
        page.locator('[data-action=mode][data-value=dark]').click()
        page.keyboard.press('Escape')
        open_character()
        dialog.get_by_role('button', name='编辑槽位 361', exact=True).click()
        editor.get_by_label('物品数量', exact=True).fill('25')
        editor.get_by_label('物品操作原因', exact=True).fill('共享资源预览')
        editor.get_by_role('button', name='预览物品变更', exact=True).click()
        expect(confirm).to_contain_text('0 → 25')
        page.set_viewport_size({'width': 390, 'height': 844})
        confirm.scroll_into_view_if_needed()
        page.screenshot(path=str(OUT / 'shared-mobile-dark.png'), full_page=True)
        assert page.evaluate('document.documentElement.scrollWidth <= innerWidth + 1')
        assert dialog.evaluate('(d) => d.scrollWidth <= d.clientWidth + 1')
        page.wait_for_timeout(300)
        baseline = len(requests)
        page.wait_for_timeout(1200)
        assert len(requests) == baseline, 'idle polling'
        assert not errors, errors
        browser.close()
    result = {'checks': ['twelve-slots', 'empty-cube-create', 'zero-clear-keeps-slot', 'replay-no-regrant',
                         'refill', 'empty-soul-create', 'same-account-role-visibility', 'account-ledger',
                         'zero-minimum', 'dark-mobile', 'no-idle-poll'],
              'receipts': receipts, 'errors': errors}
    (OUT / 'browser-result.json').write_text(json.dumps(result, ensure_ascii=False, indent=2), encoding='utf-8')
    print('PASS gateway shared resources browser checks')


if __name__ == '__main__':
    main()
