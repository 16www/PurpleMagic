<div align="center">

# PurpleMagic

![Flutter](https://img.shields.io/badge/UI-Flutter-02569B?style=flat-square)
![Go](https://img.shields.io/badge/Gateway-Go-00ADD8?style=flat-square)
![.NET 8](https://img.shields.io/badge/Launcher-.NET%208-512BD4?style=flat-square)
![MariaDB](https://img.shields.io/badge/Storage-MariaDB-003545?style=flat-square)

**A player launcher, update gateway and GM management toolkit for DNF90.**

One source repository, independently shipped components—from player login and updates to character and operations management.

[简体中文](README.zh-CN.md) ｜ [Quick preview](#quick-preview) ｜ [Feature ledger](docs/FEATURE_LEDGER.md) ｜ [Architecture](docs/ARCHITECTURE.md) ｜ [Documentation](#documentation)

</div>

## What it does

| Workflow | Current capabilities |
| --- | --- |
| Player login | Windows registration and login, client-directory discovery, channel checks and game launch |
| Updates | Configurable sources, signature and file-digest verification, download progress, pause, installation and rollback |
| Player assets | Account and character lookup; premium currency, gold, account vault and shared-resource adjustments with operation history |
| Inventory | Browse bags, equipped items and storage; inspect instances; edit quality, enhancement, amplification, forging, quantity and expiry; delete selected batches |
| Character progression | Adjust level, profession, awakening and SP/TP with change previews and durable receipts |
| Game quests | Browse character quests and definitions; accept, abandon, mark ready, complete without rewards and reset; process selected batches |
| Mail and benefits | Send to one character, selected recipients or an online snapshot; grant once per account or character; track batches and retry failures |
| Announcements and appearance | Publish launcher announcements through the Hub; choose ten GM palettes, custom accents, light/dark modes and saved appearance preferences |

The [71-item feature ledger](docs/FEATURE_LEDGER.md) records each operation's scope, APIs and remaining work. Current priorities include auxiliary-profession validation, advanced item properties, game edit leases, moderation, dungeon telemetry and daily public snapshots. Announcement editing currently uses the Hub page; integration into the independent GM page is scheduled for a later iteration.

## One gateway, two clients

The player launcher and GM Web share gateway infrastructure. A single process hosts the public player HTTPS entry, update Hub and private management API, connected to the existing game service and MariaDB. Player and management traffic use separate listeners and authentication settings.

| Component | Directory | Artifact |
| --- | --- | --- |
| Player launcher | `launcher/` | Flutter Windows UI, C# engine, installation and self-update components |
| Gateway and Hub | `gateway/` | Linux / Windows executables with management domain services and database transactions |
| GM Web | `gm-web/` | Independent static package, served at `/console/` on the management listener |
| Offline tools and migration sources | `gm/` | Original GM implementation, PVF index tools and compatibility contracts |

[Open the interactive architecture](docs/architecture/current.html) · [Repository and release boundaries](docs/REPOSITORY_BOUNDARIES.md)

### On-demand reads and recoverable operations

Accounts, inventory, mailboxes and quests are read by page, container and cursor. PVF disk indexes use the full file digest; item details and quest definitions load on demand, while the profession index caches the selected profession. A PVF update creates a new index generation. Benefit rules use a difference preview to retain their existing delivery records across generations.

Management writes go through domain transactions with revision checks, idempotent requests and durable receipts. Mail, benefits and mailbox cleanup persist batch progress for continuation and result tracking. See [API foundations](docs/FOUNDATIONS.md) for the call paths.

## Quick preview

With Python 3 installed, run from the repository root:

```text
python scripts/preview_console.py --port 8765
```

Open **http://127.0.0.1:8765/** to explore account search, item selection, mail drafts, navigation and themes using sample data. Press `Ctrl+C` to stop. See [preview interactions and data scope](docs/CONSOLE_PREVIEW.md).

For the management API, prepare the gateway configuration, MariaDB, private game endpoint and matching PVF indexes, then follow the [single-gateway setup](docs/GM_GATEWAY_IMPLEMENTATION.md). The GM page establishes a session using a login key.

## Build and check

Toolchain: Go 1.26.5, Python 3, .NET 8 and Flutter Windows. Launcher release builds also use Visual Studio C++ and NativeAOT.

```text
python scripts/check_boundaries.py
python scripts/build_gateway.py linux amd64
python scripts/package_gm_web.py
```

Gateway artifacts go to `dist/gateway/linux-amd64/`; the web package is `dist/gm-web/GM-Web.zip`. Replace `linux` with `windows` for a Windows gateway build. Build the original GM tools with `python gm/scripts/build.py linux amd64`.

```text
python scripts/verify_component.py gateway
python scripts/verify_component.py gm
python scripts/verify_component.py launcher --dotnet <dotnet8>
python scripts/verify_component.py flutter --flutter <flutter>
```

Build the launcher with `BUILD_FLUTTER.bat --flutter <flutter> --dotnet <dotnet8> --profile-dir <profile-directory> --output <new-output-directory>`. Supply the [signing profile and artwork](docs/LAUNCHER_INPUTS.md), then use `BUILD_PORTABLE.bat` to wrap the complete ZIP.

> [!IMPORTANT]
> The deployment environment supplies runtime configuration, signing material, game assets and player data. Match the player, game-service and GM EXE/PVF/profile combination, then verify the published files in the [documented release order](docs/REPOSITORY_BOUNDARIES.md).

## Documentation

| Topic | References |
| --- | --- |
| Scope, progress and page redesign | [Feature ledger](docs/FEATURE_LEDGER.md) · [Frontend plan](docs/FRONTEND_PLAN.md) |
| Services, caching and transactions | [Architecture](docs/ARCHITECTURE.md) · [15 API foundations](docs/FOUNDATIONS.md) |
| Management APIs and operations | [Current routes](docs/API_CATALOG.md) · [Mail, benefits and jobs](docs/GM_OPERATIONS_V1.md) |
| Item descriptions and indexes | [PVF item details](docs/GM_ITEM_DETAILS.md) · [Character resources](docs/GM_CHARACTER_RESOURCES.md) |
| Announcements and public queries | [Launcher announcements](docs/LAUNCHER_ANNOUNCEMENTS.md) · [Public snapshot design](docs/PUBLIC_PLAYER_SNAPSHOTS.md) |
| Versions and changes | [Changelog](changlog.md) · [Provenance and compatibility](docs/PROVENANCE.md) |

## Sources and licenses

Components retain their original licenses and copyright notices. See [source provenance](docs/PROVENANCE.md), the [GM notice](gm/NOTICE.md) and [frontend third-party notices](gm-web/THIRD_PARTY_NOTICES.md).
