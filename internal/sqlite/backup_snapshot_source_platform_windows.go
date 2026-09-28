//go:build windows

package sqlite

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

type snapshotSourcePin struct {
	file       *os.File
	parentPath string
	ancestors  []*os.File
	target     *os.File
	sidecars   []*os.File
	identity   os.FileInfo
}

func pinSnapshotSource(path string) (string, os.FileInfo, string, func() error, func(string) error, error) {
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return "", nil, "", nil, nil, snapshotSourceValidationError("snapshot source path is invalid")
	}
	absolutePath = filepath.Clean(absolutePath)
	pin, err := pinSnapshotSourceParent(absolutePath)
	if err != nil {
		return "", nil, "", nil, nil, err
	}
	_, identity, err := pin.openTarget(filepath.Base(absolutePath))
	if err != nil {
		_ = pin.close()
		return "", nil, "", nil, nil, err
	}
	if err := pin.validateSidecars(filepath.Base(absolutePath)); err != nil {
		_ = pin.close()
		return "", nil, "", nil, nil, err
	}
	sqliteOpenPath, err := pin.sqlitePath(filepath.Base(absolutePath))
	if err != nil {
		_ = pin.close()
		return "", nil, "", nil, nil, err
	}
	release := func() error { return pin.close() }
	verifyPragma := func(selectedPath string) error {
		return pin.verifyPragmaIdentity(selectedPath, identity)
	}
	return absolutePath, identity, sqliteOpenPath, release, verifyPragma, nil
}

func pinSnapshotSourceParent(path string) (*snapshotSourcePin, error) {
	parent := filepath.Dir(path)
	paths, err := snapshotSourceAncestorPaths(parent)
	if err != nil {
		return nil, snapshotSourceValidationError("snapshot source parent path is invalid")
	}
	pin := &snapshotSourcePin{parentPath: parent}
	for _, ancestor := range paths {
		file, openErr := openSnapshotSourceDirectory(ancestor)
		if openErr != nil {
			_ = pin.close()
			return nil, snapshotSourceValidationError("snapshot source path contains a reparse point or cannot be securely opened")
		}
		pin.ancestors = append(pin.ancestors, file)
	}
	if len(pin.ancestors) == 0 {
		return nil, snapshotSourceValidationError("snapshot source parent cannot be securely opened")
	}
	pin.file = pin.ancestors[len(pin.ancestors)-1]
	return pin, nil
}

func snapshotSourceAncestorPaths(parent string) ([]string, error) {
	parent = filepath.Clean(parent)
	if filepath.VolumeName(parent) == "" {
		return nil, errors.New("snapshot source parent path is not absolute")
	}
	var reversed []string
	for current := parent; ; current = filepath.Dir(current) {
		reversed = append(reversed, current)
		volumeRoot := filepath.VolumeName(current) + string(filepath.Separator)
		if current == filepath.Dir(current) || strings.EqualFold(current, volumeRoot) {
			break
		}
	}
	paths := make([]string, len(reversed))
	for i := range reversed {
		paths[i] = reversed[len(reversed)-1-i]
	}
	return paths, nil
}

func openSnapshotSourceDirectory(path string) (*os.File, error) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(
		pathPtr,
		windows.FILE_READ_ATTRIBUTES|windows.FILE_LIST_DIRECTORY,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(handle), path)
	if err := validateSnapshotSourceHandle(file, true); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

func openSnapshotSourceTarget(path string) (*os.File, error) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	flags := uint32(windows.FILE_ATTRIBUTE_NORMAL | windows.FILE_FLAG_OPEN_REPARSE_POINT)
	handle, err := windows.CreateFile(
		pathPtr,
		windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil,
		windows.OPEN_EXISTING,
		flags,
		0,
	)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(handle), path)
	if err := validateSnapshotSourceHandle(file, false); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

func validateSnapshotSourceHandle(file *os.File, directory bool) error {
	var attributes windows.ByHandleFileInformation
	err := windows.GetFileInformationByHandle(windows.Handle(file.Fd()), &attributes)
	if err != nil || attributes.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return errors.New("snapshot source path contains a reparse point")
	}
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if directory && !info.IsDir() {
		return errors.New("snapshot source parent is not a directory")
	}
	if !directory && !info.Mode().IsRegular() {
		return errors.New("snapshot source is not a regular file")
	}
	return nil
}

func (pin *snapshotSourcePin) openTarget(name string) (bool, os.FileInfo, error) {
	if pin == nil || pin.file == nil {
		return false, nil, snapshotSourceValidationError("snapshot source parent is not pinned")
	}
	file, err := openSnapshotSourceTarget(filepath.Join(pin.parentPath, name))
	if err != nil {
		return false, nil, snapshotSourceValidationError("snapshot source cannot be securely opened")
	}
	if pin.target != nil {
		_ = pin.target.Close()
	}
	pin.target = file
	info, err := file.Stat()
	if err != nil {
		return false, nil, snapshotSourceValidationError("snapshot source cannot be securely inspected")
	}
	pin.identity = info
	return false, info, nil
}

func (pin *snapshotSourcePin) validateSidecars(name string) error {
	if pin == nil || pin.file == nil {
		return snapshotSourceValidationError("snapshot source parent is not pinned")
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		file, err := openSnapshotSourceTarget(filepath.Join(pin.parentPath, name+suffix))
		if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
			continue
		}
		if err != nil {
			return snapshotSourceValidationError("snapshot source journal sidecar cannot be securely opened")
		}
		pin.sidecars = append(pin.sidecars, file)
	}
	return nil
}

func (pin *snapshotSourcePin) sqlitePath(name string) (string, error) {
	if pin == nil || pin.file == nil {
		return "", snapshotSourceValidationError("snapshot source parent is not pinned")
	}
	return filepath.Join(pin.parentPath, name), nil
}

func (pin *snapshotSourcePin) verifyPragmaIdentity(selectedPath string, expected os.FileInfo) error {
	if pin == nil || pin.target == nil {
		return snapshotSourceValidationError("snapshot source is not pinned")
	}
	held, err := pin.target.Stat()
	if err != nil {
		return snapshotSourceValidationError("snapshot source identity is unavailable")
	}
	if !os.SameFile(expected, held) {
		return errors.New("snapshot source changed while opening")
	}
	selected, err := os.Stat(selectedPath)
	if err != nil {
		return fmt.Errorf("stat snapshot source pragma path: %w", err)
	}
	if !os.SameFile(expected, selected) {
		return errors.New("opened snapshot source identity does not match requested file")
	}
	return nil
}

func (pin *snapshotSourcePin) close() error {
	if pin == nil {
		return nil
	}
	var closeErr error
	if pin.target != nil {
		closeErr = errors.Join(closeErr, pin.target.Close())
		pin.target = nil
	}
	for i := len(pin.sidecars) - 1; i >= 0; i-- {
		closeErr = errors.Join(closeErr, pin.sidecars[i].Close())
	}
	pin.sidecars = nil
	for i := len(pin.ancestors) - 1; i >= 0; i-- {
		closeErr = errors.Join(closeErr, pin.ancestors[i].Close())
	}
	pin.ancestors = nil
	pin.file = nil
	pin.identity = nil
	return closeErr
}
