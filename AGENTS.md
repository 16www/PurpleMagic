# PurpleMagic

This independent repository combines the Windows Flutter launcher, Linux player
gateway and administration hub, and GM Web. The user named it PurpleMagic on
2026-10-17. The public repository is https://github.com/Cec1c/PurpleMagic.

- Start with docs/FEATURE_LEDGER.md, docs/FOUNDATIONS.md and docs/ARCHITECTURE.md
  for current implementation facts. docs/GM_GATEWAY_ARCHITECTURE.md and
  docs/GM_API_V1_DRAFT.md retain earlier design material.
- The copied launcher baseline is 0.2.6; reconcile the exact later released
  launcher source before an integrated release. Preserve client directory discovery.
- The auxiliary-profession candidate is unfinished. Read the project-local paused
  handoff before resuming it. The 2026-10-17 inventory/extraction task does not
  resume all paused feature implementation.
- PVF indexes follow the full hash of the operator-approved PVF. Generate and
  verify a new index generation after PVF changes; do not replace the approved
  PVF with upstream assets automatically.
- An EXE-dependent server release requires final compatible EXE Release/mirror,
  then actual player-channel signature and downloaded-asset hash readbacks,
  including matching EXE/PVF/dstr/NPK/profile, before dependent server deployment.

- The 2026-10-16 target is an independently released GM Web/PWA client using a
  versioned management API in one server gateway process. Native Android is a
  possible later client, not a separate implementation of database rules.
  See docs/ARCHITECTURE.md. gm/ remains the legacy migration and offline-tool
  source. The gateway now hosts v1 GM APIs, and gm-web/
  is the independently shipped client; see docs/GM_GATEWAY_IMPLEMENTATION.md.
  Preserve separate player and management listeners and authentication.

- Read changlog.md and local-notes/PROJECT_PLAN.md before changes. The five-phase
  plan applies only to this project and its original 90cn-source workspace.
  Reference repositories do not inherit this project goal.
- Check Git status and preserve existing changes. No source is imported from a
  neighboring checkout at build time. The source manifest records the extraction.
- The GM keeps its existing module and transaction contracts under gm/. Follow
  gm/AGENTS.md for changes there. gateway/ is a separate standalone Go module.
- Game listeners, game-server startup and player databases belong to the game
  service. Private management APIs must not become public gateway routes.
- Runtime files, keys, profiles, game assets, generated binaries and private
  investigation reports remain ignored. Never use real players for test grants.
- Windows entrypoints use BAT/Python. Build, Git publication and deployment are
  separate actions. Preserve rule IDs, receipts and signed release sequences.
- For extraction/Go changes, run go list/test/vet in both modules, independent
  boundary checks and Windows/Linux builds. Database-affecting changes require
  the changed module scripts/test_mysql.py against its new disposable database.
- Launcher checks: Core Tests, EngineTests, PortableTests, Flutter analyze/test.
  Gateway changes include its route isolation and launcherhub tests. Browser
  checks use isolated fixtures. Actual player interaction is separately verified.
- Product text uses PurpleMagic names and describes features directly. Keep
  third-party attribution in provenance and license notices. Theme names use
  descriptive palette names, such as Sky Blue / 晴空蓝.
- Preserve the accepted sidebar, header and page structure. Remove decorative
  workspace/server selectors unless they perform a required action. The old GM
  supplies verified operations and contracts, not the new visual layout.
- UI and documents state behavior directly. Omit defensive copy, repeated demo
  caveats and implementation commentary in product screens. Label the demo once;
  retain meaningful error states, operation results and factual validation scope.
- Continue feature work within the accepted layout;
  theme fixes must follow the selected palette in both light and dark modes.
- The experimental public-player API reads low-priority snapshots, refreshed daily
  by default. Support a selected-character list and all characters including future
  creations. Track its contract in docs/PUBLIC_PLAYER_SNAPSHOTS.md; keep public
  snapshot reads separate from authenticated management operations.
- Preserve the operational goals of the old GM and supplied reference sources.
  Track feature coverage in docs/GM_FEATURE_MATRIX.md. Prioritize player inventory,
  equipped item instances, storage, mailbox and game quests. Game quests and GM
  batch jobs are separate features. Redesign read scope and refresh behavior while
  reusing verified domain transactions and PVF definitions.
