package config

import (
	"fmt"
	"sort"
	"strings"
)

// validateSurfaces enforces the four completeness rules on the top-level
// a false surface without a reason are all load errors — not warnings.
func validateSurfaces(table SurfaceTable) error {
	missing, extra := DiffSurfaces(table)
	if len(extra) > 0 {
		return fmt.Errorf("surfaces.%s: unknown operation slug", extra[0])
	}

	for slug, row := range table {
		if err := validateSurfaceRow(slug, row); err != nil {
			return err
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("surfaces: missing operation %q", missing[0])
	}
	return nil
}

func validateSurfaceRow(slug string, row SurfacePolicy) error {
	if row.CLI == nil {
		return fmt.Errorf("surfaces.%s: cli is required", slug)
	}
	if row.TUI == nil {
		return fmt.Errorf("surfaces.%s: tui is required", slug)
	}
	if row.HTTP == nil {
		return fmt.Errorf("surfaces.%s: http is required", slug)
	}
	if !*row.CLI || !*row.TUI || !*row.HTTP {
		if strings.TrimSpace(row.Reason) == "" {
			return fmt.Errorf("surfaces.%s: reason is required when any surface is false", slug)
		}
	}
	return nil
}

// DiffSurfaces reports every census slug absent from got and every slug
// in got that is not in CanonicalSurfaceCensus. Both slices are sorted
// and never nil, so --check can print the full delta instead of the
// first-error LoadBundle path.
func DiffSurfaces(got SurfaceTable) (missing, extra []string) {
	missing = make([]string, 0)
	extra = make([]string, 0)
	census := canonicalSurfaceSlugSet()
	for slug := range got {
		if _, ok := census[slug]; !ok {
			extra = append(extra, slug)
		}
	}
	for _, e := range CanonicalSurfaceCensus {
		if _, ok := got[e.Slug]; !ok {
			missing = append(missing, e.Slug)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	return missing, extra
}

// LoadSurfaceTable reads the surfaces: mapping from the active bundle
// wiring, including `from:` includes, without running completeness
// validation. okt config surfaces --check uses this so it can list
// every extra and missing slug; LoadBundle still rejects an incomplete
// table.
func LoadSurfaceTable(path string) (SurfaceTable, error) {
	wired, _, _, err := readWiringDetailed(path)
	if err != nil {
		return nil, err
	}
	if wired.Surfaces == nil {
		return SurfaceTable{}, nil
	}
	return wired.Surfaces, nil
}

// SurfaceScaffoldYAML emits the canonical table as a mapping
// body suitable to drop under `surfaces:` or into a module file.
// Product rows are all-true; wiring rows are all-false with
// DeniedWiringReason. Rows are grouped by entity prefix with a blank
// line between groups.
func SurfaceScaffoldYAML() string {
	table := CanonicalSurfaceTable()
	var b strings.Builder
	prevEntity := ""
	for i, e := range CanonicalSurfaceCensus {
		entity := surfaceEntity(e.Slug)
		if i > 0 && entity != prevEntity {
			b.WriteByte('\n')
		}
		prevEntity = entity
		b.WriteString(formatSurfaceRow(e.Slug, table[e.Slug]))
		b.WriteByte('\n')
	}
	return b.String()
}

func surfaceEntity(slug string) string {
	if i := strings.IndexByte(slug, '.'); i >= 0 {
		return slug[:i]
	}
	return slug
}

func formatSurfaceRow(slug string, row SurfacePolicy) string {
	line := slug + ": { cli: " + yamlSurfaceBool(row.CLI) +
		", tui: " + yamlSurfaceBool(row.TUI) +
		", http: " + yamlSurfaceBool(row.HTTP)
	if strings.TrimSpace(row.Reason) != "" {
		line += ", reason: \"" + row.Reason + "\""
	}
	return line + " }"
}

func yamlSurfaceBool(p *bool) string {
	if p != nil && *p {
		return "true"
	}
	return "false"
}
