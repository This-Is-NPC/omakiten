// Package lifecycle — archive extraction helper for `okt update`.
//
// goreleaser publishes release assets as `okt_<OS>_<arch>.tar.gz` on
// POSIX and `.zip` on Windows (.goreleaser.yml: formats: [tar.gz] +
// format_overrides Windows -> zip). Each archive bundles the `okt`
// binary alongside LICENSE/README/CONTRIBUTING, so the update path
// must locate the binary entry inside the archive rather than treat
// the whole body as the binary.
package lifecycle

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
)

// ErrArchiveEntryMissing is returned by ExtractBinary when the desired
// entry name is absent from the archive. Callers wrap this in a coded
// domain error so the JSON envelope carries the binary name that was
// expected.
var ErrArchiveEntryMissing = errors.New("archive entry missing")

// ExtractBinary scans a release archive for entry `want` and returns
// its bytes. goos selects the archive format:
//
//   - linux/darwin → gzip + tar (single-pass stream)
//   - windows      → zip (needs ReaderAt; body buffered into memory)
//
// The returned bytes are the raw file contents — atomicSwap streams
// them into the binary path. Bytes (not io.Reader) are returned so the
// SHA256 verification step in Wave 2 can hash the same buffer without
// a tee+seek dance.
func ExtractBinary(body io.Reader, goos, want string) ([]byte, error) {
	switch goos {
	case "windows":
		return extractFromZip(body, want)
	default:
		return extractFromTarGz(body, want)
	}
}

func extractFromTarGz(body io.Reader, want string) ([]byte, error) {
	gz, err := gzip.NewReader(body)
	if err != nil {
		return nil, fmt.Errorf("open gzip: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("%w: %s", ErrArchiveEntryMissing, want)
		}
		if err != nil {
			return nil, fmt.Errorf("read tar header: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		if hdr.Name != want {
			continue
		}
		buf, err := io.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("read tar entry %s: %w", want, err)
		}
		return buf, nil
	}
}

func extractFromZip(body io.Reader, want string) ([]byte, error) {
	raw, err := io.ReadAll(body)
	if err != nil {
		return nil, fmt.Errorf("read zip body: %w", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, fmt.Errorf("open zip: %w", err)
	}
	for _, f := range zr.File {
		if f.Name != want {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("open zip entry %s: %w", want, err)
		}
		buf, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, fmt.Errorf("read zip entry %s: %w", want, err)
		}
		return buf, nil
	}
	return nil, fmt.Errorf("%w: %s", ErrArchiveEntryMissing, want)
}
