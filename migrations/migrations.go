package migrations

import "embed"

// FS exposes SQL migrations to the SQLite package.
//
//go:embed *.sql
var FS embed.FS
