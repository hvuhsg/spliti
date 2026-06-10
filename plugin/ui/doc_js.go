//go:build js

// Package ui is the Dear ImGui (cimgui-go) GPU UI plugin. cimgui-go binds the C
// ImGui library via cgo, which the js/wasm target cannot compile, so the plugin
// is native-only: on js this package is intentionally empty.
package ui
