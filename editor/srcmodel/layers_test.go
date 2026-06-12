package srcmodel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const layersSrc = `package game

// Collision layers. This comment must survive rewrites.
//
//spliti:layers
const (
	LayerDefault uint32 = 1 << iota
	LayerPlayer
	_
	LayerEnemy
)

// unrelated, untouched
const Version = "1"
`

func writeLayersFile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "layers.go")
	if err := os.WriteFile(path, []byte(layersSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseLayers(t *testing.T) {
	lf, err := ParseLayersFile(writeLayersFile(t))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"LayerDefault", "LayerPlayer", "", "LayerEnemy"}
	if len(lf.Names) != len(want) {
		t.Fatalf("Names = %v", lf.Names)
	}
	for i, n := range want {
		if lf.Names[i] != n {
			t.Fatalf("Names[%d] = %q, want %q", i, lf.Names[i], n)
		}
	}
	if bit, ok := lf.Bit("LayerEnemy"); !ok || bit != 3 {
		t.Fatalf("Bit(LayerEnemy) = %d, %v", bit, ok)
	}
}

func TestFindLayersFile(t *testing.T) {
	path := writeLayersFile(t)
	dir := filepath.Dir(path)
	os.WriteFile(filepath.Join(dir, "aaa.go"), []byte("package game\n"), 0o644)
	got, ok := FindLayersFile(dir)
	if !ok || got != path {
		t.Fatalf("FindLayersFile = %q, %v", got, ok)
	}
	if _, ok := FindLayersFile(t.TempDir()); ok {
		t.Fatal("found layers in an empty dir")
	}
}

func TestLayersRenameAddSave(t *testing.T) {
	path := writeLayersFile(t)
	lf, err := ParseLayersFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := lf.Rename(1, "LayerHero"); err != nil {
		t.Fatal(err)
	}
	if err := lf.Add("LayerPickup"); err != nil {
		t.Fatal(err)
	}
	if err := lf.Rename(0, "LayerEnemy"); err == nil {
		t.Fatal("duplicate rename accepted")
	}
	if err := lf.Add("not an ident"); err == nil {
		t.Fatal("invalid identifier accepted")
	}
	if err := lf.Save(); err != nil {
		t.Fatal(err)
	}

	out, _ := os.ReadFile(path)
	src := string(out)
	for _, wantStr := range []string{
		"LayerHero", "LayerPickup",
		"This comment must survive rewrites",
		`const Version = "1"`,
	} {
		if !strings.Contains(src, wantStr) {
			t.Fatalf("missing %q in:\n%s", wantStr, src)
		}
	}
	lf2, err := ParseLayersFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bit, ok := lf2.Bit("LayerPickup"); !ok || bit != 4 {
		t.Fatalf("LayerPickup bit = %d, %v", bit, ok)
	}
	if lf2.Names[1] != "LayerHero" {
		t.Fatalf("rename lost: %v", lf2.Names)
	}
}

func TestLayersRemove(t *testing.T) {
	path := writeLayersFile(t)
	lf, err := ParseLayersFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Middle bits cannot be removed (would renumber later layers).
	if err := lf.Remove(1); err == nil {
		t.Fatal("removing a middle bit was accepted")
	}
	if err := lf.Remove(99); err == nil {
		t.Fatal("removing an out-of-range bit was accepted")
	}
	// Popping the highest bit is allowed and leaves lower bits intact.
	if err := lf.Remove(len(lf.Names) - 1); err != nil {
		t.Fatal(err)
	}
	if err := lf.Save(); err != nil {
		t.Fatal(err)
	}
	lf2, err := ParseLayersFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"LayerDefault", "LayerPlayer", ""}
	if len(lf2.Names) != len(want) {
		t.Fatalf("Names = %v, want %v", lf2.Names, want)
	}
	for i, n := range want {
		if lf2.Names[i] != n {
			t.Fatalf("Names[%d] = %q, want %q", i, lf2.Names[i], n)
		}
	}
	if _, ok := lf2.Bit("LayerEnemy"); ok {
		t.Fatal("LayerEnemy survived removal")
	}
	// The iota expression on the first spec must survive: bit 1 is still 1<<1.
	out, _ := os.ReadFile(path)
	if !strings.Contains(string(out), "1 << iota") {
		t.Fatalf("iota expression lost:\n%s", out)
	}
}
