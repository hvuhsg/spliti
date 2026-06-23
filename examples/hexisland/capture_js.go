//go:build js

package main

import "github.com/hvuhsg/spliti/app"

// addCapture is a no-op in the browser build; the screenshot driver is a
// native-only convenience (see capture.go).
func addCapture(a *app.App) {}
