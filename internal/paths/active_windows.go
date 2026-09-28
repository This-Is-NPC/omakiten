//go:build windows

package paths

func setActiveConfigFile(string, string) error { return errUnsupportedActiveMarker }

func readActiveConfigFile(string) ([]byte, error) { return nil, errUnsupportedActiveMarker }
