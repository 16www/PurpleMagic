# GM Console validation record

## Accepted design

The 2026-10-19 orchid glass layout is the current baseline. Frozen styles and layout rules are in [the design archive](../docs/design/purplemagic-orchid-20261019/README.md). The current application adds real API adapters, Token login, motion and fixed-position loading feedback while preserving that layout.

## Prior integration and visual checks — 2026-10-19

- Real Go v1 and Hub integration covered login, accounts/characters, eight inventory routes, supported item writes, growth, assets, deduplicated mail, mailbox deletion, quests, automatic benefits, announcements, sources and connection profiles against disposable MariaDB.
- Desktop light/dark and 390 px mobile views were reviewed across the seven sections. Mobile tables and tabs scroll inside their containers.
- The login form, 30-day remembered sessions, restart recovery, expiry and revocation were checked. Queries and account navigation use a fixed-size header spinner without inserting a temporary content row.
- The feedback pass covered clearable numeric fields, SP/TP balances and level defaults, individual/batch deletion, full online filtering and compatible item-ID replacement. Avatar rows do not expose equipment reinforcement controls.
- Pet equipment, pets, avatars and emblems included empty-fixture route/ownership checks. Pet growth, complete avatar properties, sockets/enchantments and token-coupon writes remain outside the current contracts.

## Public source synchronization — 2026-10-20

The publication worktree repeats frontend tests, TypeScript/Vite build and static packaging, both Go modules' checks and disposable MariaDB suites, dependency boundaries, Windows/Linux builds, C# component tests and Flutter analyze/test. Results are recorded in [the publication check summary](../docs/PUBLIC_SYNC_20261020.md).

The publication copies current implementation files and the frozen design styles. Private screenshots, credentials, local environment paths and deployment receipts are kept outside the repository.
