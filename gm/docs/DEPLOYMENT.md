# 独立部署 / Deployment

## Binary

Download the Windows amd64 or Linux amd64 archive from Releases and verify its
SHA256 against `SHA256SUMS`. Extract it into a dedicated GM directory.
Copy `config/gm.example.json` to a protected `runtime/gm.json` and replace every
placeholder. Source builds use Go 1.26.5 and Python 3.10+:

```text
python scripts/build.py windows amd64
python scripts/build.py linux amd64
```

The executable is written to `dist/<platform>-amd64/`. Run from the repository
root or, for a release archive, substitute its root executable path:

```text
dist/windows-amd64/DNF90GM.exe --config runtime/gm.json --check
dist/windows-amd64/DNF90GM.exe --config runtime/gm.json --build-pvf-index
dist/windows-amd64/DNF90GM.exe --config runtime/gm.json --build-item-details
dist/windows-amd64/DNF90GM.exe --config runtime/gm.json --init-schema
```

Linux: `./dist/linux-amd64/DNF90GM --config runtime/gm.json --init-schema`.
`--check` checks syntax only. `--init-schema` initializes the four benefit tables
and then starts GM; it never creates or enables a reward rule. Existing enabled
rules resume scanning. Subsequent runs can omit `--init-schema`.

This integration candidate requires an offline PVF disk index. Run
`--build-pvf-index` once before starting the new binary; it verifies the configured
PVF SHA256, writes a new immutable directory index, and exits without opening a
database or listener. It deliberately refuses to overwrite an existing index.
For a new PVF, use a new index path and update its expected SHA256 together.
The importer still uses the full archive parser temporarily. Build it on a machine
with enough memory, then copy the index alongside the matching read-only PVF.
After preparing the PVF directory, `--build-item-details` compiles the searchable
summary and compressed item details, then exits. Valid detail generations are
reused by PVF SHA256 and parser version. Files reside beside `itemIndexPath`;
include them when copying the matching PVF/index set. Runtime requests read only
the selected record. See [item index preparation](../../docs/GM_ITEM_DETAILS.md).
`--check`, `--build-pvf-index`, `--build-item-details`, and `--init-schema` are separate operations.

Visit `http://127.0.0.1:18112/` on the GM host and log in with the configured
`password` (at least 32 characters). For remote access use an SSH tunnel or an
authenticated HTTPS reverse proxy. The existing launcher Hub token/remembered
login is a separate integration and is not required or bundled here.

## Configuration

| Field | Requirement |
| --- | --- |
| `listen` | Explicit loopback/private IP; example `127.0.0.1:18112` |
| `password` | Unique management secret, at least 32 characters |
| `registrationDsn` | Existing registration database, `parseTime=true`, no multiStatements |
| `repositoryDsn` / `databases` / `tablePrefix` | Exact existing game database mapping |
| `accountTable` | Existing registration table; default example `dnf90_web_accounts` |
| `pvfPath` / `pvfSha256` | Read-only matching PVF file and its actual SHA256 |
| `itemIndexPath` | Writable persistent cache, e.g. `runtime/item-index.json` |
| `pvfIndexPath` | Immutable offline PVF index; defaults to `itemIndexPath` plus `.pvfidx` when omitted |
| `gameApi` / `gameToken` | Private game API v1 and matching administrator secret |
| `scanSeconds` | 5–3600; example 30 |

The database user needs access to the mapped game tables and GM policy/job tables.
Use restricted existing credentials; the program does not create game schemas.
Protect config files using owner-only permissions/ACLs. Linux users can run
`chmod 600 runtime/gm.json`; the service user must also be able to read PVF and
write the state directory.

## Linux Docker

`compose.yaml` uses Linux host networking so GM can reach existing private
database and game endpoints. GM itself binds loopback according to its config.
Set these deployment environment variables using absolute paths:

```text
GM_CONFIG_FILE=/srv/dnf90gm/gm.json
GM_PVF_FILE=/srv/dnf-assets/Script.pvf
GM_STATE_DIR=/srv/dnf90gm/state
GM_UID=10001
GM_GID=10001
```

Create the state directory and grant that UID access. The container config must
use `/assets/Script.pvf`, `/runtime/item-index.json`, and `127.0.0.1:18112` as its
PVF path, index path and listener. Other private DB/API fields remain explicit.
If changing the listener, update the Compose health check as well.

```sh
docker compose build
docker compose run --rm --no-deps gm --config /config/gm.json --build-pvf-index
docker compose up -d
docker compose ps
```

This Compose project contains only GM. It does not create, replace, or stop a game
server/database. For an existing deployment read [MIGRATION.md](MIGRATION.md)
before starting a second scanner or changing port ownership.

The one-shot index command never starts a scanner. The runtime validates both
the original PVF SHA256 and the index checksum and has no in-process full-PVF
fallback. Chunk/text caches are bounded at 8 MiB / 2 MiB and 512 entries each;
directory reads use a separate 4 MiB page cache (16 KiB pages).
The shared string pools and item projections consume additional memory. Keep
PVF and index files immutable while the service runs. Container limits still
need Linux workload validation; local Go heap figures are not Docker RSS figures.
