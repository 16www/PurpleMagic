"""Exercise pet equipment configuration and linked removal in the isolated preview."""
import json
from pathlib import Path
from playwright.sync_api import sync_playwright, expect

ROOT = Path(__file__).resolve().parents[1]
STATE = ROOT / 'runtime/preview/gateway-live'
OUT = ROOT / 'runtime/verification/gateway-artifacts'


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
            page.get_by_role('button', name='打开星河', exact=True).click()
            page.get_by_role('button', name='查看星河·剑影', exact=True).click()
            dialog.get_by_role('tab', name='背包与仓库', exact=True).click()
            dialog.get_by_label('物品容器').select_option('equipped')

        def get(path):
            response = context.request.get(origin + '/api/v1/' + path)
            assert response.status == 200, response.text()
            return response.json()['data']

        def preview(reason):
            editor.get_by_label('物品操作原因', exact=True).fill(reason)
            editor.get_by_role('button', name='预览物品变更', exact=True).click()
            expect(confirm).to_contain_text(reason)

        def submit():
            with page.expect_response(lambda r: r.url.endswith('/inventory-operations') and r.request.method == 'POST') as captured:
                editor.get_by_role('button', name='确认物品提交', exact=True).click()
            response = captured.value
            assert response.status == 200, response.text()
            receipt = response.json()['data']
            receipts.append(receipt)
            original = response.request
            replay = context.request.post(original.url, data=original.post_data,
                                          headers={k: v for k, v in original.all_headers().items()
                                                   if k in ('content-type', 'x-csrf-token', 'idempotency-key')})
            assert replay.status == 200 and replay.json()['data'] == receipt
            expect(editor.get_by_role('status')).to_contain_text('已完成，共 ' + str(len(receipt['changes'])) + ' 格')

        open_character()
        expect(dialog.locator('tbody tr')).to_have_count(4)
        original_pet = get('characters/30/inventory/equipped/26')['pet']
        original_red = get('characters/30/inventory/equipped/27')
        dialog.locator('[data-resource-detail="27"]').click()
        details = dialog.get_by_label('所选记录详情')
        expect(details).to_contain_text('红色')
        expect(details).to_contain_text('已匹配')
        dialog.get_by_role('button', name='编辑槽位 27', exact=True).click()
        expect(editor.get_by_label('强化等级', exact=True)).to_have_count(0)
        expect(editor.get_by_label('物品数量', exact=True)).to_have_count(0)
        editor.get_by_label('有效天数', exact=True).fill('30')
        preview('调整红色宠物装备期限')
        expect(confirm).to_contain_text('同步更新红色宠物装备记录')
        confirm.scroll_into_view_if_needed()
        page.screenshot(path=str(OUT / 'artifact-configure.png'), full_page=True)
        submit()
        red = get('characters/30/inventory/equipped/27')
        assert red['petArtifact']['color'] == 'red'
        assert red['expiresAt'] > original_red['expiresAt']
        assert red['relatedRevision'] != original_red['relatedRevision']
        assert get('characters/30/inventory/equipped/27/options')['canDelete'] is True
        page.reload()
        open_character()
        assert get('characters/30/inventory/equipped/27')['expiresAt'] == red['expiresAt']
        dialog.get_by_label('物品容器').select_option('pet-equipment')
        expect(dialog.locator('tbody tr')).to_have_count(3)
        dialog.get_by_role('button', name='编辑槽位 140', exact=True).click()
        editor.get_by_label('有效天数', exact=True).fill('14')
        preview('调整背包宠物装备期限')
        expect(confirm).not_to_contain_text('同步更新红色宠物装备记录')
        submit()
        dialog.get_by_label('选择槽位 140', exact=True).check()
        editor.get_by_role('button', name='删除所选', exact=True).click()
        preview('清理背包宠物装备')
        submit()
        expect(dialog.locator('tbody tr')).to_have_count(2)
        assert get('characters/30/inventory/equipped/27')['relatedRevision'] == red['relatedRevision']
        page.keyboard.press('Escape')
        page.locator('#theme-open').click()
        page.locator('[data-action=preset][data-value=ember-dusk]').click()
        page.locator('[data-action=mode][data-value=dark]').click()
        page.keyboard.press('Escape')
        open_character()
        for slot in (28, 29):
            dialog.get_by_label('选择槽位 ' + str(slot), exact=True).check()
        editor.get_by_role('button', name='删除所选', exact=True).click()
        preview('清理蓝色与绿色宠物装备')
        expect(confirm).to_contain_text('同时移除蓝色宠物装备记录')
        expect(confirm).to_contain_text('同时移除绿色宠物装备记录')
        page.set_viewport_size({'width': 390, 'height': 844})
        confirm.scroll_into_view_if_needed()
        page.screenshot(path=str(OUT / 'artifact-delete-mobile.png'), full_page=True)
        assert page.evaluate('document.documentElement.scrollWidth <= innerWidth + 1')
        assert dialog.evaluate('(d) => d.scrollWidth <= d.clientWidth + 1')
        submit()
        expect(dialog.locator('tbody tr')).to_have_count(2)
        assert get('characters/30/inventory/equipped/27/options')['canDelete'] is True
        pet = get('characters/30/inventory/equipped/26')['pet']
        for key in ('name', 'level', 'experience', 'equippedKey', 'townDisplay'):
            assert pet[key] == original_pet[key]
        dialog.get_by_role('tab', name='物品操作记录', exact=True).click()
        expect(dialog.locator('tbody tr')).to_have_count(4)
        dialog.get_by_role('button', name='查看物品回执', exact=True).first.click()
        expect(details).to_contain_text('同时移除蓝色宠物装备记录')
        page.wait_for_timeout(300)
        baseline = len(requests)
        page.wait_for_timeout(1200)
        assert len(requests) == baseline, 'idle polling'
        assert not errors, errors
        browser.close()
    (OUT / 'browser-result.json').write_text(json.dumps({
        'checks': ['linked-detail', 'pvf-options', 'worn-expiration', 'bag-expiration', 'bag-delete',
                   'bag-change-preserves-projection', 'two-color-delete', 'creature-preserved',
                   'receipt-replay', 'reload-readback', 'ledger', 'dark-mobile', 'no-idle-poll'],
        'receipts': receipts, 'errors': errors}, ensure_ascii=False, indent=2), encoding='utf-8')
    print('PASS gateway artifact browser checks')


if __name__ == '__main__':
    main()
