package config

import "testing"

func newTestCatalog(active, baseline map[string]string) *Catalog {
	var act, base *Language
	if active != nil {
		act = &Language{Code: "xx", Name: "Test", Native: "Test", Keys: active}
	}
	if baseline != nil {
		base = &Language{Code: "en", Name: "English", Native: "English", Keys: baseline}
	}
	return NewCatalog(act, base)
}

func TestCatalog_Get_activeWins(t *testing.T) {
	c := newTestCatalog(
		map[string]string{"cli.hello": "Olá"},
		map[string]string{"cli.hello": "Hello"},
	)
	if got := c.Get("cli.hello"); got != "Olá" {
		t.Fatalf("Get: got %q, want %q", got, "Olá")
	}
}

func TestCatalog_Get_fallsBackToBaseline(t *testing.T) {
	c := newTestCatalog(
		map[string]string{},
		map[string]string{"cli.hello": "Hello"},
	)
	if got := c.Get("cli.hello"); got != "Hello" {
		t.Fatalf("Get: got %q, want %q", got, "Hello")
	}
}

func TestCatalog_Get_missingKeyReturnsKey(t *testing.T) {
	c := newTestCatalog(
		map[string]string{},
		map[string]string{},
	)
	if got := c.Get("cli.nope"); got != "cli.nope" {
		t.Fatalf("Get: got %q, want %q", got, "cli.nope")
	}
}

func TestCatalog_Get_nilActiveFallsBack(t *testing.T) {
	c := newTestCatalog(nil, map[string]string{"cli.hello": "Hello"})
	if got := c.Get("cli.hello"); got != "Hello" {
		t.Fatalf("Get: got %q, want %q", got, "Hello")
	}
}

func TestCatalog_Get_nilCatalogReturnsKey(t *testing.T) {
	var c *Catalog
	if got := c.Get("cli.hello"); got != "cli.hello" {
		t.Fatalf("nil catalog Get: got %q, want %q", got, "cli.hello")
	}
}

func TestCatalog_Resolve_singleToken(t *testing.T) {
	c := newTestCatalog(nil, map[string]string{"tui.app.name": "Omakiten"})
	if got := c.Resolve("${{intl:tui.app.name}}"); got != "Omakiten" {
		t.Fatalf("Resolve: got %q, want %q", got, "Omakiten")
	}
}

func TestCatalog_Resolve_multipleTokens(t *testing.T) {
	c := newTestCatalog(nil, map[string]string{
		"a.b": "alpha",
		"c.d": "beta",
	})
	got := c.Resolve("${{intl:a.b}} and ${{intl:c.d}}")
	want := "alpha and beta"
	if got != want {
		t.Fatalf("Resolve: got %q, want %q", got, want)
	}
}

func TestCatalog_Resolve_mixedContent(t *testing.T) {
	c := newTestCatalog(nil, map[string]string{"tui.user": "Alice"})
	got := c.Resolve("Hello ${{intl:tui.user}}, you have 3 items")
	want := "Hello Alice, you have 3 items"
	if got != want {
		t.Fatalf("Resolve: got %q, want %q", got, want)
	}
}

func TestCatalog_Resolve_unknownKeyReturnsKeyLiteral(t *testing.T) {
	c := newTestCatalog(nil, map[string]string{})
	if got := c.Resolve("${{intl:does.not.exist}}"); got != "does.not.exist" {
		t.Fatalf("Resolve: got %q, want %q", got, "does.not.exist")
	}
}

func TestCatalog_Resolve_unknownNamespaceVerbatim(t *testing.T) {
	c := newTestCatalog(nil, map[string]string{})
	cases := []string{
		"${{env:HOME}}",
		"${{var:user}}",
		"${{i18n:foo}}",
	}
	for _, in := range cases {
		if got := c.Resolve(in); got != in {
			t.Fatalf("Resolve(%q): got %q, want verbatim", in, got)
		}
	}
}

func TestCatalog_Resolve_escapeProducesLiteral(t *testing.T) {
	c := newTestCatalog(nil, map[string]string{"intl.foo": "should-not-appear"})
	got := c.Resolve("$${{intl:intl.foo}}")
	want := "${{intl:intl.foo}}"
	if got != want {
		t.Fatalf("Resolve: got %q, want %q", got, want)
	}
}

func TestCatalog_Resolve_malformedTokenVerbatim(t *testing.T) {
	c := newTestCatalog(nil, map[string]string{})
	cases := []string{
		"${{intl:",
		"${{intl:foo",
		"${{intl:}}",
		"${{:foo}}",
		"${{intl:foo bar}}",
	}
	for _, in := range cases {
		if got := c.Resolve(in); got != in {
			t.Fatalf("Resolve(%q): got %q, want verbatim", in, got)
		}
	}
}

func TestCatalog_Resolve_singlePassNoRecursion(t *testing.T) {
	c := newTestCatalog(nil, map[string]string{
		"a.b": "${{intl:c.d}}",
		"c.d": "should-not-appear",
	})
	got := c.Resolve("${{intl:a.b}}")
	want := "${{intl:c.d}}"
	if got != want {
		t.Fatalf("Resolve: got %q (recursed), want %q (single-pass)", got, want)
	}
}

func TestCatalog_Resolve_noTokenPassthrough(t *testing.T) {
	c := newTestCatalog(nil, map[string]string{})
	in := "literal text with no tokens"
	if got := c.Resolve(in); got != in {
		t.Fatalf("Resolve passthrough: got %q, want %q", got, in)
	}
}

func TestCatalog_Resolve_nilCatalogPassthrough(t *testing.T) {
	var c *Catalog
	in := "${{intl:any.key}}"
	if got := c.Resolve(in); got != in {
		t.Fatalf("nil catalog Resolve: got %q, want %q", got, in)
	}
}

func TestCatalog_Resolve_escapeMixedWithLiveToken(t *testing.T) {
	c := newTestCatalog(nil, map[string]string{"x.y": "ZZZ"})
	got := c.Resolve("escaped $${{intl:x.y}} and live ${{intl:x.y}}")
	want := "escaped ${{intl:x.y}} and live ZZZ"
	if got != want {
		t.Fatalf("Resolve: got %q, want %q", got, want)
	}
}
