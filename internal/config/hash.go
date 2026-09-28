package config

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
)

func hashBytes(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// HashFile returns the sha256 hex digest of the file at path.
func HashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// AmbiguousPublicationError means the target may have changed before a
// durability step reported an error. Callers must inspect the current path
// before retrying or repairing it.
type AmbiguousPublicationError struct {
	Path      string
	Operation string
	Err       error
}

func (e *AmbiguousPublicationError) Error() string {
	return e.Operation + " " + e.Path + " may have published: " + e.Err.Error()
}

func (e *AmbiguousPublicationError) Unwrap() error { return e.Err }

func IsAmbiguousPublication(err error) bool {
	var target *AmbiguousPublicationError
	return errors.As(err, &target)
}
