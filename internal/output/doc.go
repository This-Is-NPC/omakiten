// Package output renders CLI/MCP responses as a single-line JSON
// envelope (`{"ok": true, "data": …}` for success, `{"ok": false,
// "code": "…", "msg": "…", "details": {…}}` for coded failures). Shared
// by every command that emits machine-readable output so agents see one
// canonical shape instead of per-command variants.
package output
