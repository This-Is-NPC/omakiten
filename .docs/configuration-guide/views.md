# View configuration

`config.views` sets default sort, filter, and time-window behavior for CLI/TUI/MCP read surfaces. It seeds views; it does not define workflow or command semantics.

Source files:

- `internal/config/bundle.go` (`ViewSettings`).
- `internal/config/validator.go::validateViewSettings`.
- `internal/config/snapshot.go::LogsWindowDays`.

## Contents

- [`config.views`](#configviews)
- [Allowed values](#allowed-values)
- [Logs window](#logs-window)
- [Update when](#update-when)

## `config.views`

```yaml
config:
  views:
    board:
      sort:   { field: created_at, order: desc }
      filter: { priority: [] }
    table:
      sort:   { field: created_at, order: desc }
      filter: { priority: [], bucket: [] }
    graph:
      sort:   { field: id, order: asc }
    logs:
      sort:   { order: desc }
      limit:  50
      window_days: 30
      filter: { source: [] }
    task_activity:
      sort:   { order: asc }
```

Every view block and required sort field/order is validator-required. Empty filter lists mean "all values allowed".

## Allowed values

| Setting | Allowed values |
|---|---|
| `board.sort.field`, `table.sort.field` | `id`, `title`, `priority`, `created_at` |
| `board.sort.order`, `table.sort.order` | `asc`, `desc` |
| `graph.sort.field` | `id`, `title` |
| `graph.sort.order` | `asc`, `desc` |
| `logs.sort.order` | `asc`, `desc`; `logs.sort.field` must be empty |
| `logs.limit` | int `> 0` |
| `logs.window_days` | int `> 0` |
| `task_activity.sort.order` | `asc`, `desc`; `task_activity.sort.field` must be empty |
| `board.filter.priority`, `table.filter.priority` | subset of `config.priorities[].value` |
| `table.filter.bucket` | subset of bucket keys in the active workflow |

`logs.filter.source` is retained only as a legacy parse sink. The unified Logs inspector filters by event category at query time, not by this config value.

## Logs window

`config.views.logs.window_days` is the default time horizon for:

- TUI Stats Logs.
- CLI `okt logs` when `--since` is omitted.
- MCP `logs.list` when `since` is omitted.

Callers can still pass an explicit time floor for one query.

## Update when

- `ViewSettings` gains or loses a field.
- Allowed sort/filter values change.
- A surface starts consuming a new view default.
- Logs window semantics change.
