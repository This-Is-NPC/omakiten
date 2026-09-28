package bundledraft

import (
	"context"

	"omakiten/internal/config"
)

// Editor is the on-disk bundle surface a draft commits through. The production
// implementation is the application layer's current-path bundle editor; the
// draft only ever names this port, so it stays free of file I/O and of the
// layer that owns it.
//
// Apply runs the mutator against the bundle and version captured when the
// draft was opened, then returns the freshly re-loaded result for validation.
type Editor interface {
	Path() string
	SetPath(path string)
	ConfigDir() string
	RootDir() string
	Load() (config.Bundle, error)
	LoadPlan() (config.Bundle, string, map[string]string, error)
	Hash() (string, error)
	Apply(ctx context.Context, bundle config.Bundle, sourceHashes map[string]string, mutate func(*config.Bundle) error) (config.Bundle, error)
}
