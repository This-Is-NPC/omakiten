package daemon

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/filelock"
	"omakiten/internal/paths"
)

const (
	lockFile      = "serve.lock"
	discoveryFile = "serve.json"
	tokenFile     = "serve.token"
)

// Discovery is the serve.json document clients read to find the daemon.
type Discovery struct {
	URL       string `json:"url"`
	PID       int    `json:"pid"`
	Version   string `json:"version"`
	TokenFile string `json:"token_file"`
	StartedAt string `json:"started_at"`
}

// instance holds the single-instance lock and the files published under
// the state directory while the daemon runs.
type instance struct {
	dir    string
	lock   *os.File
	unlock func() error
}

// claimInstance takes the state-directory lock, or reports the running
// daemon from its discovery file.
func claimInstance(catalog *config.Catalog) (*instance, error) {
	dir, err := paths.StateDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(filepath.Join(dir, lockFile), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	unlock, locked, err := filelock.TryLock(lock)
	if err != nil {
		_ = lock.Close()
		return nil, err
	}
	if !locked {
		_ = lock.Close()
		details := map[string]any{"discovery": filepath.Join(dir, discoveryFile)}
		if running, readErr := readDiscovery(dir); readErr == nil {
			details["url"], details["pid"] = running.URL, running.PID
		}
		return nil, domain.NewError(domain.ErrValidation, catalog.Get("cli.serve.error.running"), details)
	}
	return &instance{dir: dir, lock: lock, unlock: unlock}, nil
}

// publish writes a fresh token and the discovery document.
func (i *instance) publish(url, version string) (string, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", err
	}
	token := hex.EncodeToString(secret)
	tokenPath := filepath.Join(i.dir, tokenFile)
	if err := config.WriteAtomic(tokenPath, []byte(token)); err != nil {
		return "", err
	}
	discovery, err := json.MarshalIndent(Discovery{
		URL:       url,
		PID:       os.Getpid(),
		Version:   version,
		TokenFile: tokenPath,
		StartedAt: time.Now().UTC().Format(time.RFC3339),
	}, "", "  ")
	if err != nil {
		return "", err
	}
	return token, config.WriteAtomic(filepath.Join(i.dir, discoveryFile), append(discovery, '\n'))
}

// DiscoveryPath is where a running daemon publishes serve.json.
func (i *instance) DiscoveryPath() string {
	return filepath.Join(i.dir, discoveryFile)
}

// release removes the published files, then drops the lock.
func (i *instance) release() error {
	var errs []error
	for _, name := range []string{discoveryFile, tokenFile} {
		if err := os.Remove(filepath.Join(i.dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	errs = append(errs, i.unlock(), i.lock.Close())
	return errors.Join(errs...)
}

func readDiscovery(dir string) (Discovery, error) {
	raw, err := os.ReadFile(filepath.Join(dir, discoveryFile))
	if err != nil {
		return Discovery{}, err
	}
	var discovery Discovery
	return discovery, json.Unmarshal(raw, &discovery)
}
