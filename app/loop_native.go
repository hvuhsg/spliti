//go:build !js

package app

// driveLoop runs the frame loop synchronously on the calling goroutine until the
// app stops. This is the normal desktop driver; Run blocks here until Stop or
// MaxFrames, then returns so its deferred onExit hooks fire.
func (a *App) driveLoop() {
	for a.running {
		a.step()
	}
}
