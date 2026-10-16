package agentlaunch

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"omakiten/internal/filelock"
	"omakiten/internal/paths"
)

// SessionFiles are the files of one session under the state directory: the
// prompt the harness reads, the JSONL transcript of its output, and the
// lock held while any process of the run lives.
type SessionFiles struct {
	Session    string
	Prompt     string
	Transcript string
	Lock       string
}

// Files returns the files of a session, creating their directory.
func Files(sessionID string) (SessionFiles, error) {
	dir, err := paths.StateDir()
	if err != nil {
		return SessionFiles{}, err
	}
	dir = filepath.Join(dir, "agents")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return SessionFiles{}, err
	}
	base := filepath.Join(dir, sessionID)
	return SessionFiles{Session: sessionID, Prompt: base + ".md", Transcript: base + ".jsonl", Lock: base + ".lock"}, nil
}

// lockRetry is how often Hold retries a lock another process holds for a
// moment, and lockWait how long it keeps trying.
const (
	lockRetry = 20 * time.Millisecond
	lockWait  = 10 * time.Second
)

// Lock is a session's lock, held by its supervisor and inherited by its
// harness.
type Lock struct {
	file   *os.File
	unlock func() error
}

// Release gives the lock up.
func (l *Lock) Release() {
	_ = l.unlock()
	_ = l.file.Close()
}

// Hold takes the session's lock for the life of the calling supervisor. A
// process testing liveness holds it only for an instant, so Hold retries
// until lockWait passes.
func Hold(ctx context.Context, files SessionFiles) (*Lock, error) {
	file, err := os.OpenFile(files.Lock, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(lockWait)
	for {
		unlock, locked, err := filelock.TryLock(file)
		if err != nil {
			_ = file.Close()
			return nil, err
		}
		if locked {
			return &Lock{file: file, unlock: unlock}, nil
		}
		if time.Now().After(deadline) {
			_ = file.Close()
			return nil, errors.New("another supervisor holds the session lock")
		}
		select {
		case <-ctx.Done():
			_ = file.Close()
			return nil, ctx.Err()
		case <-time.After(lockRetry):
		}
	}
}

// lockHeld reports whether a live process holds the lock at path. A
// missing lock is not held; a platform without locks counts as held, so
// no run is ever taken for dead.
func lockHeld(path string) bool {
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if errors.Is(err, fs.ErrNotExist) {
		return false
	}
	if err != nil {
		return true
	}
	defer func() { _ = file.Close() }()
	unlock, locked, err := filelock.TryLock(file)
	if err != nil {
		return true
	}
	if locked {
		_ = unlock()
	}
	return !locked
}
