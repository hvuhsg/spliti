//go:build !windows

package editor

import (
	"os"
	"os/exec"

	"github.com/creack/pty"
)

// ptySession is a shell process running under a PTY master. The Terminal panel
// reads f for output and writes keystrokes to it; resize delivers SIGWINCH.
type ptySession struct {
	cmd *exec.Cmd
	f   *os.File
}

// startPTY launches the user's $SHELL (falling back to /bin/sh) on a new PTY
// sized to cols×rows.
func startPTY(cols, rows int) (*ptySession, error) {
	sh := os.Getenv("SHELL")
	if sh == "" {
		sh = "/bin/sh"
	}
	cmd := exec.Command(sh)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		return nil, err
	}
	return &ptySession{cmd: cmd, f: f}, nil
}

func (p *ptySession) Read(b []byte) (int, error)  { return p.f.Read(b) }
func (p *ptySession) Write(b []byte) (int, error) { return p.f.Write(b) }

func (p *ptySession) resize(cols, rows int) error {
	return pty.Setsize(p.f, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}

// kill terminates the shell and releases the PTY. Safe to call more than once.
func (p *ptySession) kill() {
	if p.cmd != nil && p.cmd.Process != nil {
		p.cmd.Process.Kill()
	}
	if p.f != nil {
		p.f.Close()
	}
}
