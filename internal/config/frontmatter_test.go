package config

import (
	"strings"
	"testing"
)

func TestSplitFrontmatter(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantFM      string
		wantBody    string
		wantErrSubs string
	}{
		{
			name:     "valid frontmatter and body",
			input:    "---\nname: Foo\n---\nhello world\n",
			wantFM:   "name: Foo",
			wantBody: "hello world",
		},
		{
			name:     "frontmatter only with no body",
			input:    "---\nname: Foo\n---\n",
			wantFM:   "name: Foo",
			wantBody: "",
		},
		{
			name:     "CRLF line endings normalize",
			input:    "---\r\nname: Foo\r\n---\r\nhello\r\n",
			wantFM:   "name: Foo",
			wantBody: "hello",
		},
		{
			name:     "BOM is tolerated",
			input:    "\xEF\xBB\xBF---\nname: Foo\n---\nbody\n",
			wantFM:   "name: Foo",
			wantBody: "body",
		},
		{
			name:        "missing opening delimiter",
			input:       "name: Foo\n",
			wantErrSubs: "opening",
		},
		{
			name:        "missing closing delimiter",
			input:       "---\nname: Foo\nstill here\n",
			wantErrSubs: "closing",
		},
		{
			name:     "empty frontmatter is fine",
			input:    "---\n---\nbody only\n",
			wantFM:   "",
			wantBody: "body only",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, body, err := SplitFrontmatter([]byte(tt.input))
			if tt.wantErrSubs != "" {
				if err == nil {
					t.Fatalf("SplitFrontmatter() error = nil, want substring %q", tt.wantErrSubs)
				}
				if !strings.Contains(err.Error(), tt.wantErrSubs) {
					t.Fatalf("SplitFrontmatter() error = %v, want substring %q", err, tt.wantErrSubs)
				}
				return
			}
			if err != nil {
				t.Fatalf("SplitFrontmatter() error = %v", err)
			}
			if string(fm) != tt.wantFM {
				t.Fatalf("frontmatter = %q, want %q", string(fm), tt.wantFM)
			}
			if string(body) != tt.wantBody {
				t.Fatalf("body = %q, want %q", string(body), tt.wantBody)
			}
		})
	}
}

func TestJoinFrontmatterRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		fm   string
		body string
	}{
		{name: "with body", fm: "name: Foo", body: "hello"},
		{name: "empty body", fm: "name: Foo", body: ""},
		{name: "multi-line body", fm: "name: Foo\ndescription: Bar", body: "line one\nline two"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			joined := JoinFrontmatter([]byte(tt.fm), []byte(tt.body))
			fm, body, err := SplitFrontmatter(joined)
			if err != nil {
				t.Fatalf("round trip Split error = %v\nencoded = %q", err, joined)
			}
			if string(fm) != tt.fm {
				t.Fatalf("round trip fm = %q, want %q", string(fm), tt.fm)
			}
			if string(body) != tt.body {
				t.Fatalf("round trip body = %q, want %q", string(body), tt.body)
			}
		})
	}
}
