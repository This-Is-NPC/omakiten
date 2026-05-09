package app

import (
	"context"
	"testing"

	"omakiten/internal/domain"
)

func TestProjectServiceInit(t *testing.T) {
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t, 1000))
	defer func() { _ = store.Close() }()

	service := NewProjectService(store)

	// Empty name falls back to filepath.Base(absRoot)
	p, err := service.Init(ctx, "", "", "/work/my-project")
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if p.Name != "my-project" {
		t.Fatalf("Init().Name = %q, want %q", p.Name, "my-project")
	}
	if p.Slug != "my-project" {
		t.Fatalf("Init().Slug = %q, want %q", p.Slug, "my-project")
	}

	// Normalize slug from name
	p2, err := service.Init(ctx, "", "", "/work/Another Project")
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if p2.Slug != "another-project" {
		t.Fatalf("Init().Slug = %q, want %q", p2.Slug, "another-project")
	}

	// Custom slug
	p3, err := service.Init(ctx, "Custom", "custom-slug", "/work/ignored")
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if p3.Name != "Custom" {
		t.Fatalf("Init().Name = %q, want %q", p3.Name, "Custom")
	}
	if p3.Slug != "custom-slug" {
		t.Fatalf("Init().Slug = %q, want %q", p3.Slug, "custom-slug")
	}

	// Slug becomes empty after normalization -> error
	_, err = service.Init(ctx, "!!!", "!!!", "/work/root")
	if err == nil {
		t.Fatal("Init() error = nil, want validation error")
	}
	assertCodedError(t, err, domain.ErrValidation)

	// Cannot reuse existing project slug
	_, err = service.Init(ctx, "", project.Slug, project.RootPath)
	if err != nil {
		t.Fatalf("Init() existing slug error = %v", err)
	}
}

func TestNormalizeSlug(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello world", "hello-world"},
		{"  Hello World  ", "hello-world"},
		{"Hello-World", "hello-world"},
		{"hello---world", "hello-world"},
		{"---hello---world---", "hello-world"},
		{"hello123world", "hello123world"},
		{"hello world!@#$%", "hello-world"},
		{"", ""},
		{"!!!", ""},
	}

	for _, tc := range tests {
		actual := normalizeSlug(tc.input)
		if actual != tc.expected {
			t.Errorf("normalizeSlug(%q) = %q, want %q", tc.input, actual, tc.expected)
		}
	}
}
