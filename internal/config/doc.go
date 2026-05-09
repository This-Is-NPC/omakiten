// Package config loads, validates, and exposes the user's YAML kit and
// the per-entity markdown frontmatter shapes (skills, personas, laws,
// templates, workflows). Pure file parsing + validation; the SQLite
// adapter (internal/sqlite) handles persistence of the loaded shapes.
// Validator rejects mis-shapen config with coded errors so the CLI/MCP
// boundary can surface them verbatim.
package config
