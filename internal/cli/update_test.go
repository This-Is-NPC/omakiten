package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/lifecycle"
	"omakiten/internal/releaseverify"
	"omakiten/internal/sqlite"
	"omakiten/internal/updater"
)

// tarGzWith builds an in-memory gzipped tar containing entries. The
// production update path reads the same shape goreleaser ships, so
// the test stub must mirror it (binary + auxiliary files) instead of
// returning raw binary bytes that would mask the extract step.
func tarGzWith(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range entries {
		hdr := &tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("tar header %s: %v", name, err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatalf("tar body %s: %v", name, err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gz close: %v", err)
	}
	return buf.Bytes()
}

// stubFetcher returns Tag on every call. Err short-circuits the JSON
// path so the unhappy path can be exercised without a real network.
type stubFetcher struct {
	Tag string
	Err error
}

func (s stubFetcher) Latest(context.Context) (string, error) {
	if s.Err != nil {
		return "", s.Err
	}
	return s.Tag, nil
}

// stubDownloader serves per-asset bodies. checksums.txt is auto-
// derived from the assets map when no explicit entry is set, so the
// test author only declares the binary archive + the production
// checksum-verify path stays exercised.
type stubDownloader struct {
	Assets map[string][]byte
	Err    error
}

func (s stubDownloader) Download(_ context.Context, _, asset string) (io.ReadCloser, error) {
	if s.Err != nil {
		return nil, s.Err
	}
	if body, ok := s.Assets[asset]; ok {
		return io.NopCloser(bytes.NewReader(body)), nil
	}
	if asset == "checksums.txt" {
		return io.NopCloser(strings.NewReader(autoChecksums(s.Assets))), nil
	}
	// The signed metadata files are fetched on every apply path. Their
	// content only matters to the injected verifier, so serve a
	// placeholder here and let the tests that care about signature policy
	// live in internal/releaseverify.
	if strings.HasSuffix(asset, ".sigstore.json") || strings.HasPrefix(asset, "release-manifest-") {
		return io.NopCloser(strings.NewReader("{}")), nil
	}
	return nil, fmt.Errorf("stubDownloader: no body for %s", asset)
}

// acceptingReleaseVerifier stands in for the strict Sigstore verifier on the
// updater tests that are about swap/backup/health-check behaviour rather than
// signature policy. It still returns the archive's real digest, so the
// retained SHA-256 comparison in runUpdate stays exercised.
func acceptingReleaseVerifier() releaseVerifierFn {
	return func(_ context.Context, rel releaseverify.Release) (releaseverify.Result, error) {
		sum := sha256.Sum256(rel.Archive)
		return releaseverify.Result{
			Tag:           releaseverify.Tag(rel.Version),
			SourceCommit:  strings.Repeat("a", 40),
			ArchiveSHA256: fmt.Sprintf("%x", sum),
		}, nil
	}
}

func autoChecksums(assets map[string][]byte) string {
	var b strings.Builder
	for name, body := range assets {
		sum := sha256.Sum256(body)
		fmt.Fprintf(&b, "%x  %s\n", sum, name)
	}
	return b.String()
}

type recordingDefaultsRefresher struct {
	calls      int
	binaryPath string
	err        error
}

func (r *recordingDefaultsRefresher) fn() updateDefaultsRefresherFn {
	return func(_ context.Context, binaryPath string) error {
		r.calls++
		r.binaryPath = binaryPath
		return r.err
	}
}

func TestRunUpdate_CheckUpgradeAvailable(t *testing.T) {
	c := updateClient{
		Fetcher: stubFetcher{Tag: "0.32.0"},
		Current: "0.31.0",
	}
	res, err := runUpdate(context.Background(), c, updateInputs{Check: true})
	if err != nil {
		t.Fatalf("runUpdate: %v", err)
	}
	payload, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("payload type: got %T want map[string]any", res)
	}
	if payload["code"] != "update_available" {
		t.Fatalf("code: got %v want update_available", payload["code"])
	}
	if payload["action"] != "upgrade" {
		t.Fatalf("action: got %v want upgrade", payload["action"])
	}
	if payload["applied"] != false {
		t.Fatalf("applied: got %v want false on --check", payload["applied"])
	}
	if payload["current"] != "0.31.0" || payload["latest"] != "0.32.0" {
		t.Fatalf("current/latest: got %v/%v", payload["current"], payload["latest"])
	}
}

func TestRunUpdate_CheckUpToDate(t *testing.T) {
	c := updateClient{
		Fetcher: stubFetcher{Tag: "v0.31.0"},
		Current: "0.31.0",
	}
	res, err := runUpdate(context.Background(), c, updateInputs{Check: true})
	if err != nil {
		t.Fatalf("runUpdate: %v", err)
	}
	payload, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("payload type: got %T want map[string]any", res)
	}
	if payload["code"] != "update_not_required" {
		t.Fatalf("code: got %v want update_not_required", payload["code"])
	}
	if payload["action"] != "noop" {
		t.Fatalf("action: got %v want noop", payload["action"])
	}
}

func TestUpdateCommandCheckDoesNotWireMutableState(t *testing.T) {
	prevFactory := defaultUpdateClientFactory
	defaultUpdateClientFactory = func(string) (updateClient, error) {
		return updateClient{Fetcher: stubFetcher{Tag: "0.32.0"}, Current: "0.31.0"}, nil
	}
	t.Cleanup(func() { defaultUpdateClientFactory = prevFactory })

	root := filepath.Join(t.TempDir(), "omakiten")
	dbPath := filepath.Join(root, "data", "omakiten.db")
	cfgPath := filepath.Join(root, "config", "omakase.yaml")
	out := runCLI(t, dbPath, cfgPath, "update", "--check")
	envelope := decodeEnvelope(t, out)
	data := envelope["data"].(map[string]any)
	if data["code"] != "update_available" || data["applied"] != false {
		t.Fatalf("check payload = %v, want update_available applied=false", data)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("update --check created mutable state under %s; stat err=%v", root, err)
	}
}

type updateCommandRefusalCase struct {
	current string
	latest  string
	goos    string
	wantErr bool
}

func TestRunUpdateCommandRefusedPathsDoNotTouchDatabase(t *testing.T) {
	previousFactory := defaultUpdateClientFactory
	previousGOOS := currentGOOS
	t.Cleanup(func() {
		defaultUpdateClientFactory = previousFactory
		currentGOOS = previousGOOS
	})

	cases := map[string]updateCommandRefusalCase{
		"noop":      {current: "0.31.0", latest: "0.31.0"},
		"dev build": {current: "dev", latest: "0.32.0", wantErr: true},
		"windows":   {current: "0.31.0", latest: "0.32.0", goos: "windows", wantErr: true},
		"refused":   {current: "0.29.0", latest: "0.30.0", wantErr: true},
	}
	for name, tc := range cases {
		for _, existing := range []bool{false, true} {
			label := "missing"
			if existing {
				label = "existing"
			}
			t.Run(name+"/"+label, func(t *testing.T) {
				runUpdateCommandRefusalCase(t, tc, existing, previousGOOS)
			})
		}
	}
}

func runUpdateCommandRefusalCase(t *testing.T, tc updateCommandRefusalCase, existing bool, defaultGOOS string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "state")
	dbPath := filepath.Join(root, "data", "omakiten.db")
	configPath := filepath.Join(root, "config", "omakase.yaml")
	before := []byte(nil)
	if existing {
		store, err := sqlite.Open(context.Background(), dbPath)
		if err != nil {
			t.Fatalf("seed sqlite.Open: %v", err)
		}
		if err := store.Close(); err != nil {
			t.Fatalf("seed sqlite.Close: %v", err)
		}
		var readErr error
		before, readErr = os.ReadFile(dbPath)
		if readErr != nil {
			t.Fatalf("read seeded database: %v", readErr)
		}
	}

	currentGOOS = tc.goos
	if currentGOOS == "" {
		currentGOOS = defaultGOOS
	}
	defaultUpdateClientFactory = func(string) (updateClient, error) {
		return updateClient{Fetcher: stubFetcher{Tag: tc.latest}, Current: tc.current}, nil
	}
	cmd := &cobra.Command{Use: "root"}
	_, runErr := runUpdateCommand(context.Background(), cmd, &runtimeOptions{dbPath: dbPath, configPath: configPath}, updateInputs{SkipDefaults: true})
	if (runErr != nil) != tc.wantErr {
		t.Fatalf("runUpdateCommand error = %v, wantErr=%t", runErr, tc.wantErr)
	}
	if existing {
		after, err := os.ReadFile(dbPath)
		if err != nil {
			t.Fatalf("read database after refusal: %v", err)
		}
		if !bytes.Equal(after, before) {
			t.Fatal("refused/noop update mutated the existing database")
		}
		return
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("refused/noop update created missing database state; stat error = %v", err)
	}
}

func TestPrepareUpdateDiscoveryDoesNotCreateMissingDatabase(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "missing", "nested", "omakiten.db")
	opts := &runtimeOptions{dbPath: dbPath, project: "project"}

	if err := prepareUpdateDiscovery(context.Background(), opts); err == nil {
		t.Fatal("prepareUpdateDiscovery accepted a missing database")
	}
	if _, err := os.Stat(filepath.Dir(dbPath)); !os.IsNotExist(err) {
		t.Fatalf("read-only discovery created missing parent directory; stat error = %v", err)
	}
}

func TestRunUpdate_NoopWhenCurrent(t *testing.T) {
	c := updateClient{
		Fetcher: stubFetcher{Tag: "0.31.0"},
		Current: "0.31.0",
	}
	res, err := runUpdate(context.Background(), c, updateInputs{Yes: true})
	if err != nil {
		t.Fatalf("runUpdate: %v", err)
	}
	payload, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("payload type: got %T want map[string]any", res)
	}
	if payload["code"] != "update_not_required" {
		t.Fatalf("code: got %v want update_not_required", payload["code"])
	}
}

func TestRunUpdate_YesSwapsBinary(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("posix-only path: zip+tar archive shape differs on Windows")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "okt")
	if err := os.WriteFile(bin, []byte("OLD"), 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}

	archive := tarGzWith(t, map[string][]byte{
		"okt":             []byte("NEW_INNER_BINARY"),
		"LICENSE":         []byte("MIT"),
		"README.md":       []byte("readme"),
		"CONTRIBUTING.md": []byte("contrib"),
	})
	asset, err := updater.AssetName(goruntime.GOOS, goruntime.GOARCH)
	if err != nil {
		t.Fatalf("updater.AssetName: %v", err)
	}
	c := updateClient{
		Fetcher:         stubFetcher{Tag: "0.32.0"},
		Downloader:      stubDownloader{Assets: map[string][]byte{asset: archive}},
		Current:         "0.31.0",
		BinaryPath:      bin,
		ReleaseVerifier: acceptingReleaseVerifier(),
	}
	res, err := runUpdate(context.Background(), c, updateInputs{Yes: true})
	if err != nil {
		t.Fatalf("runUpdate: %v", err)
	}
	payload, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("payload type: got %T want map[string]any", res)
	}
	if payload["code"] != "update_completed" {
		t.Fatalf("code: got %v want update_completed", payload["code"])
	}
	if payload["applied"] != true {
		t.Fatalf("applied: got %v want true", payload["applied"])
	}
	got, err := os.ReadFile(bin)
	if err != nil {
		t.Fatalf("read bin: %v", err)
	}
	if string(got) != "NEW_INNER_BINARY" {
		t.Fatalf("swap bytes: got %q want NEW_INNER_BINARY (raw archive bytes would indicate extract step is missing)", string(got))
	}
	info, _ := os.Stat(bin)
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("swap perms: got %v want 0755", info.Mode().Perm())
	}
}

func TestRunUpdate_YesRefreshesDefaultsAfterSwap(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("posix-only path: tar archive shape")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "okt")
	if err := os.WriteFile(bin, []byte("OLD"), 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	archive := tarGzWith(t, map[string][]byte{"okt": []byte("NEW")})
	asset, err := updater.AssetName(goruntime.GOOS, goruntime.GOARCH)
	if err != nil {
		t.Fatalf("updater.AssetName: %v", err)
	}
	refresher := &recordingDefaultsRefresher{}
	c := updateClient{
		Fetcher:           stubFetcher{Tag: "0.32.0"},
		Downloader:        stubDownloader{Assets: map[string][]byte{asset: archive}},
		Current:           "0.31.0",
		BinaryPath:        bin,
		DefaultsRefresher: refresher.fn(),
		ReleaseVerifier:   acceptingReleaseVerifier(),
	}

	res, err := runUpdate(context.Background(), c, updateInputs{Yes: true})
	if err != nil {
		t.Fatalf("runUpdate: %v", err)
	}
	if refresher.calls != 1 {
		t.Fatalf("defaults refresher calls = %d, want 1", refresher.calls)
	}
	if refresher.binaryPath != bin {
		t.Fatalf("defaults refresher binary path = %q, want %q", refresher.binaryPath, bin)
	}
	if got, _ := os.ReadFile(bin); string(got) != "NEW" {
		t.Fatalf("bin = %q want NEW before defaults refresh runs", got)
	}
	payload, _ := res.(map[string]any)
	if payload["defaults_refreshed"] != true {
		t.Fatalf("defaults_refreshed = %v, want true", payload["defaults_refreshed"])
	}
}

func TestRunUpdate_PostSwapRefreshReplacesManagedFilesAndPreservesCustomFiles(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("posix-only path: tar archive shape")
	}
	dir := t.TempDir()
	root := filepath.Join(dir, "config-root")
	if err := config.EnsureDefaultFiles(root); err != nil {
		t.Fatalf("EnsureDefaultFiles(): %v", err)
	}
	configPath := filepath.Join(root, "config", "omakase.yaml")
	if err := os.WriteFile(configPath, []byte("stale managed file\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(managed): %v", err)
	}
	customPath := filepath.Join(root, "config", "custom", "user.yaml")
	if err := os.WriteFile(customPath, []byte("user-owned file\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(custom): %v", err)
	}
	bin := filepath.Join(dir, "okt")
	if err := os.WriteFile(bin, []byte("OLD"), 0o755); err != nil {
		t.Fatalf("seed binary: %v", err)
	}
	archive := tarGzWith(t, map[string][]byte{"okt": []byte("NEW")})
	asset, err := updater.AssetName(goruntime.GOOS, goruntime.GOARCH)
	if err != nil {
		t.Fatalf("updater.AssetName: %v", err)
	}
	c := updateClient{
		Fetcher:    stubFetcher{Tag: "0.32.0"},
		Downloader: stubDownloader{Assets: map[string][]byte{asset: archive}},
		Current:    "0.31.0",
		BinaryPath: bin,
		ConfigPath: configPath,
		DefaultsRefresher: func(context.Context, string) error {
			return config.RefreshDefaultFiles(root)
		},
		ReleaseVerifier: acceptingReleaseVerifier(),
	}
	if _, err := runUpdate(context.Background(), c, updateInputs{Yes: true}); err != nil {
		t.Fatalf("runUpdate: %v", err)
	}
	if got, err := os.ReadFile(configPath); err != nil {
		t.Fatalf("ReadFile(managed): %v", err)
	} else if !bytes.Contains(got, []byte("merge_from: ./modules/base-config.yaml")) {
		t.Fatalf("post-swap refresh did not restore managed default: %q", got)
	}
	if got, err := os.ReadFile(customPath); err != nil {
		t.Fatalf("ReadFile(custom): %v", err)
	} else if string(got) != "user-owned file\n" {
		t.Fatalf("post-swap refresh changed custom file: %q", got)
	}
}

func TestRunUpdate_SkipDefaultsSkipsPostSwapRefresh(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("posix-only path: tar archive shape")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "okt")
	if err := os.WriteFile(bin, []byte("OLD"), 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	archive := tarGzWith(t, map[string][]byte{"okt": []byte("NEW")})
	asset, err := updater.AssetName(goruntime.GOOS, goruntime.GOARCH)
	if err != nil {
		t.Fatalf("updater.AssetName: %v", err)
	}
	refresher := &recordingDefaultsRefresher{}
	c := updateClient{
		Fetcher:           stubFetcher{Tag: "0.32.0"},
		Downloader:        stubDownloader{Assets: map[string][]byte{asset: archive}},
		Current:           "0.31.0",
		BinaryPath:        bin,
		DefaultsRefresher: refresher.fn(),
		ReleaseVerifier:   acceptingReleaseVerifier(),
	}

	res, err := runUpdate(context.Background(), c, updateInputs{Yes: true, SkipDefaults: true})
	if err != nil {
		t.Fatalf("runUpdate: %v", err)
	}
	if refresher.calls != 0 {
		t.Fatalf("defaults refresher calls = %d, want 0 with --skip-defaults", refresher.calls)
	}
	payload, _ := res.(map[string]any)
	if payload["defaults_refreshed"] != false || payload["defaults_refresh_skipped"] != true {
		t.Fatalf("defaults payload = refreshed:%v skipped:%v, want false/true", payload["defaults_refreshed"], payload["defaults_refresh_skipped"])
	}
}

func TestRunUpdate_DefaultsRefreshFailureReportsAppliedBinary(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("posix-only path: tar archive shape")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "okt")
	if err := os.WriteFile(bin, []byte("OLD"), 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	archive := tarGzWith(t, map[string][]byte{"okt": []byte("NEW")})
	asset, err := updater.AssetName(goruntime.GOOS, goruntime.GOARCH)
	if err != nil {
		t.Fatalf("updater.AssetName: %v", err)
	}
	refresher := &recordingDefaultsRefresher{err: errors.New("refresh subprocess failed")}
	c := updateClient{
		Fetcher:           stubFetcher{Tag: "0.32.0"},
		Downloader:        stubDownloader{Assets: map[string][]byte{asset: archive}},
		Current:           "0.31.0",
		BinaryPath:        bin,
		DefaultsRefresher: refresher.fn(),
		ReleaseVerifier:   acceptingReleaseVerifier(),
	}

	_, err = runUpdate(context.Background(), c, updateInputs{Yes: true})
	if err == nil {
		t.Fatalf("expected defaults refresh failure")
	}
	if refresher.calls != 1 {
		t.Fatalf("defaults refresher calls = %d, want 1", refresher.calls)
	}
	if got, _ := os.ReadFile(bin); string(got) != "NEW" {
		t.Fatalf("binary should already be updated after defaults failure, got %q", got)
	}
	var coded *domain.CodedError
	if !errors.As(err, &coded) || coded.Code != domain.ErrUpdateFailed {
		t.Fatalf("error = %v, want ErrUpdateFailed", err)
	}
	if coded.Details["reason"] != "defaults_refresh_failed" {
		t.Fatalf("reason = %v, want defaults_refresh_failed", coded.Details["reason"])
	}
	if coded.Details["applied"] != true {
		t.Fatalf("applied = %v, want true because binary swap already completed", coded.Details["applied"])
	}
	if coded.Details["manual_command"] != updateDefaultsManualCommand {
		t.Fatalf("manual_command = %v, want %s", coded.Details["manual_command"], updateDefaultsManualCommand)
	}
}

func TestRunUpdate_DefaultsRefreshFailureReportsExactConfigRepairCommand(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("posix-only path: tar archive shape")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "okt")
	if err := os.WriteFile(bin, []byte("OLD"), 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	archive := tarGzWith(t, map[string][]byte{"okt": []byte("NEW")})
	asset, err := updater.AssetName(goruntime.GOOS, goruntime.GOARCH)
	if err != nil {
		t.Fatalf("updater.AssetName: %v", err)
	}
	configPath := filepath.Join(dir, "with space", "config", "omakase.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "with space", "config", ".active"), []byte("omakase.yaml\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	refresher := &recordingDefaultsRefresher{err: errors.New("refresh subprocess failed")}
	c := updateClient{
		Fetcher:                   stubFetcher{Tag: "0.32.0"},
		Downloader:                stubDownloader{Assets: map[string][]byte{asset: archive}},
		Current:                   "0.31.0",
		BinaryPath:                bin,
		DefaultsRefresher:         refresher.fn(),
		DefaultsRefreshConfigPath: configPath,
		ReleaseVerifier:           acceptingReleaseVerifier(),
	}

	_, err = runUpdate(context.Background(), c, updateInputs{Yes: true})
	if err == nil {
		t.Fatalf("expected defaults refresh failure")
	}
	var coded *domain.CodedError
	if !errors.As(err, &coded) || coded.Code != domain.ErrUpdateFailed {
		t.Fatalf("error = %v, want ErrUpdateFailed", err)
	}
	wantCommand := "okt --config '" + configPath + "' config refresh-defaults"
	if coded.Details["manual_command"] != wantCommand {
		t.Fatalf("manual_command = %v, want %s", coded.Details["manual_command"], wantCommand)
	}
	if coded.Details["config_path"] != configPath {
		t.Fatalf("config_path = %v, want %s", coded.Details["config_path"], configPath)
	}
}

func TestRunUpdate_DefaultsRefreshFailureUsesSetupForUnusableConfigRoot(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("posix-only path: tar archive shape")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "okt")
	if err := os.WriteFile(bin, []byte("OLD"), 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	archive := tarGzWith(t, map[string][]byte{"okt": []byte("NEW")})
	asset, err := updater.AssetName(goruntime.GOOS, goruntime.GOARCH)
	if err != nil {
		t.Fatalf("updater.AssetName: %v", err)
	}
	configPath := filepath.Join(dir, "missing-root", "config", "omakase.yaml")
	c := updateClient{
		Fetcher:                   stubFetcher{Tag: "0.32.0"},
		Downloader:                stubDownloader{Assets: map[string][]byte{asset: archive}},
		Current:                   "0.31.0",
		BinaryPath:                bin,
		DefaultsRefresher:         (&recordingDefaultsRefresher{err: errors.New("refresh subprocess failed")}).fn(),
		DefaultsRefreshConfigPath: configPath,
		ReleaseVerifier:           acceptingReleaseVerifier(),
	}

	_, err = runUpdate(context.Background(), c, updateInputs{Yes: true})
	if err == nil {
		t.Fatal("expected defaults refresh failure")
	}
	var coded *domain.CodedError
	if !errors.As(err, &coded) || coded.Code != domain.ErrUpdateFailed {
		t.Fatalf("error = %v, want ErrUpdateFailed", err)
	}
	if coded.Details["manual_command"] != "okt setup" {
		t.Fatalf("manual_command = %v, want okt setup", coded.Details["manual_command"])
	}
}

func TestRunUpdate_ExtractFailureSurfacesCodedError(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("posix-only path: tar archive shape")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "okt")
	if err := os.WriteFile(bin, []byte("OLD"), 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Archive missing the `okt` entry: extract should fail before swap.
	archive := tarGzWith(t, map[string][]byte{"LICENSE": []byte("MIT")})
	asset, err := updater.AssetName(goruntime.GOOS, goruntime.GOARCH)
	if err != nil {
		t.Fatalf("updater.AssetName: %v", err)
	}
	c := updateClient{
		Fetcher:         stubFetcher{Tag: "0.32.0"},
		Downloader:      stubDownloader{Assets: map[string][]byte{asset: archive}},
		Current:         "0.31.0",
		BinaryPath:      bin,
		ReleaseVerifier: acceptingReleaseVerifier(),
	}
	_, err = runUpdate(context.Background(), c, updateInputs{Yes: true})
	if err == nil {
		t.Fatalf("expected error for archive missing binary entry")
	}
	var coded *domain.CodedError
	if !errors.As(err, &coded) {
		t.Fatalf("error type: got %T want *domain.CodedError", err)
	}
	if coded.Code != domain.ErrUpdateFailed {
		t.Fatalf("code: got %s want %s", coded.Code, domain.ErrUpdateFailed)
	}

	got, _ := os.ReadFile(bin)
	if string(got) != "OLD" {
		t.Fatalf("binary should remain unchanged on extract failure: got %q", string(got))
	}

	_ = lifecycle.ErrArchiveEntryMissing
}

func TestRunUpdate_FetchFailureSurfacesCodedError(t *testing.T) {
	c := updateClient{
		Fetcher: stubFetcher{Err: errors.New("dial tcp: no route to host")},
		Current: "0.31.0",
	}
	_, err := runUpdate(context.Background(), c, updateInputs{Check: true})
	if err == nil {
		t.Fatalf("runUpdate: expected error")
	}
	var coded *domain.CodedError
	if !errors.As(err, &coded) {
		t.Fatalf("error type: got %T want *domain.CodedError", err)
	}
	if coded.Code != domain.ErrUpdateFailed {
		t.Fatalf("code: got %s want %s", coded.Code, domain.ErrUpdateFailed)
	}
}

func TestRunUpdate_DownloadFailureSurfacesCodedError(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("currentGOOS swap exercises posix path")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "okt")
	if err := os.WriteFile(bin, []byte("OLD"), 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	c := updateClient{
		Fetcher:         stubFetcher{Tag: "0.32.0"},
		Downloader:      stubDownloader{Err: errors.New("502 bad gateway")},
		Current:         "0.31.0",
		BinaryPath:      bin,
		ReleaseVerifier: acceptingReleaseVerifier(),
	}
	_, err := runUpdate(context.Background(), c, updateInputs{Yes: true})
	if err == nil {
		t.Fatalf("expected error")
	}
	var coded *domain.CodedError
	if !errors.As(err, &coded) || coded.Code != domain.ErrUpdateFailed {
		t.Fatalf("error: got %v want ErrUpdateFailed", err)
	}
	if got, _ := os.ReadFile(bin); string(got) != "OLD" {
		t.Fatalf("binary mutated on download failure: %q", string(got))
	}
}

func TestRunUpdate_ChecksumMismatchAborts(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("posix archive shape")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "okt")
	if err := os.WriteFile(bin, []byte("OLD"), 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	archive := tarGzWith(t, map[string][]byte{"okt": []byte("PWNED")})
	asset, err := updater.AssetName(goruntime.GOOS, goruntime.GOARCH)
	if err != nil {
		t.Fatalf("updater.AssetName: %v", err)
	}
	// The authenticated digest disagrees with the bytes actually on the
	// wire. runUpdate re-checks SHA-256 against the verified expectation,
	// so this defence-in-depth comparison must still abort the swap.
	c := updateClient{
		Fetcher: stubFetcher{Tag: "0.32.0"},
		Downloader: stubDownloader{Assets: map[string][]byte{
			asset: archive,
		}},
		Current:    "0.31.0",
		BinaryPath: bin,
		ReleaseVerifier: func(_ context.Context, rel releaseverify.Release) (releaseverify.Result, error) {
			return releaseverify.Result{
				Tag:           releaseverify.Tag(rel.Version),
				ArchiveSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte("DIFFERENT_BINARY"))),
			}, nil
		},
	}
	_, err = runUpdate(context.Background(), c, updateInputs{Yes: true})
	if err == nil {
		t.Fatalf("expected checksum mismatch error")
	}
	var coded *domain.CodedError
	if !errors.As(err, &coded) || coded.Code != domain.ErrUpdateFailed {
		t.Fatalf("error: got %v want ErrUpdateFailed", err)
	}
	if !strings.Contains(coded.Message, "checksum mismatch") {
		t.Fatalf("message should mention checksum mismatch: %q", coded.Message)
	}
	if got, _ := os.ReadFile(bin); string(got) != "OLD" {
		t.Fatalf("binary mutated on checksum mismatch: %q", string(got))
	}
}

// TestRunUpdate_SignatureFailureAbortsBeforeExtract proves the strict path
// fails closed: when the release verifier rejects the download, the update
// stops before the archive is extracted and the installed binary is left byte
// for byte as it was.
func TestRunUpdate_SignatureFailureAbortsBeforeExtract(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("posix archive shape")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "okt")
	if err := os.WriteFile(bin, []byte("OLD"), 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	archive := tarGzWith(t, map[string][]byte{"okt": []byte("PWNED")})
	asset, err := updater.AssetName(goruntime.GOOS, goruntime.GOARCH)
	if err != nil {
		t.Fatalf("updater.AssetName: %v", err)
	}
	c := updateClient{
		Fetcher:    stubFetcher{Tag: "0.32.0"},
		Downloader: stubDownloader{Assets: map[string][]byte{asset: archive}},
		Current:    "0.31.0",
		BinaryPath: bin,
		ReleaseVerifier: func(context.Context, releaseverify.Release) (releaseverify.Result, error) {
			return releaseverify.Result{}, errors.New("sigstore bundle has no Rekor inclusion proof")
		},
	}
	_, err = runUpdate(context.Background(), c, updateInputs{Yes: true})
	if err == nil {
		t.Fatalf("expected signature verification error")
	}
	var coded *domain.CodedError
	if !errors.As(err, &coded) || coded.Code != domain.ErrUpdateFailed {
		t.Fatalf("error: got %v want ErrUpdateFailed", err)
	}
	if !strings.Contains(coded.Message, "Rekor inclusion proof") {
		t.Fatalf("message should carry the verifier cause: %q", coded.Message)
	}
	if got, _ := os.ReadFile(bin); string(got) != "OLD" {
		t.Fatalf("binary mutated on verification failure: %q", string(got))
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("verification failure left staged files behind: %v", entries)
	}
}

// TestRunUpdate_MissingVerifierRefusesUpdate proves there is no unsigned
// fallback: a client without a verifier aborts rather than trusting
// checksum-over-TLS.
func TestRunUpdate_MissingVerifierRefusesUpdate(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "okt")
	if err := os.WriteFile(bin, []byte("OLD"), 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	c := updateClient{
		Fetcher:    stubFetcher{Tag: "0.32.0"},
		Downloader: stubDownloader{Assets: map[string][]byte{}},
		Current:    "0.31.0",
		BinaryPath: bin,
	}
	_, err := runUpdate(context.Background(), c, updateInputs{Yes: true})
	if err == nil {
		t.Fatalf("expected refusal without a release verifier")
	}
	if got, _ := os.ReadFile(bin); string(got) != "OLD" {
		t.Fatalf("binary mutated without a verifier: %q", string(got))
	}
}

// TestRunUpdate_RefusesDowngradeAndLegacyTargets covers the version policy
// gates: a rollback target and a target at or before the signed-release
// cutoff are both refused before anything is downloaded.
func TestRunUpdate_RefusesDowngradeAndLegacyTargets(t *testing.T) {
	cases := []struct {
		name    string
		current string
		latest  string
		wantSub string
	}{
		{name: "downgrade", current: "0.32.0", latest: "0.31.0", wantSub: "downgrade"},
		{name: "legacy cutoff", current: "0.29.0", latest: "0.30.0", wantSub: "0.30.0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertUpdateRefusal(t, tc)
		})
	}
}

func assertUpdateRefusal(t *testing.T, tc struct {
	name    string
	current string
	latest  string
	wantSub string
}) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "okt")
	if err := os.WriteFile(bin, []byte("OLD"), 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	c := updateClient{Fetcher: stubFetcher{Tag: tc.latest}, Downloader: stubDownloader{Err: errors.New("network must not be touched")}, Current: tc.current, BinaryPath: bin, ReleaseVerifier: acceptingReleaseVerifier()}
	_, err := runUpdate(context.Background(), c, updateInputs{Yes: true})
	if err == nil {
		t.Fatalf("expected refusal for %s", tc.name)
	}
	var coded *domain.CodedError
	if !errors.As(err, &coded) || coded.Code != domain.ErrUpdateFailed {
		t.Fatalf("error: got %v want ErrUpdateFailed", err)
	}
	if !strings.Contains(coded.Message, tc.wantSub) {
		t.Fatalf("message %q does not mention %q", coded.Message, tc.wantSub)
	}
	if got, _ := os.ReadFile(bin); string(got) != "OLD" {
		t.Fatalf("binary mutated on refusal: %q", got)
	}
}

// TestRunUpdate_FetchesEveryPublishedMetadataAsset pins the exact artifact
// names #1972 emits, so a producer-side rename cannot silently degrade the
// consumer to a partially verified update.
func TestRunUpdate_FetchesEveryPublishedMetadataAsset(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("posix archive shape")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "okt")
	if err := os.WriteFile(bin, []byte("OLD"), 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	archive := tarGzWith(t, map[string][]byte{"okt": []byte("NEW")})
	asset, err := updater.AssetName(goruntime.GOOS, goruntime.GOARCH)
	if err != nil {
		t.Fatalf("updater.AssetName: %v", err)
	}
	recorder := &recordingDownloader{inner: stubDownloader{Assets: map[string][]byte{asset: archive}}}

	var seen releaseverify.Release
	c := updateClient{
		Fetcher:    stubFetcher{Tag: "0.32.0"},
		Downloader: recorder,
		Current:    "0.31.0",
		BinaryPath: bin,
		ReleaseVerifier: func(ctx context.Context, rel releaseverify.Release) (releaseverify.Result, error) {
			seen = rel
			return acceptingReleaseVerifier()(ctx, rel)
		},
	}
	if _, err := runUpdate(context.Background(), c, updateInputs{Yes: true}); err != nil {
		t.Fatalf("runUpdate: %v", err)
	}
	for _, want := range []string{
		asset,
		"checksums.txt",
		"release-manifest-v0.32.0.json",
		"release-manifest-v0.32.0.sigstore.json",
		"checksums-v0.32.0.sigstore.json",
		"release-provenance-v0.32.0.sigstore.json",
	} {
		if !recorder.asked[want] {
			t.Errorf("updater never requested %s (asked: %v)", want, recorder.asked)
		}
	}
	if seen.Repository != updateRepo || seen.Version != "0.32.0" || seen.ArchiveName != asset {
		t.Errorf("verifier input = %s/%s/%s, want %s/0.32.0/%s", seen.Repository, seen.Version, seen.ArchiveName, updateRepo, asset)
	}
	for name, data := range map[string][]byte{
		"archive":           seen.Archive,
		"checksums":         seen.Checksums,
		"manifest":          seen.Manifest,
		"manifest bundle":   seen.ManifestBundle,
		"checksums bundle":  seen.ChecksumsBundle,
		"provenance bundle": seen.ProvenanceBundle,
	} {
		if len(data) == 0 {
			t.Errorf("verifier received an empty %s", name)
		}
	}
}

// recordingDownloader notes every asset name the updater asks for.
type recordingDownloader struct {
	inner stubDownloader
	asked map[string]bool
}

func (r *recordingDownloader) Download(ctx context.Context, tag, asset string) (io.ReadCloser, error) {
	if r.asked == nil {
		r.asked = map[string]bool{}
	}
	r.asked[asset] = true
	return r.inner.Download(ctx, tag, asset)
}

func TestRunUpdate_WindowsRefused(t *testing.T) {
	prev := currentGOOS
	currentGOOS = "windows"
	t.Cleanup(func() { currentGOOS = prev })

	c := updateClient{Fetcher: stubFetcher{Tag: "0.32.0"}, Current: "0.31.0"}
	_, err := runUpdate(context.Background(), c, updateInputs{Yes: true})
	if err == nil {
		t.Fatalf("expected windows-unsupported error")
	}
	var coded *domain.CodedError
	if !errors.As(err, &coded) || coded.Code != domain.ErrUpdateFailed {
		t.Fatalf("error: got %v want ErrUpdateFailed", err)
	}
}

func TestRunUpdate_DevBuildRefused(t *testing.T) {
	cases := []string{"", "dev", "  "}
	for _, current := range cases {
		c := updateClient{Fetcher: stubFetcher{Tag: "0.32.0"}, Current: current}
		_, err := runUpdate(context.Background(), c, updateInputs{Check: true})
		if err == nil {
			t.Fatalf("dev build %q: expected error", current)
		}
		var coded *domain.CodedError
		if !errors.As(err, &coded) || coded.Code != domain.ErrValidation {
			t.Fatalf("dev build %q: error code got %v want ErrValidation", current, err)
		}
	}
}

func TestNormalizeVersion_StripsV(t *testing.T) {
	cases := []struct{ in, want string }{
		{"v0.31.0", "0.31.0"},
		{"0.31.0", "0.31.0"},
		{"  v1.2.3  ", "1.2.3"},
		{"", ""},
		{"dev", "dev"},
	}
	for _, c := range cases {
		if got := normalizeVersion(c.in); got != c.want {
			t.Errorf("normalizeVersion(%q): got %q want %q", c.in, got, c.want)
		}
	}
}

func TestUpdateConfirm_AcceptOnY(t *testing.T) {
	model := updateConfirmModel{current: "0.31.0", latest: "0.32.0"}
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	final, ok := updated.(updateConfirmModel)
	if !ok {
		t.Fatalf("model type: got %T want updateConfirmModel", updated)
	}
	if !final.accepted || final.declined {
		t.Fatalf("accepted=%v declined=%v want accepted=true", final.accepted, final.declined)
	}
}

func TestUpdateConfirm_DeclineOnN(t *testing.T) {
	model := updateConfirmModel{current: "0.31.0", latest: "0.32.0"}
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	final, ok := updated.(updateConfirmModel)
	if !ok {
		t.Fatalf("model type: got %T want updateConfirmModel", updated)
	}
	if final.accepted || !final.declined {
		t.Fatalf("accepted=%v declined=%v want declined=true", final.accepted, final.declined)
	}
}

// TestRunUpdate_AssetTooLargeAborts pins the download size cap. A
// compromised CDN or MITM that streams a payload above updater.MaxAssetSize
// must be rejected with a coded ErrUpdateFailed before the SHA256
// verify step (which would itself buffer the entire body in memory).
// The fake downloader returns updater.MaxAssetSize+1 bytes of garbage so the
// LimitReader trips before checksum compare runs.
func TestRunUpdate_AssetTooLargeAborts(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("posix archive shape")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "okt")
	if err := os.WriteFile(bin, []byte("OLD"), 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	oversized := bytes.Repeat([]byte{0x42}, int(updater.MaxAssetSize)+1)
	asset, err := updater.AssetName(goruntime.GOOS, goruntime.GOARCH)
	if err != nil {
		t.Fatalf("updater.AssetName: %v", err)
	}
	c := updateClient{
		Fetcher: stubFetcher{Tag: "0.32.0"},
		Downloader: stubDownloader{Assets: map[string][]byte{
			asset: oversized,
		}},
		Current:         "0.31.0",
		BinaryPath:      bin,
		ReleaseVerifier: acceptingReleaseVerifier(),
	}
	_, err = runUpdate(context.Background(), c, updateInputs{Yes: true})
	if err == nil {
		t.Fatalf("expected size-cap error")
	}
	var coded *domain.CodedError
	if !errors.As(err, &coded) || coded.Code != domain.ErrUpdateFailed {
		t.Fatalf("error: got %v want ErrUpdateFailed", err)
	}
	if !strings.Contains(coded.Message, "size cap") {
		t.Fatalf("message should mention size cap: %q", coded.Message)
	}
	if got, _ := os.ReadFile(bin); string(got) != "OLD" {
		t.Fatalf("binary mutated on size-cap rejection: %q", string(got))
	}
}

// recordingBackupFactory captures how many times the factory was
// invoked and lets the test pin a resolution error. Used to assert the
// `--check` / noop short-circuits never resolve the backup wiring
// (regression guard: pre-fix, an unresolvable BackupDir aborted
// `okt update --check` because wiring happened eagerly in RunE).
type recordingBackupFactory struct {
	runner updateBackupRunner
	err    error
	calls  int
}

func (r *recordingBackupFactory) build(context.Context) (updateBackupRunner, error) {
	r.calls++
	return r.runner, r.err
}

func TestRunUpdate_CheckSkipsBackupFactory(t *testing.T) {
	factory := &recordingBackupFactory{err: errors.New("paths.BackupDir: state home unwritable")}
	refresher := &recordingDefaultsRefresher{err: errors.New("defaults refresh should not run")}
	c := updateClient{
		Fetcher:           stubFetcher{Tag: "0.32.0"},
		Current:           "0.31.0",
		BackupFactory:     factory.build,
		DefaultsRefresher: refresher.fn(),
	}
	res, err := runUpdate(context.Background(), c, updateInputs{Check: true})
	if err != nil {
		t.Fatalf("runUpdate(--check) error = %v, want nil even when backup factory would error", err)
	}
	if factory.calls != 0 {
		t.Fatalf("BackupFactory invoked %d times on --check; want 0 (no swap → no snapshot needed)", factory.calls)
	}
	if refresher.calls != 0 {
		t.Fatalf("DefaultsRefresher invoked %d times on --check; want 0", refresher.calls)
	}
	payload, _ := res.(map[string]any)
	if payload["code"] != "update_available" {
		t.Fatalf("code: got %v want update_available", payload["code"])
	}
}

func TestUpdateDefaultsRefreshArgsIncludesConfigPath(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config", "omakase.yaml")
	args := updateDefaultsRefreshArgs(configPath)
	want := []string{"--config", configPath, "config", "refresh-defaults"}
	if fmt.Sprint(args) != fmt.Sprint(want) {
		t.Fatalf("updateDefaultsRefreshArgs = %v, want %v", args, want)
	}
}

func TestCappedOutputBufferTruncates(t *testing.T) {
	buf := newCappedOutputBuffer(5)
	if _, err := buf.Write([]byte("abcdef")); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if _, err := buf.Write([]byte("ghi")); err != nil {
		t.Fatalf("second write: %v", err)
	}
	got := buf.String()
	if !strings.HasPrefix(got, "abcde") {
		t.Fatalf("capped output = %q, want abcde prefix", got)
	}
	if !strings.Contains(got, "subprocess output truncated after 5 bytes; 4 bytes suppressed") {
		t.Fatalf("capped output missing truncation marker: %q", got)
	}
}

func TestDefaultUpdateClientWiresDefaultsRefresher(t *testing.T) {
	c, err := defaultUpdateClient("0.31.0")
	if err != nil {
		t.Fatalf("defaultUpdateClient: %v", err)
	}
	if c.DefaultsRefresher == nil {
		t.Fatalf("defaultUpdateClient must wire defaults refresher for production okt update")
	}
}

func TestRunUpdate_NoopSkipsBackupFactory(t *testing.T) {
	factory := &recordingBackupFactory{err: errors.New("BackupDir unresolvable")}
	c := updateClient{
		Fetcher:       stubFetcher{Tag: "0.31.0"},
		Current:       "0.31.0",
		BackupFactory: factory.build,
	}
	if _, err := runUpdate(context.Background(), c, updateInputs{Yes: true}); err != nil {
		t.Fatalf("runUpdate(noop) error = %v, want nil — noop path must not resolve backup", err)
	}
	if factory.calls != 0 {
		t.Fatalf("BackupFactory invoked %d times on noop; want 0", factory.calls)
	}
}

func TestRunUpdate_BackupFactoryErrorAbortsSwap(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("posix archive shape")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "okt")
	if err := os.WriteFile(bin, []byte("OLD"), 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	archive := tarGzWith(t, map[string][]byte{"okt": []byte("NEW")})
	asset, err := updater.AssetName(goruntime.GOOS, goruntime.GOARCH)
	if err != nil {
		t.Fatalf("updater.AssetName: %v", err)
	}
	factory := &recordingBackupFactory{err: errors.New("mkdir state home: permission denied")}
	c := updateClient{
		Fetcher:         stubFetcher{Tag: "0.32.0"},
		Downloader:      stubDownloader{Assets: map[string][]byte{asset: archive}},
		Current:         "0.31.0",
		BinaryPath:      bin,
		BackupFactory:   factory.build,
		ReleaseVerifier: acceptingReleaseVerifier(),
	}
	_, err = runUpdate(context.Background(), c, updateInputs{Yes: true})
	if err == nil {
		t.Fatalf("expected backup-factory error to abort the swap")
	}
	var coded *domain.CodedError
	if !errors.As(err, &coded) || coded.Code != domain.ErrUpdateFailed {
		t.Fatalf("error: got %v want ErrUpdateFailed", err)
	}
	if factory.calls != 1 {
		t.Fatalf("BackupFactory calls = %d, want 1 (swap path resolves once)", factory.calls)
	}
	if got, _ := os.ReadFile(bin); string(got) != "OLD" {
		t.Fatalf("binary mutated after backup-factory failure: %q", string(got))
	}
}
