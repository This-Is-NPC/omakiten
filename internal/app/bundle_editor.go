package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"omakiten/internal/config"
)

// FileOp describes a single per-entity file mutation that participates in a
// transactional Apply call. The path must be absolute. For OpWrite, Bytes is
// the new file contents. For OpDelete, Bytes is ignored.
type FileOp struct {
	Op    FileOpKind
	Path  string
	Bytes []byte
}

type FileOpKind int

const (
	OpWrite FileOpKind = iota
	OpDelete
)

// BundleEditor coordinates atomic mutations across the omakiten.yaml wiring
// file and per-entity .md files. Every Apply that succeeds re-imports the
// resulting bundle into the materialized SQLite store, so callers never need to
// touch the store directly to keep it in sync.
//
// The editor depends on a BundleStore port (load/save/hash/atomic-write) and
// the ConfigRepository (re-import). It deliberately does not import file I/O
// helpers from `internal/config` directly so the hexagonal direction stays
// inward.
type BundleEditor struct {
	repo   ConfigRepository
	bundle BundleStore
	path   string
}

func NewBundleEditor(repo ConfigRepository, bundle BundleStore, path string) *BundleEditor {
	return &BundleEditor{repo: repo, bundle: bundle, path: path}
}

func (e *BundleEditor) Path() string {
	return e.path
}

func (e *BundleEditor) ConfigDir() string {
	return filepath.Dir(e.path)
}

// RootDir is the layout root that holds both the config/ yaml directory and
// the entity folders (skills/, laws/, personas/, templates/, themes/) as
// siblings. New entity files (created via Add flows) land under
// <root>/<kind>/custom/.
func (e *BundleEditor) RootDir() string {
	return e.bundle.ConfigRootFromYAMLPath(e.path)
}

// Load returns the current bundle as written on disk.
func (e *BundleEditor) Load() (config.Bundle, error) {
	bundle, err := e.bundle.LoadBundle(e.path)
	if err != nil {
		return config.Bundle{}, configError(e.path, err)
	}
	return bundle, nil
}

// Apply is the legacy single-callback signature retained for callers that only
// touch the wiring file. It delegates to ApplyWithFiles.
func (e *BundleEditor) Apply(ctx context.Context, mutate func(*config.Bundle) error) (config.Bundle, error) {
	return e.ApplyWithFiles(ctx, mutate, nil)
}

// ApplyWithFiles snapshots every file the FileOps will touch, runs the wiring
// mutator on the in-memory bundle, executes the file operations, then re-loads
// + re-imports the result. The order matters: any new entity file referenced
// by the mutator must exist on disk before LoadBundle re-validates, and any
// removed file must have its refs already dropped from the wiring.
//
// On any failure all renames are rolled back from the journal so the on-disk
// state matches the pre-call snapshot.
func (e *BundleEditor) ApplyWithFiles(ctx context.Context, mutate func(*config.Bundle) error, fileOps []FileOp) (config.Bundle, error) {
	journal, err := snapshotFiles(e.bundle, e.wiringSnapshotPaths(fileOps))
	if err != nil {
		return config.Bundle{}, configError(e.path, err)
	}

	bundle, err := e.bundle.LoadBundle(e.path)
	if err != nil {
		_ = journal.restore()
		return config.Bundle{}, configError(e.path, err)
	}

	if mutate != nil {
		if err := mutate(&bundle); err != nil {
			_ = journal.restore()
			return config.Bundle{}, err
		}
	}

	// Stage the new wiring before touching the entity files: this way removed
	// slugs are dropped from `omakiten.yaml` before the corresponding `.md` is
	// deleted, and newly-referenced slugs see their file land before the next
	// LoadBundle validates the wiring.
	if err := e.bundle.SaveBundle(e.path, bundle); err != nil {
		_ = journal.restore()
		return config.Bundle{}, configError(e.path, err)
	}

	if err := executeFileOps(e.bundle, fileOps); err != nil {
		_ = journal.restore()
		return config.Bundle{}, configError(e.path, err)
	}

	// Re-load from disk so that the freshly written wiring + entity files round
	// trip through the validator before being imported.
	resolved, err := e.bundle.LoadBundle(e.path)
	if err != nil {
		_ = journal.restore()
		return config.Bundle{}, configError(e.path, err)
	}

	hash, err := e.bundle.HashFile(e.path)
	if err != nil {
		_ = journal.restore()
		return config.Bundle{}, configError(e.path, err)
	}
	if err := e.repo.ImportBundle(ctx, resolved, e.path, hash); err != nil {
		_ = journal.restore()
		return config.Bundle{}, err
	}
	return resolved, nil
}

// wiringSnapshotPaths returns the set of paths that need a journal entry to
// support rollback. Always includes omakiten.yaml plus every FileOp target.
func (e *BundleEditor) wiringSnapshotPaths(ops []FileOp) []string {
	paths := []string{e.path}
	seen := map[string]struct{}{e.path: {}}
	for _, op := range ops {
		if _, dup := seen[op.Path]; dup {
			continue
		}
		seen[op.Path] = struct{}{}
		paths = append(paths, op.Path)
	}
	return paths
}

type fileSnapshot struct {
	path    string
	data    []byte
	existed bool
}

type fileJournal struct {
	bundle  BundleStore
	entries []fileSnapshot
}

func snapshotFiles(store BundleStore, paths []string) (*fileJournal, error) {
	journal := &fileJournal{bundle: store}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				journal.entries = append(journal.entries, fileSnapshot{path: path, existed: false})
				continue
			}
			return nil, fmt.Errorf("snapshot %s: %w", path, err)
		}
		journal.entries = append(journal.entries, fileSnapshot{path: path, data: data, existed: true})
	}
	return journal, nil
}

func (j *fileJournal) restore() error {
	var firstErr error
	for i := len(j.entries) - 1; i >= 0; i-- {
		entry := j.entries[i]
		if !entry.existed {
			if err := os.Remove(entry.path); err != nil && !os.IsNotExist(err) {
				if firstErr == nil {
					firstErr = err
				}
			}
			continue
		}
		if err := j.bundle.WriteAtomic(entry.path, entry.data); err != nil {
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

func executeFileOps(store BundleStore, ops []FileOp) error {
	for _, op := range ops {
		switch op.Op {
		case OpWrite:
			if err := store.WriteAtomic(op.Path, op.Bytes); err != nil {
				return fmt.Errorf("write %s: %w", op.Path, err)
			}
		case OpDelete:
			if err := os.Remove(op.Path); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("delete %s: %w", op.Path, err)
			}
		default:
			return fmt.Errorf("unknown file op kind %d", op.Op)
		}
	}
	return nil
}
