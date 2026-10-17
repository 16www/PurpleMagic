"""Verify benefit options and durable per-recipient delivery on the local fixture."""
import json
from datetime import datetime, timedelta
from pathlib import Path
from playwright.sync_api import sync_playwright, expect

ROOT=Path(__file__).resolve().parents[1]
STATE=ROOT/'runtime/preview/gateway-live'
OUT=ROOT/'runtime/verification/gateway-benefit-options'


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
        page.get_by_role('button',name='登录',exact=True).click()
        expect(page.get_by_role('button',name='打开晨雾',exact=True)).to_be_visible()
        base=ready['url'].split('/console/')[0]+'/api/v1/'

        def get(path):
            res=context.request.get(base+path)
            assert res.status==200,res.text()
            return res.json()['data']

        items=get('items?q=太刀&limit=20')['items']
        item=next(v['itemId'] for v in items if get('items/'+v['itemId']+'/grant-options')['equipment']['canForge'] and get('items/'+v['itemId']+'/grant-options')['equipment']['canAmplify'])
        page.evaluate("location.hash='mail'")
        page.get_by_role('button',name='新建福利规则',exact=True).click()
        form=page.locator('#op-compose')
        form.get_by_label('邮件标题',exact=True).fill('七日装备福利')
        form.locator('[name=scope]').select_option('character')
        form.get_by_label('物品 ID',exact=True).fill(item)
        form.get_by_label('物品数量',exact=True).fill('1')
        form.get_by_role('button',name='读取发放参数',exact=True).click()
        expect(form.get_by_label('有效天数',exact=True)).to_be_visible()
        expect(form.get_by_label('物品数量',exact=True)).to_have_value('1')
        form.get_by_label('有效天数',exact=True).fill('7')
        form.get_by_label('装备品级',exact=True).select_option('random')
        form.get_by_label('强化 / 增幅等级',exact=True).fill('12')
        form.get_by_label('增幅属性',exact=True).select_option('3')
        form.get_by_label('锻造等级',exact=True).fill('7')
        expect(form).to_contain_text('从实际投递时开始计算')
        page.screenshot(path=str(OUT/'benefit-options-desktop.png'),full_page=True)
        lost=[]

        def drop(route):
            res=route.fetch()
            assert res.status==200,res.text()
            lost.append(res.json()['data'])
            route.abort()

        page.route('**/api/v1/benefits',drop)
        form.get_by_role('button',name='保存规则',exact=True).click()
        expect(page.get_by_role('alert')).to_be_visible()
        assert len(lost)==1
        page.unroute('**/api/v1/benefits',drop)
        page.reload()
        page.get_by_role('button',name='新建福利规则',exact=True).click()
        expect(form.get_by_label('有效天数',exact=True)).to_have_value('7')
        form.get_by_role('button',name='保存规则',exact=True).click()
        expect(page.get_by_role('status')).to_contain_text('已保存')
        rule=get('benefits/'+lost[0]['id'])
        assert rule==lost[0],'recovery changed the rule'
        assert rule['reward']['attachments'][0]['expiresAt'] is None
        row=page.get_by_role('row').filter(has_text='七日装备福利')
        expect(row).to_have_count(1)
        expect(row).to_contain_text('从投递时起')
        row.get_by_role('button',name='启用',exact=True).click()
        page.locator('#dialog').get_by_role('button',name='确认启用',exact=True).click()
        expect(row).to_contain_text('已启用')
        row.get_by_role('button',name='预览名单',exact=True).click()
        detail=page.get_by_role('region',name='任务详情',exact=True)
        expect(detail).to_contain_text('待发 8')
        expect(detail).to_contain_text('每位收件人投递时随机品级')
        detail.get_by_role('button',name='确认发放',exact=True).click()
        page.locator('#dialog').get_by_role('button',name='确认并发放',exact=True).click()
        expect(detail).to_contain_text('成功 8 · 失败 0')
        job=next(v for v in get('tasks') if v.get('campaignId')==rule['id'])
        recipients=get('tasks/'+job['id']+'/recipients')
        seeds=set()
        for r in recipients:
            delivery=r['delivery']
            a=delivery['reward']['attachments'][0]
            assert datetime.fromisoformat(a['expiresAt'].replace('Z','+00:00'))-datetime.fromisoformat(delivery['grantedAt'].replace('Z','+00:00'))==timedelta(days=7)
            assert a['options']['reinforce']=='12' and a['options']['forgingLevel']=='7'
            seeds.add(a['options']['qualitySeed'])
        assert len(seeds)>1,'random quality was fixed for the whole campaign'
        page.locator('#theme-open').click()
        page.locator('[data-action=preset][data-value=ember-dusk]').click()
        page.locator('[data-action=mode][data-value=dark]').click()
        page.keyboard.press('Escape')
        page.set_viewport_size({'width':390,'height':844})
        page.evaluate("document.activeElement?.blur();window.scrollTo(0,0)")
        expect(detail.locator('.op-recipient-cards')).to_be_visible()
        page.screenshot(path=str(OUT/'benefit-receipts-mobile.png'),full_page=True)
        assert page.evaluate('document.documentElement.scrollWidth<=innerWidth+1')
        page.evaluate("location.hash='mail'")
        page.get_by_role('row').filter(has_text='七日装备福利').get_by_role('button',name='预览名单',exact=True).click()
        expect(detail).to_contain_text('待发 0 · 成功 8 · 失败 0')
        expect(detail).to_contain_text('投递于')
        page.wait_for_timeout(300)
        count=len(requests)
        page.wait_for_timeout(1600)
        assert count==len(requests) and not errors,errors
        browser.close()
    (OUT/'browser-result.json').write_text(json.dumps({'checks':['rule-options','lost-create-recovery','per-recipient-random-quality','expiry-from-delivery','durable-instance-receipts','repeated-preview-no-redelivery','mobile-theme','idle-zero-requests'],'errors':errors},ensure_ascii=False,indent=2),encoding='utf-8')
    print('PASS benefit options browser checks')


if __name__=='__main__':main()
