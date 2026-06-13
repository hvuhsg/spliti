//go:build !darwin

// Non-darwin no-op implementation. Available reports false so callers fall back
// to their in-app menu; every other call is inert.
package macmenu

type Mod uint

const (
	Cmd   Mod = 1 << 20
	Shift Mod = 1 << 17
	Opt   Mod = 1 << 19
	Ctrl  Mod = 1 << 18
)

type Menu struct{}

type Item struct{}

func Available() bool { return false }

func Top(string) *Menu { return &Menu{} }

func (m *Menu) Submenu(string) *Menu { return &Menu{} }

func (m *Menu) Item(string, func()) *Item { return &Item{} }

func (m *Menu) Separator() {}

func (it *Item) Key(string, Mod) *Item { return it }

func (it *Item) SetEnabled(bool) {}

func (it *Item) SetChecked(bool) {}

func (it *Item) SetTitle(string) {}

func Dispatch() {}
