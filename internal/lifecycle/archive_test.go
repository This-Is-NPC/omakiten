package lifecycle

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"strings"
	"testing"
)

func makeTarGz(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range entries {
		hdr := &tar.Header{
			Name:     name,
			Mode:     0o755,
			Size:     int64(len(body)),
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("write tar header %s: %v", name, err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatalf("write tar body %s: %v", name, err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	return buf.Bytes()
}

func makeZip(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create zip entry %s: %v", name, err)
		}
		if _, err := w.Write(body); err != nil {
			t.Fatalf("write zip body %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

func TestExtractBinary_TarGzReturnsInnerEntry(t *testing.T) {
	archive := makeTarGz(t, map[string][]byte{
		"okt":            []byte("INNER_BINARY"),
		"LICENSE":        []byte("MIT"),
		"README.md":      []byte("readme"),
		"CONTRIBUTING.md": []byte("contributing"),
	})
	got, err := ExtractBinary(bytes.NewReader(archive), "linux", "okt")
	if err != nil {
		t.Fatalf("ExtractBinary: %v", err)
	}
	if string(got) != "INNER_BINARY" {
		t.Fatalf("got %q want INNER_BINARY", string(got))
	}
}

func TestExtractBinary_TarGzDarwinSameAsLinux(t *testing.T) {
	archive := makeTarGz(t, map[string][]byte{"okt": []byte("DARWIN_BINARY")})
	got, err := ExtractBinary(bytes.NewReader(archive), "darwin", "okt")
	if err != nil {
		t.Fatalf("ExtractBinary darwin: %v", err)
	}
	if string(got) != "DARWIN_BINARY" {
		t.Fatalf("darwin: got %q want DARWIN_BINARY", string(got))
	}
}

func TestExtractBinary_ZipReturnsInnerEntry(t *testing.T) {
	archive := makeZip(t, map[string][]byte{
		"okt.exe": []byte("WIN_BINARY"),
		"LICENSE": []byte("MIT"),
	})
	got, err := ExtractBinary(bytes.NewReader(archive), "windows", "okt.exe")
	if err != nil {
		t.Fatalf("ExtractBinary zip: %v", err)
	}
	if string(got) != "WIN_BINARY" {
		t.Fatalf("zip: got %q want WIN_BINARY", string(got))
	}
}

func TestExtractBinary_MissingEntryTar(t *testing.T) {
	archive := makeTarGz(t, map[string][]byte{
		"LICENSE":   []byte("MIT"),
		"README.md": []byte("readme"),
	})
	_, err := ExtractBinary(bytes.NewReader(archive), "linux", "okt")
	if err == nil {
		t.Fatalf("expected error for missing entry")
	}
	if !errors.Is(err, ErrArchiveEntryMissing) {
		t.Fatalf("error type: got %v want ErrArchiveEntryMissing", err)
	}
}

func TestExtractBinary_MissingEntryZip(t *testing.T) {
	archive := makeZip(t, map[string][]byte{"LICENSE": []byte("MIT")})
	_, err := ExtractBinary(bytes.NewReader(archive), "windows", "okt.exe")
	if err == nil {
		t.Fatalf("expected error for missing entry")
	}
	if !errors.Is(err, ErrArchiveEntryMissing) {
		t.Fatalf("error type: got %v want ErrArchiveEntryMissing", err)
	}
}

func TestExtractBinary_BrokenGzip(t *testing.T) {
	_, err := ExtractBinary(strings.NewReader("not a gzip"), "linux", "okt")
	if err == nil {
		t.Fatalf("expected error for malformed gzip")
	}
}

func TestExtractBinary_BrokenZip(t *testing.T) {
	_, err := ExtractBinary(strings.NewReader("not a zip"), "windows", "okt.exe")
	if err == nil {
		t.Fatalf("expected error for malformed zip")
	}
}
