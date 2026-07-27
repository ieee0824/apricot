package buildctx

import (
	"io/fs"

	"golang.org/x/sys/unix"
)

// copyFile clones src to dst via APFS clonefile — constant-time and
// zero-extra-disk regardless of file size. When cloning is impossible
// (different volume, non-APFS filesystem) it falls back to a byte copy.
func copyFile(src, dst string, mode fs.FileMode) error {
	if err := unix.Clonefile(src, dst, unix.CLONE_NOFOLLOW); err == nil {
		return nil
	}
	return copyFileContents(src, dst, mode)
}
