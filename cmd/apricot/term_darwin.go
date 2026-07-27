package main

import (
	"os"

	"golang.org/x/sys/unix"
)

// stdinIsTerminal reports whether stdin is a terminal device. `container run
// -t -i` only works when it is (it configures the calling terminal).
func stdinIsTerminal() bool {
	_, err := unix.IoctlGetTermios(int(os.Stdin.Fd()), unix.TIOCGETA)
	return err == nil
}
