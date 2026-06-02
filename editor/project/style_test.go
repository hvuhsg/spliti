package project_test

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/hvuhsg/spliti/editor/project"
)

func TestStyle_RoundTrip_Defaults(t *testing.T) {
	src := tcell.StyleDefault
	out := project.StyleTOML{}
	got, err := project.DecodeStyle(out)
	if err != nil {
		t.Fatal(err)
	}
	if got != src {
		t.Fatalf("StyleDefault should round-trip; got %v", got)
	}
}

func TestStyle_RoundTrip_NamedColorAndBold(t *testing.T) {
	src := tcell.StyleDefault.Foreground(tcell.ColorYellow).Bold(true)
	enc := project.EncodeStyle(src)
	if enc.Fg != "yellow" || !enc.Bold {
		t.Fatalf("encode lost data: %+v", enc)
	}
	got, err := project.DecodeStyle(enc)
	if err != nil {
		t.Fatal(err)
	}
	if got != src {
		t.Fatalf("round trip mismatch: src=%v got=%v", src, got)
	}
}

func TestStyle_HexColor(t *testing.T) {
	src := tcell.StyleDefault.Foreground(tcell.NewHexColor(0xff8800)).Background(tcell.NewHexColor(0x101010))
	enc := project.EncodeStyle(src)
	if enc.Fg != "#ff8800" || enc.Bg != "#101010" {
		t.Fatalf("hex encoding: %+v", enc)
	}
	got, err := project.DecodeStyle(enc)
	if err != nil {
		t.Fatal(err)
	}
	if got != src {
		t.Fatalf("hex round trip mismatch: src=%v got=%v", src, got)
	}
}

func TestStyle_Decode_RejectsUnknownColor(t *testing.T) {
	_, err := project.DecodeStyle(project.StyleTOML{Fg: "not-a-color"})
	if err == nil {
		t.Fatal("expected error for unknown color")
	}
}

func TestStyle_IsZero(t *testing.T) {
	if !(project.StyleTOML{}).IsZero() {
		t.Fatal("zero StyleTOML should report IsZero")
	}
	if (project.StyleTOML{Fg: "red"}).IsZero() {
		t.Fatal("style with fg should not be IsZero")
	}
}
