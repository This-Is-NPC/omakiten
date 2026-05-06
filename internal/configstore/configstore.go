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
// Slugifier ports. A single zero-value instance is enough — no state lives
// here; all I/O is parameterized by the path/root passed per call.
type Adapter struct{}

// New returns the production adapter. Callers usually wire it once at the
// composition root and hand it to every config-touching app service.
func New() *Adapter {
	return &Adapter{}
}

// --- BundleStore -----------------------------------------------------------

func (Adapter) LoadBundle(path string) (config.Bundle, error) {
	return config.LoadBundle(path)
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
