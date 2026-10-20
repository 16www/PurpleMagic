# Activity management / 游戏活动管理

The GM section provides activity search, grouping, saved and effective states,
an editable draft, a separate save confirmation and an optional restart action.
Activity management is available without selecting a character. Definitions come
from the configured private game API; absent reward details remain absent.

## API

- `GET /api/v1/activities` returns `data.activityControls` (revision,
  restartRequired and activities) and `data.restartAvailable`.
- `POST /api/v1/activities` accepts `{revision, activities:[{id,enabled}]}`.
  Every enabled flag and revision must be explicit. Duplicate/unknown IDs and
  unknown fields are rejected. Stale revisions return 409. Only actual value
  changes reach the game; a complete unchanged draft returns the current
  snapshot without creating overrides or increasing the revision.
- `POST /api/v1/activities/restart` accepts only `{revision}`. It requires the
  current saved revision and a pending restart. Saving never restarts the game.

All writes require the administrator session, same-origin validation and CSRF.
The external GM forwards typed v1 requests over the authenticated service link;
the database gateway alone calls the private game `activities` / `save_activities`
actions. Existing activity scope, settings keys and revisions stay in the game.

`enabled` is the saved setting, `effectiveEnabled` is the running process setting,
and `pendingRestart` compares the two. An entry visibility switch describes the
entry only. It does not establish gameplay or reward acceptance.

## Optional Linux restart broker

Set `management.gameRestartSocket` to an absolute local Unix socket path. When
omitted, saving remains available and the page asks the operator to restart.
The gateway process must use the configured deployment user (normally `dnf90`).

`deploy/remote/activity_restart_helper.py` and the systemd template implement the
narrow broker extracted from the legacy GM. The root-owned helper accepts only
the deployment UID and one revision; it rechecks the game snapshot and schedules
only the fixed `dnf90.service`. Browser requests cannot name commands or services.
The management listener has no direct root privileges.

Install the helper as root-owned code under `/usr/local/lib/purplemagic`, with
root-owned parent directories that the gateway user cannot rename or replace.
Keep its configuration under a root-owned `/etc/purplemagic` (group-read access
for the gateway is sufficient; the gateway must not be able to replace it).
Adjust the template's protected gateway
configuration path and keep its receipt directory root-only. The helper can read
both the gateway's nested `management` configuration and a legacy GM mapping.
Keep the old helper/configuration available when planning rollback.

The broker persists a `prepared` receipt before scheduling and an `accepted`
receipt after systemd accepts the scheduled unit. A `prepared` receipt means the
scheduling outcome is unknown; the broker does not automatically repeat it.

An `accepted` receipt means only that systemd accepted the scheduled unit. If
the recorded unit or the `dnf90.service` restart later fails, the broker does not
receive or persist that failure: the receipt remains `accepted`, the same revision
is not scheduled again (including after a broker restart), and the management
process rejects the same revision as already requested while that state remains
in memory. Check the recorded unit, systemd and `dnf90.service` status, the broker
receipt, the game process and the current `restartRequired` value before any
manual recovery. Clearing or altering a receipt requires a separately validated
operator procedure; this document intentionally provides no file-removal command.

The UI reports an unconfirmed result and refreshes running state; `restartRequested`
means accepted, not completed. Completion is established only after the game
returns by reading `restartRequired=false` with `effectiveEnabled` matching the
intended saved state.

The API allows 25 seconds for this optional action; the socket exchange uses
22 seconds, covering the helper's bounded snapshot and systemd calls. The private
service client and reverse proxy must allow at least 30 seconds.

## Legacy registration schema

For the original 90CN registration table without a `*_player_accounts` status
table, set `management.registrationProfile` to `legacy-90cn`. Account reads use
the real registration table; this login model has no disabled-account policy.
The default profile retains the status-table join; unknown profiles are rejected.
No player state table is fabricated during installation.

## Validation

Go tests cover session/CSRF, exact private fields, false flags, stale revisions,
duplicate/unknown IDs, no-op saves, separate restart, failures/replays and the
external-service boundary. Console tests cover adapters, confirmations, draft
retention, changed-row saves and response recovery. The Python helper tests use
temporary directories and a scheduling fixture; they never restart a real game.
Database compatibility tests require a disposable database. Production verification
uses authenticated reads and compares all existing activity values.

Native game interaction after an intentional save/restart is separate acceptance.
Other v1 asset edits still require the game's current character/account admission
protocols; installing activity management does not weaken those checks.
