package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
)

func TestSuccess(t *testing.T) {
	e := Success(map[string]any{"key": "value"})
	if !e.OK {
		t.Fatal("Success().OK = false")
	}
	if e.Data == nil {
		t.Fatal("Success().Data = nil")
	}
}

func TestFailure(t *testing.T) {
	e := Failure("err_code", "message", map[string]any{"detail": 1})
	if e.OK {
		t.Fatal("Failure().OK = true")
	}
	if e.Code != "err_code" {
		t.Fatalf("Failure().Code = %q, want err_code", e.Code)
	}
	if e.Message != "message" {
		t.Fatalf("Failure().Message = %q, want message", e.Message)
	}
}

func TestMarshal(t *testing.T) {
	e := Success("hello")
	data, err := Marshal(e)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var decoded Envelope
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if !decoded.OK {
		t.Fatal("decoded.OK = false")
	}
}

func TestWrite(t *testing.T) {
	var buf bytes.Buffer
	e := Success("data")
	if err := Write(&buf, e); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if !bytes.HasSuffix(buf.Bytes(), []byte("\n")) {
		t.Fatal("Write() output missing trailing newline")
	}

	// Error path: writer that always fails
	errWriter := &failWriter{}
	if err := Write(errWriter, e); err == nil {
		t.Fatal("Write() error = nil, want write error")
	}
}

type failWriter struct{}

func (w *failWriter) Write(p []byte) (n int, err error) {
	return 0, errors.New("write failed")
}
