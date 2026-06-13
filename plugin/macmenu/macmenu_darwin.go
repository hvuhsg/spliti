// Package macmenu drives the native macOS application menu bar (the menus that
// live beside the Apple logo). It is a thin, reusable wrapper over Cocoa's
// NSMenu: callers build a tree of menus and items, attach Go callbacks, and
// pump clicks once per frame with Dispatch.
//
// All calls must happen on the process main thread — the same thread that owns
// the NSApplication (where render3d locks the OS thread and the schedule runs).
// Item clicks fire on that thread during the Cocoa event pump (glfw.PollEvents);
// macmenu only enqueues them, so the registered callbacks run when the caller
// invokes Dispatch, never reentrantly inside the event pump.
//
// On every non-darwin platform the package compiles to no-ops (see
// macmenu_other.go) and Available reports false, so callers can wire the native
// menu unconditionally and keep an in-app fallback for other platforms.
package macmenu

/*
#cgo LDFLAGS: -framework Cocoa
#include <stdlib.h>

// Implemented in macmenu_darwin.m.
int  macmenuNewMenu(const char* title);
void macmenuAddTop(int menu, const char* title);
void macmenuAddSubmenu(int parent, int child, const char* title);
void macmenuAddItem(int menu, int tag, const char* title);
void macmenuAddSeparator(int menu);
void macmenuSetKey(int tag, const char* key, unsigned long mods);
void macmenuSetEnabled(int tag, int enabled);
void macmenuSetChecked(int tag, int checked);
void macmenuSetTitle(int tag, const char* title);
*/
import "C"

import (
	"sync"
	"unsafe"
)

// Mod is a key-equivalent modifier mask. The values mirror Cocoa's
// NSEventModifierFlags so they can be passed straight through to AppKit.
type Mod uint

const (
	Cmd   Mod = 1 << 20
	Shift Mod = 1 << 17
	Opt   Mod = 1 << 19
	Ctrl  Mod = 1 << 18
)

// Menu is a handle to a native NSMenu (a top-level menu or a submenu).
type Menu struct{ h C.int }

// Item is a handle to a native NSMenuItem. Use it to update enabled/checked
// state or its title each frame.
type Item struct{ tag C.int }

var (
	mu      sync.Mutex
	actions = map[int]func(){}
	pending []int
	nextTag int
)

// Available reports whether native menus are supported on this platform.
func Available() bool { return true }

// Top creates a top-level menu and inserts it into the application menu bar,
// after the standard application menu and in call order.
func Top(title string) *Menu {
	ct := C.CString(title)
	defer C.free(unsafe.Pointer(ct))
	h := C.macmenuNewMenu(ct)
	C.macmenuAddTop(h, ct)
	return &Menu{h: h}
}

// Submenu creates a child menu nested under m.
func (m *Menu) Submenu(title string) *Menu {
	ct := C.CString(title)
	defer C.free(unsafe.Pointer(ct))
	h := C.macmenuNewMenu(ct)
	C.macmenuAddSubmenu(m.h, h, ct)
	return &Menu{h: h}
}

// Item appends a clickable item to m. action runs on the next Dispatch after
// the user selects it.
func (m *Menu) Item(title string, action func()) *Item {
	mu.Lock()
	tag := nextTag
	nextTag++
	actions[tag] = action
	mu.Unlock()

	ct := C.CString(title)
	defer C.free(unsafe.Pointer(ct))
	C.macmenuAddItem(m.h, C.int(tag), ct)
	return &Item{tag: C.int(tag)}
}

// Separator appends a divider line to m.
func (m *Menu) Separator() { C.macmenuAddSeparator(m.h) }

// Key assigns a keyboard equivalent (e.g. "s" with Cmd for ⌘S). key is the
// lowercase letter; mods is the modifier mask.
func (it *Item) Key(key string, mods Mod) *Item {
	ck := C.CString(key)
	defer C.free(unsafe.Pointer(ck))
	C.macmenuSetKey(it.tag, ck, C.ulong(mods))
	return it
}

// SetEnabled greys the item out (false) or makes it selectable (true).
func (it *Item) SetEnabled(b bool) { C.macmenuSetEnabled(it.tag, cbool(b)) }

// SetChecked toggles the item's checkmark.
func (it *Item) SetChecked(b bool) { C.macmenuSetChecked(it.tag, cbool(b)) }

// SetTitle replaces the item's label.
func (it *Item) SetTitle(title string) {
	ct := C.CString(title)
	defer C.free(unsafe.Pointer(ct))
	C.macmenuSetTitle(it.tag, ct)
}

// Dispatch runs the callbacks for any items selected since the last call. Call
// it once per frame on the main thread.
func Dispatch() {
	mu.Lock()
	tags := pending
	pending = nil
	acts := make([]func(), 0, len(tags))
	for _, t := range tags {
		acts = append(acts, actions[t])
	}
	mu.Unlock()
	for _, a := range acts {
		if a != nil {
			a()
		}
	}
}

func cbool(b bool) C.int {
	if b {
		return 1
	}
	return 0
}

// goMenuAction is the C→Go bridge invoked by the NSMenuItem target when an item
// is clicked. It only enqueues; the callback runs later in Dispatch.
//
//export goMenuAction
func goMenuAction(tag C.int) {
	mu.Lock()
	pending = append(pending, int(tag))
	mu.Unlock()
}
