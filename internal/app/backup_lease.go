package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const backupLeaseFilename = ".omakiten-backup.lock"

const backupLockRetryInterval = 10 * time.Millisecond

func waitForBackupLockRetry(ctx context.Context) error {
	timer := time.NewTimer(backupLockRetryInterval)
	select {
	case <-ctx.Done():
		if !timer.Stop() {
			<-timer.C
		}
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type backupDirectoryLease struct {
	service   *BackupService
	root      *os.Root
	dirPath   string
	dirInfo   os.FileInfo
	lockFile  *os.File
	lockInfo  os.FileInfo
	generated map[string]os.FileInfo
}

// WithLease holds a rooted, cross-process backup-directory lease for the full
// callback. Unix locks the pinned directory inode; Windows pins the persistent
// lock path with a no-delete-share handle before taking its byte-range lock.
func (s *BackupService) WithLease(ctx context.Context, run func(BackupLease) error) (returnErr error) {
	if run == nil {
		return errors.New("backup lease callback is required")
	}
	root, dirPath, dirInfo, err := openBackupRoot(s.destDir)
	if err != nil {
		return err
	}
	lease := &backupDirectoryLease{
		service:   s,
		root:      root,
		dirPath:   dirPath,
		dirInfo:   dirInfo,
		generated: make(map[string]os.FileInfo),
	}
	defer func() {
		returnErr = errors.Join(returnErr, lease.close())
	}()

	lockFile, err := root.OpenFile(backupLeaseFilename, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("open backup lease: %w", err)
	}
	lease.lockFile = lockFile
	lockPathInfo, err := root.Lstat(backupLeaseFilename)
	if err != nil || !lockPathInfo.Mode().IsRegular() || lockPathInfo.Mode()&os.ModeSymlink != 0 {
		return errors.New("backup lease path is not a regular file")
	}
	lockHandleInfo, err := lockFile.Stat()
	if err != nil || !os.SameFile(lockPathInfo, lockHandleInfo) {
		return errors.New("backup lease file changed while opening")
	}
	if err := validateBackupFileSecurity(lockPathInfo, "backup lease file"); err != nil {
		return err
	}
	lease.lockInfo = lockHandleInfo
	unlock, err := lockBackupDirectory(ctx, dirPath, dirInfo, lockHandleInfo)
	if err != nil {
		return fmt.Errorf("acquire backup lease: %w", err)
	}
	defer func() {
		returnErr = errors.Join(returnErr, unlock())
	}()
	if err := lease.Validate(); err != nil {
		return err
	}
	return run(lease)
}

// DestructiveOperationResult separates committed mutations from retryable failures.
type DestructiveOperationResult struct {
	BackupPath        string
	MutationCompleted bool
	Err               error
}

// RunLeasedDestructiveOperation holds one lease across recovery, mutation, and pruning.
func RunLeasedDestructiveOperation(ctx context.Context, backup BackupLeaser, run func(RecoveryLease) DestructiveOperationResult) (DestructiveOperationResult, error) {
	var operation DestructiveOperationResult
	leaseErr := backup.WithLease(ctx, func(lease BackupLease) error {
		operation = run(lease)
		if operation.BackupPath != "" {
			if operation.MutationCompleted {
				_ = lease.PruneRetaining(operation.BackupPath)
			} else {
				_ = lease.PruneFailedRetaining(operation.BackupPath)
			}
		}
		return nil
	})
	return operation, leaseErr
}

func (l *backupDirectoryLease) close() error {
	var err error
	if l.lockFile != nil {
		err = errors.Join(err, l.lockFile.Close())
		l.lockFile = nil
	}
	if l.root != nil {
		err = errors.Join(err, l.root.Close())
		l.root = nil
	}
	return err
}

func (l *backupDirectoryLease) Validate() error {
	_, current, err := validateBackupDirectoryPath(l.dirPath)
	if err != nil {
		return fmt.Errorf("validate leased backup directory: %w", err)
	}
	rootInfo, err := l.root.Stat(".")
	if err != nil {
		return fmt.Errorf("stat leased backup root: %w", err)
	}
	if !os.SameFile(l.dirInfo, current) || !os.SameFile(l.dirInfo, rootInfo) {
		return errors.New("backup directory changed while leased")
	}
	if err := validateBackupFileSecurity(current, "backup directory"); err != nil {
		return err
	}
	lockPathInfo, err := l.root.Lstat(backupLeaseFilename)
	if err != nil || lockPathInfo.Mode()&os.ModeSymlink != 0 || !lockPathInfo.Mode().IsRegular() || !os.SameFile(l.lockInfo, lockPathInfo) {
		return errors.New("backup lease pathname changed while leased")
	}
	lockHandleInfo, err := l.lockFile.Stat()
	if err != nil || !os.SameFile(l.lockInfo, lockHandleInfo) || !os.SameFile(lockPathInfo, lockHandleInfo) {
		return errors.New("backup lease handle changed while leased")
	}
	if err := validateBackupFileSecurity(lockPathInfo, "backup lease file"); err != nil {
		return err
	}
	for name, expected := range l.generated {
		generated, err := l.root.Lstat(name)
		if err != nil || generated.Mode()&os.ModeSymlink != 0 || !generated.Mode().IsRegular() || !os.SameFile(expected, generated) {
			return fmt.Errorf("generated backup %s changed while leased", name)
		}
	}
	return nil
}

func (l *backupDirectoryLease) Write(ctx context.Context) (string, error) {
	return l.write(ctx, nil)
}

func (l *backupDirectoryLease) WriteSnapshot(ctx context.Context, write func(string) error) (string, error) {
	if write == nil {
		return "", errors.New("snapshot writer is required")
	}
	return l.write(ctx, write)
}

func (l *backupDirectoryLease) write(ctx context.Context, write func(string) error) (string, error) {
	if err := l.Validate(); err != nil {
		return "", err
	}
	path, err := l.service.write(ctx, write)
	if err != nil {
		return "", err
	}
	if err := l.Validate(); err != nil {
		return "", err
	}
	name, err := l.nameWithinRoot(path)
	if err != nil {
		return "", err
	}
	info, err := l.root.Lstat(name)
	if err != nil {
		return "", fmt.Errorf("stat generated backup: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", errors.New("generated backup is not a regular file")
	}
	l.generated[name] = info
	return path, nil
}

func (l *backupDirectoryLease) Discard(path string) error {
	if err := l.Validate(); err != nil {
		return err
	}
	name, err := l.nameWithinRoot(path)
	if err != nil {
		return err
	}
	expected, ok := l.generated[name]
	if !ok {
		return errors.New("refusing to discard a backup not generated by this lease")
	}
	if err := quarantineAndRemoveBackup(l.root, name, expected, nil); err != nil {
		return fmt.Errorf("discard generated backup: %w", err)
	}
	delete(l.generated, name)
	return nil
}

func (l *backupDirectoryLease) PruneRetaining(retainedPath string) error {
	err := l.pruneRetaining(retainedPath, l.service.retention)
	if err != nil && l.service.pruneWarn != nil {
		l.service.pruneWarn(err)
	}
	return err
}

// PruneFailedRetaining bounds backups after a destructive operation returns an
// error while conservatively protecting its current recovery image. Retention
// disabled normally means "accumulate", but an error path uses a floor/cap of
// one so repeated pre-lock failures cannot grow storage without bound.
func (l *backupDirectoryLease) PruneFailedRetaining(retainedPath string) error {
	retention := l.service.retention
	if retention <= 0 {
		retention = 1
	}
	err := l.pruneRetaining(retainedPath, retention)
	if err != nil && l.service.pruneWarn != nil {
		l.service.pruneWarn(err)
	}
	return err
}

// pruneRetaining removes only identity-stable regular files beneath the pinned
// root. A replacement of the destination path aborts the pass before removal;
// matching symlinks, directories, and unrelated names are never candidates.
func (l *backupDirectoryLease) pruneRetaining(retainedPath string, retention int) error {
	if err := l.Validate(); err != nil {
		return err
	}
	if retention <= 0 {
		return nil
	}

	var retainedName string
	var retainedInfo os.FileInfo
	if retainedPath != "" {
		var err error
		retainedName, err = l.nameWithinRoot(retainedPath)
		if err != nil {
			return err
		}
		retainedInfo = l.generated[retainedName]
		currentRetained, err := l.root.Lstat(retainedName)
		if err != nil {
			return fmt.Errorf("stat retained backup: %w", err)
		}
		if currentRetained.Mode()&os.ModeSymlink != 0 || !currentRetained.Mode().IsRegular() {
			return errors.New("retained backup is not a regular file")
		}
		if retainedInfo != nil && !os.SameFile(retainedInfo, currentRetained) {
			return errors.New("retained backup changed while leased")
		}
		retainedInfo = currentRetained
	}

	directory, err := l.root.Open(".")
	if err != nil {
		return fmt.Errorf("open backup root for pruning: %w", err)
	}
	entries, readErr := directory.ReadDir(-1)
	closeErr := directory.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return fmt.Errorf("list backup root: %w", err)
	}
	type backupEntry struct {
		name string
		info os.FileInfo
	}
	candidates := make([]backupEntry, 0, len(entries))
	for _, entry := range entries {
		if !backupFilenamePattern.MatchString(entry.Name()) {
			continue
		}
		info, err := l.root.Lstat(entry.Name())
		if err != nil {
			return fmt.Errorf("lstat backup candidate %s: %w", entry.Name(), err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			continue
		}
		candidates = append(candidates, backupEntry{name: entry.Name(), info: info})
	}
	if len(candidates) <= retention {
		return nil
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].info.ModTime().Equal(candidates[j].info.ModTime()) {
			return candidates[i].name > candidates[j].name
		}
		return candidates[i].info.ModTime().After(candidates[j].info.ModTime())
	})
	keep := make(map[string]struct{}, retention)
	if retainedName != "" {
		keep[retainedName] = struct{}{}
	}
	for _, candidate := range candidates {
		if len(keep) >= retention {
			break
		}
		keep[candidate.name] = struct{}{}
	}
	for _, candidate := range candidates {
		if _, ok := keep[candidate.name]; ok {
			continue
		}
		if err := l.Validate(); err != nil {
			return err
		}
		if retainedName != "" {
			protected, err := l.root.Lstat(retainedName)
			if err != nil || !protected.Mode().IsRegular() || !os.SameFile(retainedInfo, protected) {
				return errors.New("retained backup changed during pruning")
			}
		}
		if err := quarantineAndRemoveBackup(l.root, candidate.name, candidate.info, retainedInfo); err != nil {
			return fmt.Errorf("remove backup %s: %w", candidate.name, err)
		}
		delete(l.generated, candidate.name)
	}
	return nil
}

func (l *backupDirectoryLease) nameWithinRoot(path string) (string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve backup path: %w", err)
	}
	absPath = filepath.Clean(absPath)
	if filepath.Dir(absPath) != l.dirPath {
		return "", errors.New("backup path is outside the leased directory")
	}
	name := filepath.Base(absPath)
	if name == "." || name == string(filepath.Separator) {
		return "", errors.New("backup path has no filename")
	}
	return name, nil
}

func openBackupRoot(path string) (*os.Root, string, os.FileInfo, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, "", nil, fmt.Errorf("resolve backup directory: %w", err)
	}
	absPath = filepath.Clean(absPath)

	existingPath := absPath
	missing := make([]string, 0, 4)
	for {
		info, statErr := os.Lstat(existingPath)
		if statErr == nil {
			if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
				return nil, "", nil, errors.New("backup directory path has a non-directory or symlink component")
			}
			break
		}
		if !os.IsNotExist(statErr) {
			return nil, "", nil, fmt.Errorf("inspect backup directory: %w", statErr)
		}
		parent := filepath.Dir(existingPath)
		if parent == existingPath {
			return nil, "", nil, errors.New("backup directory has no existing ancestor")
		}
		missing = append(missing, filepath.Base(existingPath))
		existingPath = parent
	}
	_, existingInfo, err := validateBackupDirectoryPath(existingPath)
	if err != nil {
		return nil, "", nil, err
	}
	root, err := os.OpenRoot(existingPath)
	if err != nil {
		return nil, "", nil, fmt.Errorf("open backup root: %w", err)
	}
	rootInfo, err := root.Stat(".")
	if err != nil || !os.SameFile(existingInfo, rootInfo) {
		_ = root.Close()
		return nil, "", nil, errors.New("backup directory changed while opening")
	}

	for index := len(missing) - 1; index >= 0; index-- {
		name := missing[index]
		if err := root.Mkdir(name, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			_ = root.Close()
			return nil, "", nil, fmt.Errorf("create backup directory component %s: %w", name, err)
		}
		pathInfo, err := root.Lstat(name)
		if err != nil || pathInfo.Mode()&os.ModeSymlink != 0 || !pathInfo.IsDir() {
			_ = root.Close()
			return nil, "", nil, errors.New("created backup directory component changed identity")
		}
		next, err := root.OpenRoot(name)
		if err != nil {
			_ = root.Close()
			return nil, "", nil, fmt.Errorf("open created backup directory component %s: %w", name, err)
		}
		nextInfo, err := next.Stat(".")
		if err != nil || !os.SameFile(pathInfo, nextInfo) {
			_ = next.Close()
			_ = root.Close()
			return nil, "", nil, errors.New("created backup directory changed while opening")
		}
		_ = root.Close()
		root = next
	}

	_, info, err := validateBackupDirectoryPath(absPath)
	if err != nil {
		_ = root.Close()
		return nil, "", nil, err
	}
	rootInfo, err = root.Stat(".")
	if err != nil || !os.SameFile(info, rootInfo) {
		_ = root.Close()
		return nil, "", nil, errors.New("backup directory changed while creating")
	}
	if err := validateBackupFileSecurity(info, "backup directory"); err != nil {
		_ = root.Close()
		return nil, "", nil, err
	}
	return root, absPath, info, nil
}

func validateBackupDirectoryPath(path string) (string, os.FileInfo, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", nil, err
	}
	absPath = filepath.Clean(absPath)
	components := []string{absPath}
	for current := absPath; ; {
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		components = append(components, parent)
		current = parent
	}
	var leaf os.FileInfo
	for index := len(components) - 1; index >= 0; index-- {
		info, err := os.Lstat(components[index])
		if err != nil {
			return "", nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", nil, errors.New("backup directory path contains a symlink")
		}
		if index == 0 {
			leaf = info
		}
	}
	if leaf == nil || !leaf.IsDir() {
		return "", nil, errors.New("backup directory path is not a directory")
	}
	return absPath, leaf, nil
}

// quarantineAndRemoveBackup atomically moves a candidate away from its public
// name through the pinned root, verifies the moved inode, and only then unlinks
// it. A mismatch is restored without overwriting a concurrently-created name.
func quarantineAndRemoveBackup(root *os.Root, name string, expected, retained os.FileInfo) error {
	quarantine, err := randomBackupQuarantineName()
	if err != nil {
		return err
	}
	if err := root.Rename(name, quarantine); err != nil {
		return fmt.Errorf("quarantine backup candidate: %w", err)
	}
	quarantined, statErr := root.Lstat(quarantine)
	matches := statErr == nil && quarantined.Mode()&os.ModeSymlink == 0 && quarantined.Mode().IsRegular() && os.SameFile(expected, quarantined)
	protected := matches && retained != nil && os.SameFile(retained, quarantined)
	if matches && !protected {
		if err := root.Remove(quarantine); err != nil {
			return fmt.Errorf("unlink quarantined backup: %w", err)
		}
		return nil
	}

	restoreErr := root.Link(quarantine, name)
	if restoreErr == nil {
		restored, err := root.Lstat(name)
		if err != nil || statErr != nil || !os.SameFile(quarantined, restored) {
			restoreErr = errors.New("restored backup identity could not be verified")
		} else if err := root.Remove(quarantine); err != nil {
			restoreErr = fmt.Errorf("remove restored quarantine link: %w", err)
		}
	}
	identityErr := errors.New("backup candidate changed before quarantined removal")
	if protected {
		identityErr = errors.New("refusing to remove retained backup identity")
	}
	if statErr != nil {
		identityErr = errors.Join(identityErr, statErr)
	}
	return errors.Join(identityErr, restoreErr)
}

func randomBackupQuarantineName() (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", fmt.Errorf("generate backup quarantine name: %w", err)
	}
	return ".omakiten-prune-" + hex.EncodeToString(random[:]), nil
}
