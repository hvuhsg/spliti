//go:build js

package render3d

import "errors"

// newAssetWatcher reports that hot-reload is unsupported in the browser: wasm
// assets are served immutably and there is no filesystem-change notification. The
// loader logs this and continues without hot-reload.
func newAssetWatcher(func(format string, args ...any)) (fileWatcher, error) {
	return nil, errors.New("asset hot-reload is not supported on wasm")
}
