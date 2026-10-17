# GM console preview

The frontend lives in `gm-web/` and ships as a static package. The layout uses a 210 px sidebar
that collapses to 54 px, a 50 px header, grouped navigation and compact content
cards. The theme editor provides the color and appearance controls.

## Open the preview

From the project root:

```text
python scripts/preview_console.py --port 8765
```

Open `http://127.0.0.1:8765/`. The loopback server serves `preview.html` with
synthetic data. Mail confirmations create session drafts; update-source drafts
persist in the browser. Stop the server with Ctrl+C.

The gateway management listener serves authenticated queries at `/console/`. The
legacy Hub mount uses `/gm/console/`. This entry uses `index.html` and the v1 API for account,
character and item lookup, mail previews and delivery, account/character benefit
rules and task receipts. See [the operations contract](GM_OPERATIONS_V1.md).
Through the private Hub, the gateway page also manages launcher announcements:
create, edit, preview, reorder and delete using the existing signed publication
API. See [the announcement contract](LAUNCHER_ANNOUNCEMENTS.md). Update-source
and release management remain available at `/admin/`.
The independent GM announcement page, bans, dungeon telemetry and public
snapshots are planned in the feature ledger.

The live item catalog includes complete PVF details, attributes, conditions and
set bonuses. For a local preview with extracted item samples, use the
[`--item-details` preview option](GM_ITEM_DETAILS.md#本地验证与预览).

## Preview pages

| Page | Interaction |
| --- | --- |
| Overview | Service and task summaries, four metrics, recent tasks, quick actions |
| Accounts and characters | Search, status filter, pagination, account selection and character dialog |
| Item catalog | ID/name search, categories, detail, quantities and mail attachments |
| Mail and benefits | Recipient, title, body, attachments, preview and draft confirmation |
| Task history | State filters and task details |
| Gateway and updates | HTTPS source validation, channel selection, draft persistence and reset |
| Settings | The theme editor and query preferences |

Search runs on submission or refresh. Wide tables scroll within their cards.
Mobile navigation and The theme editor support keyboard navigation, focus return
and Escape. Desktop sidebar collapse persists across reloads.

The theme editor includes ten palettes, custom accent, system/light/dark modes,
rounding, density, font, content width and effects. Appearance and source drafts
use `soft-gm-console.*` localStorage keys. Account, mail and task demo records
last for the page session.

## Verification

With Playwright and Chromium installed, run from the project root:

```text
python scripts/verify_console.py
python scripts/verify_console_live.py
python gm/scripts/test_mysql.py --console-browser-script scripts/verify_console_backend.py
python scripts/verify_integration.py announcements
```

The first two commands cover the demo and browser API fixtures. The third runs
the Go HTTP service against a disposable MariaDB Docker database. Receipts and
screenshots are under `runtime/verification/`. The visual comparison is recorded
in `design-qa.md`.
The fourth command runs the shipped announcement editor against an isolated Hub,
including publication, conflict handling, preview and cancellation behavior.

See [frontend attribution](../gm/internal/services/gmtool/wwwroot/console/THIRD_PARTY_NOTICES.md)
for pinned sources and licenses. **Soft…** is the working name.
