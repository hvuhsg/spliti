package editor_test

import "os"

// writeFileImpl is the actual file-write used by scene_load_test.go.
// Split out so the test file's helper has a single import to track.
func writeFileImpl(path string, data []byte) error {
	return os.WriteFile(path, data, 0o644)
}
