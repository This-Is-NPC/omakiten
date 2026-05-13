// Package configstore is the adapter that satisfies the app-layer config
// ports (BundleStore, EntityFileWriter, Slugifier). It owns the I/O and the
// disk-layout knowledge for the on-disk omakiten config bundle, keeping the
// app layer free of direct file-system or `internal/config` package calls
// for I/O purposes (the app may still import `internal/config` for the
// pure-data Bundle/Law/Persona/Skill model types).
//
// The implementation today delegates to functions in `internal/config` to
// avoid duplicating the loader/saver/migration logic — the boundary is
// enforced at the import direction (app → port → configstore → config),
// not by re-implementation. Future moves of the helper bodies into this
// package are mechanical and do not require a port change.
package configstore

import (
	"omakiten/internal/config"
)

// Adapter is the production wiring of the BundleStore, EntityFileWriter and
// Slugifier ports. The optional repoLocalDir captures a `.omakiten/`
// directory discovered via config.FindRepoLocal at composition time; when
// set, LoadBundle applies the repo-local override layer.
type Adapter struct {
	repoLocalDir string
}

// New returns the production adapter wired without repo-local discovery.
// Use NewWithRepoLocal at the composition root to apply a discovered
// `.omakiten/` directory.
func New() *Adapter {
	return &Adapter{}
}

// NewWithRepoLocal returns an adapter that layers a discovered repo-local
// `.omakiten/` directory on top of the user-global wiring at LoadBundle
// time. An empty repoLocalDir is equivalent to New().
func NewWithRepoLocal(repoLocalDir string) *Adapter {
	return &Adapter{repoLocalDir: repoLocalDir}
}

// RepoLocalDir reports the discovered `.omakiten/` directory, or "" when
// the adapter was wired without one. Lets composition roots forward the
// same value to other ports (e.g. direct `config.LoadBundle` callers
// that bypass the port for backwards compatibility).
func (a Adapter) RepoLocalDir() string { return a.repoLocalDir }

// --- BundleStore -----------------------------------------------------------

func (a Adapter) LoadBundle(path string) (config.Bundle, error) {
	return config.LoadBundleWithRepoLocal(path, a.repoLocalDir)
}

func (Adapter) SaveBundle(path string, bundle config.Bundle) error {
	return config.SaveBundle(path, bundle)
}

func (Adapter) HashFile(path string) (string, error) {
	return config.HashFile(path)
}

func (Adapter) WriteAtomic(path string, data []byte) error {
	return config.WriteAtomic(path, data)
}

func (Adapter) EnsureDefaultFiles(rootDir string) error {
	return config.EnsureDefaultFiles(rootDir)
}

func (Adapter) MigrateLayout(rootDir string) error {
	return config.MigrateLayout(rootDir)
}

func (Adapter) ConfigRootFromYAMLPath(path string) string {
	return config.ConfigRootFromYAMLPath(path)
}

// --- EntityFileWriter ------------------------------------------------------

func (Adapter) LawFileBytes(law config.Law) ([]byte, error) {
	return config.LawFileBytes(law)
}

func (Adapter) PersonaFileBytes(persona config.Persona) ([]byte, error) {
	return config.PersonaFileBytes(persona)
}

func (Adapter) SkillFileBytes(skill config.Skill) ([]byte, error) {
	return config.SkillFileBytes(skill)
}

func (Adapter) EntityFilePath(rootDir string, kind config.EntityKind, slug string) string {
	return config.EntityFilePath(rootDir, kind, slug)
}

func (Adapter) CustomEntityFilePath(rootDir string, kind config.EntityKind, slug string) string {
	return config.CustomEntityFilePath(rootDir, kind, slug)
}

// --- Slugifier -------------------------------------------------------------

func (Adapter) Slugify(s string) string {
	return config.Slugify(s)
}
