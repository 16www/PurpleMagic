"""Exercise level changes through the actual gateway and disposable preview."""
import json
from pathlib import Path
from playwright.sync_api import sync_playwright, expect

ROOT=Path(__file__).resolve().parents[1]
STATE=ROOT/'runtime/preview/gateway-live'
OUT=ROOT/'runtime/verification/gateway-level'

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
        panel=dialog.get_by_role('region',name='角色等级管理',exact=True)
        def open_character(account='晨雾',name='晨雾·剑影'):
            page.get_by_role('button',name='打开'+account,exact=True).click()
            page.get_by_role('button',name='查看'+name,exact=True).click()
            dialog.get_by_role('tab',name='等级与成长',exact=True).click()
            expect(panel.get_by_role('button',name='刷新等级与记录')).to_be_visible()
        def preview(level,reason):
            panel.get_by_label('目标等级',exact=True).fill(str(level))
            panel.get_by_label('等级操作原因',exact=True).fill(reason)
            panel.get_by_role('button',name='预览等级变更',exact=True).click()
            expect(panel.get_by_label('等级变更确认')).to_contain_text(reason)
        def submit(level):
            panel.get_by_role('button',name='确认等级提交',exact=True).click()
            expect(panel.get_by_role('status',name='等级操作结果')).to_contain_text('等级已调整至 '+str(level))
            expect(panel.locator('[data-level-current]')).to_have_text(str(level))
        open_character();expect(panel.locator('[data-level-current]')).to_have_text('90')
        base=ready['url'].split('/console/')[0]+'/api/v1/'
        def get(path):
            r=context.request.get(base+path);assert r.status==200,r.text();return r.json()['data']
        original_read=get('characters/10/level')
        original=original_read['balance']
        preview(85,'等级回调')
        panel.get_by_label('等级变更确认').locator('.level-stats summary').click()
        page.screenshot(path=str(OUT/'level-confirm.png'),full_page=True)
        lost=[]
        def lose_response(route):
            if route.request.method!='POST' or lost:route.continue_();return
            response=route.fetch();assert response.status==200,response.text()
            lost.append(True);route.abort('connectionreset')
        page.route('**/level-operations',lose_response)
        panel.get_by_role('button',name='确认等级提交',exact=True).click()
        expect(panel.get_by_role('button',name='重试等级并取得回执')).to_be_enabled()
        page.reload();open_character();expect(panel.locator('[data-level-current]')).to_have_text('85')
        panel.get_by_role('button',name='重试等级并取得回执').click()
        expect(panel.get_by_role('status',name='等级操作结果')).to_contain_text('等级已调整至 85')
        expect(panel.locator('.level-history > details')).to_have_count(1)
        page.unroute('**/level-operations',lose_response)
        changed=get('characters/10/level');b=changed['balance']
        assert b['experience']==str(changed['definition']['minimumExperience']) and b['experience']!='0'
        for kind in ('Sp','Tp'):
            assert b['points']['total'+kind]-b['points']['remaining'+kind]==original['points']['total'+kind]-original['points']['remaining'+kind]
        assert b['stats']['stat_hp_max']!=original['stats']['stat_hp_max']
        dialog.get_by_role('tab',name='基本资料',exact=True).click()
        expect(dialog).to_contain_text('Lv.85')
        dialog.get_by_role('tab',name='SP / TP',exact=True).click()
        expect(dialog.locator('[data-points-remaining=sp]')).to_have_text(str(b['points']['remainingSp']))
        dialog.get_by_role('tab',name='等级与成长',exact=True).click()
        preview(90,'恢复等级');submit(90)
        restored=get('characters/10/level')['balance']
        # The original full-record fixture writer clamps negative stats. Level
        # recomputation writes the actual PVF values, including negative resistance.
        assert restored['stats']==original_read['definition']['stats']
        assert {k:v for k,v in restored.items() if k!='stats'}=={k:v for k,v in original.items() if k!='stats'}
        panel.get_by_label('目标等级',exact=True).fill('1')
        panel.get_by_label('等级操作原因',exact=True).fill('低于已消耗拒绝')
        panel.get_by_role('button',name='预览等级变更').click()
        expect(panel.get_by_role('alert')).to_contain_text('小于已消耗')
        for invalid in ('0','91'):
            panel.get_by_label('目标等级',exact=True).fill(invalid)
            panel.get_by_role('button',name='预览等级变更').click()
            expect(panel.get_by_role('alert')).to_contain_text('1–90')
        # Another admin adjusts SP after the level preview.
        preview(85,'过时等级预览');cap=get('capabilities');current=get('characters/10/skill-points')
        body={'accountId':current['accountId'],'action':'add','target':'sp','amount':'5','revision':current['revision'],'definitionRevision':current['definitionRevision'],'reason':'并行技能点调整'}
        headers={'X-CSRF-Token':cap['csrfToken']}
        r=context.request.post(base+'characters/10/skill-point-operations/preview',data=body,headers=headers);assert r.status==200,r.text()
        body['planToken']=r.json()['data']['planToken'];headers['Idempotency-Key']='browser-level-points-conflict'
        r=context.request.post(base+'characters/10/skill-point-operations',data=body,headers=headers);assert r.status==200,r.text()
        panel.get_by_role('button',name='确认等级提交').click()
        expect(panel.get_by_role('alert')).to_contain_text('已变更')
        panel.get_by_role('button',name='刷新等级与记录').click()
        expect(panel.locator('[data-level-current]')).to_have_text('90')
        expect(panel.locator('.level-history > details')).to_have_count(2)
        preview(85,'技能点调整后重算');submit(85)
        panel.locator('.level-history > details').first.locator('summary').first.click()
        page.screenshot(path=str(OUT/'level-desktop.png'),full_page=True)
        dialog.get_by_role('button',name='关闭',exact=True).click()
        open_character('晨雾','晨雾·逐光');expect(panel.locator('[data-level-current]')).to_have_text('90')
        expect(panel.locator('.level-history')).to_contain_text('暂无')
        dialog.get_by_role('button',name='关闭',exact=True).click()
        open_character('星河','星河·逐光');expect(panel.get_by_role('alert')).to_contain_text('尚无技能点账本')
        expect(panel.get_by_role('button',name='预览等级变更')).to_be_disabled()
        dialog.get_by_role('button',name='关闭',exact=True).click()
        open_character('流光','流光·剑影');preview(80,'在线角色拒绝')
        panel.get_by_role('button',name='确认等级提交').click();expect(panel.get_by_role('alert')).to_contain_text('账号仍连接游戏')
        dialog.get_by_role('button',name='关闭',exact=True).click()
        page.locator('#theme-open').click();page.locator('[data-action=preset][data-value=ember-dusk]').click()
        page.locator('[data-action=mode][data-value=dark]').click();page.keyboard.press('Escape');open_character()
        expect(panel.locator('[data-level-current]')).to_have_text('85')
        page.screenshot(path=str(OUT/'level-dark.png'),full_page=True)
        page.set_viewport_size({'width':390,'height':844});page.screenshot(path=str(OUT/'level-mobile.png'),full_page=True)
        preview(90,'移动端等级预览');panel.get_by_label('等级变更确认').scroll_into_view_if_needed()
        page.screenshot(path=str(OUT/'level-mobile-confirm.png'),full_page=True)
        panel.get_by_label('等级变更确认').locator('.level-stats summary').click()
        panel.get_by_label('等级变更确认').locator('.level-stats').scroll_into_view_if_needed()
        page.screenshot(path=str(OUT/'level-mobile-stats.png'),full_page=True)
        assert page.evaluate('document.documentElement.scrollWidth<=innerWidth+1')
        page.wait_for_timeout(300);baseline=len(requests);page.wait_for_timeout(1600);assert len(requests)==baseline
        assert not errors,errors
        browser.close()
    (OUT/'browser-result.json').write_text(json.dumps({'checks':['real-gateway','real-PVF-cumulative-XP','downgrade-upgrade','stat-recompute','spent-points-retained','summary-and-SP-tab-consistency','reload-replay','below-spent-reject','invalid-level','SP-edit-conflict','character-isolation','missing-ledger','online-reject','dark-theme','mobile','no-idle-poll'],'errors':errors},ensure_ascii=False,indent=2),encoding='utf-8')
    print('PASS gateway level browser checks')

if __name__=='__main__':main()
