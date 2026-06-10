//go:build js

// rf-lab's UI is Dear ImGui (cimgui-go), which binds C via cgo and so cannot
// build for js/wasm. This stub keeps `GOOS=js go build ./...` green; run the
// lab natively instead: CGO_ENABLED=1 go run ./examples/rf-lab
package main

func main() {
	println("rf-lab requires a native build (its ImGui UI uses cgo); run: go run ./examples/rf-lab")
}
