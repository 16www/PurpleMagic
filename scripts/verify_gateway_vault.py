"""Exercise account vault transactions through the actual independent GM Web."""
import json
from pathlib import Path
from playwright.sync_api import sync_playwright, expect

ROOT = Path(__file__).resolve().parents[1]
STATE = ROOT / 'runtime/preview/gateway-live'
OUT = ROOT / 'runtime/verification/gateway-vault'


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
        vault = dialog.get_by_role('region', name='账号金库管理')

        def open_character(account='远山', name='远山·剑影'):
            page.get_by_role('button', name='打开'+account, exact=True).click()
            page.get_by_role('button', name='查看'+name, exact=True).click()
            dialog.get_by_role('tab', name='账号金库', exact=True).click()
            expect(vault.locator('.vault-balance')).not_to_have_text('读取中…')

        def read_api(path):
            return page.evaluate("async path => {const r=await fetch('/api/v1/'+path);if(!r.ok)throw Error(r.status);return r.json();}", path)['data']

        def preview(action, amount, capacity, reason):
            vault.get_by_label('金库金币操作', exact=True).select_option(action)
            if action != 'keep':
                vault.get_by_label('金库金币数量', exact=True).fill(amount)
            vault.get_by_label('金库容量目标', exact=True).select_option(capacity)
            vault.get_by_label('金库操作原因', exact=True).fill(reason)
            vault.get_by_role('button', name='预览金库变更', exact=True).click()
            expect(vault.get_by_label('金库变更确认')).to_contain_text(reason)

        def submit(gold, slots):
            vault.get_by_role('button', name='确认金库提交', exact=True).click()
            expect(vault.get_by_role('status')).to_contain_text('金币 '+gold+'，容量 '+slots+' 格')
            expect(vault.locator('.vault-balance')).to_have_text(gold)
            expect(vault.locator('.vault-capacity')).to_have_text(slots+' 格')

        open_character()
        expect(vault.locator('.vault-capacity')).to_have_text('未开通')
        character_before = read_api('characters/20/gold')
        cera_before = read_api('accounts/preview:2/currency')
        preview('add', '5000', '8', '金库开通与补偿')
        assert read_api('accounts/preview:2/vault')['values'] == {'gold': '0', 'capacity': 0, 'created': False}
        page.screenshot(path=str(OUT / 'vault-confirm.png'), full_page=True)
        lost = []

        def lose_response(route):
            if route.request.method != 'POST' or lost:
                route.continue_()
                return
            response = route.fetch()
            assert response.status == 200
            lost.append(True)
            route.abort('connectionreset')

        page.route('**/vault-operations', lose_response)
        vault.get_by_role('button', name='确认金库提交', exact=True).click()
        expect(vault.get_by_role('button', name='重试金库并取得回执', exact=True)).to_be_enabled()
        assert lost
        page.reload()
        # The pending operation and receipt belong to the account, across roles.
        open_character('远山', '远山·逐光')
        expect(vault.locator('.vault-balance')).to_have_text('5000')
        expect(vault.locator('.vault-capacity')).to_have_text('8 格')
        vault.get_by_role('button', name='重试金库并取得回执', exact=True).click()
        expect(vault.get_by_role('status')).to_contain_text('金币 5000')
        expect(vault.locator('tbody tr')).to_have_count(1)
        page.unroute('**/vault-operations', lose_response)
        preview('subtract', '50', 'keep', '异常金币扣回')
        submit('4950', '8')
        vault.get_by_role('button', name='扩至最大容量', exact=True).click()
        expect(vault.get_by_label('金库容量目标', exact=True)).to_have_value('240')
        vault.get_by_label('金库操作原因', exact=True).fill('扩至最大金库')
        vault.get_by_role('button', name='预览金库变更', exact=True).click()
        expect(vault.get_by_label('金库变更确认')).to_contain_text('240 格')
        submit('4950', '240')
        assert vault.get_by_label('金库容量目标', exact=True).locator('option').all_text_contents() == ['保持容量', '240 格']
        preview('set', '400000000', 'keep', '金库金币上限')
        submit('400000000', '240')
        vault.get_by_label('金库金币操作', exact=True).select_option('add')
        vault.get_by_label('金库金币数量', exact=True).fill('1')
        vault.get_by_label('金库操作原因', exact=True).fill('越界检查')
        vault.get_by_role('button', name='预览金库变更', exact=True).click()
        expect(vault.get_by_role('alert')).to_contain_text('超出允许范围')
        preview('set', '0', 'keep', '余额清零')
        submit('0', '240')
        preview('add', '5', 'keep', '并发版本检查')
        page.evaluate("""async () => {
            const api=async(p,b,k)=>{const cap=await (await fetch('/api/v1/capabilities')).json(); const r=await fetch('/api/v1/'+p,{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':cap.data.csrfToken,...(k?{'Idempotency-Key':k}:{})},body:JSON.stringify(b)});if(!r.ok)throw Error('concurrent '+r.status);return (await r.json()).data;};
            const s=(await (await fetch('/api/v1/accounts/preview:2/vault')).json()).data;
            const b={gold:{action:'set',amount:'3'},revision:s.revision,definitionRevision:s.definition.revision,reason:'另一个管理员'};
            const p=await api('accounts/preview:2/vault-operations/preview',b);
            await api('accounts/preview:2/vault-operations',{...b,planToken:p.planToken},crypto.randomUUID());
        }""")
        vault.get_by_role('button', name='确认金库提交', exact=True).click()
        expect(vault.get_by_role('alert')).to_contain_text('已变更')
        vault.get_by_role('button', name='刷新金库与流水', exact=True).click()
        expect(vault.locator('.vault-balance')).to_have_text('3')
        expect(vault.locator('tbody tr')).to_have_count(6)
        assert read_api('characters/20/gold') == character_before
        assert read_api('accounts/preview:2/currency')['balance'] == cera_before['balance']
        assert read_api('accounts/preview:2/currency-adjustments') == []
        dialog.get_by_role('button', name='关闭', exact=True).click()
        open_character()
        expect(vault.locator('.vault-balance')).to_have_text('3')
        expect(vault.locator('tbody tr')).to_have_count(6)
        dialog.get_by_role('button', name='关闭', exact=True).click()
        open_character('晨雾', '晨雾·剑影')
        expect(vault.locator('.vault-balance')).to_have_text('0')
        expect(vault.locator('tbody')).to_contain_text('暂无 GM')
        dialog.get_by_role('button', name='关闭', exact=True).click()
        open_character()
        page.wait_for_timeout(300)
        baseline = len(requests)
        page.wait_for_timeout(1600)
        assert len(requests) == baseline, 'idle requests'
        page.screenshot(path=str(OUT / 'vault-desktop.png'), full_page=True)
        dialog.get_by_role('button', name='关闭', exact=True).click()
        page.locator('#theme-open').click()
        page.locator('[data-action=preset][data-value=ember-dusk]').click()
        page.locator('[data-action=mode][data-value=dark]').click()
        page.keyboard.press('Escape')
        open_character()
        expect(vault.locator('.vault-balance')).to_have_text('3')
        page.screenshot(path=str(OUT / 'vault-dark.png'), full_page=True)
        page.set_viewport_size({'width': 390, 'height': 844})
        page.screenshot(path=str(OUT / 'vault-mobile.png'), full_page=True)
        assert page.evaluate('document.documentElement.scrollWidth<=innerWidth+1')
        assert not errors, errors
        browser.close()
    (OUT / 'result.json').write_text(json.dumps({'checks': ['real-gateway', 'create-vault-and-gold-atomic', 'preview-zero-write', 'lost-response-replay-across-roles', 'gold-add-subtract-set', 'capacity-max', 'pvf-tier-gold-limit', 'stale-preview-conflict', 'account-isolation', 'character-gold-cera-preserved', 'shared-ledger', 'no-idle-poll', 'dark-theme', 'mobile'], 'errors': errors}, ensure_ascii=False, indent=2), encoding='utf-8')
    print('PASS gateway vault browser checks')


if __name__ == '__main__':
    main()
