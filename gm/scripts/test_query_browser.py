"""Read-only shipped frontend request audit; every request stays in a fixture."""
import collections
import json
import mimetypes
from pathlib import Path
from urllib.parse import urlsplit, parse_qs
from playwright.sync_api import sync_playwright, expect

ROOT = Path(__file__).resolve().parents[1]
WEB = ROOT / 'internal/services/gmtool/wwwroot'
OUT = ROOT / 'runtime/tests/query-browser'
OUT.mkdir(parents=True, exist_ok=True)
calls, errors, unexpected = [], [], []
state = {'loading': 0, 'hold101': False, 'held': None, 'heldDetail': None}

def characters(account):
    return {'success': True, 'characters': [{'characterId': account * 10, 'name': f'角色-{account}', 'level': 90, 'jobName': 'fixture'}]}

def send(route, data):
    route.fulfill(content_type='application/json', body=json.dumps(data, ensure_ascii=False))

def route(request):
    u = urlsplit(request.request.url)
    if u.netloc != 'gm-audit.test':
        unexpected.append(request.request.url); request.abort(); return
    if not u.path.startswith('/api/'):
        file = WEB / ('index.html' if u.path == '/' else u.path.lstrip('/'))
        if not file.resolve().is_relative_to(WEB.resolve()) or not file.is_file():
            request.fulfill(status=404); return
        request.fulfill(content_type=mimetypes.guess_type(file.name)[0] or 'application/octet-stream', body=file.read_bytes()); return
    calls.append({'method': request.request.method, 'path': u.path, 'query': u.query})
    if request.request.method != 'GET':
        unexpected.append(calls[-1]); request.abort(); return
    if u.path == '/api/environment':
        send(request, {'success': True, 'features': [], 'database': 'fixture', 'pvf': 'fixture.pvf'})
    elif u.path == '/api/status':
        loading = state['loading'] > 0
        if loading: state['loading'] -= 1
        send(request, {'success': True, 'configured': True, 'ready': not loading, 'loading': loading, 'authenticationRequired': True, 'authenticated': True, 'canChangeSource': False, 'database': 'fixture', 'pvf': 'fixture.pvf'})
    elif u.path == '/api/accounts':
        send(request, {'success': True, 'accounts': [{'accountId': 101+i, 'name': n, 'cera': 0, 'tokenCera': 0, 'luckyStar': 0, 'characterCount': 1, 'characterNames': []} for i,n in enumerate(['a','ab','abc'])]})
    elif u.path == '/api/characters':
        account = int(parse_qs(u.query)['accountId'][0])
        if state['hold101'] and account == 101:
            state['held'] = request; return
        send(request, characters(account))
    elif u.path in ('/api/characters/1010', '/api/accounts/101/detail'):
        state['heldDetail'] = request
    elif u.path == '/api/items/categories':
        send(request, {'success': True, 'ready': True, 'equipment': [], 'stackable': [], 'jobs': []})
    elif u.path == '/api/items/browse':
        send(request, {'success': True, 'total': 0, 'offset': 0, 'count': 0, 'results': []})
    else:
        unexpected.append(calls[-1]); send(request, {'success': False, 'error': 'unhandled fixture endpoint'})

def delta(start):
    return calls[start:]

with sync_playwright() as p:
    browser = p.chromium.launch(headless=True)
    page = browser.new_page(viewport={'width': 1440, 'height': 1000})
    page.route('**/*', route)
    page.on('pageerror', lambda e: errors.append(str(e)))
    page.clock.install()
    page.goto('http://gm-audit.test/', wait_until='networkidle')
    expect(page.locator('#workspace')).to_be_visible()
    expect(page.locator('#char-list')).to_contain_text('角色-101')
    initial = list(calls)
    before = len(calls)
    page.clock.run_for(60000)
    page.wait_for_timeout(150)
    idle = delta(before)
    before = len(calls)
    page.locator('#account-search').press_sequentially('abc', delay=50)
    expect(page.locator('#account-select')).to_have_value('103')
    expect(page.locator('#char-list')).to_contain_text('角色-103')
    typed = delta(before)
    before = len(calls)
    page.evaluate("() => { document.querySelector('#search-input').value = '剑'; return searchItems(0); }")
    page.wait_for_timeout(150)
    item_search = delta(before)
    page.locator('#account-search').fill('')
    state['hold101'] = True
    page.select_option('#account-select', '101')
    page.wait_for_timeout(100)
    assert state['held'] is not None
    page.select_option('#account-select', '102')
    expect(page.locator('#char-list')).to_contain_text('角色-102')
    send(state['held'], characters(101)); state['held'] = None
    page.wait_for_timeout(150)
    expect(page.locator('#char-list')).to_contain_text('角色-102')
    expect(page.locator('#char-list')).not_to_contain_text('角色-101')
    stale = {'selectedAccount': page.locator('#account-select').input_value(), 'renderedCharacters': page.locator('#char-list').inner_text()}
    state['hold101'] = False

    # Switching away and back must not revive an older request for the same ID.
    state['hold101'] = True
    page.select_option('#account-select', '101')
    page.wait_for_timeout(100)
    old = state['held']; assert old is not None
    page.select_option('#account-select', '102')
    state['hold101'] = False
    page.select_option('#account-select', '101')
    expect(page.locator('#char-list')).to_contain_text('角色-101')
    outdated = characters(101)
    outdated['characters'][0]['name'] = '过期角色列表'
    send(old, outdated); state['held'] = None
    page.wait_for_timeout(150)
    expect(page.locator('#char-list')).not_to_contain_text('过期角色列表')

    # Stale detail successes must not reopen a panel after account selection.
    for endpoint in ('character', 'account'):
        page.select_option('#account-select', '101')
        expect(page.locator('#char-list')).to_contain_text('角色-101')
        state['heldDetail'] = None
        if endpoint == 'character':
            page.locator('#char-list li').click()
        else:
            page.evaluate('() => { void showAccountPanel(); }')
        page.wait_for_timeout(100)
        held = state['heldDetail']; assert held is not None
        page.select_option('#account-select', '102')
        expect(page.locator('#char-list')).to_contain_text('角色-102')
        send(held, {'success': True, 'name': '过期详情'})
        page.wait_for_timeout(150)
        expect(page.locator('#detail')).to_be_hidden()
        expect(page.locator('#account-panel')).to_be_hidden()
    # An old failed character request must not show a toast on the new account.
    page.select_option('#account-select', '101')
    expect(page.locator('#char-list')).to_contain_text('角色-101')
    state['heldDetail'] = None
    page.locator('#char-list li').click()
    page.wait_for_timeout(100)
    held = state['heldDetail']; assert held is not None
    page.select_option('#account-select', '102')
    send(held, {'success': False, 'error': '过期请求失败'})
    page.wait_for_timeout(150)
    expect(page.locator('body')).not_to_contain_text('过期请求失败')
    stale['reselectAndStaleDetails'] = 'passed'
    state['loading'] = 2
    before = len(calls)
    page.reload(wait_until='networkidle')
    for _ in range(3):
        page.clock.run_for(1100); page.wait_for_timeout(100)
    expect(page.locator('#workspace')).to_be_visible()
    loading = delta(before)
    before = len(calls)
    page.clock.run_for(60000); page.wait_for_timeout(150)
    after_ready = delta(before)
    result = {'initialRequests': initial, 'idle60VirtualSeconds': idle, 'typingABC': typed, 'oneItemSearch': item_search, 'accountResponseRace': stale, 'loadingToReady': loading, 'ready60VirtualSeconds': after_ready, 'pageErrors': errors, 'unexpectedRequests': unexpected, 'source': 'complete shipped HTML and JS, mocked API, no production connection'}
    (OUT/'query-browser.json').write_text(json.dumps(result,ensure_ascii=False,indent=2),encoding='utf-8')
    print(json.dumps(result,ensure_ascii=False,indent=2))
    browser.close()
    assert not errors and not unexpected
    assert not idle and not after_ready
