"""Verify creature details and linked removal using the disposable gateway preview."""
import json
from pathlib import Path
from playwright.sync_api import sync_playwright, expect

ROOT = Path(__file__).resolve().parents[1]
STATE = ROOT / 'runtime/preview/gateway-live'
OUT = ROOT / 'runtime/verification/gateway-pets'


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

        def open_character():
            page.get_by_role('button', name='打开远山', exact=True).click()
            page.get_by_role('button', name='查看远山·剑影', exact=True).click()
            dialog.get_by_role('tab', name='背包与仓库', exact=True).click()
            dialog.get_by_label('物品容器').select_option('pets')

        def get(path):
            r = context.request.get(origin + '/api/v1/' + path)
            assert r.status == 200, r.text()
            return r.json()['data']

        def remove(slot, pet_name, equipped=False):
            dialog.get_by_label('选择槽位 ' + str(slot), exact=True).check()
            expect(editor.get_by_role('button', name='配置所选', exact=True)).to_be_disabled()
            expect(dialog.get_by_role('button', name='编辑槽位 ' + str(slot), exact=True)).to_have_count(0)
            editor.get_by_role('button', name='删除所选', exact=True).click()
            editor.get_by_label('物品操作原因', exact=True).fill('清理宠物 ' + pet_name)
            editor.get_by_role('button', name='预览物品变更', exact=True).click()
            expect(confirm).to_contain_text('同时删除宠物成长记录：' + pet_name)
            expect(confirm).to_contain_text('经验 1200')
            if equipped:
                expect(confirm).to_contain_text('清除当前出战关联')
            confirm.scroll_into_view_if_needed()
            page.screenshot(path=str(OUT / ('pet-equipped-mobile.png' if equipped else 'pet-delete-confirm.png')), full_page=True)
            assert page.evaluate('document.documentElement.scrollWidth <= innerWidth + 1')
            assert dialog.evaluate('(d) => d.scrollWidth <= d.clientWidth + 1')
            with page.expect_response(lambda r: r.url.endswith('/inventory-operations') and r.request.method == 'POST') as captured:
                editor.get_by_role('button', name='确认物品提交', exact=True).click()
            response = captured.value
            assert response.status == 200, response.text()
            receipt = response.json()['data']
            receipts.append(receipt)
            assert receipt['changes'][0]['before']['pet']['name'] == pet_name
            original = response.request
            replay = context.request.post(original.url, data=original.post_data,
                                          headers={k: v for k, v in original.all_headers().items()
                                                   if k in ('content-type', 'x-csrf-token', 'idempotency-key')})
            assert replay.status == 200 and replay.json()['data'] == receipt
            expect(editor.get_by_role('status')).to_contain_text('删除已完成，共 1 格')

        open_character()
        expect(dialog.locator('tbody tr')).to_have_count(2)
        other_before = get('characters/20/inventory/pets/1')
        worn_before = get('characters/20/inventory/equipped/26')
        dialog.locator('[data-resource-detail="0"]').click()
        details = dialog.get_by_label('所选记录详情')
        expect(details).to_contain_text('宠物名称')
        expect(details).to_contain_text('云朵')
        expect(details).to_contain_text('宠物等级')
        expect(details).to_contain_text('饱食度')
        expect(details).not_to_contain_text('品级种子')
        expect(details).not_to_contain_text('徽章 1')
        details.scroll_into_view_if_needed()
        expect(details.get_by_text('云朵', exact=True)).to_be_in_viewport()
        page.screenshot(path=str(OUT / 'pet-details.png'), full_page=True)
        remove(0, '云朵')
        expect(dialog.locator('tbody tr')).to_have_count(1)
        assert get('characters/20/inventory/pets/1')['pet']['name'] == other_before['pet']['name']
        assert get('characters/20/inventory/equipped/26')['pet']['equippedKey'] == worn_before['pet']['equippedKey']
        page.reload()
        open_character()
        expect(dialog.locator('tbody tr')).to_have_count(1)
        dialog.get_by_role('tab', name='物品操作记录', exact=True).click()
        expect(dialog.locator('tbody tr')).to_have_count(1)
        dialog.get_by_role('button', name='查看物品回执', exact=True).first.click()
        expect(details).to_contain_text('同时删除宠物成长记录：云朵')
        page.keyboard.press('Escape')
        page.locator('#theme-open').click()
        page.locator('[data-action=preset][data-value=ember-dusk]').click()
        page.locator('[data-action=mode][data-value=dark]').click()
        page.keyboard.press('Escape')
        open_character()
        dialog.get_by_label('物品容器').select_option('equipped')
        expect(dialog.locator('tbody tr')).to_have_count(1)
        page.set_viewport_size({'width': 390, 'height': 844})
        remove(26, '小羽', True)
        expect(dialog).to_contain_text('暂无记录')
        remaining = get('characters/20/inventory/pets/1')['pet']
        assert remaining['equippedKey'] == '' and remaining['townDisplay'] is True
        assert remaining['name'] == '流星' and remaining['level'] == '6'
        dialog.get_by_role('tab', name='物品操作记录', exact=True).click()
        expect(dialog.locator('tbody tr')).to_have_count(2)
        page.wait_for_timeout(300)
        baseline = len(requests)
        page.wait_for_timeout(1200)
        assert len(requests) == baseline, 'idle polling'
        assert not errors, errors
        browser.close()
    (OUT / 'browser-result.json').write_text(json.dumps({
        'checks': ['growth-detail', 'dedicated-delete-only', 'linked-bag-removal', 'linked-worn-removal',
                   'remaining-pet-preserved', 'equipped-pointer-cleared', 'receipt-replay', 'reload-readback',
                   'character-ledger', 'dark-mobile', 'no-idle-poll'],
        'receipts': receipts, 'errors': errors}, ensure_ascii=False, indent=2), encoding='utf-8')
    print('PASS gateway pets browser checks')


if __name__ == '__main__':
    main()
