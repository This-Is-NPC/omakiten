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
	Suffix       string
	MaxFileBytes int64
	// Decode parses raw into the domain type. isCustom is stamped by the
	// walker (false for files at dir/, true for files at dir/custom/) so
	// the decoder can embed scope on the returned item.
	Decode    func(path string, raw []byte, isCustom bool) (T, error)
	SlugOf    func(T) string
	Collision CollisionPolicy
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
	suffix := strings.ToLower(opts.Suffix)
	files, err := listFilesIn(dir, []string{suffix}, false)
	if err != nil {
		return nil, nil, err
	}
	customs, err := listFilesIn(filepath.Join(dir, "custom"), []string{suffix}, true)
	if err != nil {
		return nil, nil, err
	}
	files = append(files, customs...)

	type entry struct {
		item     T
		source   string
		isCustom bool
	}
	bySlug := map[string]entry{}
	seenScope := map[string]bool{} // slug → scope of last winner
	order := []string{}

	for _, file := range files {
		raw, readErr := readFileBounded(file.Path, opts.MaxFileBytes)
		if readErr != nil {
			return nil, nil, readErr
		}
		item, decodeErr := opts.Decode(file.Path, raw, file.IsCustom)
		if decodeErr != nil {
			return nil, nil, decodeErr
		}
		slug := opts.SlugOf(item)
		if _, exists := bySlug[slug]; exists {
			previousIsCustom := seenScope[slug]
			sameScope := previousIsCustom == file.IsCustom
			switch opts.Collision {
			case CollideError:
				return nil, nil, fmt.Errorf("%s: duplicate slug %q (also defined in %s)", file.Path, slug, bySlug[slug].source)
			case CollideOverwrite:
				if sameScope {
					return nil, nil, fmt.Errorf("%s: duplicate slug %q (also defined in %s)", file.Path, slug, bySlug[slug].source)
				}
				// Cross-scope: defaults are walked first, customs second,
				// so the second arrival is always the custom — let it win.
			case CollideKeepFirst:
				if sameScope {
					return nil, nil, fmt.Errorf("%s: duplicate slug %q (also defined in %s)", file.Path, slug, bySlug[slug].source)
				}
				// Cross-scope: keep the first (default) winner — skip the custom.
				continue
			default:
				return nil, nil, fmt.Errorf("%s: unknown CollisionPolicy %d", file.Path, opts.Collision)
			}
		} else {
			order = append(order, slug)
		}
		bySlug[slug] = entry{item: item, source: file.Path, isCustom: file.IsCustom}
		seenScope[slug] = file.IsCustom
	}

	sort.Strings(order)
	out := make([]T, 0, len(order))
	for _, slug := range order {
		out = append(out, bySlug[slug].item)
	}
	return out, nil, nil
}

