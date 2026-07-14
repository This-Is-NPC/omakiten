package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSnapshotDatabaseIncludesCommittedWALFramesWithPinnedReader(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "source.db")
	db, err := sql.Open("sqlite", sourcePath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(3)
	if _, err := db.ExecContext(ctx, `PRAGMA journal_mode = WAL`); err != nil {
		t.Fatalf("enable WAL: %v", err)
	}
	if _, err := db.ExecContext(ctx, `PRAGMA wal_autocheckpoint = 0`); err != nil {
		t.Fatalf("disable autocheckpoint: %v", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE snapshot_rows(id INTEGER PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatalf("baseline checkpoint: %v", err)
	}

	reader, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("reader conn: %v", err)
	}
	defer func() { _ = reader.Close() }()
	if _, err := reader.ExecContext(ctx, `BEGIN`); err != nil {
		t.Fatalf("reader BEGIN: %v", err)
	}
	defer func() { _, _ = reader.ExecContext(context.Background(), `ROLLBACK`) }()
	var initial int
	if err := reader.QueryRowContext(ctx, `SELECT COUNT(*) FROM snapshot_rows`).Scan(&initial); err != nil || initial != 0 {
		t.Fatalf("reader baseline = %d, %v", initial, err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO snapshot_rows(value) VALUES ('committed in wal')`); err != nil {
		t.Fatalf("committed WAL insert: %v", err)
	}

	plainPath := filepath.Join(dir, "plain-main-file.db")
	mainBytes, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("read main database: %v", err)
	}
	if err := os.WriteFile(plainPath, mainBytes, 0o600); err != nil {
		t.Fatalf("write plain main-file copy: %v", err)
	}
	plain, err := sql.Open("sqlite", plainPath)
	if err != nil {
		t.Fatalf("open plain copy: %v", err)
	}
	var plainCount int
	if err := plain.QueryRowContext(ctx, `SELECT COUNT(*) FROM snapshot_rows`).Scan(&plainCount); err != nil {
		_ = plain.Close()
		t.Fatalf("query plain copy: %v", err)
	}
	_ = plain.Close()
	if plainCount != 0 {
		t.Fatalf("plain main-file copy contains %d rows; fixture did not prove a WAL/main gap", plainCount)
	}
	walBefore, err := os.Stat(sourcePath + "-wal")
	if err != nil {
		t.Fatalf("stat source WAL before snapshot: %v", err)
	}

	snapshotPath := filepath.Join(dir, "snapshot.db")
	if err := SnapshotDatabase(ctx, sourcePath, snapshotPath); err != nil {
		t.Fatalf("SnapshotDatabase: %v", err)
	}
	info, err := os.Stat(snapshotPath)
	if err != nil {
		t.Fatalf("stat snapshot: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("snapshot mode = %o, want 600", info.Mode().Perm())
	}
	snapshot, err := sql.Open("sqlite", snapshotPath)
	if err != nil {
		t.Fatalf("open snapshot: %v", err)
	}
	defer func() { _ = snapshot.Close() }()
	var snapshotCount int
	if err := snapshot.QueryRowContext(ctx, `SELECT COUNT(*) FROM snapshot_rows WHERE value = 'committed in wal'`).Scan(&snapshotCount); err != nil {
		t.Fatalf("query snapshot: %v", err)
	}
	if snapshotCount != 1 {
		t.Fatalf("committed WAL rows in snapshot = %d, want 1", snapshotCount)
	}
	var integrity string
	if err := snapshot.QueryRowContext(ctx, `PRAGMA quick_check`).Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("snapshot quick_check = %q, %v", integrity, err)
	}
	walAfter, err := os.Stat(sourcePath + "-wal")
	if err != nil {
		t.Fatalf("stat source WAL after snapshot: %v", err)
	}
	if walAfter.Size() != walBefore.Size() {
		t.Fatalf("snapshot mutated/checkpointed source WAL: before=%d after=%d", walBefore.Size(), walAfter.Size())
	}
	var pinnedCount int
	if err := reader.QueryRowContext(ctx, `SELECT COUNT(*) FROM snapshot_rows`).Scan(&pinnedCount); err != nil || pinnedCount != 0 {
		t.Fatalf("pinned reader snapshot changed = %d, %v", pinnedCount, err)
	}
	var sourceCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM snapshot_rows`).Scan(&sourceCount); err != nil || sourceCount != 1 {
		t.Fatalf("source rows after snapshot = %d, %v", sourceCount, err)
	}
}

func TestSnapshotDatabaseCancellationAndFailureCleanTemporaryFile(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "source.db")
	db, err := sql.Open("sqlite", sourcePath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE snapshot_rows(id INTEGER PRIMARY KEY)`); err != nil {
		_ = db.Close()
		t.Fatalf("create source: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close source: %v", err)
	}

	canceled, cancel := context.WithCancel(ctx)
	canceledPath := filepath.Join(dir, "canceled.db")
	canceledTemp := canceledPath + ".tmp"
	cleanupMarker := errors.New("forced deferred cleanup report")
	if err := snapshotDatabaseWithOptions(canceled, sourcePath, canceledPath, false, snapshotHooks{AfterVacuum: cancel, DeferredCleanupError: cleanupMarker}); !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), cleanupMarker.Error()) {
		t.Fatalf("canceled SnapshotDatabase error = %v, want context.Canceled joined with cleanup error", err)
	}
	for _, path := range []string{canceledPath, canceledTemp} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("canceled snapshot left %s: %v", path, err)
		}
	}

	blockedFinal := filepath.Join(dir, "blocked.db")
	if err := os.Mkdir(blockedFinal, 0o700); err != nil {
		t.Fatalf("Mkdir blocked final: %v", err)
	}
	partialTemp := filepath.Join(dir, "blocked.db.tmp")
	if err := SnapshotDatabase(ctx, sourcePath, blockedFinal); err == nil {
		t.Fatal("SnapshotDatabase rename over directory error = nil")
	}
	if _, err := os.Stat(partialTemp); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed snapshot left temporary file: %v", err)
	}
}

func TestSnapshotDatabaseUsesPrivateRandomizedStagingAndPreservesCallerTemp(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "source.db")
	db, err := sql.Open("sqlite", sourcePath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE snapshot_rows(id INTEGER PRIMARY KEY)`); err != nil {
		_ = db.Close()
		t.Fatalf("create source: %v", err)
	}
	_ = db.Close()

	destinationPath := filepath.Join(dir, "snapshot.db")
	callerTemp := destinationPath + ".tmp"
	if err := os.WriteFile(callerTemp, []byte("caller owned"), 0o600); err != nil {
		t.Fatalf("write caller temp: %v", err)
	}
	var stagingDir string
	err = snapshotDatabaseWithOptions(ctx, sourcePath, destinationPath, false, snapshotHooks{
		AfterStageCreated: func(dirPath, filePath string) {
			stagingDir = dirPath
			dirInfo, statErr := os.Stat(dirPath)
			if statErr != nil || dirInfo.Mode().Perm() != 0o700 {
				t.Fatalf("staging directory mode = %v, %v; want 0700", dirInfo, statErr)
			}
			fileInfo, statErr := os.Stat(filePath)
			if statErr != nil || fileInfo.Mode().Perm() != 0o600 {
				t.Fatalf("staging file mode = %v, %v; want 0600", fileInfo, statErr)
			}
			if filepath.Dir(dirPath) != dir || filepath.Base(dirPath) == filepath.Base(destinationPath)+".tmp" {
				t.Fatalf("staging directory is not randomized under destination parent: %q", dirPath)
			}
		},
	})
	if err != nil {
		t.Fatalf("snapshotDatabaseWithOptions: %v", err)
	}
	if body, err := os.ReadFile(callerTemp); err != nil || string(body) != "caller owned" {
		t.Fatalf("caller temp changed = %q, %v", body, err)
	}
	if _, err := os.Stat(stagingDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("staging directory survived success: %v", err)
	}
}

func TestSnapshotDatabasePublicationRejectsRacesAndSymlinks(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "source.db")
	db, err := sql.Open("sqlite", sourcePath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE snapshot_rows(id INTEGER PRIMARY KEY)`); err != nil {
		_ = db.Close()
		t.Fatalf("create source: %v", err)
	}
	_ = db.Close()

	t.Run("destination appears during vacuum", func(t *testing.T) {
		destinationPath := filepath.Join(dir, "appeared.db")
		err := snapshotDatabaseWithOptions(ctx, sourcePath, destinationPath, false, snapshotHooks{
			AfterVacuum: func() {
				if writeErr := os.WriteFile(destinationPath, []byte("racer"), 0o600); writeErr != nil {
					t.Fatalf("write racing destination: %v", writeErr)
				}
			},
		})
		if err == nil {
			t.Fatal("no-replace snapshot overwrote racing destination")
		}
		if body, readErr := os.ReadFile(destinationPath); readErr != nil || string(body) != "racer" {
			t.Fatalf("racing destination changed = %q, %v", body, readErr)
		}
	})

	t.Run("parent symlink", func(t *testing.T) {
		realParent := filepath.Join(dir, "real-parent")
		if err := os.Mkdir(realParent, 0o700); err != nil {
			t.Fatalf("Mkdir: %v", err)
		}
		linkedParent := filepath.Join(dir, "linked-parent")
		if err := os.Symlink(realParent, linkedParent); err != nil {
			t.Fatalf("Symlink: %v", err)
		}
		if err := SnapshotDatabase(ctx, sourcePath, filepath.Join(linkedParent, "snapshot.db")); err == nil {
			t.Fatal("snapshot accepted symlinked destination parent")
		}
	})

	t.Run("parent replaced during vacuum", func(t *testing.T) {
		parent := filepath.Join(dir, "racing-parent")
		attacker := filepath.Join(dir, "attacker-parent")
		if err := os.Mkdir(parent, 0o700); err != nil {
			t.Fatalf("Mkdir parent: %v", err)
		}
		if err := os.Mkdir(attacker, 0o700); err != nil {
			t.Fatalf("Mkdir attacker: %v", err)
		}
		movedParent := parent + ".moved"
		destinationPath := filepath.Join(parent, "snapshot.db")
		preservedStagePath := filepath.Join(dir, "parent-race-stage.db")
		var stagePath string
		var replacementErr error
		err := snapshotDatabaseWithOptions(ctx, sourcePath, destinationPath, false, snapshotHooks{
			AfterStageCreated: func(_, path string) { stagePath = path },
			AfterVacuum: func() {
				replacementErr = os.Rename(parent, movedParent)
				if replacementErr != nil {
					return
				}
				if symlinkErr := os.Symlink(attacker, parent); symlinkErr != nil {
					t.Fatalf("replace parent with symlink: %v", symlinkErr)
				}
			},
			BeforePublish: func() {
				if replacementErr != nil {
					if linkErr := os.Link(stagePath, preservedStagePath); linkErr != nil {
						t.Fatalf("preserve verified stage after denied parent race: %v", linkErr)
					}
				}
			},
		})
		if replacementErr != nil {
			t.Logf("destination parent replacement denied by platform: %v", replacementErr)
			if err != nil {
				t.Fatalf("snapshot after denied parent race: %v", err)
			}
			if err := os.WriteFile(preservedStagePath, []byte("verified parent-race inode"), 0o600); err != nil {
				t.Fatal(err)
			}
			if body, readErr := os.ReadFile(destinationPath); readErr != nil || string(body) != "verified parent-race inode" {
				t.Fatalf("snapshot after denied parent race is not verified inode: body=%q read=%v", body, readErr)
			}
			return
		}
		if err == nil {
			t.Fatal("snapshot accepted destination parent replacement")
		}
		if entries, readErr := os.ReadDir(attacker); readErr != nil || len(entries) != 0 {
			t.Fatalf("attacker directory received snapshot state: entries=%v error=%v", entries, readErr)
		}
		if entries, readErr := os.ReadDir(movedParent); readErr != nil || len(entries) != 0 {
			t.Fatalf("private staging survived parent race: entries=%v error=%v", entries, readErr)
		}
	})

	t.Run("direct symlink", func(t *testing.T) {
		victim := filepath.Join(dir, "victim.db")
		if err := os.WriteFile(victim, []byte("victim"), 0o600); err != nil {
			t.Fatalf("write victim: %v", err)
		}
		destinationPath := filepath.Join(dir, "direct-link.db")
		if err := os.Symlink(victim, destinationPath); err != nil {
			t.Fatalf("Symlink: %v", err)
		}
		if err := SnapshotDatabaseReplace(ctx, sourcePath, destinationPath); err == nil {
			t.Fatal("force snapshot accepted direct destination symlink")
		}
		if body, err := os.ReadFile(victim); err != nil || string(body) != "victim" {
			t.Fatalf("destination symlink victim changed = %q, %v", body, err)
		}
	})

	t.Run("force destination becomes symlink during vacuum", func(t *testing.T) {
		victim := filepath.Join(dir, "racing-victim.db")
		if err := os.WriteFile(victim, []byte("racing victim"), 0o600); err != nil {
			t.Fatalf("write victim: %v", err)
		}
		destinationPath := filepath.Join(dir, "racing-direct-link.db")
		err := snapshotDatabaseWithOptions(ctx, sourcePath, destinationPath, true, snapshotHooks{
			AfterVacuum: func() {
				if symlinkErr := os.Symlink(victim, destinationPath); symlinkErr != nil {
					t.Fatalf("create racing destination symlink: %v", symlinkErr)
				}
			},
		})
		if err == nil {
			t.Fatal("force snapshot accepted a destination symlink created during vacuum")
		}
		if body, err := os.ReadFile(victim); err != nil || string(body) != "racing victim" {
			t.Fatalf("racing destination symlink victim changed = %q, %v", body, err)
		}
	})
}

func TestSnapshotDatabasePublishesOnlyVerifiedStagedInode(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "source.db")
	db, err := sql.Open("sqlite", sourcePath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE snapshot_rows(id INTEGER PRIMARY KEY); INSERT INTO snapshot_rows VALUES (1)`); err != nil {
		_ = db.Close()
		t.Fatalf("create source: %v", err)
	}
	_ = db.Close()

	for _, test := range []struct {
		name   string
		attack func(t *testing.T, stageDir, stageFile, preservedPath string)
	}{
		{
			name: "stage file replaced",
			attack: func(t *testing.T, _, stageFile, preservedPath string) {
				t.Helper()
				if err := os.Link(stageFile, preservedPath); err != nil {
					t.Fatalf("preserve verified inode: %v", err)
				}
				if err := os.Remove(stageFile); err != nil {
					t.Fatalf("remove verified stage name: %v", err)
				}
				if err := os.WriteFile(stageFile, []byte("attacker replacement"), 0o600); err != nil {
					t.Fatalf("replace stage file: %v", err)
				}
			},
		},
		{
			name: "stage directory replaced",
			attack: func(t *testing.T, stageDir, stageFile, preservedPath string) {
				t.Helper()
				if err := os.Link(stageFile, preservedPath); err != nil {
					t.Fatalf("preserve verified inode: %v", err)
				}
				movedStage := stageDir + ".moved"
				if err := os.Rename(stageDir, movedStage); err != nil {
					t.Logf("stage directory replacement denied: %v", err)
					return
				}
				if err := os.Mkdir(stageDir, 0o700); err != nil {
					t.Fatalf("replace stage directory: %v", err)
				}
				if err := os.WriteFile(stageFile, []byte("attacker replacement"), 0o600); err != nil {
					t.Fatalf("write attacker stage file: %v", err)
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			destinationPath := filepath.Join(dir, strings.ReplaceAll(test.name, " ", "-")+".db")
			preservedPath := destinationPath + ".verified"
			var stageDir, stageFile string
			err := snapshotDatabaseWithOptions(ctx, sourcePath, destinationPath, false, snapshotHooks{
				AfterStageCreated: func(dirPath, filePath string) {
					stageDir, stageFile = dirPath, filePath
				},
				BeforePublish: func() {
					test.attack(t, stageDir, stageFile, preservedPath)
				},
			})
			if err != nil {
				if _, statErr := os.Lstat(destinationPath); !errors.Is(statErr, os.ErrNotExist) {
					t.Fatalf("failed publication left destination: %v (snapshot error: %v)", statErr, err)
				}
				return
			}
			if err := os.WriteFile(preservedPath, []byte("verified staged inode"), 0o600); err != nil {
				t.Fatalf("write preserved verified inode: %v", err)
			}
			if body, readErr := os.ReadFile(destinationPath); readErr != nil || string(body) != "verified staged inode" {
				t.Fatalf("published snapshot is not the verified staged inode: body=%q read=%v", body, readErr)
			}
		})
	}
}

func TestSnapshotDatabaseRejectsSourceDestinationHardLinkAlias(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "source.db")
	db, err := sql.Open("sqlite", sourcePath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE snapshot_rows(id INTEGER PRIMARY KEY)`); err != nil {
		_ = db.Close()
		t.Fatalf("create source: %v", err)
	}
	_ = db.Close()

	for _, replace := range []bool{false, true} {
		t.Run(map[bool]string{false: "no force", true: "force"}[replace], func(t *testing.T) {
			destinationPath := filepath.Join(dir, map[bool]string{false: "alias.db", true: "force-alias.db"}[replace])
			if err := os.Link(sourcePath, destinationPath); err != nil {
				t.Fatalf("link source to destination: %v", err)
			}
			if err := snapshotDatabaseWithOptions(ctx, sourcePath, destinationPath, replace, snapshotHooks{}); err == nil || !strings.Contains(err.Error(), "same file") {
				t.Fatalf("snapshot alias error = %v, want same-file rejection", err)
			}
			sourceInfo, sourceErr := os.Stat(sourcePath)
			destinationInfo, destinationErr := os.Stat(destinationPath)
			if sourceErr != nil || destinationErr != nil || !os.SameFile(sourceInfo, destinationInfo) {
				t.Fatalf("alias changed after rejection: source=%v destination=%v", sourceErr, destinationErr)
			}
		})
	}

	t.Run("force alias appears before publish", func(t *testing.T) {
		destinationPath := filepath.Join(dir, "late-force-alias.db")
		err := snapshotDatabaseWithOptions(ctx, sourcePath, destinationPath, true, snapshotHooks{
			BeforePublish: func() {
				if linkErr := os.Link(sourcePath, destinationPath); linkErr != nil {
					t.Fatalf("create late source alias: %v", linkErr)
				}
			},
		})
		if err == nil || !strings.Contains(err.Error(), "same file") {
			t.Fatalf("late force alias error = %v, want same-file rejection", err)
		}
	})
}

func TestSnapshotDatabaseRollsBackPostPublishFailures(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "source.db")
	db, err := sql.Open("sqlite", sourcePath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE snapshot_rows(id INTEGER PRIMARY KEY)`); err != nil {
		_ = db.Close()
		t.Fatalf("create source: %v", err)
	}
	_ = db.Close()

	for _, test := range []struct {
		name     string
		injected error
		hooks    snapshotHooks
	}{
		{name: "staging cleanup", injected: errors.New("injected staging cleanup failure")},
		{name: "directory durability", injected: errors.New("injected directory sync failure")},
	} {
		if test.name == "staging cleanup" {
			test.hooks.PostPublishCleanupError = test.injected
		} else {
			test.hooks.PostPublishSyncError = test.injected
		}
		for _, replace := range []bool{false, true} {
			mode := map[bool]string{false: "no force", true: "force"}[replace]
			t.Run(test.name+"/"+mode, func(t *testing.T) {
				destinationPath := filepath.Join(dir, strings.ReplaceAll(test.name+"-"+mode, " ", "-")+".db")
				var previousInfo os.FileInfo
				if replace {
					if err := os.WriteFile(destinationPath, []byte("existing"), 0o600); err != nil {
						t.Fatalf("write force destination: %v", err)
					}
					previousInfo, err = os.Lstat(destinationPath)
					if err != nil {
						t.Fatalf("stat force destination: %v", err)
					}
				}
				err := snapshotDatabaseWithOptions(ctx, sourcePath, destinationPath, replace, test.hooks)
				if err == nil {
					t.Fatal("injected post-publication failure returned nil")
				}
				if !errors.Is(err, test.injected) {
					t.Fatalf("snapshot failed before injected post-publication hook: %v", err)
				}
				if !replace {
					if _, statErr := os.Lstat(destinationPath); !errors.Is(statErr, os.ErrNotExist) {
						t.Fatalf("no-force post-publication failure left destination: %v (snapshot error: %v)", statErr, err)
					}
				} else {
					restoredInfo, statErr := os.Lstat(destinationPath)
					body, readErr := os.ReadFile(destinationPath)
					if statErr != nil || readErr != nil || string(body) != "existing" || !os.SameFile(previousInfo, restoredInfo) {
						t.Fatalf("force rollback did not restore exact destination: info=%v body=%q stat=%v read=%v snapshot=%v", restoredInfo, body, statErr, readErr, err)
					}
				}
				entries, readErr := os.ReadDir(dir)
				if readErr != nil {
					t.Fatalf("read destination directory: %v", readErr)
				}
				for _, entry := range entries {
					if strings.HasPrefix(entry.Name(), ".omakiten-rollback-") || strings.HasPrefix(entry.Name(), ".omakiten-publish-") {
						t.Fatalf("post-publication rollback left private link %q", entry.Name())
					}
				}
			})
		}
	}
}

func TestSnapshotDatabaseForceFinalizationIdentityAndDurability(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "source.db")
	db, err := sql.Open("sqlite", sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE snapshot_rows(id INTEGER PRIMARY KEY); INSERT INTO snapshot_rows VALUES (1)`); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	_ = db.Close()

	t.Run("destination substitution retains rollback inode", func(t *testing.T) {
		caseDir := t.TempDir()
		destinationPath := filepath.Join(caseDir, "substituted.db")
		previousAliasPath := filepath.Join(caseDir, "substituted-previous.db")
		preservedPublicationPath := filepath.Join(caseDir, "substituted-publication.db")
		attackerPath := filepath.Join(caseDir, "substituted-attacker.db")
		if err := os.WriteFile(destinationPath, []byte("previous"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Link(destinationPath, previousAliasPath); err != nil {
			t.Fatalf("retain original inode test alias: %v", err)
		}
		if err := os.WriteFile(attackerPath, []byte("attacker"), 0o600); err != nil {
			t.Fatal(err)
		}
		err := snapshotDatabaseWithOptions(ctx, sourcePath, destinationPath, true, snapshotHooks{
			BeforeRollbackLinkCleanup: func() {
				if err := os.Rename(destinationPath, preservedPublicationPath); err != nil {
					t.Fatalf("preserve publication before substitution: %v", err)
				}
				if err := os.Rename(attackerPath, destinationPath); err != nil {
					t.Fatalf("substitute snapshot destination: %v", err)
				}
			},
		})
		var publicationErr *SnapshotPublicationError
		if err == nil || !errors.As(err, &publicationErr) {
			t.Fatalf("substituted finalization error = %v, want SnapshotPublicationError", err)
		}
		if publicationErr.IdentityAtPath {
			t.Fatal("substituted destination reported the verified publication identity")
		}
		if publicationErr.PublishedIdentity == nil {
			t.Fatal("substituted finalization omitted verified publication identity")
		}
		if count, queryErr := snapshotRowCount(preservedPublicationPath); queryErr != nil || count != 1 {
			t.Fatalf("preserved publication rows = %d, %v", count, queryErr)
		}
		if body, readErr := os.ReadFile(destinationPath); readErr != nil || string(body) != "attacker" {
			t.Fatalf("substituted destination changed = %q, %v", body, readErr)
		}
		var rollbackPath string
		entries, readErr := os.ReadDir(caseDir)
		if readErr != nil {
			t.Fatal(readErr)
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".omakiten-rollback-") {
				rollbackPath = filepath.Join(caseDir, entry.Name())
			}
		}
		if rollbackPath == "" {
			t.Fatal("destination substitution removed the original rollback inode")
		}
		if err := os.WriteFile(rollbackPath, []byte("retained original inode"), 0o600); err != nil {
			t.Fatal(err)
		}
		if body, readErr := os.ReadFile(previousAliasPath); readErr != nil || string(body) != "retained original inode" {
			t.Fatalf("retained rollback is not exact original inode: body=%q read=%v", body, readErr)
		}
	})

	t.Run("post-unlink sync failure leaves explicit publication", func(t *testing.T) {
		caseDir := t.TempDir()
		destinationPath := filepath.Join(caseDir, "post-unlink-sync.db")
		previousAliasPath := filepath.Join(caseDir, "post-unlink-previous.db")
		if err := os.WriteFile(destinationPath, []byte("previous"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Link(destinationPath, previousAliasPath); err != nil {
			t.Fatalf("retain original inode test alias: %v", err)
		}
		injected := errors.New("injected post-unlink directory sync failure")
		err := snapshotDatabaseWithOptions(ctx, sourcePath, destinationPath, true, snapshotHooks{PostRollbackLinkSyncError: injected})
		var publicationErr *SnapshotPublicationError
		if !errors.Is(err, injected) || !errors.As(err, &publicationErr) || !publicationErr.IdentityAtPath {
			t.Fatalf("post-unlink sync error = %v, publication=%+v", err, publicationErr)
		}
		if publicationErr.PublishedIdentity == nil {
			t.Fatal("post-unlink failure omitted verified publication identity")
		}
		if count, queryErr := snapshotRowCount(destinationPath); queryErr != nil || count != 1 {
			t.Fatalf("post-unlink publication rows = %d, %v", count, queryErr)
		}
		if err := os.WriteFile(previousAliasPath, []byte("mutated original inode"), 0o600); err != nil {
			t.Fatal(err)
		}
		if body, readErr := os.ReadFile(destinationPath); readErr != nil || string(body) == "mutated original inode" {
			t.Fatalf("post-unlink failure restored original inode: body=%q read=%v", body, readErr)
		}
		if strings.Contains(err.Error(), "before restoration") {
			t.Fatalf("post-unlink failure falsely attempted old-inode restoration: %v", err)
		}
		entries, readErr := os.ReadDir(caseDir)
		if readErr != nil {
			t.Fatal(readErr)
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".omakiten-rollback-") {
				t.Fatalf("post-unlink sync failure retained nonexistent rollback state %q", entry.Name())
			}
		}
	})
}

func TestSnapshotDatabaseForceAndNoForcePublication(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "source.db")
	db, err := sql.Open("sqlite", sourcePath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE snapshot_rows(id INTEGER PRIMARY KEY); INSERT INTO snapshot_rows VALUES (1)`); err != nil {
		_ = db.Close()
		t.Fatalf("create source: %v", err)
	}
	_ = db.Close()
	destinationPath := filepath.Join(dir, "existing.db")
	if err := os.WriteFile(destinationPath, []byte("existing"), 0o600); err != nil {
		t.Fatalf("write existing destination: %v", err)
	}
	if err := SnapshotDatabase(ctx, sourcePath, destinationPath); err == nil {
		t.Fatal("no-force snapshot replaced existing destination")
	}
	if body, err := os.ReadFile(destinationPath); err != nil || string(body) != "existing" {
		t.Fatalf("no-force destination changed = %q, %v", body, err)
	}
	previousAliasPath := filepath.Join(dir, "existing-previous.db")
	if err := os.Link(destinationPath, previousAliasPath); err != nil {
		t.Fatalf("retain existing destination test alias: %v", err)
	}
	publicationAliasPath := filepath.Join(dir, "existing-publication.db")
	if err := snapshotDatabaseWithOptions(ctx, sourcePath, destinationPath, true, snapshotHooks{
		BeforeRollbackLinkCleanup: func() {
			if linkErr := os.Link(destinationPath, publicationAliasPath); linkErr != nil {
				t.Fatalf("retain verified publication test alias: %v", linkErr)
			}
		},
	}); err != nil {
		t.Fatalf("force snapshot: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read snapshot directory: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".omakiten-rollback-") || strings.HasPrefix(entry.Name(), ".omakiten-publish-") {
			t.Fatalf("successful force snapshot left private link %q", entry.Name())
		}
	}
	snapshot, err := sql.Open("sqlite", destinationPath)
	if err != nil {
		t.Fatalf("open replaced snapshot: %v", err)
	}
	var count int
	if err := snapshot.QueryRowContext(ctx, `SELECT COUNT(*) FROM snapshot_rows`).Scan(&count); err != nil || count != 1 {
		_ = snapshot.Close()
		t.Fatalf("force snapshot rows = %d, %v", count, err)
	}
	if err := snapshot.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(previousAliasPath, []byte("mutated previous inode"), 0o600); err != nil {
		t.Fatal(err)
	}
	if body, readErr := os.ReadFile(destinationPath); readErr != nil || string(body) == "mutated previous inode" {
		t.Fatalf("successful finalization retained old inode: body=%q read=%v", body, readErr)
	}
	if err := os.WriteFile(publicationAliasPath, []byte("mutated verified inode"), 0o600); err != nil {
		t.Fatal(err)
	}
	if body, readErr := os.ReadFile(destinationPath); readErr != nil || string(body) != "mutated verified inode" {
		t.Fatalf("successful finalization did not retain exact verified inode: body=%q read=%v", body, readErr)
	}
}

func TestSnapshotDatabaseRejectsSourceSymlinkComponents(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	realParent := filepath.Join(dir, "real-source")
	if err := os.Mkdir(realParent, 0o700); err != nil {
		t.Fatalf("Mkdir source parent: %v", err)
	}
	realSource := filepath.Join(realParent, "source.db")
	db, err := sql.Open("sqlite", realSource)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE snapshot_rows(id INTEGER PRIMARY KEY)`); err != nil {
		_ = db.Close()
		t.Fatalf("create source: %v", err)
	}
	_ = db.Close()

	directLink := filepath.Join(dir, "direct-source.db")
	if err := os.Symlink(realSource, directLink); err != nil {
		t.Fatalf("Symlink source: %v", err)
	}
	if err := SnapshotDatabase(ctx, directLink, filepath.Join(dir, "direct-output.db")); err == nil {
		t.Fatal("snapshot accepted direct source symlink")
	}

	parentLink := filepath.Join(dir, "linked-source-parent")
	if err := os.Symlink(realParent, parentLink); err != nil {
		t.Fatalf("Symlink source parent: %v", err)
	}
	if err := SnapshotDatabase(ctx, filepath.Join(parentLink, "source.db"), filepath.Join(dir, "parent-output.db")); err == nil {
		t.Fatal("snapshot accepted source path with symlinked parent")
	}
}

func snapshotRowCount(path string) (int, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return 0, err
	}
	var count int
	queryErr := db.QueryRow(`SELECT COUNT(*) FROM snapshot_rows`).Scan(&count)
	return count, errors.Join(queryErr, db.Close())
}
