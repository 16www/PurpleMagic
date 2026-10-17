"""Verify actual indexed PVF details through the shipped console and local preview."""
import argparse
import json
import threading
from pathlib import Path
from urllib.parse import urlsplit
from playwright.sync_api import sync_playwright, expect
from preview_console import create_server


def run(args):
    assert urlsplit(args.url).hostname=='127.0.0.1'
    args.output.mkdir(parents=True,exist_ok=True)
    errors=[];calls=[]
    preview=create_server(item_details=args.preview_fixture)
    thread=threading.Thread(target=preview.serve_forever,daemon=True);thread.start()
    try:
        with sync_playwright() as p:
            browser=p.chromium.launch(headless=True)
            context=browser.new_context(viewport={'width':1600,'height':1000},locale='zh-CN')
            page=context.new_page()
            page.on('pageerror',lambda e:errors.append(str(e)))
            page.on('request',lambda r:calls.append((r.method,urlsplit(r.url).path)) if '/api/' in r.url else None)
            page.goto(args.url+'/console/#items')
            page.get_by_label('管理密码',exact=True).fill('isolated-item-browser-password')
            page.get_by_role('button',name='登录',exact=True).click()
            expect(page.get_by_role('textbox',name='查询物品')).to_be_visible()
            def lookup(id,name):
                page.get_by_role('textbox',name='查询物品').fill(str(id))
                page.get_by_role('button',name='查询',exact=True).click()
                page.get_by_role('button',name='查看'+name+'详情').click()
                expect(page.locator('[aria-label="物品详情"] h2')).to_have_text(name)
            lookup(101030517,'荒古遗尘巨剑')
            detail=page.locator('[aria-label="物品详情"]')
            expect(detail).to_contain_text('伤害最高可达300万')
            expect(detail).to_contain_text('第七使徒')
            expect(detail).to_contain_text('鬼剑士')
            expect(detail).to_contain_text('守护者')
            page.screenshot(path=str(args.output/'weapon-light.png'),full_page=True)
            detail.locator('summary').filter(has_text='触发条件与技能规则').click()
            expect(detail.locator('.pvf-children').first).to_be_visible()
            lookup(100050050,'幽魂魅影长袍')
            expect(detail).to_contain_text('所有职业Lv70的技能Lv +2')
            expect(detail).to_contain_text('幽魂魅影')
            expect(detail).to_contain_text('件套')
            page.locator('#theme-open').click();page.locator('[data-action=preset][data-value=ember-dusk]').click();page.locator('[data-action=mode][data-value=dark]').click();page.keyboard.press('Escape');page.wait_for_timeout(250)
            page.screenshot(path=str(args.output/'set-ember.png'),full_page=True)
            page.set_viewport_size({'width':390,'height':844});page.screenshot(path=str(args.output/'set-mobile.png'),full_page=True)
            assert page.evaluate('document.documentElement.scrollWidth<=innerWidth+1')
            page.set_viewport_size({'width':1600,'height':1000})
            lookup(490002780,'魔界抗疲劳秘药 (70点)')
            expect(detail).to_contain_text('70')
            detail.locator('summary').filter(has_text='使用条件').click()
            expect(detail).to_contain_text('有效天数')
            page.screenshot(path=str(args.output/'consumable.png'),full_page=True)
            before=len(calls);page.clock.install();page.clock.run_for(60000);page.wait_for_timeout(100);assert len(calls)==before
            # Leaving the directory clears the selected item.
            page.locator('nav a[href="#players"]').click()
            expect(page.locator('[aria-label="物品详情"]')).to_have_count(0)
            demo=context.new_page();demo.on('pageerror',lambda e:errors.append(str(e)))
            demo.on('console',lambda e:errors.append(e.text) if e.type=='error' else None)
            demo.goto(f'http://127.0.0.1:{preview.server_port}/?pvf=1#items')
            expect(demo.locator('.item-row')).to_have_count(8)
            expect(demo.locator('[data-item="101030517"]')).to_be_visible()
            demo.locator('[data-item="101030517"]').click()
            expect(demo.locator('[aria-label="物品详情"]')).to_contain_text('伤害最高可达300万')
            demo.screenshot(path=str(args.output/'preview-real-items.png'),full_page=True)
            assert not errors,errors
            browser.close()
        (args.output/'result.json').write_text(json.dumps({'passed':True,'checks':['real PVF indexed detail API','equipment effects and job list','set bonuses','consumable conditions','nested fields','warm theme','390px layout','idle zero polling','real-data preview'],'errors':errors,'calls':calls},ensure_ascii=False,indent=2),encoding='utf-8')
        print('PASS real indexed PVF -> console details -> theme/mobile/preview')
    finally:
        preview.shutdown();preview.server_close()


if __name__=='__main__':
    p=argparse.ArgumentParser(description=__doc__);p.add_argument('--url',required=True);p.add_argument('--output',type=Path,required=True);p.add_argument('--preview-fixture',type=Path,required=True);run(p.parse_args())
