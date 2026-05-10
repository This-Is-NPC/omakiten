package defaults

import "embed"

// FS contains the shareable default kit and themes shipped with the binary.
//
//go:embed omakiten.yaml themes/*.yaml skills/*.md laws/*.md personas/*.md templates/*.md notifications/*.yaml
var FS embed.FS
