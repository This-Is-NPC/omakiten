package config

import (
	"strings"
	"testing"
)

func themeWithColors(colors map[string]string) Theme {
	return Theme{Version: 1, Key: "demo", Name: "Demo", Colors: colors}
}

func TestResolveColor_transparent(t *testing.T) {
	rc, err := ResolveColor("transparent", themeWithColors(nil))
	if err != nil {
		t.Fatalf("transparent: %v", err)
	}
	if !rc.IsTransparent() {
		t.Fatalf("expected transparent")
	}
}

func TestResolveColor_themeReference(t *testing.T) {
	theme := themeWithColors(map[string]string{"accent": "#39ff14"})
	rc, err := ResolveColor("$theme.accent", theme)
	if err != nil {
		t.Fatalf("$theme.accent: %v", err)
	}
	if rc.IsTransparent() {
		t.Fatalf("expected real color")
	}
	if string(rc.Color) != "#39ff14" {
		t.Fatalf("got %q, want #39ff14", rc.Color)
	}
}

func TestResolveColor_themeMissingKey(t *testing.T) {
	theme := themeWithColors(map[string]string{"primary": "#000000"})
	_, err := ResolveColor("$theme.absent", theme)
	if err == nil || !strings.Contains(err.Error(), "no color") {
		t.Fatalf("expected error about missing key, got %v", err)
	}
}

func TestResolveColor_themeRefBadHex(t *testing.T) {
	theme := themeWithColors(map[string]string{"accent": "blue"})
	_, err := ResolveColor("$theme.accent", theme)
	if err == nil || !strings.Contains(err.Error(), "#rrggbb") {
		t.Fatalf("expected hex format error, got %v", err)
	}
}

func TestResolveColor_literalHex(t *testing.T) {
	rc, err := ResolveColor("#FF79C6", themeWithColors(nil))
	if err != nil {
		t.Fatalf("hex: %v", err)
	}
	if string(rc.Color) != "#FF79C6" {
		t.Fatalf("got %q", rc.Color)
	}
}

func TestResolveColor_rejectsShortHex(t *testing.T) {
	_, err := ResolveColor("#fff", themeWithColors(nil))
	if err == nil {
		t.Fatalf("expected error for short hex")
	}
}

func TestResolveColor_rejectsRandomString(t *testing.T) {
	_, err := ResolveColor("blue", themeWithColors(nil))
	if err == nil {
		t.Fatalf("expected error for non-hex/non-theme value")
	}
}

func TestResolveColor_rejectsEmpty(t *testing.T) {
	_, err := ResolveColor("", themeWithColors(nil))
	if err == nil {
		t.Fatalf("expected error for empty value")
	}
}

func TestResolveColor_rejectsEmptyThemeKey(t *testing.T) {
	_, err := ResolveColor("$theme.", themeWithColors(map[string]string{"primary": "#000000"}))
	if err == nil {
		t.Fatalf("expected error for empty theme key")
	}
}

func TestIsValidColorSyntax(t *testing.T) {
	cases := []struct {
		value   string
		wantErr bool
	}{
		{"transparent", false},
		{"$theme.primary", false},
		{"$theme.", true},
		{"#abcdef", false},
		{"#abc", true},
		{"red", true},
		{"", true},
	}
	for _, c := range cases {
		err := IsValidColorSyntax(c.value)
		if (err != nil) != c.wantErr {
			t.Errorf("IsValidColorSyntax(%q): err=%v wantErr=%v", c.value, err, c.wantErr)
		}
	}
}
