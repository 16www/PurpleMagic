"""Exercise all-character gold benefits against a disposable HTTP/DB fixture."""
import argparse
import json
from pathlib import Path
from urllib.parse import urlsplit
from playwright.sync_api import sync_playwright, expect


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--url', required=True)
    parser.add_argument('--expected', required=True, type=int)
    args = parser.parse_args()
    origin = args.url
    parsed = urlsplit(origin)
    if parsed.scheme != 'http' or parsed.hostname != '127.0.0.1':
        parser.error('Only the disposable loopback fixture is accepted')
    out = Path(__file__).resolve().parents[1]/'runtime/verification/gold-benefit'
    out.mkdir(parents=True, exist_ok=True)
    errors, calls, runs = [], [], []
    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        context = browser.new_context(viewport={'width':1440, 'height':1000})
        page = context.new_page()
        page.on('pageerror', lambda e: errors.append(str(e)))
        page.on('request', lambda r: calls.append(r.url) if '/api/' in r.url else None)
        page.goto(origin+'/console/#mail')
        page.get_by_label('管理密码', exact=True).fill('fixture')
        page.get_by_role('button', name='登录', exact=True).click()
        page.get_by_role('button', name='全部角色福利', exact=True).click()
        form = page.locator('#op-compose')
        expect(form.locator('[name=scope]')).to_have_value('character')
        expect(form.get_by_label('启用后自动发给新建角色')).to_be_checked()
        expect(form).to_contain_text('全部现有角色')
        form.get_by_label('邮件标题', exact=True).fill('全角色金币回归')
        form.get_by_label('金币', exact=True).fill('20000000')
        lost = []

        def lose_create(route):
            body = route.request.post_data_json
            assert body['gold'] == 20000000 and body['scope'] == 'character'
            assert not body.get('itemId') and 'options' not in body and 'pvfChecksum' not in body
            response = route.fetch()
            assert response.status == 200, response.text()
            lost.append(response.json()['data'])
            route.abort()

        page.route('**/api/v1/benefits', lose_create)
        form.get_by_role('button', name='保存规则', exact=True).click()
        expect(page.get_by_role('alert')).to_be_visible()
        assert len(lost) == 1
        page.unroute('**/api/v1/benefits', lose_create)
        page.reload()
        page.get_by_role('button', name='新建福利规则', exact=True).click()
        expect(form.get_by_label('金币', exact=True)).to_have_value('20000000')
        form.get_by_role('button', name='保存规则', exact=True).click()
        expect(page.get_by_role('status')).to_contain_text('已保存')
        row = page.get_by_role('row').filter(has_text='全角色金币回归')
        expect(row).to_have_count(1)
        expect(row).to_contain_text('20000000 金币')
        row.get_by_role('button', name='启用', exact=True).click()
        page.locator('#dialog').get_by_role('button', name='确认启用', exact=True).click()
        expect(row).to_contain_text('已启用')
        row.get_by_role('button', name='预览名单', exact=True).click()
        detail = page.get_by_role('region', name='任务详情', exact=True)
        expect(detail).to_contain_text(f'待发 {args.expected}')
        page.screenshot(path=str(out/'all-characters-preview.png'), full_page=True)

        def pause_run(route):
            response = route.fetch()
            assert response.status == 200, response.text()
            runs.append(response.json()['data'])
            detail.get_by_role('button', name='暂停发放', exact=True).click()
            route.fulfill(response=response)

        page.route('**/api/v1/tasks/*/run', pause_run)
        detail.get_by_role('button', name='确认发放', exact=True).click()
        page.locator('#dialog').get_by_role('button', name='确认并发放', exact=True).click()
        expect(page.get_by_role('status')).to_contain_text('已暂停')
        expect(detail).to_contain_text('成功 10')
        assert len(runs) == 1
        page.unroute('**/api/v1/tasks/*/run', pause_run)

        def lose_run(route):
            response = route.fetch()
            assert response.status == 200, response.text()
            runs.append(response.json()['data'])
            route.abort()

        # A lost reply stops the loop; refreshing the task recovers the committed batch.
        page.route('**/api/v1/tasks/*/run', lose_run)
        detail.get_by_role('button', name='继续发放', exact=True).click()
        page.locator('#dialog').get_by_role('button', name='确认并发放', exact=True).click()
        expect(page.get_by_role('alert')).to_be_visible()
        page.unroute('**/api/v1/tasks/*/run', lose_run)
        expect(detail).to_contain_text('成功 20')
        assert len(runs) == 2
        detail.get_by_role('button', name='继续发放', exact=True).click()
        page.locator('#dialog').get_by_role('button', name='确认并发放', exact=True).click()
        expect(detail).to_contain_text(f'成功 {args.expected} · 失败 0')
        # A fresh snapshot uses the same permanent per-character receipts.
        page.evaluate("location.hash='mail'")
        row.get_by_role('button', name='预览名单', exact=True).click()
        expect(detail).to_contain_text(f'待发 0 · 成功 {args.expected} · 失败 0')
        page.set_viewport_size({'width':390, 'height':844})
        expect(detail.locator('.op-recipient-cards')).to_be_visible()
        assert page.evaluate('document.documentElement.scrollWidth <= innerWidth+1')
        page.screenshot(path=str(out/'all-characters-result-mobile.png'), full_page=True)
        page.wait_for_timeout(200)
        before = len(calls)
        page.wait_for_timeout(1300)
        assert before == len(calls) and not errors, errors
        browser.close()
    (out/'result.json').write_text(json.dumps({'checks':['all-characters-shortcut','gold-only','future-roles-option','lost-create-recovery','pause-and-resume','lost-batch-recovery','multiple-batches','repeat-preview-dedup','mobile','idle-zero-requests'], 'recipients':args.expected,'errors':errors},ensure_ascii=False,indent=2),encoding='utf-8')
    print('PASS all-character gold benefit browser')


if __name__ == '__main__':
    main()
