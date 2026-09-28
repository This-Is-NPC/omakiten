package config

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// CollisionPolicy controls how LoadFromDir resolves two on-disk files that
// produce the same slug. The "default" scope is dir/<file>; the "custom"
// scope is dir/custom/<file>. The three loaders that used to inline this
// dedup logic (entity_loader, language, notification_loader) all enforced
// the same same-scope-is-an-error rule, with policy variance only on the
// cross-scope edge — captured explicitly here so each callsite reads the
// intent at a glance instead of through buried `if previous.IsCustom == …`
// branches.
type CollisionPolicy int

const (
	// CollideOverwrite errors on a same-scope duplicate (two defaults
	// or two customs) and lets custom files override defaults across
	// scopes. Matches today's behaviour for skills, laws, personas,
	// templates, language packs, and notifications.
	CollideOverwrite CollisionPolicy = iota
	// CollideError errors on any duplicate slug, regardless of scope.
	// No current consumer; the test pins the semantics so a future
	// loader can opt in without redefining the policy.
	CollideError
	// CollideKeepFirst errors on a same-scope duplicate and keeps the
	// first item across scopes (defaults win over customs). No current
	// consumer; reserved for read-only baselines where a custom file
	// must never shadow the bundled default.
	CollideKeepFirst
)

// LoadOptions configures one LoadFromDir invocation. Suffix gates the
// directory walk to a single file extension (lowercased); MaxFileBytes
// caps every read via readFileBounded; Decode is the per-domain parser
// + validator; SlugOf extracts the dedup key from a successfully decoded
// item (filename slug is not authoritative — e.g. notifications dedup on
// Notification.Name, not filename).
type LoadOptions[T any] struct {
	// Suffixes is the list of accepted file extensions (lowercased,
	// dot-prefixed). Most loaders pin one (".md" for entities), but
	// language packs and notifications accept both ".yaml" and ".yml" —
	// capture both so each loader keeps the same current behavior.
	Suffixes     []string
	MaxFileBytes int64
	// Decode parses raw into the domain type. isCustom is stamped by the
	// walker (false for files at dir/, true for files at dir/custom/) so
	// the decoder can embed scope on the returned item. The optional
	// warning is appended to the loader's accumulated warnings on
	// success — used to surface non-fatal drift like a filename slug
	// that does not match the in-file name field.
	Decode    func(path string, raw []byte, isCustom bool) (T, *SourceWarning, error)
	SlugOf    func(T) string
	Collision CollisionPolicy
	// OnDecodeError is consulted when Decode returns an error. It returns
	// an optional non-fatal SourceWarning and a `recover` flag: if true,
	// the file is skipped and the loader continues; if false, the error
	// propagates. nil callback means every Decode error is fatal.
	//
	// Notification loaders use this to tolerate user-authored custom
	// files that drift from the current schema (warning + skip) while
	// still failing hard on broken default-scope files.
	OnDecodeError func(path string, isCustom bool, err error) (*SourceWarning, bool)
}

// LoadFromDir walks dir and dir/custom for files ending in opts.Suffix,
// reads each file under opts.MaxFileBytes, calls opts.Decode to produce
// an item, and merges by opts.SlugOf according to opts.Collision. Items
// are returned in alphabetical slug order so downstream consumers get a
// stable iteration shape regardless of filesystem ordering.
//
// A missing dir returns (nil, nil, nil) so first-run paths can call this
// safely before any default install has been materialised. Defaults are
// emitted first, then customs — the merge stage decides who wins per the
// policy above.
func LoadFromDir[T any](dir string, opts LoadOptions[T]) ([]T, []SourceWarning, error) {
	suffixes := make([]string, 0, len(opts.Suffixes))
	for _, s := range opts.Suffixes {
		suffixes = append(suffixes, strings.ToLower(s))
	}
	files, err := listFilesIn(dir, suffixes, false, opts.MaxFileBytes)
	if err != nil {
		return nil, nil, err
	}
	customs, err := listFilesIn(filepath.Join(dir, "custom"), suffixes, true, opts.MaxFileBytes)
	if err != nil {
		return nil, nil, err
	}
	files = append(files, customs...)
	return mergeDirFiles(files, opts)
}

func loadFromDirReader[T any](dir string, opts LoadOptions[T], reader bundleSourceReader) ([]T, []SourceWarning, error) {
	suffixes := make([]string, 0, len(opts.Suffixes))
	for _, s := range opts.Suffixes {
		suffixes = append(suffixes, strings.ToLower(s))
	}
	files, err := reader.listFiles(dir, suffixes, false, opts.MaxFileBytes)
	if err != nil {
		return nil, nil, err
	}
	customs, err := reader.listFiles(filepath.Join(dir, "custom"), suffixes, true, opts.MaxFileBytes)
	if err != nil {
		return nil, nil, err
	}
	return mergeDirFiles(append(files, customs...), opts)
}

func mergeDirFiles[T any](files []entityFile, opts LoadOptions[T]) ([]T, []SourceWarning, error) {

	merged := newDirMerge[T]()
	for _, file := range files {
		if err := merged.add(file, opts); err != nil {
			return nil, nil, err
		}
	}

	return merged.items(), merged.warnings, nil
}

type dirEntry[T any] struct {
	item     T
	source   string
	isCustom bool
}

type dirMerge[T any] struct {
	bySlug    map[string]dirEntry[T]
	seenScope map[string]bool
	order     []string
	warnings  []SourceWarning
}

func newDirMerge[T any]() *dirMerge[T] {
	return &dirMerge[T]{bySlug: map[string]dirEntry[T]{}, seenScope: map[string]bool{}}
}

func (m *dirMerge[T]) add(file entityFile, opts LoadOptions[T]) error {
	item, warning, skipped, err := decodeDirEntry(file, opts)
	if err != nil {
		return err
	}
	if skipped {
		if warning != nil {
			m.warnings = append(m.warnings, *warning)
		}
		return nil
	}
	if warning != nil {
		m.warnings = append(m.warnings, *warning)
	}
	slug := opts.SlugOf(item)
	return m.store(file, slug, item, opts.Collision)
}

func decodeDirEntry[T any](file entityFile, opts LoadOptions[T]) (T, *SourceWarning, bool, error) {
	item, warning, err := opts.Decode(file.Path, file.Raw, file.IsCustom)
	if err == nil {
		return item, warning, false, nil
	}
	if opts.OnDecodeError != nil {
		if recoveredWarning, recovered := opts.OnDecodeError(file.Path, file.IsCustom, err); recovered {
			var zero T
			return zero, recoveredWarning, true, nil
		}
	}
	var zero T
	return zero, nil, false, err
}

func (m *dirMerge[T]) store(file entityFile, slug string, item T, policy CollisionPolicy) error {
	previous, exists := m.bySlug[slug]
	if exists {
		keep, err := resolveCollision(file, slug, previous, m.seenScope[slug], policy)
		if err != nil {
			return err
		}
		if !keep {
			return nil
		}
	} else {
		m.order = append(m.order, slug)
	}
	m.bySlug[slug] = dirEntry[T]{item: item, source: file.Path, isCustom: file.IsCustom}
	m.seenScope[slug] = file.IsCustom
	return nil
}

func resolveCollision[T any](file entityFile, slug string, previous dirEntry[T], previousIsCustom bool, policy CollisionPolicy) (bool, error) {
	sameScope := previousIsCustom == file.IsCustom
	switch policy {
	case CollideError:
		return false, duplicateSlugError(file.Path, slug, previous.source)
	case CollideOverwrite:
		if sameScope {
			return false, duplicateSlugError(file.Path, slug, previous.source)
		}
		return true, nil
	case CollideKeepFirst:
		if sameScope {
			return false, duplicateSlugError(file.Path, slug, previous.source)
		}
		return false, nil
	default:
		return false, fmt.Errorf("%s: unknown CollisionPolicy %d", file.Path, policy)
	}
}

func duplicateSlugError(path, slug, previous string) error {
	return fmt.Errorf("%s: duplicate slug %q (also defined in %s)", path, slug, previous)
}

func (m *dirMerge[T]) items() []T {
	sort.Strings(m.order)
	out := make([]T, 0, len(m.order))
	for _, slug := range m.order {
		out = append(out, m.bySlug[slug].item)
	}
	return out
}
