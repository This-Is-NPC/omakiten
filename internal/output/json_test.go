package output

import "testing"

func TestMarshalMinifiedEnvelope(t *testing.T) {
	data, err := Marshal(Success(map[string]any{"id": 42}))
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	got := string(data)
	want := `{"ok":true,"data":{"id":42}}`
	if got != want {
		t.Fatalf("Marshal() = %s, want %s", got, want)
	}
}

func TestMarshalOmitEmptyErrorFields(t *testing.T) {
	data, err := Marshal(Failure("validation_error", "bad input", nil))
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	got := string(data)
	want := `{"ok":false,"code":"validation_error","msg":"bad input"}`
	if got != want {
		t.Fatalf("Marshal() = %s, want %s", got, want)
	}
}
