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

## Activity management fixture pass — 2026-10-20

- Added the GM activity workspace with name, ID, group, description and reward search; group filtering; per-row saved, effective and pending-restart states; persistent unsaved toggles; explicit save confirmation; and a restart action that is rendered only when the backend advertises `restartAvailable`.
- The fixture and adapter tests cover `data.activityControls`, revision-guarded `{revision, activities:[{id,enabled}]}`, the separate `{revision}` restart request, draft retention after refresh, and lost-response recovery only when the server readback matches the complete desired draft.
- `npm test` passed all 34 tests and `tsc --noEmit` passed. The activity page was reviewed in the existing orchid dark and light themes at the desktop fixture viewport and at 390×844; tables remain internally scrollable and the mobile actions wrap without page-level horizontal overflow.
- The live restart proxy is not part of this frontend change. The page consumes the capability and revision response supplied by the parent integration; when it is absent, the saved-but-restart-required state is shown with an administrator restart note.


## Activity integration follow-up — 2026-10-20

Changed-row saves preserve unchanged defaults. Activity management opens without a selected character; character-specific tabs retain their context. Desktop search, toggle, save confirmation/cancel and mobile rendering were checked against isolated fixtures. The optional restart uses a protected local broker with durable scheduling receipts; production checks read existing settings without changing them.
