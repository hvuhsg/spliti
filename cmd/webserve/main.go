// Command webserve is a tiny static file server for the browser (wasm) demos in
// web/. It serves over localhost — a secure context, which WebGPU requires —
// and sets the application/wasm content type so the browser can use
// WebAssembly.instantiateStreaming.
//
// Build the demos first with scripts/build-wasm.sh, then:
//
//	go run ./cmd/webserve
//	# open http://localhost:8080/?demo=gpu-demo
package main

import (
	"flag"
	"log"
	"net/http"
	"strings"
)

func main() {
	addr := flag.String("addr", "localhost:8080", "address to listen on")
	dir := flag.String("dir", "web", "directory to serve")
	flag.Parse()

	fs := http.FileServer(http.Dir(*dir))
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Force the correct MIME for .wasm regardless of the host's mime table,
		// so instantiateStreaming accepts the response.
		if strings.HasSuffix(r.URL.Path, ".wasm") {
			w.Header().Set("Content-Type", "application/wasm")
		}
		fs.ServeHTTP(w, r)
	})

	log.Printf("serving %q at http://%s/ (Ctrl-C to stop)", *dir, *addr)
	log.Fatal(http.ListenAndServe(*addr, handler))
}
