package app_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"omakiten/internal/app"
	"omakiten/internal/config"
	"omakiten/internal/testutil"
)

func TestBundleEditorRejectsStaleBundleWithoutOverwritingNewerContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "omakiten.yaml")
	store := newBundleEditorTestStore(path)
	store.beforeHash = func(gotPath string, call int) {
		if gotPath == path && call == 2 {
			store.bundle.Config.Workflow.Active = "external"
		}
	}
	editor := app.NewBundleEditor(store, path)

	bundle, _, sourceHashes, err := editor.LoadPlan()
	if err != nil {
		t.Fatal(err)
	}
	_, err = editor.Apply(context.Background(), bundle, sourceHashes, func(bundle *config.Bundle) error {
		bundle.Config.Workflow.Active = "candidate"
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "reload and retry") {
		t.Fatalf("Apply() error = %v, want stale conflict with retry guidance", err)
	}
	if store.saves != 0 {
		t.Fatalf("SaveBundle calls = %d, want 0", store.saves)
	}
	if got := store.bundle.Config.Workflow.Active; got != "external" {
		t.Fatalf("bundle after conflict = %q, want newer external content", got)
	}
}

func TestBundleEditorRejectsStaleFileWriteAndDelete(t *testing.T) {
	for name, op := range map[string]app.FileOpKind{"write": app.OpWrite, "delete": app.OpDelete} {
		t.Run(name, func(t *testing.T) {
			assertStaleFileOp(t, op)
		})
	}
}

func assertStaleFileOp(t *testing.T, kind app.FileOpKind) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "omakiten.yaml")
	filePath := filepath.Join(filepath.Dir(path), "skills", "go.md")
	store := newBundleEditorTestStore(path)
	store.files[filePath] = []byte("original")
	store.beforeHash = func(gotPath string, call int) {
		if gotPath == filePath && call == 2 {
			store.files[filePath] = []byte("newer")
		}
	}
	editor := app.NewBundleEditor(store, path)
	op := app.FileOp{Op: kind, Path: filepath.Join("skills", "go.md"), Bytes: []byte("candidate")}

	bundle, _, sourceHashes, err := editor.LoadPlanWithFiles()
	if err != nil {
		t.Fatal(err)
	}
	fileHashes := cloneHashes(sourceHashes)
	fileHashes[filePath], err = editor.FileHash(filePath)
	if err != nil {
		t.Fatal(err)
	}
	op.ExpectedHash = fileHashes[filePath]
	_, err = editor.ApplyWithFiles(context.Background(), bundle, sourceHashes, nil, []app.FileOp{op})
	if err == nil || !strings.Contains(err.Error(), "reload and retry") {
		t.Fatalf("ApplyWithFiles() error = %v, want stale conflict with retry guidance", err)
	}
	if got := string(store.files[filePath]); got != "newer" {
		t.Fatalf("file after conflict = %q, want newer external content", got)
	}
}

func TestBundleEditorReportsPartialPublicationAndSupportsRetry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "omakiten.yaml")
	firstPath := filepath.Join(filepath.Dir(path), "skills", "first.md")
	secondPath := filepath.Join(filepath.Dir(path), "skills", "second.md")
	store := newBundleEditorTestStore(path)
	store.files[firstPath] = []byte("first old")
	store.files[secondPath] = []byte("second old")
	store.beforeHash = func(gotPath string, call int) {
		if gotPath == secondPath && call == 2 {
			store.files[secondPath] = []byte("second newer")
		}
	}
	editor := app.NewBundleEditor(store, path)
	bundle, _, sourceHashes, err := editor.LoadPlanWithFiles()
	if err != nil {
		t.Fatal(err)
	}
	fileHashes := cloneHashes(sourceHashes)
	fileHashes[firstPath], err = editor.FileHash(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	fileHashes[secondPath], err = editor.FileHash(secondPath)
	if err != nil {
		t.Fatal(err)
	}
	ops := []app.FileOp{
		{Op: app.OpWrite, Path: filepath.Join("skills", "first.md"), Bytes: []byte("first candidate"), ExpectedHash: fileHashes[firstPath]},
		{Op: app.OpWrite, Path: filepath.Join("skills", "second.md"), Bytes: []byte("second candidate"), ExpectedHash: fileHashes[secondPath]},
	}
	_, err = editor.ApplyWithFiles(context.Background(), bundle, sourceHashes, nil, ops)
	if err == nil || !strings.Contains(err.Error(), "partial") ||
		!strings.Contains(err.Error(), firstPath) || !strings.Contains(err.Error(), secondPath) ||
		!strings.Contains(err.Error(), "reload") || !strings.Contains(err.Error(), "retry") || !strings.Contains(err.Error(), "repair") {
		t.Fatalf("ApplyWithFiles() error = %v, want actionable partial-state guidance", err)
	}
	if got := string(store.files[firstPath]); got != "first candidate" {
		t.Fatalf("first file after partial failure = %q, want published candidate", got)
	}
	if got := string(store.files[secondPath]); got != "second newer" {
		t.Fatalf("second file after partial failure = %q, want newer external content", got)
	}

	store.beforeHash = nil
	bundle, _, sourceHashes, err = editor.LoadPlanWithFiles()
	if err != nil {
		t.Fatal(err)
	}
	fileHashes = cloneHashes(sourceHashes)
	fileHashes[secondPath], err = editor.FileHash(secondPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := editor.ApplyWithFiles(context.Background(), bundle, sourceHashes, nil, []app.FileOp{{
		Op: app.OpWrite, Path: filepath.Join("skills", "second.md"), Bytes: []byte("second candidate"), ExpectedHash: fileHashes[secondPath]},
	}); err != nil {
		t.Fatalf("retrying unresolved file: %v", err)
	}
	if got := string(store.files[secondPath]); got != "second candidate" {
		t.Fatalf("second file after retry = %q, want repaired candidate", got)
	}
}

func TestBundleEditorRejectsFileOutsideRootBeforePublishingBundle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "omakiten.yaml")
	store := newBundleEditorTestStore(path)
	editor := app.NewBundleEditor(store, path)
	bundle, _, sourceHashes, err := editor.LoadPlan()
	if err != nil {
		t.Fatal(err)
	}

	_, err = editor.ApplyWithFiles(context.Background(), bundle, sourceHashes, nil, []app.FileOp{{
		Op: app.OpWrite, Path: filepath.Join(filepath.Dir(path), "..", "outside.md"), Bytes: []byte("no")},
	})
	if err == nil || !strings.Contains(err.Error(), "outside the bundle root") {
		t.Fatalf("ApplyWithFiles() error = %v, want path rejection", err)
	}
	if store.saves != 0 {
		t.Fatalf("SaveBundle calls = %d, want 0", store.saves)
	}
}

func TestBundleEditorReportsUnverifiedFinalReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "omakiten.yaml")
	store := newBundleEditorTestStore(path)
	store.failLoadCall = 2
	editor := app.NewBundleEditor(store, path)
	bundle, _, sourceHashes, err := editor.LoadPlan()
	if err != nil {
		t.Fatal(err)
	}

	_, err = editor.Apply(context.Background(), bundle, sourceHashes, nil)
	if err == nil || !strings.Contains(err.Error(), "final reload was not verified") ||
		strings.Contains(err.Error(), "omakiten.yaml was not safely completed") {
		t.Fatalf("Apply() error = %v, want unverified reload guidance", err)
	}
}

func TestBundleEditorReportsAmbiguousPostPublicationFileErrors(t *testing.T) {
	for name, kind := range map[string]app.FileOpKind{"write": app.OpWrite, "delete": app.OpDelete} {
		t.Run(name, func(t *testing.T) {
			assertAmbiguousPostPublicationFileError(t, name, kind)
		})
	}
}

func assertAmbiguousPostPublicationFileError(t *testing.T, name string, kind app.FileOpKind) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "omakiten.yaml")
	filePath := filepath.Join(filepath.Dir(path), "skills", "go.md")
	store := newBundleEditorTestStore(path)
	store.files[filePath] = []byte("original")
	postPublication := &config.AmbiguousPublicationError{Path: filePath, Operation: name, Err: errors.New("directory sync failed")}
	if kind == app.OpWrite {
		store.writeErr = postPublication
	} else {
		store.removeErr = postPublication
	}
	editor := app.NewBundleEditor(store, path)
	bundle, _, sourceHashes, err := editor.LoadPlanWithFiles()
	if err != nil {
		t.Fatal(err)
	}
	expected, err := editor.FileHash(filePath)
	if err != nil {
		t.Fatal(err)
	}
	op := app.FileOp{Op: kind, Path: filepath.Join("skills", "go.md"), ExpectedHash: expected, Bytes: []byte("candidate")}
	_, err = editor.ApplyWithFiles(context.Background(), bundle, sourceHashes, nil, []app.FileOp{op})
	for _, want := range []string{"ambiguous", filePath, "reload", "retry", "repair"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("ApplyWithFiles() error = %v, want %q", err, want)
		}
	}
	if kind == app.OpWrite && string(store.files[filePath]) != "candidate" {
		t.Fatalf("post-error write = %q, want candidate bytes", store.files[filePath])
	}
	if kind == app.OpDelete {
		if _, exists := store.files[filePath]; exists {
			t.Fatal("post-error delete left the target present")
		}
	}
}

func TestBundleEditorReportsAmbiguousPostPublicationWiringError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "omakiten.yaml")
	store := newBundleEditorTestStore(path)
	store.saveErr = &config.AmbiguousPublicationError{Path: path, Operation: "write", Err: errors.New("directory sync failed")}
	editor := app.NewBundleEditor(store, path)
	bundle, _, sourceHashes, err := editor.LoadPlan()
	if err != nil {
		t.Fatal(err)
	}
	_, err = editor.Apply(context.Background(), bundle, sourceHashes, nil)
	for _, want := range []string{"ambiguous", path, "reload", "retry", "repair"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("Apply() error = %v, want %q", err, want)
		}
	}
}

type bundleEditorTestStore struct {
	testutil.DirectBundleEdits
	bundle       config.Bundle
	files        map[string][]byte
	hashCalls    map[string]int
	beforeHash   func(path string, call int)
	planLoader   func(path string) (config.Bundle, map[string]string, error)
	writeErr     error
	removeErr    error
	saveErr      error
	loadCalls    int
	failLoadCall int
	saves        int
}

func newBundleEditorTestStore(path string) *bundleEditorTestStore {
	return &bundleEditorTestStore{
		bundle:    config.Bundle{Config: config.Settings{Workflow: config.WorkflowSettings{Active: "original"}}},
		files:     map[string][]byte{path: nil},
		hashCalls: map[string]int{},
	}
}

func (s *bundleEditorTestStore) LoadBundle(string) (config.Bundle, error) {
	s.loadCalls++
	if s.failLoadCall > 0 && s.loadCalls >= s.failLoadCall {
		return config.Bundle{}, errors.New("reload failed")
	}
	return s.bundle, nil
}

func (s *bundleEditorTestStore) LoadBundlePlan(path string) (config.Bundle, map[string]string, error) {
	if s.planLoader != nil {
		return s.planLoader(path)
	}
	bundle, err := s.LoadBundle(path)
	if err != nil {
		return config.Bundle{}, nil, err
	}
	hash, err := s.HashFile(path)
	if err != nil {
		return config.Bundle{}, nil, err
	}
	return bundle, map[string]string{path: hash}, nil
}

func TestBundleEditorRejectsReplacementAfterCoherentPlanCapture(t *testing.T) {
	path := filepath.Join(t.TempDir(), "omakiten.yaml")
	store := newBundleEditorTestStore(path)
	store.planLoader = func(path string) (config.Bundle, map[string]string, error) {
		planned := store.bundle
		raw, err := json.Marshal(planned)
		if err != nil {
			return config.Bundle{}, nil, err
		}
		sum := sha256.Sum256(raw)
		store.bundle.Config.Workflow.Active = "external"
		return planned, map[string]string{path: hex.EncodeToString(sum[:])}, nil
	}
	editor := app.NewBundleEditor(store, path)
	bundle, _, sourceHashes, err := editor.LoadPlan()
	if err != nil {
		t.Fatal(err)
	}
	if store.loadCalls != 0 || len(store.hashCalls) != 0 {
		t.Fatalf("legacy LoadBundle/HashFile calls = %d/%d, want 0/0", store.loadCalls, len(store.hashCalls))
	}

	_, err = editor.Apply(context.Background(), bundle, sourceHashes, func(bundle *config.Bundle) error {
		bundle.Config.Workflow.Active = "candidate"
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "reload and retry") {
		t.Fatalf("Apply() error = %v, want stale conflict after plan capture", err)
	}
	if store.saves != 0 {
		t.Fatalf("SaveBundle calls = %d, want 0", store.saves)
	}
}

func TestBundleEditorRejectsChangedImportedSourceBeforeApply(t *testing.T) {
	path := filepath.Join(t.TempDir(), "omakiten.yaml")
	importPath := filepath.Join(filepath.Dir(path), "imports", "workflow.yaml")
	store := newBundleEditorTestStore(path)
	store.files[importPath] = []byte("workflow: old\n")
	store.planLoader = func(path string) (config.Bundle, map[string]string, error) {
		planned := store.bundle
		raw, err := json.Marshal(planned)
		if err != nil {
			return config.Bundle{}, nil, err
		}
		rootSum := sha256.Sum256(raw)
		importSum := sha256.Sum256(store.files[importPath])
		store.files[importPath] = []byte("workflow: newer\n")
		return planned, map[string]string{
			path:       hex.EncodeToString(rootSum[:]),
			importPath: hex.EncodeToString(importSum[:]),
		}, nil
	}
	editor := app.NewBundleEditor(store, path)
	bundle, _, sourceHashes, err := editor.LoadPlan()
	if err != nil {
		t.Fatal(err)
	}

	_, err = editor.Apply(context.Background(), bundle, sourceHashes, nil)
	if err == nil || !strings.Contains(err.Error(), importPath) || !strings.Contains(err.Error(), "reload and retry") {
		t.Fatalf("Apply() error = %v, want imported-source conflict", err)
	}
	if store.saves != 0 {
		t.Fatalf("SaveBundle calls = %d, want 0", store.saves)
	}
}

func TestBundleEditorSerializesConcurrentApplyPlans(t *testing.T) {
	path := filepath.Join(t.TempDir(), "omakiten.yaml")
	store := &synchronizedBundleEditorStore{inner: newBundleEditorTestStore(path)}
	editor := app.NewBundleEditor(store, path)
	bundle, _, sourceHashes, err := editor.LoadPlan()
	if err != nil {
		t.Fatal(err)
	}

	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, value := range []string{"candidate-a", "candidate-b"} {
		value := value
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, applyErr := editor.Apply(context.Background(), bundle, sourceHashes, func(next *config.Bundle) error {
				next.Config.Workflow.Active = value
				return nil
			})
			errs <- applyErr
		}()
	}
	wg.Wait()
	close(errs)

	var successes, conflicts int
	for err := range errs {
		if err == nil {
			successes++
		} else if strings.Contains(err.Error(), "reload and retry") {
			conflicts++
		} else {
			t.Fatalf("concurrent Apply() error = %v, want stale conflict or success", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent Apply() results = %d successes, %d conflicts, want 1/1", successes, conflicts)
	}
}

func TestBundleEditorsShareSamePathPublicationLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "omakiten.yaml")
	aliasPath := filepath.Join(filepath.Dir(path), ".", filepath.Base(path))
	store := &synchronizedBundleEditorStore{
		inner:            newBundleEditorTestStore(path),
		firstSaveStarted: make(chan struct{}),
		releaseFirstSave: make(chan struct{}),
	}
	first := app.NewBundleEditor(store, path)
	second := app.NewBundleEditor(store, aliasPath)
	firstBundle, _, firstHashes, err := first.LoadPlan()
	if err != nil {
		t.Fatal(err)
	}
	secondBundle, _, secondHashes, err := second.LoadPlan()
	if err != nil {
		t.Fatal(err)
	}

	firstErr := make(chan error, 1)
	go func() {
		_, applyErr := first.Apply(context.Background(), firstBundle, firstHashes, func(bundle *config.Bundle) error {
			bundle.Config.Workflow.Active = "first"
			return nil
		})
		firstErr <- applyErr
	}()
	<-store.firstSaveStarted

	secondErr := make(chan error, 1)
	go func() {
		_, applyErr := second.Apply(context.Background(), secondBundle, secondHashes, func(bundle *config.Bundle) error {
			bundle.Config.Workflow.Active = "second"
			return nil
		})
		secondErr <- applyErr
	}()
	close(store.releaseFirstSave)

	if err := <-firstErr; err != nil {
		t.Fatalf("first Apply() error = %v, want success", err)
	}
	if err := <-secondErr; err == nil || !strings.Contains(err.Error(), "reload and retry") {
		t.Fatalf("second Apply() error = %v, want stale conflict", err)
	}
	if store.inner.saves != 1 {
		t.Fatalf("SaveBundle calls = %d, want 1", store.inner.saves)
	}
}

func TestBundleEditorsForDifferentProfilesSharePublicationLock(t *testing.T) {
	root := t.TempDir()
	firstPath := filepath.Join(root, "config", "omakiten.yaml")
	secondPath := filepath.Join(root, "config", "custom", "profile.yaml")
	store := &synchronizedBundleEditorStore{
		inner:            newBundleEditorTestStore(firstPath),
		firstSaveStarted: make(chan struct{}),
		releaseFirstSave: make(chan struct{}),
	}
	store.inner.files[secondPath] = nil
	first := app.NewBundleEditor(store, firstPath)
	second := app.NewBundleEditor(store, secondPath)
	firstBundle, _, firstHashes, err := first.LoadPlan()
	if err != nil {
		t.Fatal(err)
	}
	secondBundle, _, secondHashes, err := second.LoadPlan()
	if err != nil {
		t.Fatal(err)
	}

	firstErr := make(chan error, 1)
	go func() {
		_, applyErr := first.Apply(context.Background(), firstBundle, firstHashes, func(bundle *config.Bundle) error {
			bundle.Config.Workflow.Active = "first-profile"
			return nil
		})
		firstErr <- applyErr
	}()
	<-store.firstSaveStarted

	secondErr := make(chan error, 1)
	go func() {
		_, applyErr := second.Apply(context.Background(), secondBundle, secondHashes, nil)
		secondErr <- applyErr
	}()
	close(store.releaseFirstSave)

	if err := <-firstErr; err != nil {
		t.Fatalf("first profile Apply() error = %v, want success", err)
	}
	if err := <-secondErr; err == nil || !strings.Contains(err.Error(), "reload and retry") {
		t.Fatalf("second profile Apply() error = %v, want stale conflict", err)
	}
	if store.inner.saves != 1 {
		t.Fatalf("profile SaveBundle calls = %d, want 1", store.inner.saves)
	}
}

type synchronizedBundleEditorStore struct {
	testutil.DirectBundleEdits
	inner            *bundleEditorTestStore
	mu               sync.Mutex
	firstSaveStarted chan struct{}
	releaseFirstSave chan struct{}
}

func (s *synchronizedBundleEditorStore) LoadBundle(path string) (config.Bundle, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner.LoadBundle(path)
}

func (s *synchronizedBundleEditorStore) LoadBundlePlan(path string) (config.Bundle, map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner.LoadBundlePlan(path)
}

func (s *synchronizedBundleEditorStore) SaveBundle(path string, bundle config.Bundle) error {
	s.mu.Lock()
	if s.firstSaveStarted != nil && s.inner.saves == 0 {
		close(s.firstSaveStarted)
		s.mu.Unlock()
		<-s.releaseFirstSave
		s.mu.Lock()
	}
	defer s.mu.Unlock()
	return s.inner.SaveBundle(path, bundle)
}

func (s *synchronizedBundleEditorStore) HashFile(path string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner.HashFile(path)
}

func (s *synchronizedBundleEditorStore) WriteAtomic(path string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner.WriteAtomic(path, data)
}

func (s *synchronizedBundleEditorStore) RemoveFile(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner.RemoveFile(path)
}

func (s *synchronizedBundleEditorStore) ValidatePath(root, path string) error {
	return s.inner.ValidatePath(root, path)
}

func (s *synchronizedBundleEditorStore) EnsureDefaultFiles(root string) error {
	return s.inner.EnsureDefaultFiles(root)
}

func (s *synchronizedBundleEditorStore) ConfigRootFromYAMLPath(path string) string {
	return s.inner.ConfigRootFromYAMLPath(path)
}

func (s *bundleEditorTestStore) SaveBundle(_ string, bundle config.Bundle) error {
	s.saves++
	s.bundle = bundle
	return s.saveErr
}

func (s *bundleEditorTestStore) HashFile(path string) (string, error) {
	path = canonicalTestPath(path)
	s.hashCalls[path]++
	if s.beforeHash != nil {
		s.beforeHash(path, s.hashCalls[path])
	}
	if path == "" {
		return "", errors.New("empty path")
	}
	var data []byte
	if bundlePath, ok := s.files[path]; ok {
		if bundlePath == nil {
			encoded, err := json.Marshal(s.bundle)
			if err != nil {
				return "", err
			}
			data = encoded
		} else {
			data = bundlePath
		}
	} else {
		return "", fs.ErrNotExist
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func (s *bundleEditorTestStore) WriteAtomic(path string, data []byte) error {
	path = canonicalTestPath(path)
	s.files[path] = append([]byte(nil), data...)
	return s.writeErr
}

func (s *bundleEditorTestStore) RemoveFile(path string) error {
	path = canonicalTestPath(path)
	if _, ok := s.files[path]; !ok {
		return nil
	}
	delete(s.files, path)
	return s.removeErr
}

func (s *bundleEditorTestStore) ValidatePath(root, path string) error {
	return config.ValidatePath(root, path)
}

func (s *bundleEditorTestStore) EnsureDefaultFiles(string) error { return nil }

func (s *bundleEditorTestStore) ConfigRootFromYAMLPath(path string) string { return filepath.Dir(path) }

var _ app.BundleStore = (*bundleEditorTestStore)(nil)

func cloneHashes(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for path, hash := range in {
		out[path] = hash
	}
	return out
}

func canonicalTestPath(path string) string {
	abs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return filepath.Clean(path)
	}
	return abs
}
