package sqlite

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"os"
	"time"
)

const (
	exactGenerationAttempts = 3
	rollbackTimeout         = 5 * time.Second
)

var errRollbackUnproven = errors.New("transaction rollback outcome is unproven")

type sqliteTransaction interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type transactionControl struct {
	rollback      func(context.Context, sqliteTransaction) error
	invalidate    func()
	rollbackLabel string
}

// Search retains begin/read failures and validates before the locked read;
// project deletion discards them, retries begin, validates after, and joins the
// last exhaustion cause. These are the only exact-generation policy differences.
type exactGenerationPolicy bool

const (
	projectDeleteExactGenerationPolicy exactGenerationPolicy = false
	searchReindexExactGenerationPolicy exactGenerationPolicy = true
)

func (p exactGenerationPolicy) message(search, project string) string {
	if p {
		return search
	}
	return project
}

// MaintenanceBackupCreator populates one rolling destination from a pinned connection.
type MaintenanceBackupCreator func(context.Context, func(string) error) (string, error)

type exactGenerationHooks struct {
	AfterBackup func(int)
	BeforeBegin func(int)
	AfterBegin  func(int)
}

type exactGenerationConfig struct {
	create                        MaintenanceBackupCreator
	discard                       func(string) error
	snapshot                      func(context.Context, string) error
	beforeSnapshot, afterSnapshot func() error
	hooks                         exactGenerationHooks
	transaction                   transactionControl
}

func prepareExactGeneration(ctx context.Context, conn sqliteTransaction, policy exactGenerationPolicy, cfg exactGenerationConfig) (string, int, error) {
	discardCandidate := func(path string, primary error) (string, error) {
		if bool(policy) && errors.Is(primary, errRollbackUnproven) {
			return path, primary
		}
		if err := cfg.discard(path); err != nil {
			label := policy.message("discard stale reindex backup", "discard stale project-delete backup")
			return path, errors.Join(primary, fmt.Errorf("%s: %w", label, err))
		}
		return "", primary
	}

	var lastGenerationErr error
	for attempt := 1; attempt <= exactGenerationAttempts; attempt++ {
		if err := cfg.beforeSnapshot(); err != nil {
			return "", 0, err
		}
		beforeVersion, err := readDataVersion(ctx, conn, policy.message("read search data_version", "read project-delete data_version"))
		if err != nil {
			return "", 0, err
		}
		backupPath, err := cfg.create(ctx, func(path string) error { return cfg.snapshot(ctx, path) })
		if err != nil {
			if !policy {
				err = fmt.Errorf("create project-delete backup: %w", err)
			}
			return "", 0, err
		}
		if policy {
			info, statErr := os.Lstat(backupPath)
			if statErr != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
				return backupPath, 0, errors.New("generated reindex backup is not a regular file")
			}
		}
		if cfg.hooks.AfterBackup != nil {
			cfg.hooks.AfterBackup(attempt)
		}
		if err := cfg.afterSnapshot(); err != nil {
			path, failure := discardCandidate(backupPath, err)
			return path, 0, failure
		}
		afterVersion, err := readDataVersion(ctx, conn, policy.message("read search data_version", "read project-delete data_version"))
		if err != nil {
			if policy {
				return backupPath, 0, err
			}
			path, failure := discardCandidate(backupPath, err)
			return path, 0, failure
		}
		if afterVersion != beforeVersion {
			lastGenerationErr = errors.New(policy.message("search index changed while backup was created", "database changed while project-delete backup was created"))
			if path, failure := discardCandidate(backupPath, lastGenerationErr); path != "" {
				return path, 0, failure
			}
			continue
		}
		if cfg.hooks.BeforeBegin != nil {
			cfg.hooks.BeforeBegin(attempt)
		}
		if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
			lastGenerationErr = fmt.Errorf("%s: %w", policy.message("begin immediate", "acquire project-delete writer lock"), err)
			if policy {
				validationErr := cfg.afterSnapshot()
				if validationErr != nil {
					path, failure := discardCandidate(backupPath, errors.Join(validationErr, lastGenerationErr))
					return path, 0, failure
				}
			}
			if policy {
				return backupPath, 0, lastGenerationErr
			}
			if path, failure := discardCandidate(backupPath, lastGenerationErr); path != "" {
				return path, 0, failure
			}
			continue
		}
		if cfg.hooks.AfterBegin != nil {
			cfg.hooks.AfterBegin(attempt)
		}
		if policy {
			if err := cfg.afterSnapshot(); err != nil {
				path, failure := discardCandidate(backupPath, errors.Join(err, rollbackTransactionControlled(ctx, conn, cfg.transaction)))
				return path, 0, failure
			}
		}
		lockedVersion, err := readDataVersion(ctx, conn, policy.message("read search data_version", "read project-delete data_version"))
		if err != nil {
			failure := errors.Join(err, rollbackTransactionControlled(ctx, conn, cfg.transaction))
			if policy {
				return backupPath, 0, failure
			}
			path, failure := discardCandidate(backupPath, failure)
			return path, 0, failure
		}
		if lockedVersion != afterVersion {
			lastGenerationErr = errors.New(policy.message("search index changed before repair lock", "database changed before project-delete writer lock"))
			if rollbackErr := rollbackTransactionControlled(ctx, conn, cfg.transaction); rollbackErr != nil {
				return backupPath, 0, errors.Join(lastGenerationErr, rollbackErr)
			}
			if path, failure := discardCandidate(backupPath, lastGenerationErr); path != "" {
				return path, 0, failure
			}
			continue
		}
		if !policy {
			if err := cfg.afterSnapshot(); err != nil {
				path, failure := discardCandidate(backupPath, errors.Join(err, rollbackTransactionControlled(ctx, conn, cfg.transaction)))
				return path, 0, failure
			}
		}
		return backupPath, attempt, nil
	}
	exhausted := errors.New(policy.message("search index changed during every confirmed backup attempt", "database changed during every project-delete backup attempt"))
	if !policy {
		exhausted = errors.Join(exhausted, lastGenerationErr)
	}
	return "", 0, exhausted
}

func readDataVersion(ctx context.Context, db sqliteTransaction, label string) (int64, error) {
	var version int64
	if err := db.QueryRowContext(ctx, `PRAGMA data_version`).Scan(&version); err != nil {
		return 0, fmt.Errorf("%s: %w", label, err)
	}
	return version, nil
}

func rollbackTransactionControlled(ctx context.Context, conn sqliteTransaction, control transactionControl) error {
	rollback := control.rollback
	if rollback == nil {
		rollback = rollbackTransaction
	}
	if err := rollback(ctx, conn); err != nil {
		if control.invalidate != nil {
			control.invalidate()
		}
		return fmt.Errorf("%s: %w", control.rollbackLabel, errors.Join(errRollbackUnproven, err))
	}
	return nil
}

func rollbackTransaction(ctx context.Context, conn sqliteTransaction) error {
	rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout)
	defer cancel()
	_, err := conn.ExecContext(rollbackCtx, "ROLLBACK")
	return err
}

func invalidateSQLiteConn(conn *sql.Conn) {
	_ = conn.Raw(func(any) error { return driver.ErrBadConn })
	_ = conn.Close()
}
