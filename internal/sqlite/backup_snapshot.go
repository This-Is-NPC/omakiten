package sqlite

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type snapshotHooks struct {
	AfterStageCreated                                                        func(string, string)
	AfterVacuum, BeforePublish                                               func()
	BeforePublishCheck                                                       func() error
	BeforeRollbackLinkCleanup                                                func()
	PostPublishCleanupError, PostPublishSyncError, PostRollbackLinkSyncError error
	DeferredCleanupError                                                     error
}

type snapshotExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type snapshotPathIdentity struct {
	path string
	info os.FileInfo
}

const snapshotStageFileName = "snapshot.db"

type snapshotStage struct {
	parent           *os.Root
	name             string
	root             *os.Root
	file, directory  *os.File
	prepare, release func() error
}

func (stage *snapshotStage) cleanup() error {
	var cleanupErr error
	if release := stage.release; release != nil {
		stage.release = nil
		cleanupErr = errors.Join(cleanupErr, wrapSnapshotCloseError("release staged snapshot pathname binding", release()))
	}
	if file := stage.file; file != nil {
		stage.file = nil
		cleanupErr = errors.Join(cleanupErr, wrapSnapshotCloseError("close staged snapshot", file.Close()))
	}
	if stage.root != nil {
		if err := stage.root.Remove(snapshotStageFileName); err != nil && !errors.Is(err, os.ErrNotExist) {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("remove staged snapshot file: %w", err))
		}
	}
	if directory := stage.directory; directory != nil {
		stage.directory = nil
		cleanupErr = errors.Join(cleanupErr, wrapSnapshotCloseError("close pinned snapshot staging directory", directory.Close()))
	}
	if root := stage.root; root != nil {
		stage.root = nil
		cleanupErr = errors.Join(cleanupErr, wrapSnapshotCloseError("close snapshot staging directory", root.Close()))
	}
	parent, name := stage.parent, stage.name
	stage.parent, stage.name, stage.prepare = nil, "", nil
	if parent != nil && name != "" {
		if err := parent.RemoveAll(name); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("remove private snapshot staging: %w", err))
		}
	}
	return cleanupErr
}

type snapshotPublication struct {
	root                                        *os.Root
	directory                                   *os.File
	rootIdentity                                os.FileInfo
	path, name                                  string
	identity                                    os.FileInfo
	rollbackName                                string
	rollbackIdentity                            os.FileInfo
	postPublicationSyncErr, postRollbackSyncErr error
	beforeRollbackCleanup                       func()
	state                                       snapshotPublicationState
}

type snapshotPublicationState uint8

const snapshotPublicationReversible, snapshotPublicationCommitted snapshotPublicationState = 0, 1

func (publication *snapshotPublication) finish(failure error) error {
	if publication.root == nil {
		return failure
	}
	if failure == nil && publication.identity != nil {
		failure = errors.Join(syncPublishedSnapshotDirectory(publication.directory), publication.postPublicationSyncErr)
		if failure == nil && publication.rollbackName != "" {
			if publication.beforeRollbackCleanup != nil {
				publication.beforeRollbackCleanup()
			}
			publication.state, failure = removeSnapshotRollbackLink(publication.root, publication.directory, publication.name, publication.identity, publication.rollbackName, publication.rollbackIdentity, publication.postRollbackSyncErr)
			if publication.state == snapshotPublicationCommitted {
				publication.rollbackName = ""
			}
		}
	}
	if failure != nil && publication.identity != nil && publication.state == snapshotPublicationReversible {
		var resolved bool
		var rollbackErr error
		if publication.rollbackName != "" {
			resolved, rollbackErr = restoreReplacedSnapshot(publication.root, publication.directory, publication.name, publication.identity, publication.rollbackName, publication.rollbackIdentity)
		} else {
			resolved, rollbackErr = removePublishedSnapshot(publication.root, publication.directory, publication.name, publication.identity)
		}
		failure = errors.Join(failure, rollbackErr)
		if resolved {
			publication.identity, publication.rollbackName = nil, ""
		}
	}
	if publication.identity == nil && publication.rollbackName != "" {
		_, cleanupErr := removeSnapshotRollbackLink(publication.root, publication.directory, publication.name, publication.rollbackIdentity, publication.rollbackName, publication.rollbackIdentity, nil)
		failure = errors.Join(failure, cleanupErr)
		publication.rollbackName = ""
	}
	var identityAtPath bool
	if publication.identity != nil && failure != nil {
		current, err := publication.root.Lstat(publication.name)
		identityAtPath = err == nil && os.SameFile(publication.identity, current)
	}
	if publication.directory != nil {
		if err := publication.directory.Close(); err != nil && failure != nil {
			failure = errors.Join(failure, fmt.Errorf("close pinned snapshot destination directory: %w", err))
		}
		publication.directory = nil
	}
	if err := publication.root.Close(); err != nil && failure != nil {
		failure = errors.Join(failure, fmt.Errorf("close snapshot destination directory: %w", err))
	}
	publication.root = nil
	if publication.identity != nil && failure != nil {
		failure = &SnapshotPublicationError{
			PublishedPath:     publication.path,
			PublishedIdentity: publication.identity,
			IdentityAtPath:    identityAtPath,
			Err:               failure,
		}
	}
	publication.path, publication.name, publication.rollbackName = "", "", ""
	publication.rootIdentity, publication.identity, publication.rollbackIdentity = nil, nil, nil
	publication.postPublicationSyncErr, publication.postRollbackSyncErr = nil, nil
	publication.beforeRollbackCleanup = nil
	return failure
}

// SnapshotPublicationError reports a verified publication with an unresolved final state.
type SnapshotPublicationError struct {
	PublishedPath     string
	PublishedIdentity os.FileInfo
	IdentityAtPath    bool
	Err               error
}

func (err *SnapshotPublicationError) Error() string {
	return fmt.Sprintf("snapshot may remain published at %q: %v", err.PublishedPath, err.Err)
}

func (err *SnapshotPublicationError) Unwrap() error {
	return err.Err
}

// Snapshot writes a consistent image from a connection owned by this Store.
// Normal stores acquire one connection from the live pool; maintenance stores
// reuse their identity-verified pinned connection.
func (s *Store) Snapshot(ctx context.Context, destinationPath string) error {
	conn, release, err := s.acquireSearchMaintenanceConn(ctx)
	if err != nil {
		return err
	}
	defer release()
	return snapshotWithExecutor(ctx, conn, destinationPath, false, snapshotHooks{})
}

// SnapshotDatabase writes a transactionally consistent SQLite image without
// replacing an existing destination.
func SnapshotDatabase(ctx context.Context, sourcePath, destinationPath string) error {
	return snapshotDatabaseWithOptions(ctx, sourcePath, destinationPath, false, snapshotHooks{})
}

// SnapshotDatabaseReplace is the explicit force-capable counterpart. It may
// replace an existing regular file, but rejects destination symlinks and other
// file types.
func SnapshotDatabaseReplace(ctx context.Context, sourcePath, destinationPath string) error {
	return snapshotDatabaseWithOptions(ctx, sourcePath, destinationPath, true, snapshotHooks{})
}

func snapshotDatabaseWithOptions(ctx context.Context, sourcePath, destinationPath string, replace bool, hooks snapshotHooks) (returnErr error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	sourcePath, sourceIdentity, err := validateMaintenancePath(sourcePath)
	if err != nil {
		return fmt.Errorf("validate snapshot source: %w", err)
	}
	destinationPath, err = filepath.Abs(destinationPath)
	if err != nil {
		return fmt.Errorf("resolve snapshot destination: %w", err)
	}
	if sourcePath == destinationPath {
		return errors.New("snapshot destination must differ from source")
	}
	db, err := sql.Open("sqlite", sqliteFileURI(sourcePath, "mode=ro"))
	if err != nil {
		return fmt.Errorf("open snapshot source: %w", err)
	}
	var conn *sql.Conn
	closeSource := func() error {
		var closeErr error
		if conn != nil {
			if err := conn.Close(); err != nil {
				closeErr = errors.Join(closeErr, fmt.Errorf("close snapshot source connection: %w", err))
			}
			conn = nil
		}
		if db != nil {
			if err := db.Close(); err != nil {
				closeErr = errors.Join(closeErr, fmt.Errorf("close snapshot source database: %w", err))
			}
			db = nil
		}
		return closeErr
	}
	defer func() {
		returnErr = errors.Join(returnErr, closeSource())
	}()
	conn, err = db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("pin snapshot source connection: %w", err)
	}
	var selectedPath string
	if err := conn.QueryRowContext(ctx, `SELECT file FROM pragma_database_list WHERE name = 'main'`).Scan(&selectedPath); err != nil {
		return fmt.Errorf("read snapshot source identity: %w", err)
	}
	_, selectedIdentity, err := validateMaintenancePath(selectedPath)
	if err != nil || !os.SameFile(sourceIdentity, selectedIdentity) {
		return errors.New("opened snapshot source identity does not match requested file")
	}
	_, currentIdentity, err := validateMaintenancePath(sourcePath)
	if err != nil || !os.SameFile(sourceIdentity, currentIdentity) {
		return errors.New("snapshot source path changed while opening")
	}
	callerBeforePublishCheck := hooks.BeforePublishCheck
	hooks.BeforePublishCheck = func() error {
		var checkErr error
		if callerBeforePublishCheck != nil {
			checkErr = callerBeforePublishCheck()
		}
		return errors.Join(checkErr, closeSource())
	}
	return snapshotWithExecutor(ctx, conn, destinationPath, replace, hooks)
}

func snapshotWithExecutor(ctx context.Context, executor snapshotExecutor, destinationPath string, replace bool, hooks snapshotHooks) (returnErr error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	destinationPath, err := filepath.Abs(destinationPath)
	if err != nil {
		return fmt.Errorf("resolve snapshot destination: %w", err)
	}
	sourceIdentity, err := snapshotExecutorIdentity(ctx, executor)
	if err != nil {
		return err
	}
	parentPath := filepath.Dir(destinationPath)
	identities, err := ensureSecureSnapshotDirectory(parentPath)
	if err != nil {
		return err
	}
	publication := snapshotPublication{
		path:                   destinationPath,
		name:                   filepath.Base(destinationPath),
		postPublicationSyncErr: hooks.PostPublishSyncError,
		beforeRollbackCleanup:  hooks.BeforeRollbackLinkCleanup,
		postRollbackSyncErr:    hooks.PostRollbackLinkSyncError,
	}
	publication.root, err = os.OpenRoot(parentPath)
	if err != nil {
		return fmt.Errorf("open snapshot destination directory: %w", err)
	}
	publication.rootIdentity, err = publication.root.Stat(".")
	if err != nil || !os.SameFile(identities[len(identities)-1].info, publication.rootIdentity) {
		return publication.finish(errors.New("snapshot destination directory changed while opening"))
	}
	publication.directory, err = publication.root.Open(".")
	if err != nil {
		return publication.finish(fmt.Errorf("pin snapshot destination directory: %w", err))
	}
	stage := snapshotStage{parent: publication.root}
	defer func() {
		deferredCleanup := stage.name != ""
		cleanupErr := stage.cleanup()
		if deferredCleanup {
			cleanupErr = errors.Join(cleanupErr, hooks.DeferredCleanupError)
		}
		returnErr = publication.finish(errors.Join(returnErr, cleanupErr))
	}()
	if err := validateSnapshotDestination(publication.root, publication.name, replace, sourceIdentity); err != nil {
		return err
	}

	stage.name, err = createSnapshotStage(publication.root)
	if err != nil {
		return err
	}
	stage.root, err = publication.root.OpenRoot(stage.name)
	if err != nil {
		return fmt.Errorf("open snapshot staging directory: %w", err)
	}
	stage.file, err = stage.root.OpenFile(snapshotStageFileName, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create private snapshot staging file: %w", err)
	}
	stageDirPath := filepath.Join(parentPath, stage.name)
	stageFilePath := filepath.Join(stageDirPath, snapshotStageFileName)
	stage.directory, err = stage.root.Open(".")
	if err != nil {
		return fmt.Errorf("pin snapshot staging directory: %w", err)
	}
	var sqliteStagePath string
	sqliteStagePath, stage.prepare, stage.release, err = bindSnapshotStage(stage.file, stage.directory, stageFilePath)
	if err != nil {
		return err
	}
	if hooks.AfterStageCreated != nil {
		hooks.AfterStageCreated(stageDirPath, stageFilePath)
	}
	if _, err := executor.ExecContext(ctx, `VACUUM INTO ?`, sqliteStagePath); err != nil {
		return fmt.Errorf("vacuum snapshot: %w", err)
	}
	if hooks.AfterVacuum != nil {
		hooks.AfterVacuum()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	stagedInfo, err := stage.file.Stat()
	if err != nil {
		return fmt.Errorf("read staged snapshot identity before verification: %w", err)
	}
	namedStageInfo, err := stage.root.Lstat(snapshotStageFileName)
	if err != nil || namedStageInfo.Mode()&os.ModeSymlink != 0 || !namedStageInfo.Mode().IsRegular() || !os.SameFile(stagedInfo, namedStageInfo) {
		return errors.New("staged snapshot identity changed before verification")
	}
	if err := verifySQLiteSnapshot(ctx, sqliteStagePath); err != nil {
		return err
	}
	namedStageInfo, err = stage.root.Lstat(snapshotStageFileName)
	if err != nil || !os.SameFile(stagedInfo, namedStageInfo) {
		return errors.New("staged snapshot identity changed during verification")
	}
	if err := stage.file.Chmod(0o600); err != nil {
		return fmt.Errorf("chmod snapshot: %w", err)
	}
	if err := stage.file.Sync(); err != nil {
		return fmt.Errorf("sync snapshot: %w", err)
	}
	stagedInfo, err = stage.file.Stat()
	if err != nil {
		return fmt.Errorf("read staged snapshot identity: %w", err)
	}
	if err := stage.prepare(); err != nil {
		return fmt.Errorf("prepare staged snapshot pathname binding for publication: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if hooks.BeforePublish != nil {
		hooks.BeforePublish()
	}
	if hooks.BeforePublishCheck != nil {
		if err := hooks.BeforePublishCheck(); err != nil {
			return err
		}
	}
	if err := validateSecureSnapshotDirectory(identities); err != nil {
		return err
	}
	currentRootInfo, err := publication.root.Stat(".")
	if err != nil || !os.SameFile(publication.rootIdentity, currentRootInfo) {
		return errors.New("snapshot destination directory changed before publish")
	}

	if err := validateSnapshotDestination(publication.root, publication.name, replace, sourceIdentity); err != nil {
		return err
	}
	if err := validateSnapshotStageForPublication(stage.root, snapshotStageFileName, stagedInfo); err != nil {
		return err
	}
	publishName := publication.name
	if replace {
		publishName, err = randomSnapshotName(".omakiten-publish-")
		if err != nil {
			return err
		}
	}
	if err := linkSnapshotFile(stage.file, stage.directory, snapshotStageFileName, publication.directory, publishName); err != nil {
		linkErr := fmt.Errorf("link verified snapshot for publication: %w", err)
		if !replace {
			if current, statErr := publication.root.Lstat(publication.name); statErr == nil && os.SameFile(stagedInfo, current) {
				publication.identity = current
			}
		} else if current, statErr := publication.root.Lstat(publishName); statErr == nil && os.SameFile(stagedInfo, current) {
			_, cleanupErr := removePublishedSnapshot(publication.root, publication.directory, publishName, stagedInfo)
			return errors.Join(linkErr, cleanupErr)
		}
		return linkErr
	}
	if !replace {
		publication.identity = stagedInfo
	}
	candidateInfo, err := publication.root.Lstat(publishName)
	if err != nil || candidateInfo.Mode()&os.ModeSymlink != 0 || !candidateInfo.Mode().IsRegular() || !os.SameFile(stagedInfo, candidateInfo) {
		identityErr := errors.New("linked snapshot identity does not match verified staged file")
		if !replace {
			return identityErr
		}
		if err == nil && os.SameFile(stagedInfo, candidateInfo) {
			_, cleanupErr := removePublishedSnapshot(publication.root, publication.directory, publishName, stagedInfo)
			return errors.Join(identityErr, cleanupErr)
		}
		return identityErr
	}
	if replace {
		if err := validateSnapshotDestination(publication.root, publication.name, true, sourceIdentity); err != nil {
			_, cleanupErr := removePublishedSnapshot(publication.root, publication.directory, publishName, candidateInfo)
			return errors.Join(err, cleanupErr)
		}
		publication.rollbackName, publication.rollbackIdentity, err = prepareSnapshotReplacement(publication.root, publication.directory, publication.name)
		if err != nil {
			_, cleanupErr := removePublishedSnapshot(publication.root, publication.directory, publishName, candidateInfo)
			return errors.Join(err, cleanupErr)
		}
		if err := renameSnapshotLink(publication.root, publication.directory, publishName, publication.name); err != nil {
			renameErr := fmt.Errorf("replace snapshot destination: %w", err)
			if current, statErr := publication.root.Lstat(publication.name); statErr == nil && os.SameFile(stagedInfo, current) {
				publication.identity = current
				return renameErr
			}
			_, cleanupErr := removePublishedSnapshot(publication.root, publication.directory, publishName, candidateInfo)
			return errors.Join(renameErr, cleanupErr)
		}
		publication.identity = stagedInfo
	}
	finalInfo, err := publication.root.Lstat(publication.name)
	if err != nil || finalInfo.Mode()&os.ModeSymlink != 0 || !finalInfo.Mode().IsRegular() || !os.SameFile(stagedInfo, finalInfo) || os.SameFile(sourceIdentity, finalInfo) {
		return errors.New("final snapshot publication identity check failed")
	}
	publication.identity = finalInfo

	return errors.Join(stage.cleanup(), hooks.PostPublishCleanupError)
}

func prepareSnapshotReplacement(root *os.Root, directory *os.File, destinationName string) (string, os.FileInfo, error) {
	pathInfo, err := root.Lstat(destinationName)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil, nil
	}
	if err != nil || pathInfo.Mode()&os.ModeSymlink != 0 || !pathInfo.Mode().IsRegular() {
		return "", nil, errors.New("force snapshot destination changed before rollback link")
	}
	handle, err := root.OpenFile(destinationName, os.O_RDONLY, 0)
	if err != nil {
		return "", nil, fmt.Errorf("open force snapshot destination for rollback: %w", err)
	}
	handleInfo, err := handle.Stat()
	if err != nil || !os.SameFile(pathInfo, handleInfo) {
		_ = handle.Close()
		return "", nil, errors.New("force snapshot destination changed while pinning rollback inode")
	}
	rollbackName, err := randomSnapshotName(".omakiten-rollback-")
	if err != nil {
		_ = handle.Close()
		return "", nil, err
	}
	if err := linkSnapshotFile(handle, directory, destinationName, directory, rollbackName); err != nil {
		_ = handle.Close()
		return "", nil, fmt.Errorf("retain force snapshot rollback inode: %w", err)
	}
	rollbackInfo, err := root.Lstat(rollbackName)
	if err != nil || rollbackInfo.Mode()&os.ModeSymlink != 0 || !rollbackInfo.Mode().IsRegular() || !os.SameFile(handleInfo, rollbackInfo) {
		if err == nil && os.SameFile(handleInfo, rollbackInfo) {
			_ = removeSnapshotLink(root, directory, rollbackName)
		}
		_ = handle.Close()
		return "", nil, errors.New("force snapshot rollback link identity mismatch")
	}
	// The private hard link pins the old inode, so the Windows handle can be
	// released before replacement without weakening rollback identity.
	if err := handle.Close(); err != nil {
		return rollbackName, rollbackInfo, fmt.Errorf("close force snapshot destination after rollback link: %w", err)
	}
	return rollbackName, rollbackInfo, nil
}

func restoreReplacedSnapshot(root *os.Root, directory *os.File, destinationName string, published os.FileInfo, rollbackName string, previous os.FileInfo) (bool, error) {
	rollbackInfo, err := root.Lstat(rollbackName)
	if err != nil || rollbackInfo.Mode()&os.ModeSymlink != 0 || !rollbackInfo.Mode().IsRegular() || !os.SameFile(previous, rollbackInfo) {
		return false, errors.New("force snapshot rollback inode changed before restoration")
	}
	current, err := root.Lstat(destinationName)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("inspect force snapshot destination for restoration: %w", err)
	}
	if err == nil && (current.Mode()&os.ModeSymlink != 0 || !current.Mode().IsRegular() || !os.SameFile(published, current)) {
		return false, errors.New("force snapshot destination changed before restoration")
	}
	renameErr := renameSnapshotLink(root, directory, rollbackName, destinationName)
	restored, statErr := root.Lstat(destinationName)
	if statErr != nil || !os.SameFile(previous, restored) {
		return false, errors.Join(fmt.Errorf("restore force snapshot destination: %w", renameErr), statErr)
	}
	if rollback, err := root.Lstat(rollbackName); err == nil {
		return false, fmt.Errorf("force snapshot rollback link survived restoration (same identity: %t)", os.SameFile(previous, rollback))
	} else if !errors.Is(err, os.ErrNotExist) {
		return true, fmt.Errorf("verify force snapshot rollback link removal: %w", err)
	}
	if err := syncPublishedSnapshotDirectory(directory); err != nil {
		return true, fmt.Errorf("sync restored force snapshot directory: %w", err)
	}
	return true, renameErr
}

func removeSnapshotRollbackLink(root *os.Root, directory *os.File, destinationName string, published os.FileInfo, rollbackName string, previous os.FileInfo, postSyncErr error) (snapshotPublicationState, error) {
	destination, err := root.Lstat(destinationName)
	if err != nil || destination.Mode()&os.ModeSymlink != 0 || !destination.Mode().IsRegular() || !os.SameFile(published, destination) {
		return snapshotPublicationReversible, errors.Join(errors.New("force snapshot destination changed before rollback-link cleanup"), err)
	}
	rollback, err := root.Lstat(rollbackName)
	if err != nil || rollback.Mode()&os.ModeSymlink != 0 || !rollback.Mode().IsRegular() || !os.SameFile(previous, rollback) {
		return snapshotPublicationReversible, errors.New("force snapshot rollback link changed before cleanup")
	}
	if err := removeSnapshotLink(root, directory, rollbackName); err != nil {
		return snapshotPublicationReversible, fmt.Errorf("remove force snapshot rollback link: %w", err)
	}
	if _, err := root.Lstat(rollbackName); !errors.Is(err, os.ErrNotExist) {
		return snapshotPublicationCommitted, fmt.Errorf("verify force snapshot rollback-link cleanup: %w", err)
	}
	if err := errors.Join(syncPublishedSnapshotDirectory(directory), postSyncErr); err != nil {
		return snapshotPublicationCommitted, fmt.Errorf("sync force snapshot rollback-link cleanup: %w", err)
	}
	return snapshotPublicationCommitted, nil
}

func snapshotExecutorIdentity(ctx context.Context, executor snapshotExecutor) (os.FileInfo, error) {
	var sourcePath string
	if err := executor.QueryRowContext(ctx, `SELECT file FROM pragma_database_list WHERE name = 'main'`).Scan(&sourcePath); err != nil {
		return nil, fmt.Errorf("read snapshot source path: %w", err)
	}
	_, identity, err := validateMaintenancePath(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("validate snapshot source identity: %w", err)
	}
	return identity, nil
}

func wrapSnapshotCloseError(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func validateSnapshotDestination(root *os.Root, name string, replace bool, sourceIdentity os.FileInfo) error {
	info, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect snapshot destination: %w", err)
	}
	if os.SameFile(sourceIdentity, info) {
		return errors.New("snapshot source and destination are the same file")
	}
	if !replace {
		return errors.New("snapshot destination already exists")
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("force snapshot destination must be a regular non-symlink file")
	}
	return nil
}

func randomSnapshotName(prefix string) (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", fmt.Errorf("generate snapshot publication name: %w", err)
	}
	return prefix + hex.EncodeToString(random[:]), nil
}

func createSnapshotStage(root *os.Root) (string, error) {
	for range 10 {
		name, err := randomSnapshotName(".omakiten-backup-")
		if err != nil {
			return "", err
		}
		if err := root.Mkdir(name, 0o700); err == nil {
			return name, nil
		} else if !errors.Is(err, os.ErrExist) {
			return "", fmt.Errorf("create private snapshot staging directory: %w", err)
		}
	}
	return "", errors.New("create private snapshot staging directory: name collision limit reached")
}

func removePublishedSnapshot(root *os.Root, directory *os.File, name string, expected os.FileInfo) (bool, error) {
	current, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect published snapshot for rollback: %w", err)
	}
	if current.Mode()&os.ModeSymlink != 0 || !current.Mode().IsRegular() || !os.SameFile(expected, current) {
		return false, errors.New("published snapshot identity changed before rollback")
	}
	if err := removeSnapshotLink(root, directory, name); err != nil {
		return false, fmt.Errorf("remove published snapshot after failure: %w", err)
	}
	if current, err := root.Lstat(name); err == nil {
		return false, fmt.Errorf("snapshot destination still exists after rollback (same identity: %t)", os.SameFile(expected, current))
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("verify snapshot publication rollback: %w", err)
	}
	if err := syncPublishedSnapshotDirectory(directory); err != nil {
		return true, fmt.Errorf("sync snapshot publication rollback: %w", err)
	}
	return true, nil
}

func ensureSecureSnapshotDirectory(path string) ([]snapshotPathIdentity, error) {
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve snapshot directory: %w", err)
	}
	components := []string{filepath.Clean(absolutePath)}
	for current := components[0]; ; {
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		components = append(components, parent)
		current = parent
	}
	identities := make([]snapshotPathIdentity, 0, len(components))
	for index := len(components) - 1; index >= 0; index-- {
		component := components[index]
		info, err := os.Lstat(component)
		if errors.Is(err, os.ErrNotExist) {
			if err := os.Mkdir(component, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
				return nil, fmt.Errorf("create snapshot directory component: %w", err)
			}
			info, err = os.Lstat(component)
		}
		if err != nil {
			return nil, fmt.Errorf("inspect snapshot directory component: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return nil, errors.New("snapshot destination path contains a symlink or non-directory component")
		}
		identities = append(identities, snapshotPathIdentity{path: component, info: info})
	}
	return identities, nil
}

func validateSecureSnapshotDirectory(identities []snapshotPathIdentity) error {
	for _, identity := range identities {
		info, err := os.Lstat(identity.path)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || !os.SameFile(identity.info, info) {
			return errors.New("snapshot destination path changed before publish")
		}
	}
	return nil
}

func verifySQLiteSnapshot(ctx context.Context, path string) (returnErr error) {
	uri := sqliteFileURI(path, "mode=ro")
	db, err := sql.Open("sqlite", uri)
	if err != nil {
		return fmt.Errorf("open generated snapshot: %w", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("close generated snapshot verification database: %w", err))
		}
	}()
	var result string
	if err := db.QueryRowContext(ctx, `PRAGMA quick_check`).Scan(&result); err != nil {
		return fmt.Errorf("verify generated snapshot: %w", err)
	}
	if result != "ok" {
		return errors.New("verify generated snapshot: quick_check failed")
	}
	return nil
}
