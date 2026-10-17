"""Isolated browser contract tests for the embedded, authenticated console."""
import json
import mimetypes
from pathlib import Path
from urllib.parse import urlsplit, parse_qs, unquote
from playwright.sync_api import sync_playwright, expect

ROOT=Path(__file__).resolve().parents[1]
WEB=ROOT/'gm/internal/services/gmtool/wwwroot'
OUT=ROOT/'runtime/verification/console-live'
OUT.mkdir(parents=True,exist_ok=True)
calls=[];errors=[];unexpected=[];checks=[]
state={'authenticated':False,'hold':None,'held':None,'fail':False,'empty':False}
now='2026-10-15T10:00:00Z'

def respond(route,data,status=200,page=None):
    body={'requestId':'fixture-request','observedAt':now}
    body['data' if status==200 else 'error']=data
    if page is not None:body['page']=page
    route.fulfill(status=status,content_type='application/json',body=json.dumps(body,ensure_ascii=False))

def account(id):
    return {'accountId':f'dnf:{id}','panelId':id,'name':f'玩家{id}','disabled':False,'characterCount':1,'lastLoginAt':None,'createdAt':now}

def character(id):
    return {'characterId':f'c{id}','accountId':f'dnf:{id}','name':f'角色{id}<b>','job':'0','level':90,'createdAt':now}

def route(r):
    u=urlsplit(r.request.url)
    if u.netloc!='console.test':unexpected.append(r.request.url);r.abort();return
    # Match Go net/http's decoded URL.Path; the browser encodes account colons.
    u=u._replace(path=unquote(u.path))
    if not u.path.startswith('/api/'):
        path=u.path.removeprefix('/gm')
        path=path+'index.html' if path.endswith('/') else path
        file=(WEB/path.lstrip('/')).resolve()
        if not file.is_relative_to(WEB.resolve()) or not file.is_file():unexpected.append(path);r.fulfill(status=404);return
        r.fulfill(content_type=mimetypes.guess_type(file.name)[0] or 'application/octet-stream',body=file.read_bytes());return
    calls.append({'method':r.request.method,'path':u.path,'query':u.query})
    if u.path=='/api/auth/login':
        assert r.request.method=='POST'
        state['authenticated']=r.request.post_data_json['password']=='fixture'
        r.fulfill(content_type='application/json',body=json.dumps({'success':state['authenticated'],'error':None if state['authenticated'] else '密码错误。'}));return
    if u.path=='/api/auth/logout':
        state['authenticated']=False;r.fulfill(content_type='application/json',body='{"success":true}');return
    if r.request.method!='GET':unexpected.append(calls[-1]);r.abort();return
    if not state['authenticated']:respond(r,{'code':'authentication_required','message':'请登录'},401);return
    if state['hold']==u.path:state['held']=r;return
    if state['fail']:respond(r,{'code':'dependency_unavailable','message':'数据库暂不可用'},503);return
    q=parse_qs(u.query)
    if u.path=='/api/v1/capabilities':respond(r,{'features':{'accounts.read':True,'characters.read':True,'items.read':True}})
    elif u.path in ['/api/v1/benefits','/api/v1/tasks']:respond(r,[],page={'limit':20})
    elif u.path=='/api/v1/accounts':
        ids=['9007199254740993'] if q.get('cursor') else ['1','2']
        rows=[] if state['empty'] else [account(i) for i in ids]
        respond(r,rows,page={'limit':20,'nextCursor':'' if q.get('cursor') else 'page2'})
    elif u.path.endswith('/characters') and u.path.startswith('/api/v1/accounts/'):
        respond(r,[character(u.path.split('/')[-2].removeprefix('dnf:'))])
    elif u.path=='/api/v1/characters':respond(r,[character('2')],page={'limit':20})
    elif u.path.startswith('/api/v1/characters/'):
        respond(r,{'character':character(u.path.rsplit('/',1)[1].removeprefix('c')),'online':'unknown'})
    elif u.path=='/api/v1/items':respond(r,{'items':[{'itemId':'490009735','name':'天空礼盒','kind':'stackable','minLevel':0,'category':'盒子','invalid':False}],'total':1},page={'limit':20})
    else:unexpected.append(calls[-1]);r.fulfill(status=404)

def run():
    with sync_playwright() as p:
        browser=p.chromium.launch(headless=True)
        page=browser.new_page(viewport={'width':1440,'height':1000},locale='zh-CN')
        page.route('**/*',route);page.on('pageerror',lambda e:errors.append(str(e)))
        page.clock.install()
        page.goto('http://console.test/console/')
        expect(page.get_by_role('heading',name='登录管理工作台')).to_be_visible()
        page.get_by_label('管理密码').fill('wrong');page.get_by_role('button',name='登录',exact=True).click()
        expect(page.get_by_role('alert')).to_contain_text('密码错误')
        page.get_by_label('管理密码').fill('fixture');page.get_by_role('button',name='登录',exact=True).click()
        expect(page.get_by_role('heading',name='工作台概览')).to_be_visible()
        assert not page.evaluate('Object.values(localStorage).some(v=>v.includes("fixture"))')
        checks.append('authentication required, failed login, valid login, password not persisted')
        page.locator('nav a[href="#players"]').click()
        expect(page.get_by_role('button',name='打开玩家1',exact=True)).to_be_visible()
        before=len(calls);page.get_by_label('查询账号或角色').fill('探险')
        page.clock.run_for(60000);page.wait_for_timeout(100)
        assert len(calls)==before, calls[before:]
        page.get_by_role('button',name='查询',exact=True).click()
        expect(page.get_by_role('button',name='打开玩家1',exact=True)).to_be_visible()
        assert parse_qs(calls[-1]['query'])['q']==['探险']
        page.get_by_role('button',name='下一页',exact=True).click()
        expect(page.locator('#main')).to_contain_text('dnf:9007199254740993')
        page.get_by_role('button',name='上一页',exact=True).click()
        expect(page.get_by_role('button',name='打开玩家1',exact=True)).to_be_visible()
        checks.append('one minute idle and typing cause zero requests, explicit search, previous/next cursor, string IDs')
        # Deliberately ignore AbortController to prove the request epoch guard.
        page.evaluate('''() => {const actual=fetch;window.fetch=(path,options)=>{const {signal,...rest}=options;return actual(path,rest)}}''')
        state['hold']='/api/v1/accounts/dnf:1/characters'
        page.get_by_role('button',name='打开玩家1',exact=True).click();page.wait_for_timeout(80)
        assert state['held'] is not None, calls[-3:]
        old=state['held'];state['hold']=None;state['held']=None
        page.get_by_role('button',name='打开玩家2',exact=True).click()
        expect(page.locator('[aria-label="账号详情"]')).to_contain_text('角色2<b>')
        respond(old,[character('1')]);page.wait_for_timeout(80)
        expect(page.locator('[aria-label="账号详情"]')).not_to_contain_text('角色1')
        assert page.locator('#main b').count()==0
        state['hold']='/api/v1/characters/c2'
        page.get_by_role('button',name='查看角色2<b>',exact=True).click();page.wait_for_timeout(80)
        old=state['held'];assert old is not None
        page.get_by_role('button',name='关闭',exact=True).click()
        state['hold']=None;state['held']=None
        page.get_by_role('button',name='打开玩家1',exact=True).click()
        expect(page.locator('[aria-label="账号详情"]')).to_contain_text('角色1')
        respond(old,{'code':'dependency_unavailable','message':'old error'},503);page.wait_for_timeout(80)
        expect(page.get_by_role('dialog')).not_to_be_visible();expect(page.locator('#main')).not_to_contain_text('old error')
        page.get_by_role('button',name='查看角色1<b>',exact=True).click()
        expect(page.get_by_role('dialog')).to_contain_text('未知')
        page.get_by_role('button',name='关闭',exact=True).click()
        checks.append('late success and late failure discarded despite ignored abort, escaped names, unknown online state')
        page.get_by_label('查询范围').select_option('characters')
        expect(page.get_by_label('账号状态')).to_be_disabled()
        page.get_by_role('button',name='查询',exact=True).click()
        expect(page.locator('tbody')).to_contain_text('角色2')
        assert calls[-1]['path']=='/api/v1/characters'
        state['fail']=True;page.get_by_role('button',name='刷新',exact=True).click()
        expect(page.get_by_role('alert')).to_contain_text('数据库暂不可用')
        expect(page.locator('tbody')).to_have_count(0)
        state['fail']=False;state['empty']=True;page.get_by_role('button',name='重置',exact=True).click()
        expect(page.locator('#main')).to_contain_text('没有匹配结果')
        state['empty']=False
        page.locator('nav a[href="#items"]').click()
        expect(page.locator('tbody')).to_contain_text('天空礼盒')
        page.screenshot(path=str(OUT/'items-light.png'),full_page=True)
        before=len(calls);page.clock.run_for(60000);page.wait_for_timeout(80);assert len(calls)==before
        page.locator('nav a[href="#mail"]').click()
        expect(page.locator('#main')).to_contain_text('邮件服务待就绪')
        expect(page.get_by_role('button',name='预览邮件',exact=True)).to_be_disabled()
        assert not page.get_by_role('button',name='发送',exact=True).count()
        checks.append('separate role search, errors distinct from empty, item directory, no simulated mail data')
        page.locator('#theme-open').click();page.locator('[data-action=mode][data-value=dark]').click();page.keyboard.press('Escape')
        expect(page.locator('html')).to_have_attribute('data-theme','dark')
        page.locator('nav a[href="#players"]').click()
        expect(page.get_by_role('button',name='打开玩家1',exact=True)).to_be_visible()
        page.set_viewport_size({'width':390,'height':844})
        assert page.evaluate('document.documentElement.scrollWidth<=innerWidth'),page.evaluate("[...document.querySelectorAll('#main, .shell-content, .card, .live-search, .table-wrap')].map(e=>[e.className,e.getBoundingClientRect().left,e.getBoundingClientRect().width])")
        page.screenshot(path=str(OUT/'players-mobile-dark.png'),full_page=True)
        page.locator('#nav-toggle').click();expect(page.locator('#sidebar')).to_be_visible();page.keyboard.press('Escape')
        page.set_viewport_size({'width':1440,'height':1000})
        state['authenticated']=False;page.get_by_role('button',name='刷新',exact=True).click()
        expect(page.get_by_role('heading',name='登录管理工作台')).to_be_visible()
        expect(page.locator('tbody')).to_have_count(0)
        before=len(calls);page.clock.run_for(60000);page.wait_for_timeout(80);assert len(calls)==before
        # Relative assets also resolve at the private Hub prefix.
        state['authenticated']=True;page.goto('http://console.test/gm/console/#items')
        expect(page.locator('tbody')).to_contain_text('天空礼盒')
        page.get_by_role('button',name='退出',exact=True).click()
        expect(page.get_by_role('heading',name='登录管理工作台')).to_be_visible()
        assert not state['authenticated']
        checks.append('theme persistence, mobile viewport, expired session clears data without retry loop, Hub prefix, logout')
        assert not errors,errors
        assert not unexpected,unexpected
        assert not any('demo-data.js' in c.get('path','') for c in calls)
        browser.close()
    (OUT/'result.json').write_text(json.dumps({'checks':checks,'calls':calls,'errors':errors,'unexpected':unexpected},ensure_ascii=False,indent=2),encoding='utf-8')
    print('PASS live console:',len(checks),'scenarios')

if __name__=='__main__':run()
