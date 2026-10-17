"""Check the loopback interactive GM preview and capture its current UI."""
import json
from pathlib import Path
from playwright.sync_api import sync_playwright, expect

ROOT = Path(__file__).resolve().parents[1]


def main():
    out = ROOT / 'runtime/verification/live-preview'
    out.mkdir(parents=True, exist_ok=True)
    errors = []
    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        page = browser.new_page(viewport={'width':1440,'height':1000})
        page.on('pageerror', lambda e: errors.append(str(e)))
        page.goto('http://127.0.0.1:8767/console/#players')
        expect(page.get_by_text('本地预览 · 示例玩家',exact=True)).to_be_visible()
        page.get_by_label('管理密码').fill('preview')
        page.get_by_role('button',name='登录',exact=True).click()
        expect(page.locator('tbody tr')).to_have_count(3)
        page.get_by_role('button',name='打开晨雾',exact=True).click()
        expect(page.locator('[aria-label="账号详情"]')).to_contain_text('晨雾·剑影')
        page.screenshot(path=str(out/'accounts.png'),full_page=True)
        page.get_by_role('button',name='查看晨雾·剑影',exact=True).click()
        dialog = page.get_by_role('dialog')
        dialog.get_by_role('tab',name='背包与仓库',exact=True).click()
        expect(dialog.locator('tbody tr')).to_have_count(20)
        dialog.get_by_role('button',name='实例详情',exact=True).first.click()
        expect(dialog.locator('[aria-label="所选记录详情"]')).to_contain_text('当前耐久')
        page.screenshot(path=str(out/'inventory.png'),full_page=True)
        dialog.get_by_role('button',name='查看物品定义',exact=True).click()
        expect(dialog.locator('[aria-label="所选记录详情"]')).to_contain_text('PVF')
        dialog.get_by_role('tab',name='邮箱',exact=True).click()
        dialog.locator('tbody tr').filter(has_text='冒险启程礼包').get_by_role('button',name='查看邮件').click()
        expect(dialog).to_contain_text('欢迎来到阿拉德')
        page.screenshot(path=str(out/'mailbox.png'),full_page=True)
        dialog.get_by_role('tab',name='游戏任务',exact=True).click()
        expect(dialog.locator('tbody tr')).to_have_count(5)
        dialog.get_by_role('button',name='任务详情',exact=True).first.click()
        expect(dialog.locator('[aria-label="所选记录详情"]')).to_contain_text('前置任务')
        page.screenshot(path=str(out/'quests.png'),full_page=True)
        dialog.get_by_role('tab',name='任务库',exact=True).click()
        expect(dialog.locator('tbody tr')).to_have_count(20)
        query = dialog.locator('tbody tr td').first.evaluate('(el)=>el.firstChild.textContent')
        dialog.get_by_label('任务名称或 ID').fill(query)
        dialog.get_by_role('button',name='查询任务库',exact=True).click()
        expect(dialog.locator('tbody')).to_contain_text(query)
        expect(dialog.get_by_label('任务名称或 ID')).to_have_value(query)
        dialog.get_by_role('button',name='关闭',exact=True).click()
        page.locator('nav a[href="#items"]').click()
        page.get_by_label('查询物品').fill('490009735')
        page.get_by_role('button',name='查询',exact=True).click()
        expect(page.locator('tbody tr')).to_have_count(1)
        page.locator('tbody tr button[data-item-detail]').click()
        expect(page.locator('[aria-label="物品详情"]')).to_contain_text('490009735')
        page.screenshot(path=str(out/'item-details.png'),full_page=True)
        page.locator('nav a[href="#mail"]').click()
        page.get_by_role('button',name='新建福利规则',exact=True).click()
        expect(page.locator('tbody')).to_contain_text('账号启程福利')
        expect(page.locator('tbody')).to_contain_text('角色成长补给')
        page.screenshot(path=str(out/'benefits.png'),full_page=True)
        page.locator('#theme-open').click()
        page.locator('[data-action=preset][data-value=ember-dusk]').click()
        page.locator('[data-action=mode][data-value=dark]').click()
        page.keyboard.press('Escape')
        page.wait_for_timeout(400)
        page.screenshot(path=str(out/'benefits-dark.png'),full_page=True)
        page.set_viewport_size({'width':390,'height':844})
        page.screenshot(path=str(out/'mobile.png'),full_page=True)
        assert page.evaluate('document.documentElement.scrollWidth<=innerWidth+1')
        assert not errors,errors
        browser.close()
    (out/'result.json').write_text(json.dumps({'url':'http://127.0.0.1:8767/console/#players','errors':errors,'checks':['login','accounts','inventory','real-item-definition','mailbox','game-quests','quest-search','items','benefit-scopes','theme','mobile']},ensure_ascii=False,indent=2),encoding='utf-8')
    print('PASS current GM preview with V106 indexes')


if __name__ == '__main__':
    main()
