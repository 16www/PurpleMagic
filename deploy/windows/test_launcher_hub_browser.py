"""Headless UI acceptance against TestBrowserHarness only, never a live server."""
import argparse
import ipaddress
import json
from pathlib import Path
from urllib.parse import urlparse

from playwright.sync_api import expect, sync_playwright


def run(receipt, output):
    target = json.loads(receipt.read_text())["url"]
    url = urlparse(target)
    if url.scheme != "http" or not ipaddress.ip_address(url.hostname).is_loopback:
        raise ValueError("The browser fixture must be loopback HTTP")
    output.mkdir(parents=True, exist_ok=True)
    errors = []
    try:
        with sync_playwright() as p:
            browser = p.chromium.launch(channel="msedge", headless=True)
            context = browser.new_context(viewport={"width": 1440, "height": 1060})
            page = context.new_page()
            page.on("pageerror", lambda e: errors.append(str(e)))
            page.goto(target + "/admin/")
            expect(page.locator("#login")).to_be_visible()
            page.screenshot(path=str(output / "admin-login.png"), full_page=True)
            page.fill("#token", "test-admin-token-that-is-long-and-isolated")
            page.check("#remember")
            page.locator("#login-form button").click()
            expect(page.locator("#workspace")).to_be_visible()
            assert page.input_value("#token") == ""
            assert page.evaluate("localStorage.length") == 0
            assert page.evaluate("sessionStorage.length") == 0
            cookie = next(c for c in context.cookies() if c["name"] == "dnf90_admin")
            assert cookie["httpOnly"] and cookie["expires"] > 0
            page.select_option("#source-list", "author")
            page.click("#inspect")
            expect(page.locator("#candidate-status")).to_contain_text("符合本服")
            expect(page.locator("#files .file")).to_have_count(7)
            expect(page.locator("#files .file").filter(has_text="connection.json")).to_contain_text("排除")
            expect(page.locator("#publish")).to_be_disabled()
            page.check("#confirmed")
            page.click("#publish")
            expect(page.locator("#version")).to_have_text("test-release")
            page.fill("#news-title", "真实接口发布公告")
            page.fill("#news-body", "仅在隔离测试后台发布。")
            page.fill("#news-date", "10 / 13")
            page.click("#save-news")
            expect(page.locator("#message")).to_contain_text("公告已签名发布")
            page.screenshot(path=str(output / "admin-dashboard.png"), full_page=True)
            state = context.storage_state()
            context.close()
            second = browser.new_context(storage_state=state)
            page = second.new_page()
            page.on("pageerror", lambda e: errors.append(str(e)))
            page.goto(target + "/admin/")
            expect(page.locator("#workspace")).to_be_visible()
            page.click("#logout")
            expect(page.locator("#login")).to_be_visible()
            assert not any(c["name"] == "dnf90_admin" for c in second.cookies())
            # The isolated GM fixture exercises relative assets and injected logout
            # handling; it performs no real GM or player-data operation.
            page.fill("#token", "test-admin-token-that-is-long-and-isolated")
            page.locator("#login-form button").click()
            expect(page.locator("#workspace")).to_be_visible()
            page.click("#gm")
            expect(page.locator("body")).to_have_attribute("data-ready", "yes")
            page.click("#gm-logout")
            expect(page).to_have_url(target + "/admin/")
            expect(page.locator("#login")).to_be_visible()
            assert not any(c["name"] == "dnf90_admin" for c in second.cookies())
            assert not errors, errors
            browser.close()
        (output / "browser-receipt.json").write_text(json.dumps({"browser": "Edge headless", "fixture": "temporary Go test hub and GM fixture; no production services", "checks": ["login", "remember cookie", "no token in web storage", "source inspection", "compatibility gate", "publish", "signed news", "remembered browser restart", "logout revocation", "GM relative scripts", "GM logout and session revocation"], "page_errors": errors, "passed": True}, ensure_ascii=False, indent=2), encoding="utf-8")
        print("PASS headless admin login/source/publish/news/remember/logout and GM adapter; zero page errors")
    finally:
        Path(str(receipt) + ".done").write_text("done")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("receipt", type=Path)
    parser.add_argument("output", type=Path)
    args = parser.parse_args()
    run(args.receipt, args.output)
