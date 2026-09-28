//go:build linux

package config

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

//nolint:funlen,gocognit // The concurrent publication probe is intentionally kept as one scenario.
func TestWriteAtomicPublishesWholeFiles(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.md")
	original := bytes.Repeat([]byte("original\n"), 256)
	changed := bytes.Repeat([]byte("changed\n"), 256)
	if err := os.WriteFile(target, original, 0o600); err != nil {
		t.Fatalf("seed target: %v", err)
	}

	var wg sync.WaitGroup
	stop := make(chan struct{})
	readErr := make(chan string, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			got, err := os.ReadFile(target)
			if err != nil {
				select {
				case readErr <- err.Error():
				default:
				}
				return
			}
			if !bytes.Equal(got, original) && !bytes.Equal(got, changed) {
				select {
				case readErr <- "reader observed partial bytes":
				default:
				}
				return
			}
		}
	}()
	for i := 0; i < 100; i++ {
		if err := WriteAtomic(target, changed); err != nil {
			close(stop)
			wg.Wait()
			t.Fatalf("WriteAtomic: %v", err)
		}
		if err := WriteAtomic(target, original); err != nil {
			close(stop)
			wg.Wait()
			t.Fatalf("WriteAtomic restore: %v", err)
		}
	}
	close(stop)
	wg.Wait()
	select {
	case err := <-readErr:
		t.Fatal(err)
	default:
	}
}

func TestWriteAtomicRejectsTargetSymlink(t *testing.T) {
	dir := t.TempDir()
	victim := filepath.Join(t.TempDir(), "victim")
	target := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(victim, []byte("preserve\n"), 0o600); err != nil {
		t.Fatalf("write victim: %v", err)
	}
	if err := os.Symlink(victim, target); err != nil {
		t.Fatalf("symlink target: %v", err)
	}
	if err := WriteAtomic(target, []byte("must not follow\n")); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("WriteAtomic error = %v, want symlink rejection", err)
	}
	got, err := os.ReadFile(victim)
	if err != nil || string(got) != "preserve\n" {
		t.Fatalf("victim = %q, %v", got, err)
	}
}

func TestRemoveFileValidatesCurrentTarget(t *testing.T) {
	dir := t.TempDir()
	victim := filepath.Join(t.TempDir(), "victim")
	target := filepath.Join(dir, "entity.md")
	if err := os.WriteFile(victim, []byte("preserve\n"), 0o600); err != nil {
		t.Fatalf("write victim: %v", err)
	}
	if err := os.Symlink(victim, target); err != nil {
		t.Fatalf("symlink target: %v", err)
	}
	if err := RemoveFile(target); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("RemoveFile error = %v, want symlink rejection", err)
	}
	if err := os.Remove(target); err != nil {
		t.Fatalf("remove symlink: %v", err)
	}
	if err := os.WriteFile(target, []byte("remove\n"), 0o600); err != nil {
		t.Fatalf("write target: %v", err)
	}
	if err := RemoveFile(target); err != nil {
		t.Fatalf("RemoveFile regular target: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("target after remove: %v", err)
	}
	if err := RemoveFile(target); err != nil {
		t.Fatalf("RemoveFile missing target: %v", err)
	}
}

func TestPostPublicationSyncFailureIsAmbiguous(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "entity.md")
	if err := os.WriteFile(target, []byte("original\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	previous := syncDirectory
	t.Cleanup(func() { syncDirectory = previous })
	syncDirectory = func(int) error { return errors.New("injected sync failure") }

	err := WriteAtomic(target, []byte("published\n"))
	var ambiguous *AmbiguousPublicationError
	if !errors.As(err, &ambiguous) || ambiguous.Operation != "write" || ambiguous.Path != target {
		t.Fatalf("WriteAtomic error = %v, want ambiguous write for %s", err, target)
	}
	got, readErr := os.ReadFile(target)
	if readErr != nil || string(got) != "published\n" {
		t.Fatalf("target after ambiguous write = %q, %v", got, readErr)
	}

	err = RemoveFile(target)
	if !errors.As(err, &ambiguous) || ambiguous.Operation != "delete" || ambiguous.Path != target {
		t.Fatalf("RemoveFile error = %v, want ambiguous delete for %s", err, target)
	}
	if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
		t.Fatalf("target after ambiguous delete: %v", statErr)
	}
}

func TestReadFileAtRelativePinsDirectoryAcrossPathSwap(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "entities")
	outside := t.TempDir()
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("mkdir entities: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skill.md"), []byte("owned\n"), 0o644); err != nil {
		t.Fatalf("write owned file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outside, "skill.md"), []byte("escaped\n"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	dirFD, _, err := openDirNoFollow(dir, false)
	if err != nil {
		t.Fatalf("open entity directory: %v", err)
	}
	defer func() { _ = os.NewFile(uintptr(dirFD), dir).Close() }()
	if err := os.Rename(dir, dir+"-old"); err != nil {
		t.Fatalf("rename entity directory: %v", err)
	}
	if err := os.Symlink(outside, dir); err != nil {
		t.Fatalf("replace entity directory: %v", err)
	}
	got, err := readFileAt(dirFD, "skill.md", filepath.Join(dir, "skill.md"), MaxEntityFileBytes)
	if err != nil || string(got) != "owned\n" {
		t.Fatalf("pinned read = %q, %v", got, err)
	}
}

func TestReadFileAtExpectedRejectsReplacement(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "skill.md")
	if err := os.WriteFile(target, []byte("original\n"), 0o600); err != nil {
		t.Fatalf("write target: %v", err)
	}
	dirFD, _, err := openDirNoFollow(dir, false)
	if err != nil {
		t.Fatalf("open directory: %v", err)
	}
	defer func() { _ = os.NewFile(uintptr(dirFD), dir).Close() }()
	expected, err := unixFileIdentityAt(dirFD, filepath.Base(target))
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	if err := os.Rename(target, target+".old"); err != nil {
		t.Fatalf("replace target: %v", err)
	}
	if err := os.WriteFile(target, []byte("replacement\n"), 0o600); err != nil {
		t.Fatalf("write replacement: %v", err)
	}
	if _, err := readFileAtExpected(dirFD, filepath.Base(target), target, MaxEntityFileBytes, expected); err == nil {
		t.Fatal("read accepted a replaced current path")
	}
}
