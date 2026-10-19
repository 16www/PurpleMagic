"""Exercise profession changes through the actual isolated gateway preview."""
import json
from pathlib import Path
from playwright.sync_api import sync_playwright, expect

ROOT=Path(__file__).resolve().parents[1]
STATE=ROOT/'runtime/preview/gateway-live'
OUT=ROOT/'runtime/verification/gateway-profession'

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
        page.get_by_role('button',name='登录',exact=True).click()
        dialog=page.locator('#dialog')
        panel=dialog.get_by_role('region',name='角色职业管理',exact=True)
        def open_character(account='晨雾',name='晨雾·剑影'):
            page.get_by_role('button',name='打开'+account,exact=True).click()
            page.get_by_role('button',name='查看'+name,exact=True).click()
            dialog.get_by_role('tab',name='转职与觉醒',exact=True).click()
            expect(panel.get_by_role('button',name='刷新职业与记录')).to_be_visible()
        base=ready['url'].split('/console/')[0]+'/api/v1/'
        def get(path):
            r=context.request.get(base+path);assert r.status==200,r.text();return r.json()['data']
        def preview(job,grow,reason):
            expect(panel.get_by_label('目标职业',exact=True)).to_be_enabled()
            panel.get_by_label('目标职业',exact=True).select_option(str(job))
            panel.get_by_label('转职与觉醒阶段',exact=True).select_option(str(grow))
            panel.get_by_label('职业操作原因',exact=True).fill(reason)
            panel.get_by_role('button',name='预览职业变更').click()
            expect(panel.get_by_label('职业变更确认',exact=True)).to_be_visible()
        def submit(job,grow):
            panel.get_by_role('button',name='确认职业提交').click()
            expect(panel.get_by_label('职业操作结果')).to_be_visible()
            actual=get('characters/10/profession')['balance'];assert (actual['job'],actual['growType'])==(job,grow)
            return actual
        open_character()
        expect(panel.locator('[data-profession-current]')).to_have_text('鬼剑士')
        initial=get('characters/10/profession')['balance']
        assert (initial['job'],initial['growType'])==(0,0)
        preview(0,1,'转职剑魂')
        expect(panel.get_by_label('职业变更确认')).to_contain_text('重建免费技能')
        first=submit(0,1);assert first['growth']['points']['remainingSp']==first['growth']['points']['totalSp']
        assert first['learnedSkills']>0
        preview(0,17,'第一次觉醒')
        expect(panel.get_by_label('职业变更确认')).to_contain_text('保留已学技能')
        awakened=submit(0,17);assert awakened['growth']['points']==first['growth']['points']
        preview(0,1,'降觉醒并重建技能')
        route='**/api/v1/characters/10/profession-operations'
        def lose_response(r):
            response=r.fetch();assert response.status==200;r.abort()
        page.route(route,lose_response)
        panel.get_by_role('button',name='确认职业提交').click()
        expect(panel.get_by_role('button',name='重试职业并取得回执')).to_be_enabled()
        page.unroute(route,lose_response);page.reload();open_character()
        panel.get_by_role('button',name='重试职业并取得回执').click()
        expect(panel.get_by_label('职业操作结果')).to_be_visible()
        expect(panel.locator('.profession-history > details')).to_have_count(3)
        downgraded=get('characters/10/profession')['balance'];assert downgraded['learnedSkills']==first['learnedSkills']
        preview(2,1,'切换神枪手职业');switched=submit(2,1)
        dialog.get_by_role('tab',name='基本资料',exact=True).click()
        expect(dialog).to_contain_text('职业编号 2')
        dialog.get_by_role('tab',name='转职与觉醒',exact=True).click()
        expect(panel.locator('[data-profession-current]')).to_have_text('漫游枪手')
        preview(0,0,'返回基础职业');submit(0,0)
        preview(0,17,'预览技能与任务影响')
        panel.get_by_label('职业变更确认').get_by_text('技能与任务变化',exact=True).click()
        page.screenshot(path=str(OUT/'profession-desktop.png'),full_page=True)
        panel.get_by_role('button',name='返回修改职业').click()
        requests.clear();page.wait_for_timeout(1800);assert not requests,requests
        dialog.get_by_role('button',name='关闭',exact=True).click()
        open_character('星河','星河·逐光');expect(panel.get_by_role('alert')).to_contain_text('尚无技能点账本')
        expect(panel.get_by_role('button',name='预览职业变更')).to_be_disabled()
        dialog.get_by_role('button',name='关闭',exact=True).click()
        open_character('流光','流光·剑影');preview(0,1,'在线拒绝')
        panel.get_by_role('button',name='确认职业提交').click();expect(panel.get_by_role('alert')).to_contain_text('账号仍连接游戏')
        dialog.get_by_role('button',name='关闭',exact=True).click()
        page.locator('#theme-open').click();page.locator('[data-action=preset][data-value=ember-dusk]').click()
        page.locator('[data-action=mode][data-value=dark]').click();page.keyboard.press('Escape')
        open_character();preview(0,33,'手机二觉预览')
        page.set_viewport_size({'width':390,'height':844})
        panel.get_by_label('职业变更确认').scroll_into_view_if_needed()
        page.screenshot(path=str(OUT/'profession-mobile.png'),full_page=True)
        assert page.evaluate('document.documentElement.scrollWidth<=innerWidth')
        assert not errors,errors
        result={'checks':['real-gateway','real-PVF-profession-index','class-change','awakening-preserves','downgrade-refunds','job-change','summary-consistency','lost-response-reload','history','missing-ledger','online-reject','no-idle-poll','dark-theme','mobile'],'errors':errors}
        (OUT/'browser-result.json').write_text(json.dumps(result,ensure_ascii=False,indent=2),encoding='utf-8')
        browser.close()
    print('PASS profession web operations, recovery and responsive layouts')

if __name__=='__main__':main()
