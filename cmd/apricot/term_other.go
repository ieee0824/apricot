//go:build !darwin

package main

// stdinIsTerminal on non-darwin platforms (only relevant for cross-platform
// test runs; apple container itself is macOS-only) reports no terminal, which
// takes the conservative path of dropping stdin_open.
func stdinIsTerminal() bool {
	return false
}
