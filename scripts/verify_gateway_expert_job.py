"""Verify auxiliary profession management in the isolated production gateway."""
import json
from pathlib import Path
from playwright.sync_api import sync_playwright, expect

ROOT=Path(__file__).resolve().parents[1]
STATE=ROOT/'runtime/preview/gateway-live'
OUT=ROOT/'runtime/verification/gateway-expert-job'

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
        panel=dialog.get_by_role('region',name='角色副职业管理',exact=True)
        def open_character(account='晨雾',name='晨雾·剑影'):
            page.get_by_role('button',name='打开'+account,exact=True).click()
            page.get_by_role('button',name='查看'+name,exact=True).click()
            dialog.get_by_role('tab',name='副职业',exact=True).click()
            expect(panel.get_by_role('button',name='刷新副职业与记录')).to_be_visible()
        base=ready['url'].split('/console/')[0]+'/api/v1/'
        def get(path):
            r=context.request.get(base+path)
            assert r.status==200,r.text()
            return r.json()['data']
        def preview(typ,mode,value,reason):
            expect(panel.get_by_label('目标副职业',exact=True)).to_be_enabled()
            panel.get_by_label('目标副职业',exact=True).select_option(str(typ))
            if typ:
                panel.get_by_label('设置方式',exact=True).select_option(mode)
                panel.get_by_label('目标副职业等级' if mode=='level' else '目标副职业经验',exact=True).fill(str(value))
            panel.get_by_label('副职业操作原因').fill(reason)
            panel.get_by_role('button',name='预览副职业变更').click()
            expect(panel.get_by_label('副职业变更确认',exact=True)).to_be_visible()
        def submit(typ,experience):
            panel.get_by_role('button',name='确认副职业提交').click()
            expect(panel.get_by_label('副职业操作结果')).to_be_visible()
            actual=get('characters/10/expert-job')['balance']
            assert (actual['type'],actual['experience'])==(typ,experience),actual
            return actual
        open_character()
        expect(panel.locator('[data-expert-job-current]')).to_have_text('无副职业')
        initial=get('characters/10/expert-job')['balance']
        assert initial['type']==0
        preview(3,'level',11,'调整为满级分解师')
        expect(panel.get_by_label('副职业变更确认')).to_contain_text('清除旧副职业')
        first=submit(3,1067)
        assert first['level']==11
        preview(3,'experience',133,'精确设置熟练经验')
        expect(panel.get_by_label('副职业变更确认')).to_contain_text('保留已学配方与设备状态')
        after=submit(3,133)
        assert after['level']==3
        preview(2,'level',5,'切换炼金术师')
        route='**/api/v1/characters/10/expert-job-operations'
        def lose_response(r):
            response=r.fetch();assert response.status==200;r.abort()
        page.route(route,lose_response)
        panel.get_by_role('button',name='确认副职业提交').click()
        expect(panel.get_by_role('button',name='重试副职业并取得回执')).to_be_enabled()
        page.unroute(route,lose_response);page.reload();open_character()
        panel.get_by_role('button',name='重试副职业并取得回执').click()
        expect(panel.get_by_label('副职业操作结果')).to_be_visible()
        expect(panel.locator('.expert-job-history > details')).to_have_count(3)
        assert get('characters/10/expert-job')['balance']['type']==2
        preview(1,'level',7,'附魔师预览')
        page.screenshot(path=str(OUT/'expert-job-desktop.png'),full_page=True)
        submit(1,585)
        preview(4,'level',1,'控偶师');submit(4,0)
        preview(0,'experience',0,'取消副职业');submit(0,0)
        records=get('characters/10/expert-job-operations?limit=20')
        assert len(records)==6
        quests=get('characters/10/quests?limit=100')
        assert not any(q['questId'] in (2702,2708,2710,2712) for q in quests)
        requests.clear();page.wait_for_timeout(1800);assert not requests,requests
        dialog.get_by_role('button',name='关闭',exact=True).click()
        open_character('流光','流光·剑影');preview(3,'level',1,'在线拒绝')
        panel.get_by_role('button',name='确认副职业提交').click()
        expect(panel.get_by_role('alert')).to_contain_text('角色在线')
        dialog.get_by_role('button',name='关闭',exact=True).click()
        page.locator('#theme-open').click();page.locator('[data-action=preset][data-value=ember-dusk]').click()
        page.locator('[data-action=mode][data-value=dark]').click();page.keyboard.press('Escape')
        open_character();preview(1,'level',3,'手机副职业预览')
        page.set_viewport_size({'width':390,'height':844})
        panel.get_by_label('副职业变更确认').scroll_into_view_if_needed()
        page.screenshot(path=str(OUT/'expert-job-mobile.png'),full_page=True)
        assert page.evaluate('document.documentElement.scrollWidth<=innerWidth')
        assert not errors,errors
        (OUT/'browser-result.json').write_text(json.dumps({'checks':['four-types','level-from-PVF','experience','switch-and-clear','quest-cleanup','lost-response-reload','history','online-reject','no-polling','dark-theme','mobile'],'errors':errors},ensure_ascii=False,indent=2),encoding='utf-8')
        browser.close()
    print('PASS expert job operations, recovery and responsive layouts')

if __name__=='__main__':main()
