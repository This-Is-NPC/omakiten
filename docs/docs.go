// Package docs embeds the user guides so okt serves the documentation of the
// release that is installed.
package docs

import "embed"

// FS holds the user guides at the top of docs/; contributor guides under
// internal/ are not shipped.
//
//go:embed *.md
var FS embed.FS
