//go:build windows

package editor

import (
	"os"
	"os/exec"
)

// execReplace approximates a unix exec on Windows: spawn the new binary
// detached, then let the current process finish exiting.
func execReplace(bin string) error {
	cmd := exec.Command(bin, os.Args[1:]...)
	cmd.Stdout, cmd.Stderr, cmd.Stdin = os.Stdout, os.Stderr, os.Stdin
	return cmd.Start()
}
