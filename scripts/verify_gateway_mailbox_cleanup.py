"""Verify filtered cross-page cleanup and interrupted progress in a disposable preview."""
import json
from pathlib import Path
from playwright.sync_api import sync_playwright, expect

ROOT=Path(__file__).resolve().parents[1]
STATE=ROOT/'runtime/preview/gateway-live'
OUT=ROOT/'runtime/verification/gateway-mailbox-cleanup'


def main():
    OUT.mkdir(parents=True,exist_ok=True)
    ready=json.loads((STATE/'ready.json').read_text(encoding='utf-8'))
    errors,requests=[],[]
    with sync_playwright() as p:
        browser=p.chromium.launch(headless=True)
        context=browser.new_context(ignore_https_errors=True,viewport={'width':1440,'height':1000},timezone_id='Asia/Shanghai')
        page=context.new_page()
        page.on('pageerror',lambda e:errors.append(str(e)))
        page.on('request',lambda r:requests.append(r.url) if '/api/' in r.url else None)
        page.goto(ready['url'])
        page.get_by_label('导入登录 Key').set_input_files(str(STATE/'login-key.json'))
        expect(page.get_by_label('管理密码')).not_to_have_value('')
        page.get_by_role('button',name='登录',exact=True).click()
        dialog=page.locator('#dialog')
        ops=dialog.get_by_role('region',name='邮箱删除操作')
        panel=dialog.get_by_role('region',name='跨页邮箱清理')
        base=ready['url'].split('/console/')[0]+'/api/v1/'

        def get(path):
            res=context.request.get(base+path)
            assert res.status==200,res.text()
            return res.json()['data']

        def open_character():
            page.get_by_role('button',name='打开远山',exact=True).click()
            page.get_by_role('button',name='查看远山·逐光',exact=True).click()
            dialog.get_by_role('tab',name='邮箱',exact=True).click()
            expect(ops).to_be_visible()

        def filter_mail():
            dialog.get_by_label('邮件标题或编号',exact=True).fill('历史')
            dialog.get_by_label('邮件发件人',exact=True).fill('运营')
            dialog.get_by_label('附件领取状态',exact=True).select_option('claimed')
            dialog.get_by_role('button',name='筛选邮件',exact=True).click()
            expect(dialog.locator('[data-mail-select]')).to_have_count(20)

        def preview(reason):
            ops.get_by_label('邮件删除原因').fill(reason)
            ops.get_by_role('button',name='预览跨页清理',exact=True).click()

        def summary():return panel.locator('.mail-cleanup-summary')

        def current_job():
            return get('characters/21/mailbox-cleanups')[0]

        original=get('characters/10/mailbox')
        open_character();filter_mail()
        dialog.get_by_label('起始时间',exact=True).fill('2026-10-01T08:25')
        dialog.get_by_label('截止时间（不含）',exact=True).fill('2026-10-01T08:30')
        dialog.get_by_role('button',name='筛选邮件',exact=True).click()
        expect(dialog.locator('[data-mail-select]')).to_have_count(5)

        dialog.get_by_role('button',name='清除邮件筛选',exact=True).click();filter_mail()
        lost_prepare=[]

        def lose_prepare(route):
            if lost_prepare:route.continue_();return
            response=route.fetch();assert response.status==200,response.text()
            lost_prepare.append(True);route.abort('connectionreset')

        page.route('**/mailbox-cleanups/*/prepare',lose_prepare)
        preview('跨页清理历史活动通知')
        expect(panel.get_by_role('alert')).to_be_visible()
        assert lost_prepare and current_job()['total']==200
        assert get('characters/21/mailbox-operations')==[]
        page.reload();open_character()
        panel.get_by_role('button',name='恢复邮箱清理任务').click()
        expect(summary()).to_contain_text('0 / 200')
        page.unroute('**/mailbox-cleanups/*/prepare',lose_prepare)
        expect(panel.get_by_role('button',name='继续准备名单')).to_be_enabled()
        panel.get_by_role('button',name='继续准备名单').click()
        expect(summary()).to_contain_text('待确认 · 0 / 205')
        expect(panel.get_by_role('button',name='名单下一页')).to_be_enabled()
        panel.get_by_role('button',name='名单下一页').click()
        expect(panel.locator('.mail-cleanup-items')).to_contain_text('cleanup-0021')
        assert get('characters/21/mailbox-operations')==[]
        summary().scroll_into_view_if_needed()
        page.screenshot(path=str(OUT/'cleanup-preview.png'),full_page=True)
        lost_run=[]

        def lose_run(route):
            if lost_run:route.continue_();return
            response=route.fetch();assert response.status==200,response.text()
            lost_run.append(True);route.abort('connectionreset')

        page.route('**/mailbox-cleanups/*/run',lose_run)
        panel.get_by_role('button',name='确认跨页清理').click()
        expect(panel.get_by_role('alert')).to_be_visible()
        assert lost_run and current_job()['processed']==100
        page.reload();open_character()
        panel.get_by_role('button',name='恢复邮箱清理任务').click()
        expect(summary()).to_contain_text('处理中 · 100 / 205')
        page.unroute('**/mailbox-cleanups/*/run',lose_run)
        expect(panel.get_by_role('button',name='继续清理')).to_be_enabled()
        panel.get_by_role('button',name='继续清理').click()
        expect(summary()).to_contain_text('已完成 · 205 / 205')
        expect(dialog.locator('[data-mail-select]')).to_have_count(5)
        records=get('characters/21/mailbox-operations')
        assert len(records)==3 and sum(len(v['changes']) for v in records)==205
        assert len({v['cleanupId'] for v in records})==1
        assert get('characters/10/mailbox')==original
        expect(panel.get_by_role('button',name='查看清理任务')).to_be_enabled()
        panel.get_by_role('button',name='查看清理任务').click()
        expect(panel.locator('[data-cleanup-open]')).to_have_count(1)
        preview('未领取资产范围')
        expect(summary()).to_contain_text('待确认 · 0 / 5')
        expect(panel.get_by_role('button',name='确认跨页清理')).to_be_disabled()
        expect(summary()).to_contain_text('金币 5000')
        panel.get_by_role('button',name='结束此清理').click()
        expect(summary()).to_contain_text('已结束 · 0 / 5')
        ops.get_by_label('同时丢弃未领取资产').check();preview('并发清理冲突')
        expect(summary()).to_contain_text('待确认 · 0 / 5')
        headers={'X-CSRF-Token':get('capabilities')['csrfToken']}
        body={'accountId':'preview:2','items':[{'mailId':'cleanup-0206'}],'discardUnclaimed':True,'reason':'另一管理员删除'}
        plan=context.request.post(base+'characters/21/mailbox-operations/preview',data=body,headers=headers);assert plan.status==200,plan.text()
        body.update({k:plan.json()['data'][k] for k in ('items','planToken')});headers['Idempotency-Key']='cleanup-other-admin-delete'
        response=context.request.post(base+'characters/21/mailbox-operations',data=body,headers=headers);assert response.status==200,response.text()
        expect(panel.get_by_role('button',name='确认跨页清理')).to_be_enabled()
        panel.get_by_role('button',name='确认跨页清理').click()
        expect(summary()).to_contain_text('邮箱已变更 · 0 / 5')
        assert len(get('characters/21/mailbox'))==4
        page.screenshot(path=str(OUT/'cleanup-conflict.png'),full_page=True)
        dialog.get_by_role('button',name='关闭',exact=True).click()
        page.locator('#theme-open').click()
        page.locator('[data-action=preset][data-value=ember-dusk]').click()
        page.locator('[data-action=mode][data-value=dark]').click()
        page.keyboard.press('Escape');open_character()
        ops.get_by_label('同时丢弃未领取资产').check();preview('剩余邮件清理预览')
        expect(summary()).to_contain_text('待确认 · 0 / 4')
        expect(panel.get_by_role('button',name='确认跨页清理')).to_be_enabled()
        summary().scroll_into_view_if_needed();page.screenshot(path=str(OUT/'cleanup-dark.png'),full_page=True)
        page.set_viewport_size({'width':390,'height':844});summary().scroll_into_view_if_needed()
        page.screenshot(path=str(OUT/'cleanup-mobile.png'),full_page=True)
        assert page.evaluate('document.documentElement.scrollWidth<=innerWidth+1')
        page.wait_for_timeout(300);baseline=len(requests);page.wait_for_timeout(1600);assert len(requests)==baseline,'idle requests'
        assert not errors,errors
        browser.close()
    (OUT/'result.json').write_text(json.dumps({'checks':['literal-filters','half-open-time-range','snapshot-zero-game-writes','prepare-response-recovery','cross-page-list','205-mails-three-batches','run-response-recovery','durable-progress','unclaimed-confirmation','cancel','concurrent-mailbox-change','isolated-character','themes','mobile','idle-zero-requests'],'errors':errors},ensure_ascii=False,indent=2),encoding='utf-8')
    print('PASS gateway mailbox cleanup browser checks')


if __name__=='__main__':main()
