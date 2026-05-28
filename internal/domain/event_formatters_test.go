package domain

import "testing"

func TestRegisterFormatter_RegistersAndResolves(t *testing.T) {
	const id = "__test.fmt.unique1"
	want := "registered-formatter-output"
	fn := func(EventRow) string { return want }

	registerFormatter(id, fn)

	got, ok := ResolveFormatter(id)
	if !ok {
		t.Fatalf("ResolveFormatter(%q) ok=false, want true", id)
	}
	if got == nil {
		t.Fatalf("ResolveFormatter(%q) returned nil fn", id)
	}
	if out := got(EventRow{}); out != want {
		t.Fatalf("resolved fn returned %q, want %q", out, want)
	}
}

func TestResolveFormatter_MissReturnsFalse(t *testing.T) {
	fn, ok := ResolveFormatter("nonexistent.test.id")
	if ok {
		t.Fatalf("ResolveFormatter miss ok=true, want false")
	}
	if fn != nil {
		t.Fatalf("ResolveFormatter miss returned non-nil fn, want nil")
	}
}

func TestRegisterFormatter_PanicsOnDuplicate(t *testing.T) {
	const id = "__test.fmt.unique2"
	registerFormatter(id, func(EventRow) string { return "first" })

	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("registerFormatter duplicate did not panic")
		}
	}()
	registerFormatter(id, func(EventRow) string { return "second" })
}
