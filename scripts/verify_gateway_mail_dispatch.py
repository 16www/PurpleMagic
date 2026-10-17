"""Verify specified recipients and mail instance options on the isolated gateway."""
import json
from pathlib import Path
from playwright.sync_api import sync_playwright, expect

ROOT=Path(__file__).resolve().parents[1]
STATE=ROOT/'runtime/preview/gateway-live'
OUT=ROOT/'runtime/verification/gateway-mail-dispatch'


def main():
    OUT.mkdir(parents=True,exist_ok=True)
    ready=json.loads((STATE/'ready.json').read_text(encoding='utf-8'))
    errors,requests=[],[]
    with sync_playwright() as p:
        browser=p.chromium.launch(headless=True)
        context=browser.new_context(ignore_https_errors=True,viewport={'width':1440,'height':1100})
        page=context.new_page()
        page.on('pageerror',lambda e:errors.append(str(e)))
        page.on('request',lambda r:requests.append(r.url) if '/api/' in r.url else None)
        page.goto(ready['url'])
        page.get_by_label('导入登录 Key').set_input_files(str(STATE/'login-key.json'))
        expect(page.get_by_label('管理密码')).not_to_have_value('')
        page.get_by_role('button',name='登录',exact=True).click()
        expect(page.get_by_role('button',name='打开晨雾',exact=True)).to_be_visible()
        base=ready['url'].split('/console/')[0]+'/api/v1/'

        def get(path):
            res=context.request.get(base+path)
            assert res.status==200,res.text()
            return res.json()['data']

        candidates=get('items?q=太刀&limit=20')['items']
        weapon=next(v['itemId'] for v in candidates if get('items/'+v['itemId']+'/grant-options')['equipment']['canForge'] and get('items/'+v['itemId']+'/grant-options')['equipment']['canAmplify'])
        initial={c:len(get(f'characters/{c}/mailbox')) for c in (10,11,20)}
        task_count=len(get('tasks'))
        page.evaluate("location.hash='mail'")
        form=page.locator('#op-compose')
        form.get_by_role('button',name='指定名单',exact=True).click()
        for role in ('10','11'):
            form.get_by_label('账号 ID',exact=True).fill('preview:1')
            form.get_by_label('角色 ID',exact=True).fill(role)
            form.get_by_role('button',name='加入名单',exact=True).click()
        expect(form.locator('.mail-targets li')).to_have_count(2)
        form.locator('[data-op=recipient-remove][data-id="11"]').click()
        expect(form.locator('.mail-targets li')).to_have_count(1)
        # Add a role through the existing character flow, preserving the draft.
        form.get_by_role('link',name='从账号列表添加',exact=True).click()
        page.get_by_role('button',name='打开晨雾',exact=True).click()
        page.get_by_role('button',name='查看晨雾·逐光',exact=True).click()
        page.locator('#dialog').get_by_role('button',name='发送邮件',exact=True).click()
        expect(form.locator('.mail-targets li')).to_have_count(2)
        form.get_by_label('物品 ID',exact=True).fill(weapon)
        form.get_by_label('物品 ID',exact=True).blur()
        form.get_by_label('物品数量',exact=True).fill('1')
        with page.expect_response('**/api/v1/items/*/grant-options') as capability:
            form.get_by_role('button',name='读取发放参数',exact=True).click()
        assert capability.value.status==200,capability.value.text()
        page.wait_for_timeout(300)
        if form.get_by_label('装备品级',exact=True).count()==0:
            page.screenshot(path=str(OUT/'dispatch-failure.png'),full_page=True)
            raise AssertionError({'capability':capability.value.json(),'form':form.inner_text(),'errors':errors})
        form.get_by_label('装备品级',exact=True).select_option('random')
        form.get_by_label('强化 / 增幅等级',exact=True).fill('12')
        form.get_by_label('增幅属性',exact=True).select_option('3')
        form.get_by_label('锻造等级',exact=True).fill('7')
        form.get_by_label('有效天数',exact=True).fill('7')
        form.get_by_label('邮件标题',exact=True).fill('装备发放验证')
        form.get_by_label('邮件正文',exact=True).fill('指定名单测试')
        form.get_by_label('金币',exact=True).fill('500')
        assert len(get('tasks'))==task_count,'compose buttons submitted a draft'
        page.screenshot(path=str(OUT/'mail-compose-desktop.png'),full_page=True)
        page.locator('#theme-open').click()
        page.locator('[data-action=preset][data-value=ember-dusk]').click()
        page.locator('[data-action=mode][data-value=dark]').click()
        page.keyboard.press('Escape')
        page.set_viewport_size({'width':390,'height':844})
        form.get_by_label('装备品级',exact=True).scroll_into_view_if_needed()
        page.wait_for_timeout(250)
        page.screenshot(path=str(OUT/'mail-compose-mobile.png'),full_page=True)
        assert page.evaluate('document.documentElement.scrollWidth<=innerWidth+1')
        lost=[]

        def lose_response(route):
            response=route.fetch()
            assert response.status==200,response.text()
            lost.append(response.json()['data'])
            route.abort()

        page.route('**/api/v1/mail/preview',lose_response)
        form.get_by_role('button',name='预览邮件',exact=True).click()
        expect(page.get_by_role('alert')).to_be_visible()
        assert len(lost)==1
        page.unroute('**/api/v1/mail/preview',lose_response)
        def changed_capability(route):
            response=route.fetch()
            body=response.json()
            body['data']['pvfChecksum']='f'*64
            route.fulfill(response=response,json=body)
        page.route('**/api/v1/items/*/grant-options',changed_capability)
        page.reload()
        expect(form.get_by_label('有效天数',exact=True)).to_have_value('7')
        expect(form.locator('.mail-targets li')).to_have_count(2)
        form.get_by_role('button',name='预览邮件',exact=True).click()
        detail=page.get_by_role('region',name='任务详情',exact=True)
        expect(detail).to_contain_text('2 个 · 待发 2')
        expect(detail).to_contain_text('随机品级（已固定）')
        expect(detail).to_contain_text('锻造 7')
        recovered=get('tasks/'+lost[0]['id'])
        assert recovered==lost[0],'recovery changed preview'
        page.unroute('**/api/v1/items/*/grant-options',changed_capability)
        for c in initial:
            assert len(get(f'characters/{c}/mailbox'))==initial[c],'preview sent mail'
        page.set_viewport_size({'width':1440,'height':1100})
        page.screenshot(path=str(OUT/'mail-task-confirm.png'),full_page=True)
        detail.get_by_role('button',name='确认发放',exact=True).click()
        page.locator('#dialog').get_by_role('button',name='确认并发放',exact=True).click()
        expect(detail).to_contain_text('成功 2 · 失败 0')
        for c in (10,11):
            assert len(get(f'characters/{c}/mailbox'))==initial[c]+1
        assert len(get('characters/20/mailbox'))==initial[20]
        # Existing benefit rules use the same composer but keep their own scope.
        page.evaluate("location.hash='mail'")
        page.get_by_role('button',name='新建福利规则',exact=True).click()
        form.get_by_label('邮件标题',exact=True).fill('福利规则回归')
        form.locator('[name=scope]').select_option('character')
        form.get_by_label('物品 ID',exact=True).fill('490009735')
        form.get_by_label('物品数量',exact=True).fill('1')
        form.get_by_role('button',name='读取发放参数',exact=True).click()
        expect(form.get_by_label('有效天数',exact=True)).to_be_visible()
        form.get_by_role('button',name='保存规则',exact=True).click()
        expect(page.get_by_role('status')).to_contain_text('已保存')
        rule=next(v for v in get('benefits') if v['reward']['title']=='福利规则回归')
        assert rule['scope']=='character' and not rule['enabled']
        page.wait_for_timeout(300)
        before=len(requests)
        page.wait_for_timeout(1600)
        assert len(requests)==before,'idle requests'
        assert not errors,errors
        browser.close()
    (OUT/'browser-result.json').write_text(json.dumps({'checks':['selected-list-add-remove','add-from-character','capabilities','instance-options','desktop-mobile-theme','lost-preview-recovery','changed-pvf-preserves-recovery-key','fixed-quality-expiry','confirmed-delivery','unselected-mailbox-preserved','existing-benefit-composer','idle-zero-requests'],'errors':errors},ensure_ascii=False,indent=2),encoding='utf-8')
    print('PASS gateway mail dispatch browser checks')


if __name__=='__main__':main()
