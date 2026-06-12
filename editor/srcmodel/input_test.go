package srcmodel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const inputSrc = `// Package game wires gameplay to the engine.
package game

import (
	"github.com/hvuhsg/spliti/plugin/inputs"
	"github.com/hvuhsg/spliti/plugin/inputs/actions"
)

// BuildActions is the game's input table. Comment must survive.
//
//spliti:input
func BuildActions() *actions.Map {
	m := actions.NewMap()
	m.Bind("jump", actions.Key(inputs.KeySpace), actions.Pad(inputs.GamepadA))
	m.Bind("fire", actions.Mouse(inputs.MouseButtonLeft))
	m.Bind("weird", actions.Key(someKey()))
	m.BindAxis("move-x",
		actions.ButtonAxis(actions.Key(inputs.KeyA), actions.Key(inputs.KeyD)),
		actions.PadAxis(inputs.AxisLeftX))
	m.BindAxis("move-y", actions.PadAxis(inputs.AxisLeftY).Inverted())
	return m
}
`

func writeInputFile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "input.go")
	if err := os.WriteFile(path, []byte(inputSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseInput(t *testing.T) {
	inf, err := ParseInputFile(writeInputFile(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(inf.Actions) != 3 || len(inf.Axes) != 2 {
		t.Fatalf("got %d actions, %d axes", len(inf.Actions), len(inf.Axes))
	}
	jump := inf.Action("jump")
	if jump == nil || !jump.Editable || len(jump.Sources) != 2 {
		t.Fatalf("jump = %+v", jump)
	}
	if jump.Sources[0] != (SourceRef{Kind: "key", Name: "KeySpace"}) ||
		jump.Sources[1] != (SourceRef{Kind: "pad", Name: "GamepadA"}) {
		t.Fatalf("jump sources = %+v", jump.Sources)
	}
	if w := inf.Action("weird"); w == nil || w.Editable {
		t.Fatalf("computed-arg bind not marked read-only: %+v", w)
	}
	mx := inf.Axis("move-x")
	if mx == nil || len(mx.Sources) != 2 || mx.Sources[0].Kind != "buttons" ||
		mx.Sources[0].Neg.Name != "KeyA" || mx.Sources[1].Axis != "AxisLeftX" {
		t.Fatalf("move-x = %+v", mx.Sources)
	}
	my := inf.Axis("move-y")
	if my == nil || !my.Sources[0].Inverted {
		t.Fatalf("move-y inversion lost: %+v", my)
	}
}

func TestInputEditRoundTrip(t *testing.T) {
	path := writeInputFile(t)
	inf, err := ParseInputFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Rebind jump, add a new action and axis, remove fire.
	if err := inf.SetBinding("jump", []SourceRef{{Kind: "key", Name: "KeyEnter"}}); err != nil {
		t.Fatal(err)
	}
	if err := inf.SetBinding("dash", []SourceRef{{Kind: "key", Name: "KeyLeftShift"}, {Kind: "pad", Name: "GamepadB"}}); err != nil {
		t.Fatal(err)
	}
	if err := inf.SetAxisBinding("look-y", []AxisSourceRef{{Kind: "pad", Axis: "AxisRightY", Inverted: true}}); err != nil {
		t.Fatal(err)
	}
	if !inf.RemoveBinding("fire") {
		t.Fatal("fire not removed")
	}
	if err := inf.Save(); err != nil {
		t.Fatal(err)
	}

	out, _ := os.ReadFile(path)
	src := string(out)
	for _, want := range []string{
		`m.Bind("jump", actions.Key(inputs.KeyEnter))`,
		`m.Bind("dash", actions.Key(inputs.KeyLeftShift), actions.Pad(inputs.GamepadB))`,
		`m.BindAxis("look-y", actions.PadAxis(inputs.AxisRightY).Inverted())`,
		"Comment must survive",
		`actions.Key(someKey())`, // untouched non-editable line
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("missing %q in:\n%s", want, src)
		}
	}
	if strings.Contains(src, `"fire"`) {
		t.Fatalf("fire still present:\n%s", src)
	}

	inf2, err := ParseInputFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if inf2.Action("dash") == nil || inf2.Axis("look-y") == nil || inf2.Action("fire") != nil {
		t.Fatal("round trip lost edits")
	}
}
