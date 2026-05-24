//go:build unix

package config

import (
	"os"
	"syscall"
)

// sameInode reports whether two FileInfo values describe the same
// underlying inode. Used by the same-fs rename test to prove the
// fast path did not copy-and-remove. Unix-only; Windows can add a
// matching helper when the migration ever ships there.
func sameInode(a, b os.FileInfo) bool {
	ax, aok := a.Sys().(*syscall.Stat_t)
	bx, bok := b.Sys().(*syscall.Stat_t)
	if !aok || !bok {
		return false
	}
	return ax.Ino == bx.Ino && ax.Dev == bx.Dev
}
