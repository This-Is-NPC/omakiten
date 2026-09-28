//go:build !aix && !darwin && !dragonfly && !freebsd && !illumos && !linux && !netbsd && !openbsd && !solaris && !windows

package paths

func setActiveConfigFile(string, string) error { return errUnsupportedActiveMarker }

func readActiveConfigFile(string) ([]byte, error) { return nil, errUnsupportedActiveMarker }
