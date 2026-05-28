package installer

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// TestParseHarnessSelection_ShellParity walks the same input shapes
// scripts/installer_select_test.sh exercises against install.sh's
// parse_harness_selection. Every row asserts both the harness slice
// and the SelectionStatus so the bash → Go port stays observably
// identical from a downstream caller's perspective.
func TestParseHarnessSelection_ShellParity(t *testing.T) {
	cases := []struct {
		name     string
		raw      string
		want     []string
		status   SelectionStatus
		warnFrag string
	}{
		{"numeric_comma", "1,3,5", []string{"claude-code", "opencode", "github-copilot"}, StatusOK, ""},
		{"numeric_space", "1 3 5", []string{"claude-code", "opencode", "github-copilot"}, StatusOK, ""},
		{"name_comma", "claude-code,codex", []string{"claude-code", "codex"}, StatusOK, ""},
		{"mixed", "1 codex", []string{"claude-code", "codex"}, StatusOK, ""},
		{"partial_valid", "1,bogus", []string{"claude-code"}, StatusOK, "ignoring unknown harness"},
		{"empty", "", nil, StatusEmpty, ""},
		{"whitespace_only", "   ", nil, StatusEmpty, ""},
		{"out_of_range", "99", nil, StatusInvalid, "out of range"},
		{"unknown_name", "bogus", nil, StatusInvalid, "ignoring unknown harness"},
		{"junk_separators", "crush\tcodex,, ,", []string{"crush", "codex"}, StatusOK, ""},
		{"skip_zero", "0", nil, StatusSkip, ""},
		{"skip_word", "skip", nil, StatusSkip, ""},
		{"skip_word_caps", "Skip", nil, StatusSkip, ""},
		{"skip_none_caps", "NONE", nil, StatusSkip, ""},
		{"skip_wins_over_valid", "1,0", nil, StatusSkip, ""},
		{"skip_wins_over_invalid", "bogus,skip", nil, StatusSkip, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, status, warnings := ParseHarnessSelection(tc.raw)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Fatalf("harnesses mismatch (-want +got):\n%s", diff)
			}
			if status != tc.status {
				t.Fatalf("status: got %v want %v", status, tc.status)
			}
			if tc.warnFrag == "" {
				if len(warnings) != 0 {
					t.Fatalf("unexpected warnings: %v", warnings)
				}
				return
			}
			joined := strings.Join(warnings, " | ")
			if !strings.Contains(joined, tc.warnFrag) {
				t.Fatalf("warnings missing %q: %v", tc.warnFrag, warnings)
			}
		})
	}
}

func TestResolvePreset_ShellParity(t *testing.T) {
	cases := []struct {
		name         string
		raw          string
		wantName     string
		wantFellback bool
	}{
		{"empty_uses_default", "", DefaultPreset, false},
		{"whitespace_uses_default", "   ", DefaultPreset, false},
		{"named_match", "izakaya", "izakaya", false},
		{"named_match_caps", "Shokunin", "shokunin", false},
		{"numeric_index", "3", "kaiseki", false},
		{"numeric_out_of_range", "99", DefaultPreset, true},
		{"unknown_name_falls_back", "bogus", DefaultPreset, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, fellback := ResolvePreset(tc.raw)
			if got != tc.wantName {
				t.Fatalf("preset: got %q want %q", got, tc.wantName)
			}
			if fellback != tc.wantFellback {
				t.Fatalf("fellback: got %v want %v", fellback, tc.wantFellback)
			}
		})
	}
}

// TestSupportedHarnesses_CountMatchesShell guards the install.sh
// invariant `want_count=6`. Cross-references agentsetup.SupportedHarnesses
// so adding a harness in one place without the other surfaces as a
// failing unit test rather than a silent installer regression.
func TestSupportedHarnesses_CountMatchesShell(t *testing.T) {
	const want = 6
	if got := len(SupportedHarnesses()); got != want {
		t.Fatalf("SupportedHarnesses: got %d entries, want %d (sync with install.sh SUPPORTED_HARNESSES)", got, want)
	}
}

func TestSupportedPresets_CountMatchesShell(t *testing.T) {
	const want = 4
	if got := len(SupportedPresets()); got != want {
		t.Fatalf("SupportedPresets: got %d entries, want %d (sync with install.sh SUPPORTED_PRESETS)", got, want)
	}
}
