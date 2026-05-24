package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"omakiten/internal/domain"
)

// loadFromDirItem is a tiny test struct used to exercise LoadFromDir without
// dragging in the validation rules of the real domain types (Skill, Language,
// Notification). It lets each test focus on the collision + traversal
// contract of LoadFromDir itself.
type loadFromDirItem struct {
	Slug     string
	Source   string
	IsCustom bool
}

func writeLoadFromDirFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// fixedDecoder ignores the raw bytes and returns an item keyed by the
// filename slug. Good enough for exercising traversal + collision logic
// without dragging the domain validators in.
func fixedDecoder() func(path string, raw []byte, isCustom bool) (loadFromDirItem, *SourceWarning, error) {
	return func(path string, _ []byte, isCustom bool) (loadFromDirItem, *SourceWarning, error) {
		base := filepath.Base(path)
		slug := strings.TrimSuffix(base, filepath.Ext(base))
		return loadFromDirItem{Slug: slug, Source: path, IsCustom: isCustom}, nil, nil
	}
}

func TestLoadFromDir_emptyDir(t *testing.T) {
	dir := t.TempDir()
	items, warnings, err := LoadFromDir(dir, LoadOptions[loadFromDirItem]{
		Suffixes:     []string{".yaml"},
		MaxFileBytes: 1024,
		Decode:       fixedDecoder(),
		SlugOf:       func(item loadFromDirItem) string { return item.Slug },
		Collision:    CollideOverwrite,
	})
	if err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected 0 items, got %d", len(items))
	}
	if len(warnings) != 0 {
		t.Fatalf("expected 0 warnings, got %d", len(warnings))
	}
}

func TestLoadFromDir_missingDir(t *testing.T) {
	items, warnings, err := LoadFromDir(filepath.Join(t.TempDir(), "does-not-exist"), LoadOptions[loadFromDirItem]{
		Suffixes:     []string{".yaml"},
		MaxFileBytes: 1024,
		Decode:       fixedDecoder(),
		SlugOf:       func(item loadFromDirItem) string { return item.Slug },
		Collision:    CollideOverwrite,
	})
	if err != nil {
		t.Fatalf("LoadFromDir on missing dir: %v", err)
	}
	if len(items) != 0 || len(warnings) != 0 {
		t.Fatalf("expected empty result on missing dir, got items=%d warnings=%d", len(items), len(warnings))
	}
}

func TestLoadFromDir_ignoresNonMatchingSuffix(t *testing.T) {
	dir := t.TempDir()
	writeLoadFromDirFile(t, dir, "alpha.yaml", "ignored")
	writeLoadFromDirFile(t, dir, "beta.txt", "ignored")
	writeLoadFromDirFile(t, dir, "gamma.md", "ignored")
	items, _, err := LoadFromDir(dir, LoadOptions[loadFromDirItem]{
		Suffixes:     []string{".yaml"},
		MaxFileBytes: 1024,
		Decode:       fixedDecoder(),
		SlugOf:       func(item loadFromDirItem) string { return item.Slug },
		Collision:    CollideOverwrite,
	})
	if err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}
	if len(items) != 1 || items[0].Slug != "alpha" {
		t.Fatalf("expected only alpha.yaml, got %+v", items)
	}
}

func TestLoadFromDir_sizeCap(t *testing.T) {
	dir := t.TempDir()
	writeLoadFromDirFile(t, dir, "fat.yaml", strings.Repeat("x", 256))
	_, _, err := LoadFromDir(dir, LoadOptions[loadFromDirItem]{
		Suffixes:     []string{".yaml"},
		MaxFileBytes: 64,
		Decode:       fixedDecoder(),
		SlugOf:       func(item loadFromDirItem) string { return item.Slug },
		Collision:    CollideOverwrite,
	})
	if err == nil {
		t.Fatalf("expected size-cap error, got nil")
	}
	var coded *domain.CodedError
	if !errors.As(err, &coded) || coded.Code != domain.ErrConfigTooLarge {
		t.Fatalf("expected ErrConfigTooLarge, got %v", err)
	}
}

func TestLoadFromDir_collideOverwrite_customWinsOverDefault(t *testing.T) {
	dir := t.TempDir()
	writeLoadFromDirFile(t, dir, "shared.yaml", "default")
	customDir := filepath.Join(dir, "custom")
	customPath := writeLoadFromDirFile(t, customDir, "shared.yaml", "custom")

	items, warnings, err := LoadFromDir(dir, LoadOptions[loadFromDirItem]{
		Suffixes:     []string{".yaml"},
		MaxFileBytes: 1024,
		Decode:       fixedDecoder(),
		SlugOf:       func(item loadFromDirItem) string { return item.Slug },
		Collision:    CollideOverwrite,
	})
	if err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings, got %+v", warnings)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].Source != customPath {
		t.Fatalf("expected custom to win, got source=%s", items[0].Source)
	}
	if !items[0].IsCustom {
		t.Fatalf("expected IsCustom=true on winning item")
	}
}

func TestLoadFromDir_collideOverwrite_sameScopeDuplicateIsError(t *testing.T) {
	dir := t.TempDir()
	writeLoadFromDirFile(t, dir, "shared.yaml", "default")
	writeLoadFromDirFile(t, dir, "other.yaml", "default")
	// Force a same-scope collision by mapping both files to the same slug.
	_, _, err := LoadFromDir(dir, LoadOptions[loadFromDirItem]{
		Suffixes:     []string{".yaml"},
		MaxFileBytes: 1024,
		Decode:       fixedDecoder(),
		SlugOf:       func(item loadFromDirItem) string { return "fixed" },
		Collision:    CollideOverwrite,
	})
	if err == nil {
		t.Fatalf("expected same-scope duplicate error, got nil")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected duplicate-scope error, got %v", err)
	}
}

func TestLoadFromDir_collideError_anyCollisionIsError(t *testing.T) {
	dir := t.TempDir()
	writeLoadFromDirFile(t, dir, "shared.yaml", "default")
	writeLoadFromDirFile(t, filepath.Join(dir, "custom"), "shared.yaml", "custom")
	_, _, err := LoadFromDir(dir, LoadOptions[loadFromDirItem]{
		Suffixes:     []string{".yaml"},
		MaxFileBytes: 1024,
		Decode:       fixedDecoder(),
		SlugOf:       func(item loadFromDirItem) string { return item.Slug },
		Collision:    CollideError,
	})
	if err == nil {
		t.Fatalf("expected duplicate-across-scopes error under CollideError, got nil")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected duplicate-error message, got %v", err)
	}
}

func TestLoadFromDir_collideKeepFirst_defaultWinsOverCustom(t *testing.T) {
	dir := t.TempDir()
	defaultPath := writeLoadFromDirFile(t, dir, "shared.yaml", "default")
	writeLoadFromDirFile(t, filepath.Join(dir, "custom"), "shared.yaml", "custom")
	items, _, err := LoadFromDir(dir, LoadOptions[loadFromDirItem]{
		Suffixes:     []string{".yaml"},
		MaxFileBytes: 1024,
		Decode:       fixedDecoder(),
		SlugOf:       func(item loadFromDirItem) string { return item.Slug },
		Collision:    CollideKeepFirst,
	})
	if err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}
	if len(items) != 1 || items[0].Source != defaultPath {
		t.Fatalf("expected default to win on KeepFirst, got %+v", items)
	}
	if items[0].IsCustom {
		t.Fatalf("expected IsCustom=false on winning item")
	}
}

func TestLoadFromDir_decoderErrorPropagates(t *testing.T) {
	dir := t.TempDir()
	writeLoadFromDirFile(t, dir, "boom.yaml", "anything")
	sentinel := fmt.Errorf("decoder boom")
	_, _, err := LoadFromDir(dir, LoadOptions[loadFromDirItem]{
		Suffixes:     []string{".yaml"},
		MaxFileBytes: 1024,
		Decode: func(path string, _ []byte, _ bool) (loadFromDirItem, *SourceWarning, error) {
			return loadFromDirItem{}, nil, sentinel
		},
		SlugOf:    func(item loadFromDirItem) string { return item.Slug },
		Collision: CollideOverwrite,
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel propagation, got %v", err)
	}
}

func TestLoadFromDir_onDecodeErrorRecoversCustom(t *testing.T) {
	dir := t.TempDir()
	writeLoadFromDirFile(t, dir, "good.yaml", "x")
	writeLoadFromDirFile(t, filepath.Join(dir, "custom"), "bad.yaml", "x")
	boom := fmt.Errorf("schema drift")
	items, warnings, err := LoadFromDir(dir, LoadOptions[loadFromDirItem]{
		Suffixes:     []string{".yaml"},
		MaxFileBytes: 1024,
		Decode: func(path string, _ []byte, isCustom bool) (loadFromDirItem, *SourceWarning, error) {
			if filepath.Base(path) == "bad.yaml" {
				return loadFromDirItem{}, nil, boom
			}
			base := filepath.Base(path)
			slug := strings.TrimSuffix(base, filepath.Ext(base))
			return loadFromDirItem{Slug: slug, Source: path, IsCustom: isCustom}, nil, nil
		},
		SlugOf:    func(item loadFromDirItem) string { return item.Slug },
		Collision: CollideOverwrite,
		OnDecodeError: func(path string, isCustom bool, err error) (*SourceWarning, bool) {
			if !isCustom {
				return nil, false
			}
			return &SourceWarning{Path: path, Message: err.Error()}, true
		},
	})
	if err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}
	if len(items) != 1 || items[0].Slug != "good" {
		t.Fatalf("expected only good to survive, got %+v", items)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0].Message, "schema drift") {
		t.Fatalf("expected recovery warning, got %+v", warnings)
	}
}

func TestLoadFromDir_decoderWarningPropagates(t *testing.T) {
	dir := t.TempDir()
	writeLoadFromDirFile(t, dir, "alpha.yaml", "x")
	items, warnings, err := LoadFromDir(dir, LoadOptions[loadFromDirItem]{
		Suffixes:     []string{".yaml"},
		MaxFileBytes: 1024,
		Decode: func(path string, _ []byte, isCustom bool) (loadFromDirItem, *SourceWarning, error) {
			base := filepath.Base(path)
			slug := strings.TrimSuffix(base, filepath.Ext(base))
			return loadFromDirItem{Slug: slug, Source: path, IsCustom: isCustom},
				&SourceWarning{Slug: slug, Path: path, Message: "decode-time warning"},
				nil
		},
		SlugOf:    func(item loadFromDirItem) string { return item.Slug },
		Collision: CollideOverwrite,
	})
	if err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if len(warnings) != 1 || warnings[0].Message != "decode-time warning" {
		t.Fatalf("expected propagated warning, got %+v", warnings)
	}
}

func TestLoadFromDir_alphabeticalOrder(t *testing.T) {
	dir := t.TempDir()
	writeLoadFromDirFile(t, dir, "charlie.yaml", "x")
	writeLoadFromDirFile(t, dir, "alpha.yaml", "x")
	writeLoadFromDirFile(t, dir, "bravo.yaml", "x")
	items, _, err := LoadFromDir(dir, LoadOptions[loadFromDirItem]{
		Suffixes:     []string{".yaml"},
		MaxFileBytes: 1024,
		Decode:       fixedDecoder(),
		SlugOf:       func(item loadFromDirItem) string { return item.Slug },
		Collision:    CollideOverwrite,
	})
	if err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}
	if items[0].Slug != "alpha" || items[1].Slug != "bravo" || items[2].Slug != "charlie" {
		t.Fatalf("expected alpha→bravo→charlie order, got %+v", items)
	}
}
