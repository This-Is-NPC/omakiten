package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadTokensEnforcesMinimumLength(t *testing.T) {
	long := strings.Repeat("a", minTokenBytes)
	short := strings.Repeat("b", minTokenBytes-1)
	file := func(content string) string {
		path := filepath.Join(t.TempDir(), "tokens")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	tests := []struct {
		name     string
		path     string
		fallback string
		want     int
		wantErr  bool
	}{
		{"env token", "", long, 1, false},
		{"short env token", "", short, 0, true},
		{"file tokens", file("# c\n" + long + "\n\n" + long + "x\n"), "", 2, false},
		{"one short file token", file(long + "\n" + short + "\n"), "", 0, true},
		{"no token", "", "", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens, err := loadTokens(tt.path, tt.fallback)
			if (err != nil) != tt.wantErr || len(tokens) != tt.want {
				t.Fatalf("tokens %d, err %v", len(tokens), err)
			}
			if err != nil && strings.Contains(err.Error(), short) {
				t.Fatalf("error leaks the token: %v", err)
			}
		})
	}
}
