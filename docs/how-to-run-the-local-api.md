---
type: Guide
relates_to: [cli:serve, openapi:listProjects, openapi:streamEvents]
---
# How to serve the board to a graphical client

**The question:** how does a GUI read and change every project on this
machine without shelling out to `okt`?

`okt serve` runs a local HTTP API over the same operations as the CLI and the
TUI. It answers only on the loopback interface, authenticates every request
with a token it generates, and streams committed events as they happen.

## 1. Start the daemon

```bash
okt serve
```

It prints its URL and the discovery file, then runs until `Ctrl+C` or
`SIGTERM`. `--addr 127.0.0.1:7766` pins a port; the default picks a free one.
Only one daemon runs per state directory: a second `okt serve` refuses and
names the running one.

Start it outside a project's repo-local `.omakiten/` install. From inside
one, projects without their own install have no workflow the daemon can load.

## 2. Find it and authenticate

The daemon writes two files under the application state directory
(`$OMAKITEN_HOME/state`, else `$XDG_STATE_HOME/omakiten`, else
`~/.local/state/omakiten`) and removes both on exit:

```json
{
  "url": "http://127.0.0.1:40123",
  "pid": 4242,
  "version": "0.33.0",
  "token_file": "/home/me/.local/state/omakiten/serve.token",
  "started_at": "2026-09-30T12:00:00Z"
}
```

`serve.json` is the discovery document. `serve.token` holds a new token for
each run, readable only by you. Send it as a Bearer credential:

```bash
state="${XDG_STATE_HOME:-$HOME/.local/state}/omakiten"
url=$(jq -r .url "$state/serve.json")
token=$(cat "$state/serve.token")
curl -s -H "Authorization: Bearer $token" "$url/api/v1/projects"
```

`GET /health` needs no token. A client that finds no `serve.json` can start
`okt serve` itself and wait for the file.

## 3. Read and change work

Every route lives under `/api/v1`. Project routes take the project slug:
`/api/v1/projects/example/tasks`, `/api/v1/projects/example/tasks/42`.
`GET /api/v1/openapi.json` returns the complete contract; it is generated from
the routes and the delivery types, so it always matches the running binary.

The API serves every operation the CLI runs on a project's work:

| Resource | Routes under `/api/v1/projects/{project}` |
|---|---|
| Project | the project itself (`GET`, `PATCH`, `DELETE`), `resume`, `workflow`, `workflow/orphans` |
| Tasks | `tasks`, `tasks/{task}` and its `transitions`, `assignee`, `archive`, `unarchive`, `checkpoint`, `activity`, `progress`, `dependencies`, `comments` |
| Comments | `comments/{comment}` |
| Plans | `plans`, `plans/{plan}` and its `continuation`, `waves`, `tasks/{task}`, `claims`; `waves/{wave}`; `tasks/{task}/plan` |
| Errors and solutions | `errors`, `errors/{error}/solutions`, `solutions`, `solutions/{solution}/confirmations` |
| Outside events | `events` (`POST`): an `external.<name>` event the workflow declares, which runs its hooks |
| Tags | `tags`, `tags/{tag}`; across projects, `/api/v1/tags` and `/api/v1/tags/{tag}/merge` |
| Catalogs | `laws`, `personas`, `skills`, `templates`, each with a `/{key}` read; `commands` and `commands/{command}/resolve` |
| Reads | `board`, `dependencies`, `search`, `logs`, `insights`, `metrics` |

A route that deletes or removes asks first, as the CLI does without
`--confirm`: it answers with the confirmation it needs and changes nothing
until you repeat the request with `?confirmed=true`. Creating a task that
resembles existing work works the same way, through `"confirmed": true` in the
body.

Tasks and plans travel as the same OKF Markdown file `okt task export` and
`okt plan export` write. `GET .../tasks/{task}/export` and
`GET .../plans/{plan}/export` answer `{"type": ..., "markdown": ...}`;
`POST .../tasks/import` and `POST .../plans/import` take
`{"markdown": ..., "dry_run": ..., "confirmed": ...}`. A request body is capped
at 1 MiB, so import a larger file with the CLI.

Responses use the CLI envelope. Success is `{"ok": true, "data": ...}`.
Failure is `{"ok": false, "code": ..., "msg": ..., "details": ...}` with an HTTP
status: 400 invalid input, 401 missing token, 403 operation off the HTTP
surface, 404 unknown project or task, 409 conflicting state, 422 a workflow
guard refused, 429 too many outside events in the last minute.

Messages come from the language selected with `okt config language set --gui`.
`GET /api/v1/catalog` returns every text of that language, so a client shows
the same words as the CLI and TUI. A key without translation comes back as the
key itself.

The API acts for the person at the GUI, as the TUI does. Errors and solutions
recorded through it carry source `http` and the route's operation id as their
entry point, and workflow guards judge its calls as made by a user, not an
agent.

The workflow's `surfaces.yaml` decides what the API may do: each operation has
an `http` column. Database backup and search reindex ship with `http: false`.
Deleting a project is served: unconfirmed, `DELETE /api/v1/projects/{project}`
answers what it would remove; confirmed, it writes a database backup first and
answers its path.

Two reads sit outside that table because they run no operation.
`GET /api/v1/projects/example/knowledge` returns the project's file-backed
knowledge, as `okt knowledge list --include-related` reads it: resources,
relations, and diagnostics. `GET /api/v1/projects/example/studio` returns what
the TUI Studio and Settings show: buckets, transitions with their guards (and
the sub-task workflow when its guards differ), commands, personas, laws,
skills, templates, hooks, and the effective settings.

## 4. Follow changes live

```bash
curl -N -H "Authorization: Bearer $token" \
  "$url/api/v1/events?project=example&project=other&category=task"
```

The stream carries events committed by any process: the CLI, the TUI, an
agent, or the API itself. Each message has the event id, the event type, and
the same row `okt logs` prints:

```text
id: 812
event: task.moved
data: {"id":812,"project_id":3,"event_type":"task.moved","category":"task","summary":"dev → review",...}
```

Reconnect with the `Last-Event-ID` header to receive what you missed. When the
gap is too large, or a client reads too slowly, the stream sends
`event: resync` and closes; reload your lists, then reconnect.

## Next

- [The command line](cli.md) — the same operations from a terminal.
- [How to change a workflow](how-to-customize-a-workflow.md) — the surfaces table.
- [When a command refuses](troubleshooting.md) — reading error codes.
