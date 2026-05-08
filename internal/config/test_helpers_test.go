package config

// canonicalConfigYAML returns the complete canonical config block —
// every field the strict validator demands. Used by inline-YAML tests
// that need a valid baseline plus a per-scenario override.
//
// The text mirrors `defaults/omakiten.yaml` exactly so tests run with
// the same shape production sees on a fresh install. Update both when
// the kit grows new required fields.
const canonicalConfigYAML = `config:
  output:
    json_minified: true
    omit_empty: true
  context:
    default_level: 2
    max_tokens: 12000
  mcp:
    recent_comment_limit: 5
    max_comment_chars: 0
    include_workflow_in_continue: true
    cache_prompts: true
    recent_context_limit: 3
    next_work_limit: 5
    similar_task_limit: 5
  workflow:
    active: default
  theme:
    active: omakiten
  tui:
    token_badge:
      yellow_at: 150
      red_at: 400
  template_defaults: [task, pr, comment-resume, comment-selfbranch, comment-documentation]
  priorities:
    - {id: 1, value: low, color: success}
    - {id: 2, value: normal, default: true, color: info}
    - {id: 3, value: high, color: error}
  severities:
    - {id: 1, value: info, color: info}
    - {id: 2, value: warning, default: true, color: warning}
    - {id: 3, value: error, color: error}
  views:
    board:
      sort: {field: created_at, order: desc}
      filter: {priority: []}
    table:
      sort: {field: created_at, order: desc}
      filter: {priority: [], bucket: []}
    graph:
      sort: {field: id, order: asc}
    logs:
      sort: {order: desc}
      limit: 50
      filter: {source: []}
    task_activity:
      sort: {order: asc}
`
