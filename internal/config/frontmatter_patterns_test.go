package config

import (
	"strings"
	"testing"
)

// TestSplitFrontmatter_MapBased is the reference for the map-based subtest
// + t.Parallel() pattern documented in CONTRIBUTING.md. The map literal
// rejects duplicate names at compile time, the random iteration order
// surfaces accidental case-to-case dependencies, and t.Parallel lets the
// `go test` scheduler interleave subtests across cores.
func TestSplitFrontmatter_MapBased(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		input        string
		wantFM       string
		wantBody     string
		wantErrSubs  string
	}{
		"valid frontmatter and body": {
			input:    "---\nname: Foo\n---\nhello\n",
			wantFM:   "name: Foo",
			wantBody: "hello",
		},
		"empty frontmatter": {
			input:    "---\n---\nbody\n",
			wantFM:   "",
			wantBody: "body",
		},
		"missing opening": {
			input:       "name: Foo\n",
			wantErrSubs: "opening",
		},
		"missing closing": {
			input:       "---\nname: Foo\nstill here\n",
			wantErrSubs: "closing",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fm, body, err := SplitFrontmatter([]byte(tc.input))
			if tc.wantErrSubs != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErrSubs) {
					t.Fatalf("SplitFrontmatter() err = %v, want substring %q", err, tc.wantErrSubs)
				}
				return
			}
			if err != nil {
				t.Fatalf("SplitFrontmatter() err = %v", err)
			}
			if string(fm) != tc.wantFM {
				t.Fatalf("frontmatter = %q, want %q", string(fm), tc.wantFM)
			}
			if string(body) != tc.wantBody {
				t.Fatalf("body = %q, want %q", string(body), tc.wantBody)
			}
		})
	}
}

// FuzzSplitFrontmatter is the reference for the native fuzz pattern
// documented in CONTRIBUTING.md. Seeds cover the well-formed shape, the
// CRLF + BOM tolerance branches, and the two error paths so the fuzzer
// has a representative starting set. The invariant under fuzz is the
// minimum useful one — "must not panic on arbitrary bytes" — which is
// what catches index-of-bounds and slice-allocation regressions in the
// parser. Round-trip equality is exercised by TestJoinFrontmatterRoundTrip
// against curated inputs; mixing the two layers under fuzz pulls in
// encoder edge cases (CR-only line endings, etc.) that belong to a
// different ticket.
func FuzzSplitFrontmatter(f *testing.F) {
	f.Add([]byte("---\nname: Foo\n---\nhello\n"))
	f.Add([]byte("---\r\nname: Foo\r\n---\r\nhello\r\n"))
	f.Add([]byte("\xEF\xBB\xBF---\nname: Foo\n---\nbody\n"))
	f.Add([]byte("---\n---\nbody only\n"))
	f.Add([]byte("name: Foo\n"))
	f.Add([]byte("---\nname: Foo\nstill here\n"))

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _, _ = SplitFrontmatter(data)
	})
}
