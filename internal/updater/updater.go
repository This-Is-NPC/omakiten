// Package updater downloads release assets and stages binary replacements.
package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// MaxAssetSize caps the per-asset download body so a compromised CDN
// or MITM cannot OOM the host by streaming an arbitrarily large
// payload before the SHA256 verify step runs. The published release
// archives sit comfortably below 50 MiB; the 256 MiB ceiling leaves
// generous headroom for future bundled assets while still rejecting
// pathological payloads.
const MaxAssetSize int64 = 256 << 20

// LatestFetcher resolves the latest published release tag (e.g.
// "0.19.0", without the "v" prefix). Injected into runUpdate so the
// test suite can return a deterministic value without hitting GitHub.
type LatestFetcher interface {
	Latest(ctx context.Context) (string, error)
}

// AssetDownloader fetches a release-asset tarball/zip and returns its
// body for atomicSwap to consume. Injected so the test suite can
// serve a tmp file instead of hitting github.com/releases/download.
type AssetDownloader interface {
	Download(ctx context.Context, tag, asset string) (io.ReadCloser, error)
}

// GitHubLatestFetcher polls
// `https://api.github.com/repos/<repo>/releases/latest` and parses the
// tag_name field. The bash installer uses the same endpoint so the
// two surfaces converge on the same release.
//
// GitHub's /releases/latest endpoint excludes drafts and prereleases
// by design, so `--check` will not flap on every RC tag. If the
// repository starts publishing prereleases through this endpoint we
// must switch to GET /releases?per_page=10 + filter `prerelease`.
type GitHubLatestFetcher struct {
	Repo string
	HTTP *http.Client
}

func (g *GitHubLatestFetcher) Latest(ctx context.Context) (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", g.Repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := g.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github api status %d", resp.StatusCode)
	}
	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	return strings.TrimPrefix(strings.TrimSpace(payload.TagName), "v"), nil
}

// GitHubAssetDownloader streams the platform-matched asset from the
// release-download URL. The body is returned untouched so atomicSwap
// can consume the tarball directly; the caller is responsible for
// closing it.
type GitHubAssetDownloader struct {
	Repo string
	HTTP *http.Client
}

func (g *GitHubAssetDownloader) Download(ctx context.Context, tag, asset string) (io.ReadCloser, error) {
	url := fmt.Sprintf("https://github.com/%s/releases/download/v%s/%s", g.Repo, tag, asset)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := g.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("download status %d", resp.StatusCode)
	}
	return resp.Body, nil
}

// AssetName maps GOOS/GOARCH to the asset filename install.sh
// constructs: `okt_<os>_<arch>.tar.gz`. The bash side capitalizes the
// OS token (`Linux`, `Darwin`) and renames the architecture so the
// goreleaser-published asset matches.
func AssetName(goos, goarch string) (string, error) {
	osTok := ""
	switch goos {
	case "linux":
		osTok = "Linux"
	case "darwin":
		osTok = "Darwin"
	case "windows":
		osTok = "Windows"
	default:
		return "", fmt.Errorf("unsupported platform: %s/%s", goos, goarch)
	}
	archTok := ""
	switch goarch {
	case "amd64":
		archTok = "x86_64"
	case "arm64":
		archTok = "arm64"
	default:
		return "", fmt.Errorf("unsupported platform: %s/%s", goos, goarch)
	}
	ext := ".tar.gz"
	if goos == "windows" {
		ext = ".zip"
	}
	return fmt.Sprintf("okt_%s_%s%s", osTok, archTok, ext), nil
}

// DownloadAsset streams one release asset into memory under the shared size
// cap. Every asset the updater consumes — the archive and the four signed
// metadata files — goes through this single reader so a compromised CDN
// cannot OOM the host with any one of them, and so no asset can be read
// without the cap.
func DownloadAsset(ctx context.Context, dl AssetDownloader, tag, asset string) ([]byte, error) {
	body, err := dl.Download(ctx, tag, asset)
	if err != nil {
		return nil, fmt.Errorf("download %s: %s", asset, err.Error())
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, MaxAssetSize+1))
	if err != nil {
		return nil, fmt.Errorf("download %s: %s", asset, err.Error())
	}
	if int64(len(data)) > MaxAssetSize {
		return nil, fmt.Errorf("asset %s exceeds %d-byte size cap", asset, MaxAssetSize)
	}
	return data, nil
}

// StageBinary writes body to a sibling tmp file next to dst with +x
// perms and returns its path. The two-step "stage then swap" split
// (#365 AC 2) lets runUpdate run the validator against the staged
// file before any atomic move clobbers the running binary: a failed
// health check removes the tmp and leaves the install untouched.
// Same-filesystem placement keeps the eventual rename atomic.
func StageBinary(dst string, body io.Reader) (string, error) {
	dir := filepath.Dir(dst)
	tmp, err := os.CreateTemp(dir, ".okt-update-*")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	if _, err := io.Copy(tmp, body); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return "", err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "", err
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		_ = os.Remove(tmpPath)
		return "", err
	}
	return tmpPath, nil
}

// SwapStagedBinary atomically renames a staged file over dst. POSIX
// same-filesystem rename is atomic; Windows callers are refused
// upstream in runUpdate so the EXE-in-use shape doesn't surface here.
func SwapStagedBinary(stagedPath, dst string) error {
	return os.Rename(stagedPath, dst)
}
