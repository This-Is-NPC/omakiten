package defaults

import "embed"

// FS contains the Omakase development fixture, presentation assets and agent skill.
//
//go:embed config/*.yaml config/modules/*.yaml config/themes/*.yaml themes/*.yaml skills/*.md laws/*.md personas/*.md templates/*.md notifications/*.yaml languages/*.yaml agent/omakiten/SKILL.md
var FS embed.FS
