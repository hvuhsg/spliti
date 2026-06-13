//go:build windows

package editor

import "errors"

// errNoPTY is returned by startPTY where PTY support is not implemented. The
// Terminal panel shows this instead of a live shell.
var errNoPTY = errors.New("terminal: PTY not supported on this platform")

// ptySession is the windows stub: no fields, every method is a no-op so the
// shared terminal.go compiles. ConPTY support is a follow-up.
type ptySession struct{}

func startPTY(cols, rows int) (*ptySession, error) { return nil, errNoPTY }

func (p *ptySession) Read(b []byte) (int, error)  { return 0, errNoPTY }
func (p *ptySession) Write(b []byte) (int, error) { return 0, errNoPTY }
func (p *ptySession) resize(cols, rows int) error { return nil }
func (p *ptySession) kill()                       {}
