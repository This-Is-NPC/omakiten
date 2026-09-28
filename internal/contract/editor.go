package contract

import (
	"context"

	"omakiten/internal/config"
)

// BundleEditor is the on-disk bundle mutation surface Studio needs.
// BundleEditor satisfies it; the screen must not import internal/app (D21).
type BundleEditor interface {
	Path() string
	SetPath(path string)
	ConfigDir() string
	RootDir() string
	Load() (config.Bundle, error)
	LoadPlan() (config.Bundle, string, map[string]string, error)
	Hash() (string, error)
	Apply(ctx context.Context, bundle config.Bundle, sourceHashes map[string]string, mutate func(*config.Bundle) error) (config.Bundle, error)
}
