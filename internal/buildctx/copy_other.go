//go:build !darwin

package buildctx

import "io/fs"

// copyFile on non-darwin platforms (only relevant for cross-platform test
// runs; apple container itself is macOS-only) always byte-copies.
func copyFile(src, dst string, mode fs.FileMode) error {
	return copyFileContents(src, dst, mode)
}
