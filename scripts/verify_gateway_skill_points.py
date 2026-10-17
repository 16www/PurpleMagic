"""Exercise SP/TP through the real gateway and its disposable preview database."""
import json
from pathlib import Path
from playwright.sync_api import sync_playwright, expect

ROOT=Path(__file__).resolve().parents[1]
STATE=ROOT/'runtime/preview/gateway-live'
OUT=ROOT/'runtime/verification/gateway-skill-points'

def main():
    OUT.mkdir(parents=True,exist_ok=True)
    ready=json.loads((STATE/'ready.json').read_text(encoding='utf-8'))
    errors,requests=[],[]
    with sync_playwright() as p:
        browser=p.chromium.launch(headless=True)
        context=browser.new_context(ignore_https_errors=True,viewport={'width':1440,'height':1000})
        page=context.new_page()
        page.on('pageerror',lambda e:errors.append(str(e)))
        page.on('request',lambda r:requests.append(r.url) if '/api/' in r.url else None)
        page.goto(ready['url'])
        page.get_by_label('导入登录 Key').set_input_files(str(STATE/'login-key.json'))
        expect(page.get_by_label('管理密码')).not_to_have_value('')
        page.get_by_role('button',name='登录',exact=True).click()
        dialog=page.locator('#dialog')
        panel=dialog.get_by_role('region',name='角色技能点',exact=True)
        def open_character(account='晨雾',name='晨雾·剑影'):
            page.get_by_role('button',name='打开'+account,exact=True).click()
            page.get_by_role('button',name='查看'+name,exact=True).click()
            dialog.get_by_role('tab',name='SP / TP',exact=True).click()
            expect(panel.get_by_role('button',name='刷新技能点与记录')).to_be_visible()
        def preview(action,target,amount,reason):
            panel.get_by_label('技能点操作',exact=True).select_option(action)
            if action!='sync': panel.get_by_label('点数类型',exact=True).select_option(target)
            if action not in ('sync','clear_remaining'): panel.get_by_label('技能点数量',exact=True).fill(amount)
            panel.get_by_label('技能点操作原因',exact=True).fill(reason)
            panel.get_by_role('button',name='预览技能点变更',exact=True).click()
            expect(panel.get_by_label('技能点变更确认')).to_contain_text(reason)
        def submit(sp=None,tp=None):
            panel.get_by_role('button',name='确认技能点提交',exact=True).click()
            expect(panel.get_by_role('status',name='技能点操作结果')).to_contain_text('已完成')
            if sp is not None: expect(panel.locator('[data-points-remaining=sp]')).to_have_text(str(sp))
            if tp is not None: expect(panel.locator('[data-points-remaining=tp]')).to_have_text(str(tp))
        open_character()
        expect(panel.locator('[data-points-remaining=sp]')).to_have_count(1)
        sp=int(panel.locator('[data-points-remaining=sp]').inner_text())
        tp=int(panel.locator('[data-points-remaining=tp]').inner_text())
        preview('add','sp','100','技能点活动补偿')
        page.screenshot(path=str(OUT/'points-confirm.png'),full_page=True)
        lost=[]
        def lose_response(route):
            if route.request.method!='POST' or lost: route.continue_();return
            response=route.fetch();assert response.status==200,response.text()
            lost.append(True);route.abort('connectionreset')
        page.route('**/skill-point-operations',lose_response)
        panel.get_by_role('button',name='确认技能点提交',exact=True).click()
        expect(panel.get_by_role('button',name='重试技能点并取得回执')).to_be_enabled()
        page.reload();open_character()
        expect(panel.locator('[data-points-remaining=sp]')).to_have_text(str(sp+100))
        panel.get_by_role('button',name='重试技能点并取得回执').click()
        expect(panel.get_by_role('status',name='技能点操作结果')).to_contain_text('已完成')
        expect(panel.locator('.points-history > details')).to_have_count(1)
        page.unroute('**/skill-point-operations',lose_response)
        preview('subtract','sp','25','扣回多发点数');submit(sp+75,tp)
        preview('set','tp','20','TP 配额调整');submit(sp+75,tp+20)
        preview('clear_remaining','tp','0','TP 剩余归零');submit(sp+75,0)
        preview('sync','both','0','按当前等级重新计算');submit(sp+75,0)
        # Clearing remaining does not erase the bonus or refund consumption on sync.
        panel.get_by_label('技能点操作',exact=True).select_option('set')
        panel.get_by_label('点数类型',exact=True).select_option('tp')
        panel.get_by_label('技能点数量',exact=True).fill('0')
        panel.get_by_label('技能点操作原因',exact=True).fill('低于已消耗拒绝')
        panel.get_by_role('button',name='预览技能点变更',exact=True).click()
        expect(panel.get_by_role('alert')).to_contain_text('小于已消耗')
        preview('add','sp','1','过时预览')
        base=ready['url'].split('/console/')[0]+'/api/v1/'
        cap=context.request.get(base+'capabilities').json()['data']
        read=context.request.get(base+'characters/10/skill-points').json()['data']
        body={'accountId':read['accountId'],'action':'add','target':'sp','amount':'5','revision':read['revision'],'definitionRevision':read['definitionRevision'],'reason':'并行管理员'}
        headers={'X-CSRF-Token':cap['csrfToken']}
        plan=context.request.post(base+'characters/10/skill-point-operations/preview',data=body,headers=headers)
        assert plan.status==200,plan.text()
        body['planToken']=plan.json()['data']['planToken'];headers['Idempotency-Key']='browser-points-conflict-001'
        result=context.request.post(base+'characters/10/skill-point-operations',data=body,headers=headers)
        assert result.status==200,result.text()
        panel.get_by_role('button',name='确认技能点提交').click()
        expect(panel.get_by_role('alert')).to_contain_text('已变更')
        panel.get_by_role('button',name='刷新技能点与记录').click()
        expect(panel.locator('[data-points-remaining=sp]')).to_have_text(str(sp+80))
        expect(panel.locator('.points-history > details')).to_have_count(6)
        panel.locator('.points-history > details').first.locator('summary').click()
        page.screenshot(path=str(OUT/'points-desktop.png'),full_page=True)
        # A second character keeps its own balances and operation history.
        dialog.get_by_role('button',name='关闭',exact=True).click()
        open_character('晨雾','晨雾·逐光')
        expect(panel.locator('[data-points-remaining=sp]')).to_have_text(str(sp))
        expect(panel.locator('.points-history')).to_contain_text('暂无')
        dialog.get_by_role('button',name='关闭',exact=True).click()
        open_character('星河','星河·逐光')
        expect(panel.get_by_role('alert')).to_contain_text('尚无技能点账本')
        expect(panel.get_by_role('button',name='预览技能点变更')).to_be_disabled()
        dialog.get_by_role('button',name='关闭',exact=True).click()
        open_character('流光','流光·剑影')
        preview('add','sp','1','在线拒绝')
        panel.get_by_role('button',name='确认技能点提交').click()
        expect(panel.get_by_role('alert')).to_contain_text('角色在线')
        dialog.get_by_role('button',name='关闭',exact=True).click()
        page.locator('#theme-open').click()
        page.locator('[data-action=preset][data-value=ember-dusk]').click()
        page.locator('[data-action=mode][data-value=dark]').click()
        page.keyboard.press('Escape');open_character()
        expect(panel.locator('[data-points-remaining=sp]')).to_have_text(str(sp+80))
        page.screenshot(path=str(OUT/'points-dark.png'),full_page=True)
        page.set_viewport_size({'width':390,'height':844})
        page.screenshot(path=str(OUT/'points-mobile.png'),full_page=True)
        panel.locator('#points-form').scroll_into_view_if_needed()
        page.screenshot(path=str(OUT/'points-mobile-form.png'),full_page=True)
        panel.locator('.points-history').scroll_into_view_if_needed()
        page.screenshot(path=str(OUT/'points-mobile-history.png'),full_page=True)
        assert page.evaluate('document.documentElement.scrollWidth<=innerWidth+1')
        page.wait_for_timeout(300);baseline=len(requests);page.wait_for_timeout(1600)
        assert len(requests)==baseline,'idle polling'
        assert not errors,errors
        browser.close()
    (OUT/'browser-result.json').write_text(json.dumps({'checks':['real-gateway','PVF-balance','add-subtract-set','clear-sync-retains-spent','below-spent-reject','signed-preview','reload-replay','stale-conflict','character-isolation','missing-ledger','online-reject','dark-theme','mobile','no-idle-poll'],'errors':errors},ensure_ascii=False,indent=2),encoding='utf-8')
    print('PASS gateway skill point browser checks')

if __name__=='__main__': main()
