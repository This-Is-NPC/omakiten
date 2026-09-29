//go:build windows

package config

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows has no openat(2), so this backend uses the native NT handle APIs.
// OBJ_DONT_REPARSE makes each component lookup no-follow and RootDirectory
// keeps every operation relative to the directory handle that was opened.
func openDirNoFollow(path string, create bool) (windows.Handle, string, error) {
	return openDirNoFollowAccess(path, create, windows.FILE_GENERIC_READ)
}

func openDirNoFollowAccess(path string, create bool, access uint32) (windows.Handle, string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return windows.InvalidHandle, "", err
	}
	volume := filepath.VolumeName(absolute)
	rootPath := volume + string(filepath.Separator)
	rootName, err := windows.UTF16PtrFromString(rootPath)
	if err != nil {
		return windows.InvalidHandle, "", err
	}
	root, err := windows.CreateFile(rootName, windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return windows.InvalidHandle, "", err
	}
	current := root
	components := windowsPathComponents(absolute)
	for i, component := range components {
		componentAccess := uint32(windows.FILE_GENERIC_READ)
		if create || i == len(components)-1 {
			componentAccess = access
		}
		next, openErr := openRelative(current, component, componentAccess, windows.FILE_OPEN, windows.FILE_DIRECTORY_FILE|windows.FILE_OPEN_REPARSE_POINT)
		if openErr != nil && create && (windowsErrorIs(openErr, syscall.ERROR_PATH_NOT_FOUND) || windowsErrorIs(openErr, syscall.ERROR_FILE_NOT_FOUND)) {
			createAccess := componentAccess | windows.FILE_GENERIC_WRITE
			next, openErr = openRelative(current, component, createAccess, windows.FILE_OPEN_IF, windows.FILE_DIRECTORY_FILE|windows.FILE_OPEN_REPARSE_POINT)
		}
		if openErr != nil {
			_ = windows.CloseHandle(current)
			return windows.InvalidHandle, "", noFollowOpenError(filepath.Join(absolute, component), openErr)
		}
		_ = windows.CloseHandle(current)
		current = next
	}
	return current, absolute, nil
}

func openRelative(parent windows.Handle, name string, access, disposition, options uint32) (windows.Handle, error) {
	objectName, err := windows.NewNTUnicodeString(name)
	if err != nil {
		return windows.InvalidHandle, err
	}
	oa := &windows.OBJECT_ATTRIBUTES{
		Length:        uint32(unsafe.Sizeof(windows.OBJECT_ATTRIBUTES{})),
		RootDirectory: parent,
		ObjectName:    objectName,
		Attributes:    windows.OBJ_CASE_INSENSITIVE | windows.OBJ_DONT_REPARSE,
	}
	var handle windows.Handle
	var status windows.IO_STATUS_BLOCK
	var allocation int64
	err = windows.NtCreateFile(&handle, access|windows.SYNCHRONIZE,
		oa, &status, &allocation, 0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		disposition, options|windows.FILE_SYNCHRONOUS_IO_NONALERT, 0, 0)
	if err != nil {
		return windows.InvalidHandle, err
	}
	return handle, nil
}

func windowsErrorIs(err error, want syscall.Errno) bool {
	return errors.Is(windowsErrorAsErrno(err), want)
}

func windowsErrorAsErrno(err error) error {
	if err == nil {
		return nil
	}
	var status windows.NTStatus
	if errors.As(err, &status) {
		return status.Errno()
	}
	return err
}

func windowsPathComponents(path string) []string {
	path = strings.TrimPrefix(path, filepath.VolumeName(path))
	path = strings.Trim(path, `\\/`)
	if path == "" {
		return nil
	}
	parts := strings.FieldsFunc(path, func(r rune) bool { return r == '\\' || r == '/' })
	return parts
}

func noFollowOpenError(path string, err error) error {
	return &os.PathError{Op: "open", Path: path, Err: windowsErrorAsErrno(err)}
}

func openFileNoFollow(path string) (*os.File, error) {
	dir, absolute, err := openDirNoFollow(filepath.Dir(path), false)
	if err != nil {
		return nil, err
	}
	defer func() { _ = windows.CloseHandle(dir) }()
	handle, err := openRelative(dir, filepath.Base(path), windows.FILE_GENERIC_READ, windows.FILE_OPEN, windows.FILE_NON_DIRECTORY_FILE|windows.FILE_OPEN_REPARSE_POINT)
	if err != nil {
		return nil, noFollowOpenError(filepath.Join(absolute, filepath.Base(path)), err)
	}
	if err := rejectReparseHandle(handle, filepath.Join(absolute, filepath.Base(path))); err != nil {
		_ = windows.CloseHandle(handle)
		return nil, err
	}
	return os.NewFile(uintptr(handle), filepath.Join(absolute, filepath.Base(path))), nil
}

func rejectReparseHandle(handle windows.Handle, path string) error {
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return err
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return fmt.Errorf("refusing config path %s: reparse point", path)
	}
	return nil
}

func readFileBounded(path string, max int64) ([]byte, error) {
	file, err := openFileNoFollow(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	return readBounded(file, path, max)
}

func readFileAt(dir windows.Handle, name, path string, max int64) ([]byte, error) {
	return readFileAtExpected(dir, name, path, max, nil)
}

func readFileAtExpected(dir windows.Handle, name, path string, max int64, expected os.FileInfo) ([]byte, error) {
	if expected == nil {
		var err error
		expected, err = fileInfoAt(dir, name, path)
		if err != nil {
			return nil, noFollowOpenError(path, err)
		}
	}
	handle, err := openRelative(dir, name, windows.FILE_GENERIC_READ, windows.FILE_OPEN, windows.FILE_NON_DIRECTORY_FILE|windows.FILE_OPEN_REPARSE_POINT)
	if err != nil {
		return nil, noFollowOpenError(path, err)
	}
	if err := rejectReparseHandle(handle, path); err != nil {
		_ = windows.CloseHandle(handle)
		return nil, err
	}
	file := os.NewFile(uintptr(handle), path)
	defer func() { _ = file.Close() }()
	opened, statErr := file.Stat()
	if statErr != nil || !os.SameFile(opened, expected) {
		if statErr != nil {
			return nil, statErr
		}
		return nil, fmt.Errorf("source %s changed identity before read", path)
	}
	return readBounded(file, path, max)
}

func duplicateHandle(handle windows.Handle) (windows.Handle, error) {
	var duplicate windows.Handle
	err := windows.DuplicateHandle(windows.CurrentProcess(), handle, windows.CurrentProcess(), &duplicate, 0, false, windows.DUPLICATE_SAME_ACCESS)
	return duplicate, err
}

func openDirAtRelativeWindows(root windows.Handle, relative string) (windows.Handle, error) {
	current, err := duplicateHandle(root)
	if err != nil {
		return windows.InvalidHandle, err
	}
	for _, component := range windowsPathComponents(relative) {
		next, openErr := openRelative(current, component, windows.FILE_GENERIC_READ, windows.FILE_OPEN, windows.FILE_DIRECTORY_FILE|windows.FILE_OPEN_REPARSE_POINT)
		if openErr != nil {
			_ = windows.CloseHandle(current)
			return windows.InvalidHandle, openErr
		}
		_ = windows.CloseHandle(current)
		current = next
	}
	return current, nil
}

func readFileAtRelativeWindows(root windows.Handle, rootAbsolute, path string, max int64) ([]byte, error) {
	relative, err := filepath.Rel(rootAbsolute, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("pinned source %s escapes root %s", path, rootAbsolute)
	}
	dir, err := openDirAtRelativeWindows(root, filepath.Dir(relative))
	if err != nil {
		return nil, noFollowOpenError(path, err)
	}
	defer func() { _ = windows.CloseHandle(dir) }()
	expected, err := fileInfoAt(dir, filepath.Base(relative), path)
	if err != nil {
		return nil, noFollowOpenError(path, err)
	}
	return readFileAtExpected(dir, filepath.Base(relative), path, max, expected)
}

func listFilesAtRelativeWindows(root windows.Handle, rootAbsolute, dir string, exts []string, isCustom bool, max int64) ([]entityFile, error) {
	relative, err := filepath.Rel(rootAbsolute, dir)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("pinned source %s escapes root %s", dir, rootAbsolute)
	}
	handle, err := openDirAtRelativeWindows(root, relative)
	if err != nil {
		if windowsErrorIs(err, syscall.ERROR_FILE_NOT_FOUND) || windowsErrorIs(err, syscall.ERROR_PATH_NOT_FOUND) {
			return nil, nil
		}
		return nil, noFollowOpenError(dir, err)
	}
	file := os.NewFile(uintptr(handle), dir)
	defer func() { _ = file.Close() }()
	entries, err := file.Readdir(-1)
	if err != nil {
		return nil, fmt.Errorf("read dir %s: %w", dir, err)
	}
	files := make([]entityFile, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if !entry.Mode().IsRegular() || !hasAnySuffix(strings.ToLower(name), exts) {
			continue
		}
		path := filepath.Join(dir, name)
		expected, expectedErr := fileInfoAt(handle, name, path)
		if expectedErr != nil {
			return nil, expectedErr
		}
		raw, err := readFileAtExpected(handle, name, path, max, expected)
		if err != nil {
			return nil, err
		}
		files = append(files, entityFile{Path: path, IsCustom: isCustom, Raw: raw})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

func listFilesIn(dir string, exts []string, isCustom bool, max int64) ([]entityFile, error) {
	handle, absolute, err := openDirNoFollow(dir, false)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read dir %s: %w", dir, err)
	}
	dirFile := os.NewFile(uintptr(handle), absolute)
	defer func() { _ = dirFile.Close() }()
	entries, err := dirFile.Readdir(-1)
	if err != nil {
		return nil, fmt.Errorf("read dir %s: %w", dir, err)
	}
	files := make([]entityFile, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if !entry.Mode().IsRegular() || !hasAnySuffix(strings.ToLower(name), exts) {
			continue
		}
		path := filepath.Join(absolute, name)
		expected, expectedErr := fileInfoAt(handle, name, path)
		if expectedErr != nil {
			return nil, expectedErr
		}
		raw, err := readFileAtExpected(handle, name, path, max, expected)
		if err != nil {
			return nil, err
		}
		files = append(files, entityFile{Path: path, IsCustom: isCustom, Raw: raw})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

func hardenDirNoFollow(path string) error {
	handle, _, err := openDirNoFollowAccess(path, true, windows.FILE_GENERIC_READ|windows.FILE_GENERIC_WRITE|windows.DELETE)
	if err != nil {
		return err
	}
	return windows.CloseHandle(handle)
}

func readDirNoFollow(path string) ([]os.FileInfo, error) {
	handle, absolute, err := openDirNoFollow(path, false)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(handle), absolute)
	defer func() { _ = file.Close() }()
	return file.Readdir(-1)
}

func lstatNoFollow(path string) (os.FileInfo, error) {
	dir, absolute, err := openDirNoFollow(filepath.Dir(path), false)
	if err != nil {
		return nil, err
	}
	defer func() { _ = windows.CloseHandle(dir) }()
	handle, err := openRelative(dir, filepath.Base(path), windows.FILE_READ_ATTRIBUTES, windows.FILE_OPEN, windows.FILE_OPEN_REPARSE_POINT)
	if err != nil {
		return nil, noFollowOpenError(filepath.Join(absolute, filepath.Base(path)), err)
	}
	defer func() { _ = windows.CloseHandle(handle) }()
	file := os.NewFile(uintptr(handle), filepath.Join(absolute, filepath.Base(path)))
	return file.Stat()
}

func writeAtomicNoFollow(path string, data []byte) error {
	dir, absolute, err := openDirNoFollowAccess(filepath.Dir(path), true, windows.FILE_GENERIC_READ|windows.FILE_GENERIC_WRITE|windows.DELETE)
	if err != nil {
		return err
	}
	defer func() { _ = windows.CloseHandle(dir) }()
	target := filepath.Base(path)
	return writeAtomicAtWindows(dir, absolute, target, data)
}

func writeAtomicAtWindows(dir windows.Handle, absolute, target string, data []byte) error {
	return writeAtomicAtWindowsChecked(dir, absolute, target, data)
}

func writeAtomicAtWindowsChecked(dir windows.Handle, absolute, target string, data []byte) error {
	if err := rejectAtomicTarget(dir, absolute, target); err != nil {
		return err
	}
	tmpName, handle, err := createAtomicTemp(dir)
	if err != nil {
		return err
	}
	tmp := os.NewFile(uintptr(handle), filepath.Join(absolute, tmpName))
	cleanup := true
	defer func() {
		if cleanup {
			_ = removeAt(dir, tmpName, false)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := rejectAtomicTarget(dir, absolute, target); err != nil {
		return err
	}
	if err := renameAt(dir, tmpName, target); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func rejectAtomicTarget(dir windows.Handle, absolute, name string) error {
	handle, err := openRelative(dir, name, windows.FILE_READ_ATTRIBUTES, windows.FILE_OPEN, windows.FILE_OPEN_REPARSE_POINT)
	if windowsErrorIs(err, syscall.ERROR_FILE_NOT_FOUND) || windowsErrorIs(err, syscall.ERROR_PATH_NOT_FOUND) {
		return nil
	}
	if err != nil {
		return noFollowOpenError(filepath.Join(absolute, name), err)
	}
	defer func() { _ = windows.CloseHandle(handle) }()
	return rejectReparseHandle(handle, filepath.Join(absolute, name))
}

func fileInfoAt(dir windows.Handle, name, path string) (os.FileInfo, error) {
	handle, err := openRelative(dir, name, windows.FILE_READ_ATTRIBUTES, windows.FILE_OPEN, windows.FILE_OPEN_REPARSE_POINT)
	if err != nil {
		return nil, err
	}
	if err := rejectReparseHandle(handle, path); err != nil {
		_ = windows.CloseHandle(handle)
		return nil, err
	}
	file := os.NewFile(uintptr(handle), path)
	info, statErr := file.Stat()
	closeErr := file.Close()
	return info, errors.Join(statErr, closeErr)
}

func createAtomicTemp(dir windows.Handle) (string, windows.Handle, error) {
	var random [12]byte
	for attempt := 0; attempt < 100; attempt++ {
		if _, err := rand.Read(random[:]); err != nil {
			return "", windows.InvalidHandle, err
		}
		name := fmt.Sprintf(".%s-%x.tmp", "omakiten", random)
		handle, err := openRelative(dir, name, windows.FILE_GENERIC_WRITE, windows.FILE_CREATE, windows.FILE_NON_DIRECTORY_FILE|windows.FILE_OPEN_REPARSE_POINT)
		if err == nil {
			return name, handle, nil
		}
		if !windowsErrorIs(err, syscall.ERROR_ALREADY_EXISTS) {
			return "", windows.InvalidHandle, err
		}
	}
	return "", windows.InvalidHandle, fmt.Errorf("create atomic temp: too many collisions")
}

type fileRenameInformation struct {
	ReplaceIfExists byte
	_               [7]byte
	RootDirectory   windows.Handle
	FileNameLength  uint32
	FileName        [1]uint16
}

func renameAt(dir windows.Handle, oldName, newName string) error {
	source, err := openRelative(dir, oldName, windows.FILE_GENERIC_READ|windows.DELETE, windows.FILE_OPEN, windows.FILE_OPEN_REPARSE_POINT)
	if err != nil {
		return err
	}
	defer func() { _ = windows.CloseHandle(source) }()
	return renameHandle(source, dir, newName)
}

func renameHandle(source, dir windows.Handle, name string) error {
	utf16Name, err := windows.UTF16FromString(name)
	if err != nil {
		return err
	}
	nameBytes := len(utf16Name)*2 - 2
	dummy := fileRenameInformation{}
	size := int(unsafe.Offsetof(dummy.FileName)) + nameBytes
	buffer := make([]byte, size)
	info := (*fileRenameInformation)(unsafe.Pointer(&buffer[0]))
	info.ReplaceIfExists = 1
	info.RootDirectory = dir
	info.FileNameLength = uint32(nameBytes)
	copy((*[windows.MAX_LONG_PATH]uint16)(unsafe.Pointer(&info.FileName[0]))[:nameBytes/2:nameBytes/2], utf16Name)
	var status windows.IO_STATUS_BLOCK
	return windows.NtSetInformationFile(source, &status, &buffer[0], uint32(size), windows.FileRenameInformation)
}

func removeAt(dir windows.Handle, name string, recursive bool) error {
	return removeAtPath(dir, name, recursive, name)
}

func removeAtPath(dir windows.Handle, name string, recursive bool, displayPath string) error {
	handle, err := openRelative(dir, name, windows.FILE_GENERIC_READ|windows.DELETE, windows.FILE_OPEN, windows.FILE_OPEN_REPARSE_POINT)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(handle), name)
	if err := rejectReparseHandle(handle, name); err != nil {
		_ = file.Close()
		return err
	}
	info, statErr := file.Stat()
	if statErr != nil {
		_ = file.Close()
		return statErr
	}
	if info.IsDir() && recursive {
		entries, readErr := file.Readdir(-1)
		if readErr != nil {
			_ = file.Close()
			return readErr
		}
		for _, entry := range entries {
			if err := removeAtPath(handle, entry.Name(), true, filepath.Join(displayPath, entry.Name())); err != nil {
				_ = file.Close()
				return err
			}
		}
	}
	deleteErr := deleteHandle(handle)
	closeErr := file.Close()
	if deleteErr != nil {
		return deleteErr
	}
	if closeErr != nil {
		return &AmbiguousPublicationError{Path: displayPath, Operation: "delete", Err: closeErr}
	}
	return nil
}

func RemoveFile(path string) error {
	dir, _, err := openDirNoFollowAccess(filepath.Dir(path), false, windows.FILE_GENERIC_READ|windows.FILE_GENERIC_WRITE|windows.DELETE)
	if err != nil {
		return err
	}
	defer func() { _ = windows.CloseHandle(dir) }()
	return removeAtPath(dir, filepath.Base(path), false, path)
}

func deleteHandle(handle windows.Handle) error {
	deleteInfo := byte(1)
	var status windows.IO_STATUS_BLOCK
	return windows.NtSetInformationFile(handle, &status, &deleteInfo, 1, windows.FileDispositionInformation)
}
