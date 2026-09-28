//go:build !linux && !windows && !aix && !darwin && !dragonfly && !freebsd && !illumos && !netbsd && !openbsd && !solaris

package config

import (
	"fmt"
	"os"
)

var errUnsupportedSafeIO = fmt.Errorf("config safe I/O requires a platform no-follow filesystem backend")

func readFileBounded(string, int64) ([]byte, error) { return nil, errUnsupportedSafeIO }
func listFilesIn(string, []string, bool, int64) ([]entityFile, error) {
	return nil, errUnsupportedSafeIO
}
func hardenDirNoFollow(string) error                { return errUnsupportedSafeIO }
func readDirNoFollow(string) ([]os.FileInfo, error) { return nil, errUnsupportedSafeIO }
func lstatNoFollow(string) (os.FileInfo, error)     { return nil, errUnsupportedSafeIO }
func writeAtomicNoFollow(string, []byte) error      { return errUnsupportedSafeIO }
func RemoveFile(string) error                       { return errUnsupportedSafeIO }
