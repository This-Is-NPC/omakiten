//go:build windows

package sqlite

import (
	"errors"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

type snapshotFileLinkInformation struct {
	Flags          uint32
	RootDirectory  windows.Handle
	FileNameLength uint32
	FileName       [1]uint16
}

type snapshotFileRenameInformation struct {
	ReplaceIfExists uint32
	RootDirectory   windows.Handle
	FileNameLength  uint32
	FileName        [1]uint16
}

// Windows SQLite cannot consume a handle-relative pathname. Hold identity-
// checked file and directory handles that deny delete/rename while SQLite uses
// the lexical stage path. After verification, retain the directory binding but
// transition the file binding to delete sharing so hard-link force publication
// can rename the same inode. The rooted creation APIs do not install a private
// DACL, so confidentiality still requires deployment-managed ACL inheritance.
func bindSnapshotStage(staged, stagedDirectory *os.File, path string) (string, func() error, func() error, error) {
	directoryPathPtr, err := windows.UTF16PtrFromString(filepath.Dir(path))
	if err != nil {
		return "", nil, nil, err
	}
	directoryHandle, err := windows.CreateFile(
		directoryPathPtr,
		windows.FILE_READ_ATTRIBUTES|windows.DELETE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS,
		0,
	)
	if err != nil {
		return "", nil, nil, errors.New("secure Windows SQLite staging directory handle is unavailable")
	}
	same, identityErr := sameSnapshotFileHandle(
		windows.Handle(stagedDirectory.Fd()),
		directoryHandle,
	)
	if identityErr != nil {
		return "", nil, nil, errors.Join(
			errors.New("verify secure Windows SQLite staging directory handle identity"),
			identityErr,
			windows.CloseHandle(directoryHandle),
		)
	}
	if !same {
		return "", nil, nil, errors.Join(
			errors.New("secure Windows SQLite staging directory handle identity does not match staged directory"),
			windows.CloseHandle(directoryHandle),
		)
	}
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", nil, nil, errors.Join(err, windows.CloseHandle(directoryHandle))
	}
	handle, err := windows.CreateFile(
		pathPtr,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return "", nil, nil, errors.Join(
			errors.New("secure Windows SQLite staging handle is unavailable"),
			windows.CloseHandle(directoryHandle),
		)
	}
	same, identityErr = sameSnapshotFileHandle(
		windows.Handle(staged.Fd()),
		handle,
	)
	if identityErr != nil {
		return "", nil, nil, errors.Join(
			errors.New("verify secure Windows SQLite staging handle identity"),
			identityErr,
			windows.CloseHandle(handle),
			windows.CloseHandle(directoryHandle),
		)
	}
	if !same {
		return "", nil, nil, errors.Join(
			errors.New("secure Windows SQLite staging handle identity does not match staged file"),
			windows.CloseHandle(handle),
			windows.CloseHandle(directoryHandle),
		)
	}
	lockedHandle := handle
	var publicationHandle windows.Handle
	prepared := false
	prepareForPublication := func() error {
		if prepared {
			return nil
		}
		sharedHandle, err := windows.CreateFile(
			pathPtr,
			windows.GENERIC_READ|windows.GENERIC_WRITE,
			windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
			nil,
			windows.OPEN_EXISTING,
			windows.FILE_ATTRIBUTE_NORMAL,
			0,
		)
		if err != nil {
			return errors.New("publication-compatible Windows SQLite staging handle is unavailable")
		}
		same, identityErr := sameSnapshotFileHandle(windows.Handle(staged.Fd()), sharedHandle)
		if identityErr != nil || !same {
			return errors.Join(
				errors.New("publication-compatible Windows SQLite staging handle identity does not match staged file"),
				identityErr,
				windows.CloseHandle(sharedHandle),
			)
		}
		if err := windows.CloseHandle(lockedHandle); err != nil {
			return errors.Join(err, windows.CloseHandle(sharedHandle))
		}
		lockedHandle = 0
		publicationHandle = sharedHandle
		prepared = true
		return nil
	}
	closed := false
	return path, prepareForPublication, func() error {
		if closed {
			return nil
		}
		closed = true
		var closeErr error
		if lockedHandle != 0 {
			closeErr = errors.Join(closeErr, windows.CloseHandle(lockedHandle))
		}
		if publicationHandle != 0 {
			closeErr = errors.Join(closeErr, windows.CloseHandle(publicationHandle))
		}
		return errors.Join(closeErr, windows.CloseHandle(directoryHandle))
	}, nil
}

func sameSnapshotFileHandle(left, right windows.Handle) (bool, error) {
	var leftInfo, rightInfo windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(left, &leftInfo); err != nil {
		return false, err
	}
	if err := windows.GetFileInformationByHandle(right, &rightInfo); err != nil {
		return false, err
	}
	return leftInfo.VolumeSerialNumber == rightInfo.VolumeSerialNumber &&
		leftInfo.FileIndexHigh == rightInfo.FileIndexHigh &&
		leftInfo.FileIndexLow == rightInfo.FileIndexLow, nil
}

func validateSnapshotStageForPublication(root *os.Root, name string, expected os.FileInfo) error {
	current, err := root.Lstat(name)
	if err != nil || current.Mode()&os.ModeSymlink != 0 || !current.Mode().IsRegular() || !os.SameFile(expected, current) {
		return errors.New("verified snapshot staging identity changed before publication")
	}
	return nil
}

func linkSnapshotFile(staged, _ *os.File, _ string, destinationDirectory *os.File, destinationName string) error {
	name, err := windows.UTF16FromString(destinationName)
	if err != nil {
		return err
	}
	nameLength := (len(name) - 1) * 2
	var layout snapshotFileLinkInformation
	buffer := make([]byte, int(unsafe.Offsetof(layout.FileName))+nameLength)
	info := (*snapshotFileLinkInformation)(unsafe.Pointer(&buffer[0]))
	info.RootDirectory = windows.Handle(destinationDirectory.Fd())
	info.FileNameLength = uint32(nameLength)
	copy(unsafe.Slice(&info.FileName[0], nameLength/2), name[:len(name)-1])
	var status windows.IO_STATUS_BLOCK
	return windows.NtSetInformationFile(
		windows.Handle(staged.Fd()),
		&status,
		&buffer[0],
		uint32(len(buffer)),
		windows.FileLinkInformation,
	)
}

func renameSnapshotLink(_ *os.Root, destinationDirectory *os.File, oldName, newName string) (returnErr error) {
	objectName, err := windows.NewNTUnicodeString(oldName)
	if err != nil {
		return err
	}
	attributes := windows.OBJECT_ATTRIBUTES{
		Length:        uint32(unsafe.Sizeof(windows.OBJECT_ATTRIBUTES{})),
		RootDirectory: windows.Handle(destinationDirectory.Fd()),
		ObjectName:    objectName,
		Attributes:    windows.OBJ_CASE_INSENSITIVE,
	}
	var linked windows.Handle
	var openStatus windows.IO_STATUS_BLOCK
	if err := windows.NtCreateFile(
		&linked,
		windows.DELETE|windows.SYNCHRONIZE,
		&attributes,
		&openStatus,
		nil,
		0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		windows.FILE_OPEN,
		windows.FILE_NON_DIRECTORY_FILE|windows.FILE_SYNCHRONOUS_IO_NONALERT,
		0,
		0,
	); err != nil {
		return err
	}
	defer func() {
		returnErr = errors.Join(returnErr, windows.CloseHandle(linked))
	}()
	name, err := windows.UTF16FromString(newName)
	if err != nil {
		return err
	}
	nameLength := (len(name) - 1) * 2
	var layout snapshotFileRenameInformation
	buffer := make([]byte, int(unsafe.Offsetof(layout.FileName))+nameLength)
	info := (*snapshotFileRenameInformation)(unsafe.Pointer(&buffer[0]))
	info.ReplaceIfExists = windows.FILE_RENAME_REPLACE_IF_EXISTS | windows.FILE_RENAME_POSIX_SEMANTICS
	info.RootDirectory = windows.Handle(destinationDirectory.Fd())
	info.FileNameLength = uint32(nameLength)
	copy(unsafe.Slice(&info.FileName[0], nameLength/2), name[:len(name)-1])
	var status windows.IO_STATUS_BLOCK
	return windows.NtSetInformationFile(
		linked,
		&status,
		&buffer[0],
		uint32(len(buffer)),
		windows.FileRenameInformation,
	)
}

func removeSnapshotLink(root *os.Root, _ *os.File, name string) error {
	return root.Remove(name)
}

// Rename/Link have already published the complete file. Flushing a read-only
// directory handle is unsupported on Windows and must not turn success into a
// post-publication error.
func syncPublishedSnapshotDirectory(_ *os.File) error {
	return nil
}
