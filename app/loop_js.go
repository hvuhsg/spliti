//go:build js

package app

import "syscall/js"

// driveLoop runs the frame loop under the browser's requestAnimationFrame, which
// paces frames to the display refresh and yields to the JS event loop between
// them (a blocking for-loop would freeze the page). Each rAF callback runs one
// step() and reschedules until the app stops; the calling goroutine blocks on a
// channel so main() does not return — which would tear down the wasm runtime —
// until the loop ends, at which point Run's deferred onExit hooks fire.
func (a *App) driveLoop() {
	done := make(chan struct{})

	var frame js.Func
	frame = js.FuncOf(func(js.Value, []js.Value) any {
		if a.step() {
			js.Global().Call("requestAnimationFrame", frame)
		} else {
			frame.Release()
			close(done)
		}
		return nil
	})

	js.Global().Call("requestAnimationFrame", frame)
	<-done
}
