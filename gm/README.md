<div align="center">

# DNF90GM

![Go](https://img.shields.io/badge/Go-1.26.5-00ADD8?style=flat-square&logo=go&logoColor=white)
![Database](https://img.shields.io/badge/Database-MariaDB-003545?style=flat-square&logo=mariadb)
![Platforms](https://img.shields.io/badge/Platforms-Windows%20%7C%20Linux-2563EB?style=flat-square)

A self-hosted Web GM for ServerS5A3 90CN: manage accounts, inspect PVF items, and deliver repeat-safe item-mail benefits from an independent process.

[简体中文](README.zh-CN.md) ｜ [Quick start](#quick-start) ｜ [Deployment](docs/DEPLOYMENT.md) ｜ [Migration](docs/MIGRATION.md)

</div>

> [!IMPORTANT]
> Requires a compatible ServerS5A3 MariaDB schema, matching PVF, and private game API v1. This repository contains no game assets, player database, or deployment secrets. See the [compatibility contract](docs/COMPATIBILITY.md).

## What it does

- Web login, account/character lookup, PVF item search and existing GM operations.
- The embedded `/console/` workspace provides paginated account/character and item queries with explicit refresh and Theme Studio preferences. Its `/api/v1/*` routes require the existing GM session. It also supports mail preview/delivery, account/character benefit rules and paginated task receipts with session CSRF, durable idempotency and revision checks.
- Table, detail and control surfaces follow the selected palette in both light and dark modes.
- Item details include full PVF descriptions, all usable jobs, attributes, conditions and set bonuses. Run `--build-item-details` offline to prepare the persistent compressed index; unchanged PVF generations are reused. See [item indexing](../docs/GM_ITEM_DETAILS.md).
- Character tabs provide paginated inventory, equipped items, personal/account storage, mailbox and game quests. Open individual records for instance fields, mail content or quest definitions. Prepare static quests with `--build-quest-index`; see [resource queries and PVF updates](../docs/GM_CHARACTER_RESOURCES.md).
- Through the integration project's private Hub, manage launcher announcements under **Gateway and updates**: create, edit, preview, reorder and delete, with revision checks for concurrent edits. See [the announcement contract](../docs/LAUNCHER_ANNOUNCEMENTS.md).
- Send an item by ID and quantity using transactional mail and persistent receipts.
- Preview and backfill existing characters, pause/resume benefit rules, and automatically deliver to eligible new characters.
- Choose once per account or once per character for each rule; reuse its permanent receipts after restart or mail deletion. Existing rules retain character scope.
- Query game-owned online state and activity settings through an authenticated private API.

Each benefit rule currently contains **one item**. Multiple rules provide multiple items as separate mails. Creating a new rule is a new grant; it does not share the old rule's receipt. Read [benefit semantics](docs/BENEFITS.md).

## Quick start

Install Go 1.26.5 and Python 3.10+. From the integration project root:

```sh
cd gm
python scripts/build.py windows amd64
```

For Linux use `python3 scripts/build.py linux amd64`. Copy `config/gm.example.json`
to `runtime/gm.json`, fill in the existing database mapping, PVF SHA256, private
game API secret and a unique management password. Make the state directory writable.

```text
dist/windows-amd64/DNF90GM.exe --config runtime/gm.json --check
dist/windows-amd64/DNF90GM.exe --config runtime/gm.json --build-pvf-index
dist/windows-amd64/DNF90GM.exe --config runtime/gm.json --build-item-details
dist/windows-amd64/DNF90GM.exe --config runtime/gm.json --build-quest-index
dist/windows-amd64/DNF90GM.exe --config runtime/gm.json --init-schema
```

This integration candidate requires an offline PVF disk index. `--build-pvf-index`
creates a new index and exits without opening a database or listener. The server
reads PVF on demand with 8 MiB chunk, 2 MiB text and 4 MiB directory cache limits. See
[deployment and upgrade steps](docs/DEPLOYMENT.md); string pools and item
projections consume additional memory.

Open `http://127.0.0.1:18112/` on the GM host. Linux uses
`./dist/linux-amd64/DNF90GM`. `--check` validates config syntax only;
`--init-schema` initializes GM tables and starts the service. Creating/enabling
reward rules remains an explicit administrator action. Existing enabled rules resume scanning.

Upgrading this candidate adds two account-scope benefit tables with `--init-schema`.
Older GM binaries cannot see these new rules/jobs; rolling back pauses their delivery.
Keep all current data and receipts when switching versions; see [migration](docs/MIGRATION.md).

Windows also provides `BUILD.bat` and `START_GM.bat`. The [deployment guide](docs/DEPLOYMENT.md)
covers binaries, Linux Docker, private access and every config field.

## Independence and compatibility

GM owns its HTTP server, credentials, PVF index, jobs and scanner. The game owns
login, character creation, native gameplay and mail claiming. GM retains the
existing MariaDB asset transactions and calls the private API for runtime state.

This is a separate Go module with a pinned compatibility-library snapshot;
building requires no ServerS5A3 checkout or relative module replacement.
The optional launcher Hub and its remembered token login are separate integrations.
For an existing installation, follow [migration instructions](docs/MIGRATION.md)
to preserve campaign IDs, signup snapshots and all delivery receipts.

## Development

```sh
python scripts/check_boundary.py
go test -buildvcs=false -count=1 ./...
go vet ./...
python scripts/test_mysql.py
```

The last command starts and removes a disposable MariaDB Docker container. Never
point test DSNs at a player database. Optional browser verification:
`pip install playwright`, `python -m playwright install chromium`, then
`python scripts/test_browser.py`. It uses mocked requests and sends no real rewards.
Create both platform archives with `python scripts/package.py v0.1.0`.

## Sources

Extracted from [Cec1c/ServerS5A3](https://github.com/Cec1c/ServerS5A3), derived from
[16ww/servers5a3](https://gitgud.io/16ww/servers5a3). Source paths and hashes are in
[PROVENANCE.md](docs/PROVENANCE.md). See [NOTICE.md](NOTICE.md) for retained attribution
and licensing status; no new license is asserted for imported code.
