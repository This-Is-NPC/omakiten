package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"sync"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

// FileOp describes one entity-file mutation applied alongside a bundle edit.
// Path is relative to the current bundle root. Each operation is checked
// against the target's content version immediately before publication/removal.
type FileOp struct {
	Op           FileOpKind
	Path         string
	Bytes        []byte
	ExpectedHash string
}

type FileOpKind int

const (
	OpWrite FileOpKind = iota
	OpDelete
)

// BundleEditor coordinates current-path bundle and entity-file mutations.
// Every individual write is atomic; operations are published independently
// after an ordinary content-version precondition check.
type BundleEditor struct {
	bundle BundleStore
	path   string
}

func NewBundleEditor(bundle BundleStore, path string) *BundleEditor {
	return &BundleEditor{bundle: bundle, path: path}
}

// Bundle edits are rare, and one process-wide lock avoids incomplete alias and
// shared-root coordination across profiles, runtime recreation, and platforms.
var bundleEditMu sync.Mutex

func (e *BundleEditor) Path() string { return e.path }

// SetPath repoints the editor at a different omakiten.yaml. The caller is
// responsible for importing the bundle at the new path first.
func (e *BundleEditor) SetPath(path string) { e.path = path }

func (e *BundleEditor) ConfigDir() string { return filepath.Dir(e.path) }

// RootDir returns the active config and entity layout root.
func (e *BundleEditor) RootDir() string {
	return e.bundle.ConfigRootFromYAMLPath(e.path)
}

// RelativePath converts a known entity path into the relative FileOp form.
// FileOps deliberately accept only paths below the current bundle root.
func (e *BundleEditor) RelativePath(path string) string {
	if !filepath.IsAbs(path) {
		return path
	}
	rel, err := filepath.Rel(e.RootDir(), path)
	if err != nil {
		return path
	}
	return rel
}

func (e *BundleEditor) Load() (config.Bundle, error) {
	bundle, _, _, err := e.LoadPlan()
	return bundle, err
}

// LoadPlan reads the current bundle and captures hashes for every source byte
// used during planning. Apply must receive these values back; it never
// establishes a baseline by loading again.
func (e *BundleEditor) LoadPlan() (config.Bundle, string, map[string]string, error) {
	bundle, hashes, err := e.bundle.LoadBundlePlan(e.path)
	if err != nil {
		return config.Bundle{}, "", nil, configError(e.path, err)
	}
	return bundle, hashes[e.path], hashes, nil
}

// LoadPlanWithFiles is the explicit file-edit spelling of LoadPlan. Both use
// the same coherent captured-byte plan; neither performs a second read.
func (e *BundleEditor) LoadPlanWithFiles() (config.Bundle, string, map[string]string, error) {
	return e.LoadPlan()
}

func (e *BundleEditor) Hash() (string, error) { return e.bundle.HashFile(e.path) }

func (e *BundleEditor) FileHash(path string) (string, error) { return e.bundle.HashFile(path) }

func (e *BundleEditor) Apply(ctx context.Context, bundle config.Bundle, sourceHashes map[string]string, mutate func(*config.Bundle) error) (config.Bundle, error) {
	return e.ApplyWithFiles(ctx, bundle, sourceHashes, mutate, nil)
}

// ApplyWithFiles mutates the current bundle, publishes the wiring file, then
// applies entity operations and reloads the validated result. Validation and
// publication are serialized per current bundle path; reload runs after that
// critical section. Operations remain intentionally independent: atomic
// publication prevents partial bytes in an individual file, while failures
// identify any state already published.
func (e *BundleEditor) ApplyWithFiles(_ context.Context, bundle config.Bundle, sourceHashes map[string]string, mutate func(*config.Bundle) error, fileOps []FileOp) (config.Bundle, error) {
	if mutate != nil {
		if err := mutate(&bundle); err != nil {
			return config.Bundle{}, err
		}
	}
	for _, op := range fileOps {
		if err := e.validateFileOp(op); err != nil {
			return config.Bundle{}, editorError(op.Path, err)
		}
	}
	published, err := e.publishWithLock(bundle, sourceHashes, fileOps)
	if err != nil {
		return config.Bundle{}, err
	}
	resolved, err := e.Load()
	if err != nil {
		return config.Bundle{}, editorError(e.path, reloadFailure(published, err))
	}
	return resolved, err
}

func (e *BundleEditor) publishWithLock(bundle config.Bundle, sourceHashes map[string]string, fileOps []FileOp) ([]string, error) {
	bundleEditMu.Lock()
	defer bundleEditMu.Unlock()
	var published []string
	err := e.bundle.EditBundle(e.path, func(path string) error {
		writer := NewBundleEditor(e.bundle, path)
		var err error
		if checkPath, checkErr := e.requireCurrentSources(sourceHashes); checkErr != nil {
			return editorError(checkPath, checkErr)
		}
		published, err = writer.publish(bundle, fileOps)
		return err
	})
	return published, err
}

func (e *BundleEditor) publish(bundle config.Bundle, fileOps []FileOp) ([]string, error) {
	if err := e.bundle.SaveBundle(e.path, bundle); err != nil {
		if config.IsAmbiguousPublication(err) {
			return nil, editorError(e.path, ambiguousFailure([]string{e.path}, e.path, err))
		}
		return nil, editorError(e.path, fmt.Errorf("publish wiring file may have partially succeeded; reload to inspect it, then retry or repair: %w", err))
	}
	published := []string{e.path}
	for _, op := range fileOps {
		if err := e.applyFileOp(op); err != nil {
			return nil, editorError(op.Path, e.fileOpFailure(op, published, err))
		}
		published = append(published, filepath.Join(e.RootDir(), op.Path))
	}
	return published, nil
}

func (e *BundleEditor) fileOpFailure(op FileOp, published []string, err error) error {
	if !config.IsAmbiguousPublication(err) {
		return partialFailure(published, op.Path, err)
	}
	affected := filepath.Join(e.RootDir(), op.Path)
	withAffected := append(append([]string(nil), published...), affected)
	return ambiguousFailure(withAffected, affected, err)
}

func editorError(path string, err error) error {
	return domain.NewError(domain.ErrConfigInvalid, err.Error(), map[string]any{"path": path, "error": err.Error()})
}

func (e *BundleEditor) captureVersion(path string) (string, error) {
	hash, err := e.bundle.HashFile(path)
	if err == nil {
		return hash, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	return "", fmt.Errorf("capture content version for %s: %w", path, err)
}

func (e *BundleEditor) requireCurrent(path, expected string) error {
	current, err := e.captureVersion(path)
	if err != nil {
		return err
	}
	if current != expected {
		return fmt.Errorf("file %q changed since it was loaded; reload and retry", path)
	}
	return nil
}

func (e *BundleEditor) requireCurrentSources(sourceHashes map[string]string) (string, error) {
	if _, ok := sourceHashes[e.path]; !ok {
		return e.path, fmt.Errorf("planning hashes missing wiring path %q; reload and retry", e.path)
	}
	paths := make([]string, 0, len(sourceHashes))
	for path := range sourceHashes {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		if err := e.requireCurrent(path, sourceHashes[path]); err != nil {
			return path, err
		}
	}
	return "", nil
}

func (e *BundleEditor) applyFileOp(op FileOp) error {
	if err := e.validateFileOp(op); err != nil {
		return err
	}
	path := filepath.Join(e.RootDir(), op.Path)
	if err := e.requireCurrent(path, op.ExpectedHash); err != nil {
		return err
	}
	switch op.Op {
	case OpWrite:
		if err := e.bundle.WriteAtomic(path, op.Bytes); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	case OpDelete:
		if err := e.bundle.RemoveFile(path); err != nil {
			return fmt.Errorf("delete %s: %w", path, err)
		}
	default:
		return fmt.Errorf("unknown file op kind %d", op.Op)
	}
	return nil
}

func (e *BundleEditor) validateFileOp(op FileOp) error {
	if err := e.bundle.ValidatePath(e.RootDir(), op.Path); err != nil {
		return fmt.Errorf("file %q is outside the bundle root: %w", op.Path, err)
	}
	if op.Op != OpWrite && op.Op != OpDelete {
		return fmt.Errorf("unknown file op kind %d", op.Op)
	}
	return nil
}

func partialFailure(published []string, failedPath string, err error) error {
	return fmt.Errorf("partial bundle update: published %s; %s was not safely completed: %w; reload to inspect the published state, then retry or repair %s", joinPaths(published), failedPath, err, failedPath)
}

func ambiguousFailure(published []string, affectedPath string, err error) error {
	return fmt.Errorf("ambiguous bundle update: published %s; %s may have been published before completion: %w; reload to inspect the current state, then retry or repair %s", joinPaths(published), affectedPath, err, affectedPath)
}

func reloadFailure(published []string, err error) error {
	return fmt.Errorf("bundle update published %s, but final reload was not verified: %w; reload to inspect the published state, then retry or repair", joinPaths(published), err)
}

func joinPaths(paths []string) string {
	if len(paths) == 0 {
		return "no files"
	}
	return fmt.Sprintf("files %q", paths)
}
