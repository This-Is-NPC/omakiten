package app

import "os"

// osStat is wrapped so service-level tests can stub it via build tags if
// needed. It currently delegates to os.Stat without modification.
func osStat(path string) (os.FileInfo, error) {
	return os.Stat(path)
}
