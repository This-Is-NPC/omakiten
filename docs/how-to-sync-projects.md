# How to keep a project in step across machines

**The question:** how do I work on the same project from two machines, and
what does syncing do and not do?

Omakiten is local-first. Every machine keeps its own database and every
command except the ones on this page works with no network. A server is a
peer that holds the latest version of each row. Nothing leaves a machine
unless its project is marked `synced` and you run `okt sync`.

## 1. Run the server

`okt-server` ships beside `okt` in every release archive
(`okt_<OS>_<arch>.tar.gz`, or `.zip` on Windows); the installers set up
only `okt`, so copy `okt-server` from the archive to the server host. From a
checkout, `mise run build` writes it to `.tmp/build/okt-server`.

```bash
export OKT_SERVER_TOKEN="$(head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n')"
./okt-server --db /var/lib/okt-server/sync.db
```

| Flag | Meaning |
| --- | --- |
| `--db PATH` | The server's SQLite store. Required; created owner-only (0600) when missing. |
| `--listen ADDR` | Address to listen on. Defaults to `127.0.0.1:8787`, loopback only. |
| `--token-file PATH` | A file with one bearer token per line (blank lines and `#` comments are skipped). When unset, the server reads `OKT_SERVER_TOKEN`. |
| `--version` | Print the version and exit. |

Every token must be at least 32 bytes; the server refuses to start with a
shorter one. Every request needs `Authorization: Bearer <token>`.

The server speaks plain HTTP. To serve other machines, bind a reachable
address and put a TLS-terminating reverse proxy in front of it, so the
clients use an `https://` URL.

## 2. Point a machine at it

```bash
okt remote set https://sync.example.com
okt remote show
```

The client needs the token. It reads, in order:

1. the `OMAKITEN_SYNC_TOKEN` environment variable;
2. the file `sync.token` in the Omakiten data directory.

The data directory is `$OMAKITEN_HOME/data` when `OMAKITEN_HOME` is set,
else `$XDG_DATA_HOME/omakiten`, else `~/.local/share/omakiten`. On Unix the
file must not be readable by group or others (`chmod 600`).

Plain `http://` is accepted only for loopback hosts. For another host, use
`https://`, or set `OMAKITEN_SYNC_INSECURE=1` to allow plain http on a
network you trust. The client never follows redirects.

## 3. Opt a project in

Projects are `local` by default. Mark one as synced:

```bash
okt --project example project edit --sync-scope synced
```

From then on every change to the project is recorded for the next sync. Rows
that already exist are queued the first time. Projects left `local` never
reach the server.

## 4. Sync

```bash
okt sync
okt sync --project example
```

Sync pushes local changes, then pulls remote ones, for every synced project
or only the one named. The result lists each project:

```json
{"projects":[{"slug":"example","pushed":3,"pulled":1,"rejected":0}]}
```

`rejected` counts changes that were skipped because they were invalid or
named a row of another project. The server reports its own rejected count
for pushed changes it skipped as invalid. A project that fails reports an
`error` and the others still run. When any project failed, `okt sync` exits
non-zero with a `sync_failed` error whose `details.projects` holds the same
report. Syncing is idempotent: run it again to retry.

## 5. Start another machine

```bash
okt remote set https://sync.example.com
okt remote projects
okt projects clone <uid> --root /path/to/checkout
okt --project example sync
```

`remote projects` lists what the server holds with each project's `uid`.
`clone` creates a synced local project rooted at the existing absolute
directory you name and pulls its rows. If the pull fails the project already
exists locally; finish with `okt sync --project <slug>`. Cloning the same
`uid` twice is refused.

## What syncs and what does not

| Synced | Never synced |
| --- | --- |
| The project's name and description. | The project's root path, which each machine chooses. |
| Plans and waves, tasks (with parent, plan, wave, bucket and priority). | Activity logs of tool calls (`cli.tool_call`, `tui.tool_call`). |
| Errors and solutions. | The workflow configuration (`.omakiten/` and installed presets). |
| Events and comments, other than activity logs. | Projects whose scope is `local`. |
| Task dependencies and the tags on tasks, errors and events. | Application preferences, hooks and themes. |

Buckets and priorities are sent by their identifiers, so both machines must
run the same workflow kit. `okt projects clone` refuses a project whose kit
differs from the one a project at the chosen root would use.

## How conflicts resolve

Each row is replaced as a whole.

- The version with the later hybrid logical clock stamp wins. The stamp
  orders by wall time, then a counter, then the device, so ties resolve the
  same way everywhere and a machine with a slow clock does not lose edits it
  made after a sync.
- A delete of a task, plan, error, solution or event wins over any edit,
  whatever the stamp, and is final: the row never comes back.
- Tags and task dependencies are compared by stamp for deletes too, so a tag
  or dependency removed on one machine can be added again later.
- A change stamped more than 24 hours ahead of the receiver's clock is
  parked, not applied, instead of being rejected.

## Limits

- Merging is per row, not per field. If two machines edit different fields
  of one task between syncs, the later edit replaces the other whole.
- Sync runs only when you run it. There is no background sync.
- There are no accounts, teams or per-user permissions; a token grants full
  access to every project on the server.
- A change whose parent has not arrived (a task before its plan, for
  example) is parked until the parent shows up. A project parks at most
  10,000 changes; past that the oldest are dropped.
- The workflow kit is not synced; machines must share it.
- The server does not terminate TLS.

## When sync refuses

| What you see | Cause and fix |
| --- | --- |
| `no sync remote configured` | Run `okt remote set <url>`. |
| `no sync token` | Set `OMAKITEN_SYNC_TOKEN` or write the token to `sync.token` in the data directory (mode 0600). |
| `sync.token is readable by group or others` | Run `chmod 600` on the file. |
| `invalid url ... http is only allowed for loopback hosts` | Use `https://`, or set `OMAKITEN_SYNC_INSECURE=1`. |
| `operation_denied` ("missing or invalid bearer token") | The token does not match one the server holds. |
| `project_not_found` | The `uid` or `--project` slug is not on the server, or not a synced local project. |
| `validation_error` naming a workflow kit | The machines use different kits; install the same kit before cloning. |
| `validation_error`: already cloned | The project exists locally; run `okt sync --project <slug>`. |
| `validation_error`: no pull progress | The server answered with a cursor that did not advance; check the server and its proxy. |
| `body_too_large` | One change exceeds the server's size limit. Pushes and pulls halve their page automatically on any HTTP 413; a single oversized row fails. |
| `sync_failed` | At least one project failed; read `details.projects[].error`. |
| A refused redirect | The URL redirects; point `okt remote set` at the final address. |
| `okt-server: no token` or `shorter than the 32-byte minimum` | Provide a token of at least 32 bytes with `--token-file` or `OKT_SERVER_TOKEN`. |
