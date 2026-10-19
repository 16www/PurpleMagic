"""Exercise the production gateway and currency UI in the disposable preview."""
import json
from pathlib import Path
from playwright.sync_api import sync_playwright, expect

ROOT=Path(__file__).resolve().parents[1]
STATE=ROOT/'runtime/preview/gateway-live'
OUT=ROOT/'runtime/verification/gateway-currency'


def main():
    OUT.mkdir(parents=True,exist_ok=True)
    ready=json.loads((STATE/'ready.json').read_text(encoding='utf-8'))
    errors=[]
    counts=[]
    with sync_playwright() as p:
        browser=p.chromium.launch(headless=True)
        context=browser.new_context(ignore_https_errors=True,viewport={'width':1440,'height':1000})
        page=context.new_page()
        page.on('pageerror',lambda e:errors.append(str(e)))
        page.on('request',lambda r:counts.append(r.url) if '/api/' in r.url else None)
        page.goto(ready['url'])
        page.get_by_label('导入登录 Key').set_input_files(str(STATE/'login-key.json'))
        expect(page.get_by_label('管理密码')).not_to_have_value('')
        page.get_by_role('button',name='登录',exact=True).click()
        expect(page.locator('[data-account]')).to_have_count(4)
        page.get_by_role('button',name='打开晨雾',exact=True).click()
        page.get_by_role('button',name='点券与流水',exact=True).click()
        wallet=page.get_by_role('region',name='账号点券')
        expect(wallet.locator('.currency-balance')).to_have_text('0')
        wallet.get_by_label('点券数量',exact=True).fill('250')
        wallet.get_by_label('操作原因',exact=True).fill('活动补偿')
        wallet.get_by_role('button',name='核对变更',exact=True).click()
        expect(wallet.get_by_label('点券变更确认')).to_contain_text('250')
        page.screenshot(path=str(OUT/'currency-confirm.png'),full_page=True)
        lost=[]
        def lose_response(route):
            if route.request.method!='POST' or lost:
                route.continue_();return
            response=route.fetch()
            assert response.status==200
            lost.append(True)
            route.abort('connectionreset')
        page.route('**/currency-adjustments',lose_response)
        wallet.get_by_role('button',name='确认提交',exact=True).click()
        expect(wallet.get_by_role('button',name='重试并取得回执',exact=True)).to_be_enabled()
        assert lost
        # Recover after full navigation; the original request ID stays in this tab.
        page.reload()
        page.get_by_role('button',name='打开晨雾',exact=True).click()
        page.get_by_role('button',name='点券与流水',exact=True).click()
        expect(wallet.locator('.currency-balance')).to_have_text('250')
        wallet.get_by_role('button',name='重试并取得回执',exact=True).click()
        expect(wallet.get_by_role('status')).to_contain_text('余额 250')
        expect(wallet.locator('tbody tr')).to_have_count(1)
        expect(wallet.locator('tbody')).to_contain_text('活动补偿')
        page.unroute('**/currency-adjustments',lose_response)
        wallet.get_by_label('操作',exact=True).select_option('subtract')
        wallet.get_by_label('点券数量',exact=True).fill('50')
        wallet.get_by_label('操作原因',exact=True).fill('退款扣减')
        wallet.get_by_role('button',name='核对变更',exact=True).click()
        wallet.get_by_role('button',name='确认提交',exact=True).click()
        expect(wallet.locator('.currency-balance')).to_have_text('200')
        expect(wallet.locator('tbody tr')).to_have_count(2)
        wallet.get_by_label('点券数量',exact=True).fill('201')
        wallet.get_by_label('操作原因',exact=True).fill('余额校验')
        wallet.get_by_role('button',name='核对变更',exact=True).click()
        expect(wallet.get_by_role('alert')).to_contain_text('超出允许范围')
        wallet.get_by_label('点券数量',exact=True).fill('9007199254740993')
        wallet.get_by_label('操作',exact=True).select_option('add')
        wallet.get_by_role('button',name='核对变更',exact=True).click()
        expect(wallet.get_by_label('点券变更确认')).to_contain_text('9007199254741193')
        wallet.get_by_role('button',name='返回修改',exact=True).click()
        page.get_by_role('button',name='打开远山',exact=True).click()
        page.get_by_role('button',name='点券与流水',exact=True).click()
        expect(wallet.locator('.currency-balance')).to_have_text('0')
        expect(wallet.locator('tbody')).to_contain_text('暂无 GM')
        page.get_by_role('button',name='打开晨雾',exact=True).click()
        page.get_by_role('button',name='点券与流水',exact=True).click()
        expect(wallet.locator('tbody tr')).to_have_count(2)
        page.wait_for_timeout(300)
        baseline=len(counts)
        page.wait_for_timeout(1600)
        assert len(counts)==baseline,'idle UI made requests'
        page.screenshot(path=str(OUT/'currency-desktop.png'),full_page=True)
        page.locator('#theme-open').click()
        page.locator('[data-action=preset][data-value=ember-dusk]').click()
        page.locator('[data-action=mode][data-value=dark]').click()
        page.keyboard.press('Escape')
        page.wait_for_timeout(200)
        page.screenshot(path=str(OUT/'currency-dark.png'),full_page=True)
        page.set_viewport_size({'width':390,'height':844})
        page.screenshot(path=str(OUT/'currency-mobile.png'),full_page=True)
        assert page.evaluate('document.documentElement.scrollWidth<=innerWidth+1')
        assert context.request.get(ready['url'].split('/console/')[0]+'/api/accounts/1/currency').status==404
        assert not errors,errors
        browser.close()
    (OUT/'result.json').write_text(json.dumps({'checks':['production-gateway','key-file-login','add','deduct','lost-response-reload-idempotency','account-isolation','int64-preview','insufficient-balance','no-idle-poll','dark-theme','mobile','legacy-route-404'],'errors':errors},ensure_ascii=False,indent=2),encoding='utf-8')
    print('PASS gateway currency browser checks')


if __name__=='__main__':main()
