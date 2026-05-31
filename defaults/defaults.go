package defaults

import "embed"

// FS contains the shareable default kit and themes shipped with the binary.
//
//go:embed config/*.yaml config/modules/*.yaml config/modules/workflows/*.yaml themes/*.yaml skills/*.md laws/*.md personas/*.md templates/*.md notifications/*.yaml languages/*.yaml
var FS embed.FS
