# Compatibility contract / 兼容契约

This release targets the ServerS5A3 90CN MariaDB schema and private game API v1
from commit `721927e6f6e36598d81126d50ac6fd7466710ccf`.
It is not a generic SQLite, 86JP, or arbitrary DNF server tool.
This document describes only the legacy `gm/` module; the new `gm-console/`
workbench is outside this compatibility contract.

## Stable data

- Config field names and database/table mappings are unchanged from the deployed standalone GM.
- Keep the registration database's `<accountTable>_benefit_policy`, `_benefit_signup`,
  `_benefit_jobs`, and `_benefit_campaigns` tables.
- This integration candidate adds `_benefit_acct_jobs` and `_benefit_acct_campaigns`
  for account-wide rules. Initialize them explicitly with `--init-schema` and
  include them in backups. Legacy workers never enumerate them; rollback pauses
  account-wide delivery, and upgrading again reuses the same receipts.
- Keep game account/mailbox tables and metadata receipts, including
  `test_benefits_welcome_cera_v1` and campaign/mail deduplication entries.
- Keep existing campaign IDs and character creation timestamps. Creating a new rule
  represents a new grant; it does not reuse an older rule's receipt.
- Missing/`character` scope preserves legacy receipt keys exactly. `account` scope
  uses a distinct account-metadata key and stores the actual target character and
  mailbox ID. Do not move account-wide payloads into the legacy tables.
- `--init-schema` creates missing GM benefit tables only; it does not migrate game
  tables, create rules, or enable rules. An already enabled scanner resumes normally.

## Private game API

POST `/api/game/admin` with `Authorization: Bearer <gameToken>` and JSON `version: 1`.
Supported actions: `online`, `catalog`, `activities`, `save_activities`, `encode_stack`.
The wire types are in `internal/modules/dnf/adminapi/protocol.go`.
The server's `DNFBRIDGE_PLAYER_ADMIN_SECRET` must match `gameToken`.
Do not expose this route through the public player gateway.

Database/PVF operations belong to GM; online state, effective activity state and
runtime item row encoding belong to the game. A game API outage rejects guarded
offline writes. The legacy `gm` tool does not start or restart the game process.

## New workbench boundary

The new `gm-console/` activity page is outside the legacy `gm/` module. It may use
an optional, separately deployed, root-owned local broker to schedule a restart
only for the fixed `dnf90.service`. That broker does not give the legacy `gm`
module restart capability, and browser requests cannot select a command or service.

## Updating adapters

Review schema, transaction and API changes against the pinned source manifest.
Run disposable MariaDB concurrency/rollback tests and verify the matching PVF.
Two pinned source fixtures in `testdata/server-contract` document equipment-mail
projection at extraction time; they do not validate a future server automatically.

账号初始点券和角色初始金币仍由游戏注册/创建事务执行；GM 管理政策。
物品福利由 GM 扫描并投递邮件。原生客户端领取、开盒和画面需要对应版本实测。
