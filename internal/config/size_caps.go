package config

import (
	"errors"
	"fmt"
	"io"
	"os"

	"omakiten/internal/domain"
)

// Size caps for bundled / on-disk config files. The reader wraps every
// os.Open + os.ReadFile with an io.LimitReader so a pathological YAML
// (or a malicious user-authored override) cannot exhaust memory on
// boot. Numbers picked from the code review's recommendation: entity
// files are the largest because skill / law / persona bodies can carry
// substantial markdown; the wiring yaml is small by construction; the
// notification cards are one-screen messages with a handful of action
// rows. Language packs match the wiring cap because the Hindi /
// Marathi packs already weigh ~100 KB and translators may add more.
const (
	MaxEntityFileBytes       int64 = 10 * 1024 * 1024
	MaxWiringFileBytes       int64 = 1 * 1024 * 1024
	MaxNotificationFileBytes int64 = 100 * 1024
	MaxLanguagePackBytes     int64 = 1 * 1024 * 1024
)

// readFileBounded mirrors os.ReadFile but caps the read at max+1 bytes
// so the function can distinguish "fits the budget" from "overran the
// budget". Returns ErrConfigTooLarge wrapped in a domain.CodedError on
// overflow.
func readFileBounded(path string, max int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	return readBounded(file, path, max)
}

// readBounded drains r up to max+1 bytes. Returns the first max bytes
// on success; a coded ErrConfigTooLarge on overflow so callers can
// surface the rule that fired.
func readBounded(r io.Reader, path string, max int64) ([]byte, error) {
	limited := io.LimitReader(r, max+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, tooLargeError(path, max)
	}
	return data, nil
}

func tooLargeError(path string, max int64) error {
	return domain.NewError(domain.ErrConfigTooLarge, fmt.Sprintf("config file %q exceeds size cap (%d bytes)", path, max), map[string]any{"path": path, "max_bytes": max})
}

// IsConfigTooLarge reports whether err is a wrapped ErrConfigTooLarge.
// Convenience helper for callers that want to branch on the rule
// without unwrapping the CodedError manually.
func IsConfigTooLarge(err error) bool {
	var coded *domain.CodedError
	if errors.As(err, &coded) {
		return coded.Code == domain.ErrConfigTooLarge
	}
	return false
}
