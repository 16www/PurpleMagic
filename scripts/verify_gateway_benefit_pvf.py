"""Browser acceptance against the disposable PVF revision integration fixture."""
import argparse
import json
from pathlib import Path
from playwright.sync_api import sync_playwright, expect

ROOT=Path(__file__).resolve().parents[1]
OUT=ROOT/'runtime/verification/gateway-benefit-pvf'

def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--url',required=True)
    args=parser.parse_args()
    assert args.url.startswith('http://127.0.0.1:')
    OUT.mkdir(parents=True,exist_ok=True)
    errors,requests=[],[]
    with sync_playwright() as p:
        browser=p.chromium.launch(headless=True)
        context=browser.new_context(viewport={'width':1440,'height':1100})
        page=context.new_page()
        page.on('pageerror',lambda e:errors.append(str(e)))
        page.on('request',lambda r:requests.append(r.url) if '/api/' in r.url else None)
        page.goto(args.url+'/console/#mail')
        page.get_by_label('管理密码').fill('fixture')
        page.get_by_role('button',name='登录',exact=True).click()
        row=page.get_by_role('row').filter(has_text='网页版本福利')
        expect(row).to_be_visible()
        row.get_by_role('button',name='核对 PVF',exact=True).click()
        dialog=page.locator('#dialog')
        expect(dialog.get_by_role('button',name='确认使用当前 PVF',exact=True)).to_be_visible()
        expect(dialog).to_contain_text('历史单项脚本摘要未记录')
        expect(dialog).to_contain_text('旧短剑')
        expect(dialog).to_contain_text('新短剑')
        field=dialog.locator('.pvf-field').filter(has=page.get_by_text('耐久度',exact=True))
        expect(field).to_contain_text('30')
        expect(field).to_contain_text('45')
        page.screenshot(path=str(OUT/'pvf-review-desktop.png'),full_page=True)
        lost=[]
        def drop(route):
            response=route.fetch()
            assert response.status==200,response.text()
            lost.append(response.json()['data'])
            route.abort()
        page.route('**/pvf-apply',drop)
        dialog.get_by_role('button',name='确认使用当前 PVF',exact=True).click()
        expect(dialog.get_by_role('alert')).to_be_visible()
        assert len(lost)==1
        page.unroute('**/pvf-apply',drop)
        page.reload()
        page.get_by_role('button',name='恢复确认',exact=True).click()
        dialog.get_by_role('button',name='重试确认',exact=True).click()
        expect(dialog.get_by_role('status')).to_contain_text('已核对')
        expect(dialog).to_contain_text('0 → 1')
        dialog.get_by_role('button',name='关闭',exact=True).click()
        expect(page.get_by_role('button',name='恢复确认',exact=True)).to_have_count(0)
        row.get_by_role('button',name='核对历史',exact=True).click()
        expect(dialog.locator('[data-pvf-view] > details')).to_have_count(1)
        dialog.locator('[data-pvf-view] > details > summary').click()
        expect(dialog).to_contain_text('耐久度')
        dialog.get_by_role('button',name='关闭',exact=True).click()
        page.locator('#theme-open').click()
        page.locator('[data-action=preset][data-value=ember-dusk]').click()
        page.locator('[data-action=mode][data-value=dark]').click()
        page.keyboard.press('Escape')
        row.get_by_role('button',name='核对历史',exact=True).click()
        dialog.locator('[data-pvf-view] > details > summary').click()
        page.set_viewport_size({'width':390,'height':844})
        page.screenshot(path=str(OUT/'pvf-history-mobile.png'),full_page=True)
        assert page.evaluate('document.documentElement.scrollWidth<=innerWidth+1')
        assert dialog.evaluate('(e)=>e.scrollWidth<=e.clientWidth+1')
        dialog.get_by_role('button',name='关闭',exact=True).click()
        row.get_by_role('button',name='核对 PVF',exact=True).click()
        expect(dialog).to_contain_text('此规则与当前 PVF 版本一致')
        expect(dialog.get_by_role('button',name='确认使用当前 PVF',exact=True)).to_have_count(0)
        page.wait_for_timeout(300)
        count=len(requests)
        page.wait_for_timeout(1500)
        assert len(requests)==count and not errors,errors
        browser.close()
    (OUT/'browser-result.json').write_text(json.dumps({'checks':['actual-http-preview-and-commit','legacy-definition-absence','durability-diff','lost-response-refresh-recovery','single-history','mobile-dark-theme','same-pvf-read-only','idle-no-polling'],'errors':errors},ensure_ascii=False,indent=2),encoding='utf-8')
    print('PASS benefit PVF browser')

if __name__=='__main__':main()
