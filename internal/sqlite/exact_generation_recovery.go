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

type exactGenerationAttempt struct {
	backupPath    string
	retry         bool
	generationErr error
}

func prepareExactGeneration(ctx context.Context, conn sqliteTransaction, policy exactGenerationPolicy, cfg exactGenerationConfig) (string, int, error) {
	var lastGenerationErr error
	for attempt := 1; attempt <= exactGenerationAttempts; attempt++ {
		result, err := prepareExactGenerationAttempt(ctx, conn, policy, cfg, attempt)
		if err != nil {
			return result.backupPath, 0, err
		}
		if !result.retry {
			return result.backupPath, attempt, nil
		}
		lastGenerationErr = result.generationErr
	}
	exhausted := errors.New(policy.message("search index changed during every confirmed backup attempt", "database changed during every project-delete backup attempt"))
	if !policy {
		exhausted = errors.Join(exhausted, lastGenerationErr)
	}
	return "", 0, exhausted
}

func discardExactGenerationCandidate(policy exactGenerationPolicy, cfg exactGenerationConfig, path string, primary error) (string, error) {
	if bool(policy) && errors.Is(primary, errRollbackUnproven) {
		return path, primary
	}
	if err := cfg.discard(path); err != nil {
		label := policy.message("discard stale reindex backup", "discard stale project-delete backup")
		return path, errors.Join(primary, fmt.Errorf("%s: %w", label, err))
	}
	return "", primary
}

func prepareExactGenerationAttempt(ctx context.Context, conn sqliteTransaction, policy exactGenerationPolicy, cfg exactGenerationConfig, attempt int) (exactGenerationAttempt, error) {
	backupPath, beforeVersion, afterVersion, err := createExactGenerationBackup(ctx, conn, policy, cfg, attempt)
	if err != nil {
		return exactGenerationAttempt{backupPath: backupPath}, err
	}
	if afterVersion != beforeVersion {
		generationErr := errors.New(policy.message("search index changed while backup was created", "database changed while project-delete backup was created"))
		path, failure := discardExactGenerationCandidate(policy, cfg, backupPath, generationErr)
		if path != "" {
			return exactGenerationAttempt{backupPath: path}, failure
		}
		return exactGenerationAttempt{retry: true, generationErr: generationErr}, nil
	}
	if cfg.hooks.BeforeBegin != nil {
		cfg.hooks.BeforeBegin(attempt)
	}
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return beginExactGenerationRetry(ctx, conn, policy, cfg, backupPath, attempt, err)
	}
	if cfg.hooks.AfterBegin != nil {
		cfg.hooks.AfterBegin(attempt)
	}
	if policy {
		if err := cfg.afterSnapshot(); err != nil {
			path, failure := discardExactGenerationCandidate(policy, cfg, backupPath, errors.Join(err, rollbackTransactionControlled(ctx, conn, cfg.transaction)))
			return exactGenerationAttempt{backupPath: path}, failure
		}
	}
	return validateExactGenerationLock(ctx, conn, policy, cfg, backupPath, afterVersion)
}

func createExactGenerationBackup(ctx context.Context, conn sqliteTransaction, policy exactGenerationPolicy, cfg exactGenerationConfig, attempt int) (string, int64, int64, error) {
	if err := cfg.beforeSnapshot(); err != nil {
		return "", 0, 0, err
	}
	versionLabel := policy.message("read search data_version", "read project-delete data_version")
	beforeVersion, err := readDataVersion(ctx, conn, versionLabel)
	if err != nil {
		return "", 0, 0, err
	}
	backupPath, err := cfg.create(ctx, func(path string) error { return cfg.snapshot(ctx, path) })
	if err != nil {
		if !policy {
			err = fmt.Errorf("create project-delete backup: %w", err)
		}
		return "", 0, 0, err
	}
	if policy {
		info, statErr := os.Lstat(backupPath)
		if statErr != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return backupPath, 0, 0, errors.New("generated reindex backup is not a regular file")
		}
	}
	if cfg.hooks.AfterBackup != nil {
		cfg.hooks.AfterBackup(attempt)
	}
	if err := cfg.afterSnapshot(); err != nil {
		path, failure := discardExactGenerationCandidate(policy, cfg, backupPath, err)
		return path, 0, 0, failure
	}
	afterVersion, err := readDataVersion(ctx, conn, versionLabel)
	if err != nil {
		if policy {
			return backupPath, 0, 0, err
		}
		path, failure := discardExactGenerationCandidate(policy, cfg, backupPath, err)
		return path, 0, 0, failure
	}
	return backupPath, beforeVersion, afterVersion, err
}

func beginExactGenerationRetry(ctx context.Context, conn sqliteTransaction, policy exactGenerationPolicy, cfg exactGenerationConfig, backupPath string, attempt int, beginErr error) (exactGenerationAttempt, error) {
	generationErr := fmt.Errorf("%s: %w", policy.message("begin immediate", "acquire project-delete writer lock"), beginErr)
	if policy {
		if validationErr := cfg.afterSnapshot(); validationErr != nil {
			path, failure := discardExactGenerationCandidate(policy, cfg, backupPath, errors.Join(validationErr, generationErr))
			return exactGenerationAttempt{backupPath: path}, failure
		}
		return exactGenerationAttempt{backupPath: backupPath}, generationErr
	}
	path, failure := discardExactGenerationCandidate(policy, cfg, backupPath, generationErr)
	if path != "" {
		return exactGenerationAttempt{backupPath: path}, failure
	}
	return exactGenerationAttempt{retry: true, generationErr: generationErr}, nil
}

func validateExactGenerationLock(ctx context.Context, conn sqliteTransaction, policy exactGenerationPolicy, cfg exactGenerationConfig, backupPath string, afterVersion int64) (exactGenerationAttempt, error) {
	lockedVersion, err := readDataVersion(ctx, conn, policy.message("read search data_version", "read project-delete data_version"))
	if err != nil {
		failure := errors.Join(err, rollbackTransactionControlled(ctx, conn, cfg.transaction))
		if policy {
			return exactGenerationAttempt{backupPath: backupPath}, failure
		}
		path, discardErr := discardExactGenerationCandidate(policy, cfg, backupPath, failure)
		return exactGenerationAttempt{backupPath: path}, discardErr
	}
	if lockedVersion != afterVersion {
		generationErr := errors.New(policy.message("search index changed before repair lock", "database changed before project-delete writer lock"))
		if rollbackErr := rollbackTransactionControlled(ctx, conn, cfg.transaction); rollbackErr != nil {
			return exactGenerationAttempt{backupPath: backupPath}, errors.Join(generationErr, rollbackErr)
		}
		path, failure := discardExactGenerationCandidate(policy, cfg, backupPath, generationErr)
		if path != "" {
			return exactGenerationAttempt{backupPath: path}, failure
		}
		return exactGenerationAttempt{retry: true, generationErr: generationErr}, nil
	}
	if !policy {
		if err := cfg.afterSnapshot(); err != nil {
			path, failure := discardExactGenerationCandidate(policy, cfg, backupPath, errors.Join(err, rollbackTransactionControlled(ctx, conn, cfg.transaction)))
			return exactGenerationAttempt{backupPath: path}, failure
		}
	}
	return exactGenerationAttempt{backupPath: backupPath}, nil
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
