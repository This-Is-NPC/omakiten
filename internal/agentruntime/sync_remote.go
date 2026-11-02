package agentruntime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"omakiten/internal/app"
	"omakiten/internal/config"
	"omakiten/internal/configstore"
	"omakiten/internal/domain"
	"omakiten/internal/paths"
	"omakiten/internal/sqlite"
	"omakiten/internal/syncclient"
)

const (
	// SyncTokenEnv names the environment variable that carries the sync token.
	SyncTokenEnv = "OMAKITEN_SYNC_TOKEN"
	// syncTokenFile is the token file inside the Omakiten data directory.
	syncTokenFile = "sync.token"
)

// NewSyncRemote connects to the sync server at url with the token from
// OMAKITEN_SYNC_TOKEN or, when that is unset, from the data directory file
// sync.token. insecure allows plain http to a non-loopback host.
func NewSyncRemote(url string, insecure bool) (app.SyncRemote, error) {
	token, err := syncToken()
	if err != nil {
		return nil, err
	}
	return syncclient.New(url, token, insecure, nil)
}

func syncToken() (string, error) {
	if token := strings.TrimSpace(os.Getenv(SyncTokenEnv)); token != "" {
		return token, nil
	}
	dir, err := paths.DataDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, syncTokenFile)
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", domain.NewError(domain.ErrValidation,
			fmt.Sprintf("no sync token; set %s or write the token to %s (mode 0600)", SyncTokenEnv, path),
			map[string]any{"env": SyncTokenEnv, "path": path})
	}
	if err != nil {
		return "", err
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return "", domain.NewError(domain.ErrValidation,
			fmt.Sprintf("%s is readable by group or others; run chmod 600 %s", path, path),
			map[string]any{"path": path, "mode": fmt.Sprintf("%04o", info.Mode().Perm())})
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(raw))
	if token == "" {
		return "", domain.NewError(domain.ErrValidation, fmt.Sprintf("%s is empty", path), map[string]any{"path": path})
	}
	return token, nil
}

// syncKits resolves kit keys from project configuration files without
// building a runtime. A project without a repo-local install uses fallback.
type syncKits struct {
	store    *sqlite.Store
	cs       *configstore.Adapter
	fallback string
}

var _ app.SyncKits = syncKits{}

func (k syncKits) KitKeyOf(ctx context.Context, projectID int64) (string, error) {
	project, err := k.store.FindProjectByID(ctx, projectID)
	if err != nil {
		return "", err
	}
	return k.KitKeyAt(ctx, project.RootPath)
}

func (k syncKits) KitKeyAt(_ context.Context, root string) (string, error) {
	path, found, err := config.RepoLocalConfigFile(root)
	if err != nil {
		return "", err
	}
	if !found {
		path = k.fallback
	}
	bundle, err := k.cs.LoadBundle(path)
	if err != nil {
		return "", err
	}
	return bundle.Kit.Key, nil
}
