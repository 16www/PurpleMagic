"""Exercise character gold on the actual gateway and disposable preview database."""
import json
from pathlib import Path
from playwright.sync_api import sync_playwright, expect

ROOT = Path(__file__).resolve().parents[1]
STATE = ROOT / 'runtime/preview/gateway-live'
OUT = ROOT / 'runtime/verification/gateway-gold'


def main():
    OUT.mkdir(parents=True, exist_ok=True)
    ready = json.loads((STATE / 'ready.json').read_text(encoding='utf-8'))
    errors, requests = [], []
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
        wallet = dialog.get_by_role('region', name='角色金币')

        def open_character(account='晨雾', name='晨雾·剑影'):
            page.get_by_role('button', name='打开'+account, exact=True).click()
            page.get_by_role('button', name='查看'+name, exact=True).click()
            dialog.get_by_role('tab', name='金币与流水', exact=True).click()

        def preview(action, amount, reason):
            wallet.get_by_label('金币操作', exact=True).select_option(action)
            wallet.get_by_label('金币数量', exact=True).fill(amount)
            wallet.get_by_label('金币操作原因', exact=True).fill(reason)
            wallet.get_by_role('button', name='预览金币变更', exact=True).click()
            expect(wallet.get_by_label('金币变更确认')).to_contain_text(reason)

        def submit(balance):
            wallet.get_by_role('button', name='确认金币提交', exact=True).click()
            expect(wallet.get_by_role('status')).to_contain_text('金币 '+balance)
            expect(wallet.locator('.gold-balance')).to_have_text(balance)

        open_character()
        expect(wallet.locator('.gold-balance')).to_have_text('0')
        preview('add', '100000', '活动金币补偿')
        expect(wallet.get_by_label('金币变更确认')).to_contain_text('100000')
        page.screenshot(path=str(OUT / 'gold-confirm.png'), full_page=True)
        lost = []

        def lose_response(route):
            if route.request.method != 'POST' or lost:
                route.continue_()
                return
            response = route.fetch()
            assert response.status == 200
            lost.append(True)
            route.abort('connectionreset')

        page.route('**/gold-operations', lose_response)
        wallet.get_by_role('button', name='确认金币提交', exact=True).click()
        expect(wallet.get_by_role('button', name='重试金币并取得回执', exact=True)).to_be_enabled()
        assert lost
        page.reload()
        open_character()
        expect(wallet.locator('.gold-balance')).to_have_text('100000')
        wallet.get_by_role('button', name='重试金币并取得回执', exact=True).click()
        expect(wallet.get_by_role('status')).to_contain_text('金币 100000')
        expect(wallet.locator('tbody tr')).to_have_count(1)
        page.unroute('**/gold-operations', lose_response)
        preview('subtract', '50', '异常收益扣回')
        submit('99950')
        preview('set', '2147483647', '金币上限验证')
        submit('2147483647')
        wallet.get_by_label('金币操作', exact=True).select_option('add')
        wallet.get_by_label('金币数量', exact=True).fill('1')
        wallet.get_by_label('金币操作原因', exact=True).fill('越界检查')
        wallet.get_by_role('button', name='预览金币变更', exact=True).click()
        expect(wallet.get_by_role('alert')).to_contain_text('超出允许范围')
        preview('set', '0', '余额清零')
        submit('0')
        wallet.get_by_label('金币操作', exact=True).select_option('subtract')
        wallet.get_by_label('金币数量', exact=True).fill('1')
        wallet.get_by_label('金币操作原因', exact=True).fill('余额不足检查')
        wallet.get_by_role('button', name='预览金币变更', exact=True).click()
        expect(wallet.get_by_role('alert')).to_contain_text('超出允许范围')
        # A second administrator changes the balance between preview and commit.
        preview('add', '100', '过时预览')
        base = ready['url'].split('/console/')[0] + '/api/v1/'
        cap = context.request.get(base+'capabilities').json()['data']
        current = context.request.get(base+'characters/10/gold').json()['data']
        body = {'accountId': current['accountId'], 'action': 'add', 'amount': '5', 'revision': current['revision'], 'reason': '并行管理员'}
        headers = {'X-CSRF-Token': cap['csrfToken']}
        plan = context.request.post(base+'characters/10/gold-operations/preview', data=body, headers=headers)
        assert plan.status == 200, plan.text()
        body['planToken'] = plan.json()['data']['planToken']
        headers['Idempotency-Key'] = 'browser-gold-conflict-001'
        response = context.request.post(base+'characters/10/gold-operations', data=body, headers=headers)
        assert response.status == 200, response.text()
        wallet.get_by_role('button', name='确认金币提交', exact=True).click()
        expect(wallet.get_by_role('alert')).to_contain_text('已变更')
        expect(wallet.get_by_role('button', name='重试金币并取得回执', exact=True)).to_have_count(0)
        wallet.get_by_role('button', name='刷新金币与流水', exact=True).click()
        expect(wallet.locator('.gold-balance')).to_have_text('5')
        expect(wallet.locator('tbody tr')).to_have_count(5)
        # Character scopes and account scopes keep separate wallets and ledgers.
        dialog.get_by_role('button', name='关闭', exact=True).click()
        open_character('晨雾', '晨雾·逐光')
        expect(wallet.locator('.gold-balance')).to_have_text('0')
        expect(wallet.locator('tbody')).to_contain_text('暂无 GM')
        dialog.get_by_role('button', name='关闭', exact=True).click()
        open_character('远山', '远山·剑影')
        expect(wallet.locator('.gold-balance')).to_have_text('0')
        expect(wallet.locator('tbody')).to_contain_text('暂无 GM')
        dialog.get_by_role('button', name='关闭', exact=True).click()
        open_character()
        expect(wallet.locator('tbody tr')).to_have_count(5)
        page.wait_for_timeout(300)
        baseline = len(requests)
        page.wait_for_timeout(1600)
        assert len(requests) == baseline, 'idle requests'
        page.screenshot(path=str(OUT / 'gold-desktop.png'), full_page=True)
        dialog.get_by_role('button', name='关闭', exact=True).click()
        page.locator('#theme-open').click()
        page.locator('[data-action=preset][data-value=ember-dusk]').click()
        page.locator('[data-action=mode][data-value=dark]').click()
        page.keyboard.press('Escape')
        open_character()
        expect(wallet.locator('.gold-balance')).to_have_text('5')
        page.screenshot(path=str(OUT / 'gold-dark.png'), full_page=True)
        page.set_viewport_size({'width': 390, 'height': 844})
        page.screenshot(path=str(OUT / 'gold-mobile.png'), full_page=True)
        assert page.evaluate('document.documentElement.scrollWidth<=innerWidth+1')
        assert not errors, errors
        browser.close()
    (OUT / 'result.json').write_text(json.dumps({'checks': ['real-gateway', 'key-login', 'server-preview', 'add-subtract-set', 'zero-and-int32-limit', 'lost-response-reload-replay', 'stale-preview-conflict', 'character-isolation', 'account-isolation', 'ledger', 'no-idle-poll', 'dark-theme', 'mobile'], 'errors': errors}, ensure_ascii=False, indent=2), encoding='utf-8')
    print('PASS gateway gold browser checks')


if __name__ == '__main__':
    main()
