package inputs

import "testing"

func TestNameTablesRoundTrip(t *testing.T) {
	for k, name := range keyNames {
		got, ok := KeyByName(name)
		if !ok || got != k {
			t.Fatalf("KeyByName(%q) = %v, %v", name, got, ok)
		}
	}
	for b, name := range mouseButtonNames {
		got, ok := MouseButtonByName(name)
		if !ok || got != b {
			t.Fatalf("MouseButtonByName(%q) = %v, %v", name, got, ok)
		}
	}
	for b, name := range gamepadButtonNames {
		got, ok := GamepadButtonByName(name)
		if !ok || got != b {
			t.Fatalf("GamepadButtonByName(%q) = %v, %v", name, got, ok)
		}
	}
	for a, name := range gamepadAxisNames {
		got, ok := GamepadAxisByName(name)
		if !ok || got != a {
			t.Fatalf("GamepadAxisByName(%q) = %v, %v", name, got, ok)
		}
	}
	if KeyName(KeyUnknown) != "" {
		t.Fatal("KeyUnknown should have no name")
	}
	if name := KeyName(KeySpace); name != "KeySpace" {
		t.Fatalf("KeyName(KeySpace) = %q", name)
	}
}
