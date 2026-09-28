package config

// WriteAtomic writes data to path through a temp file + rename, ensuring no
// reader observes a half-written file.
//
// WriteAtomic is a generic primitive: it serves both omakiten-owned config
// paths (the config root and its entity subtrees) and foreign harness paths
// therefore stay neutral about the parent directory's mode and never chmod a
// directory it did not create — clobbering the mode of ~/.claude/ (shared with
// Claude Code) or a user-chosen --config-path parent would be both surprising
// and a source of install/config-write errors.
//
// Parent-dir hardening therefore lives where omakiten owns the directory tree
// (see hardenDir / EnsureDefaultFiles in default_files.go), not here.
func WriteAtomic(path string, data []byte) error {
	return writeAtomicNoFollow(path, data)
}
