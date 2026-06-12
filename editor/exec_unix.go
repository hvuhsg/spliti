//go:build !windows

package editor

import (
	"os"
	"syscall"
)

// execReplace replaces the current process with the given binary (the rebuild
// re-exec). It runs from the app's exit hook, after window/terminal cleanup.
func execReplace(bin string) error {
	return syscall.Exec(bin, append([]string{bin}, os.Args[1:]...), os.Environ())
}
