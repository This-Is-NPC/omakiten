package defaults

import "embed"

// FS contains the shareable default kit and themes shipped with the binary.
//
//go:embed omakiten.yaml themes/*.yaml
var FS embed.FS
