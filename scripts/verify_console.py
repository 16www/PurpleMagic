"""Browser acceptance of the isolated console prototype and adapted Theme Studio."""
import json
import os
from pathlib import Path
import threading
from playwright.sync_api import sync_playwright, expect
from preview_console import create_server

ROOT=Path(__file__).resolve().parents[1]
OUT=ROOT/'runtime/verification/console-phase3'
OUT.mkdir(parents=True,exist_ok=True)

def main():
    errors=[]; requests=[]; checks=[]
    contrast=(ROOT/'scripts/fixtures/console_contrast.js').read_text(encoding='utf-8')
    server=create_server()
    thread=threading.Thread(target=server.serve_forever,daemon=True);thread.start()
    origin=f'http://127.0.0.1:{server.server_port}'
    try:
        with sync_playwright() as p:
            browser=p.chromium.launch(headless=True,**({'channel':os.environ['BROWSER_CHANNEL']} if os.environ.get('BROWSER_CHANNEL') else {}))
            context=browser.new_context(viewport={'width':1440,'height':1040},color_scheme='light',locale='zh-CN')
            page=context.new_page()
            page.on('pageerror',lambda error:errors.append(str(error)))
            page.on('request',lambda request:requests.append(request.url))
            page.on('response',lambda response:errors.append(f'HTTP {response.status}: {response.url}') if response.status>=400 else None)
            page.goto(origin);expect(page.get_by_role('heading',name='工作台概览')).to_be_visible()
            page.screenshot(path=str(OUT/'overview-light.png'),full_page=True)
            # Real controls: explicit search, paging, selected account/character consistency.
            page.locator('nav a[href="#players"]').click()
            page.get_by_role('button',name='下一页',exact=True).click()
            expect(page.locator('#main')).to_contain_text('雨声备忘录')
            page.get_by_role('button',name='打开雨声备忘录').click()
            expect(page.locator('[aria-label="账号详情"]')).to_contain_text('听雨')
            page.get_by_role('textbox',name='查询账号').fill('月光')
            expect(page.locator('[aria-label="账号详情"]')).to_contain_text('听雨')
            page.get_by_role('button',name='查询',exact=True).click()
            expect(page.locator('tbody tr')).to_have_count(1)
            page.get_by_role('button',name='打开月光航线').click()
            expect(page.locator('[aria-label="账号详情"]')).to_contain_text('月下航行')
            expect(page.locator('[aria-label="账号详情"]')).not_to_contain_text('听雨')
            page.get_by_role('button',name='查看',exact=True).click()
            expect(page.get_by_role('dialog')).to_contain_text('c201')
            page.get_by_role('button',name='为此角色写邮件').click()
            expect(page.locator('select[name=recipient]')).to_have_value('c201')
            page.get_by_role('button',name='预览邮件',exact=True).click()
            expect(page.get_by_role('alert')).to_contain_text('至少一项附件')
            checks.append('account paging, explicit query, selection, character context, empty mail validation')
            page.locator('nav a[href="#items"]').click()
            page.get_by_role('textbox',name='查询物品').fill('490009735')
            page.get_by_role('button',name='查询',exact=True).click()
            expect(page.locator('.item-row')).to_have_count(1)
            page.get_by_role('spinbutton',name='附件数量').fill('3')
            page.get_by_role('button',name='加入邮件草稿').click()
            page.locator('nav a[href="#mail"]').click()
            page.get_by_role('textbox',name='邮件标题').fill('<b>预览礼物</b>')
            page.get_by_role('textbox',name='邮件正文').fill('旅途愉快。')
            page.get_by_role('button',name='预览邮件',exact=True).click()
            expect(page.get_by_role('dialog')).to_contain_text('天空自选礼盒 × 3')
            expect(page.get_by_role('dialog')).to_contain_text('<b>预览礼物</b>')
            assert page.locator('#dialog b').count()==0
            page.get_by_role('button',name='返回编辑').click()
            page.locator('nav a[href="#tasks"]').click()
            expect(page.locator('tbody tr')).to_have_count(3)
            page.locator('nav a[href="#mail"]').click()
            expect(page.get_by_role('textbox',name='邮件正文')).to_have_value('旅途愉快。')
            page.get_by_role('button',name='预览邮件',exact=True).click()
            page.get_by_role('button',name='确认保存草稿').click()
            expect(page.locator('tbody tr')).to_have_count(4)
            expect(page.locator('tbody tr').first).to_contain_text('草稿')
            page.get_by_role('button',name='草稿',exact=True).click()
            expect(page.locator('tbody tr')).to_have_count(2)
            checks.append('item search, quantity, mail draft preserved, escaped preview, cancel, confirm creates draft only, task filtering')
            page.locator('nav a[href="#gateway"]').click()
            address=page.get_by_role('textbox',name='更新清单地址')
            for invalid in ['http://updates.example.com/manifest.json','https://u:p@updates.example.com/manifest.json','https://updates.example.com/manifest.json?token=x']:
                address.fill(invalid);page.get_by_role('button',name='保存配置草稿').click()
                expect(page.locator('#source-error')).to_be_visible()
            address.fill('https://updates.example.com/preview/manifest.json')
            page.get_by_label('发布渠道').select_option('preview')
            page.get_by_role('button',name='保存配置草稿').click();page.reload()
            expect(page.get_by_role('textbox',name='更新清单地址')).to_have_value('https://updates.example.com/preview/manifest.json')
            expect(page.get_by_label('发布渠道')).to_have_value('preview')
            page.get_by_role('button',name='恢复默认').click()
            checks.append('HTTPS source draft validation, credentials/query rejection, local persistence and reset')
            page.locator('nav a[href="#overview"]').click()
            page.locator('#theme-open').click()
            expect(page.get_by_role('dialog',name='主题工作室')).to_be_visible()
            assert page.locator('#app-shell').evaluate('(node)=>node.inert')
            # Theme Studio is actual adapted source, including shadow DOM focus trap.
            page.locator('.ts-close').focus();page.keyboard.press('Shift+Tab')
            assert page.locator('.ts-reset').evaluate('(node)=>node.getRootNode().activeElement===node')
            page.keyboard.press('Tab')
            assert page.locator('.ts-close').evaluate('(node)=>node.getRootNode().activeElement===node')
            page.locator('[data-action=preset][data-value=jade-circuit]').click()
            page.locator('[data-action=mode][data-value=dark]').click()
            page.screenshot(path=str(OUT/'theme-studio-dark.png'),full_page=True)
            page.locator('[data-action=radius][data-value=lg]').click()
            page.locator('[data-action=density][data-value=compact]').click()
            page.locator('[data-action=font][data-value=jetbrains-mono]').click()
            page.locator('[data-action=contentLayout][data-value=centered]').click()
            page.locator('[data-action=effects][data-value=reduced]').click()
            page.keyboard.press('Escape')
            expect(page.locator('html')).to_have_attribute('data-theme','dark')
            assert not page.locator('#app-shell').evaluate('(node)=>node.inert')
            expect(page.locator('#theme-open')).to_be_focused()
            page.reload()
            saved=page.evaluate('window.__SOFT_THEME_STUDIO__.getState()')
            assert saved['preset']=='jade-circuit' and saved['density']=='compact' and saved['radius']=='lg' and saved['effects']=='reduced' and saved['font']=='jetbrains-mono' and saved['contentLayout']=='centered',saved
            page.screenshot(path=str(OUT/'overview-jade-dark.png'),full_page=True)
            page.locator('nav a[href="#players"]').click()
            expect(page.get_by_role('heading',name='账号与角色',exact=True)).to_be_visible()
            page.locator('#theme-open').click()
            for mode in ['white','dark']:
                page.locator(f'[data-action=mode][data-value={mode}]').click()
                for preset in ['cpamp','ember-dusk','jade-circuit','coral-mist','glacier-signal','solar-flare','moss-stone','deep-current','amethyst-fog','graphite']:
                    page.locator(f'[data-action=preset][data-value={preset}]').click()
                    expect(page.locator('html')).to_have_attribute('data-cts-preset',preset)
                    page.wait_for_timeout(180)  # Let the 140ms foreground/background transition settle.
                    assert not page.evaluate(contrast),(mode,preset,page.evaluate(contrast))
                    if mode=='dark' and preset=='ember-dusk':
                        # Warm surfaces must follow the selected palette, including table headers.
                        colors=page.evaluate("""() => ['thead th','.character-card .item-icon'].map(selector=>{const ctx=document.createElement('canvas').getContext('2d');ctx.fillStyle=getComputedStyle(document.querySelector(selector)).backgroundColor;ctx.fillRect(0,0,1,1);return [...ctx.getImageData(0,0,1,1).data]})""")
                        assert all(r>b+5 for r,g,b,a in colors),('warm palette has blue surfaces',colors)
                for custom in ['#ffffff','#000000','#00ff00','#ffff00','#bd5738']:
                    page.locator('input[data-action=custom]').fill(custom)
                    expect(page.locator('html')).to_have_attribute('data-cts-preset','custom')
                    assert page.evaluate('window.__SOFT_THEME_STUDIO__.getState().customAccent')==custom
                    page.wait_for_timeout(180)
                    assert not page.evaluate(contrast),(mode,custom,page.evaluate(contrast))
            page.locator('[data-action=reset]').click()
            page.keyboard.press('Escape')
            expect(page.locator('html')).to_have_attribute('data-theme','white')
            expect(page.locator('html')).to_have_attribute('data-cts-preset','cpamp')
            page.locator('nav a[href="#overview"]').click()
            context.clear_cookies()
            page.emulate_media(color_scheme='dark')
            expect(page.locator('html')).to_have_attribute('data-theme','dark')
            page.screenshot(path=str(OUT/'overview-dark.png'),full_page=True)
            page.emulate_media(color_scheme='light',reduced_motion='reduce')
            checks.append('10 palettes in light/dark, custom accents, all theme preferences persisted, reset, system theme, modal focus trap/return and inert background')
            checks.append('sampled theme controls, headings, table cells, badges and navigation text contrast >= 4.5:1 for all palettes and extreme custom accents; warm table and item surfaces follow the palette')
            page.locator('nav a[href="#players"]').click()
            page.screenshot(path=str(OUT/'players-light.png'),full_page=True)
            page.locator('nav a[href="#items"]').click()
            page.screenshot(path=str(OUT/'items-light.png'),full_page=True)
            for width in [375,768,1440,1920]:
                page.set_viewport_size({'width':width,'height':960})
                for route in ['overview','players','items','mail','tasks','gateway','settings']:
                    page.goto(origin+'/#'+route)
                    expect(page.locator('#main h1')).to_be_visible()
                    assert page.evaluate('document.documentElement.scrollWidth <= innerWidth'),(width,route,page.evaluate('Array.from(document.querySelectorAll("#main *")).filter(n=>n.getBoundingClientRect().right>innerWidth).slice(0,12).map(n=>[n.tagName,n.className,n.getBoundingClientRect().right])'))
                if width==375:
                    assert page.locator('#sidebar').evaluate('(node)=>node.inert')
                    page.locator('#nav-toggle').click();expect(page.locator('.sidebar')).to_be_in_viewport()
                    page.locator('nav a[href="#settings"]').focus();page.keyboard.press('Tab')
                    expect(page.locator('.brand')).to_be_focused()
                    page.locator('nav a[href="#overview"]').click()
                    expect(page.locator('#nav-toggle')).to_have_attribute('aria-expanded','false')
                    page.screenshot(path=str(OUT/'overview-mobile.png'),full_page=True)
                    page.locator('#theme-open').click()
                    page.screenshot(path=str(OUT/'theme-studio-mobile.png'),full_page=True)
                    page.locator('[data-action=reset]').click();page.keyboard.press('Escape')
            checks.append('7 routes at 375/768/1440/1920 px, no document overflow, mobile navigation and Theme Studio')
            page.clock.install();before=len(requests);page.clock.fast_forward(60000)
            assert len(requests)==before,'idle page requested resources'
            assert all(url.startswith(origin+'/') for url in requests),requests
            assert not any('/api/' in url or 'restart' in url for url in requests)
            keys=page.evaluate('Object.keys(localStorage)')
            assert all(key.startswith('soft-gm-console.') for key in keys),keys
            assert not errors,errors
            checks.append('60 virtual idle seconds: no requests, no remote/API/restart traffic, namespaced storage, no browser errors')
            browser.close()
        result={'status':'passed','checks':checks,'requestCount':len(requests),'errors':errors,'screenshots':sorted(p.name for p in OUT.glob('*.png')),'data':'synthetic only','liveBackend':False}
        (OUT/'result.json').write_text(json.dumps(result,ensure_ascii=False,indent=2),encoding='utf-8')
        print(json.dumps(result,ensure_ascii=False))
    finally:
        server.shutdown();server.server_close();thread.join(timeout=5)

if __name__=='__main__':main()
