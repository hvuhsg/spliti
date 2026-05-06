# TUI & Input

The tui plugin renders entities to the terminal. The input plugin surfaces keystrokes, resizes, and mouse events. Together they're the only way to talk to the user.

## Components: Position + Glyph

The render system looks for entities with both:

```go
type Position struct{ X, Y int }              // (0,0) is the top-left cell
type Glyph struct {
    Char  rune
    Style tcell.Style
}
```

A spawned entity with these two appears on the screen at `(X, Y)` with the given character and style.

```go
m := generic.NewMap2[tui.Position, tui.Glyph](c.World())
m.NewWith(
    &tui.Position{X: 5, Y: 3},
    &tui.Glyph{Char: '@', Style: tcell.StyleDefault.Foreground(tcell.ColorLightGreen)},
)
```

That's the entire model — there's no clipping logic and no scene graph. Iteration order is arche's archetype-storage order (deterministic but not user-controlled). If two entities share a `Position`, the second one in iteration order wins the cell.

### `Layer` — explicit draw order when you need it

Add a `Layer` component to put an entity above the unlayered crowd:

```go
type Layer struct{ Z int }

m := generic.NewMap3[tui.Position, tui.Glyph, tui.Layer](c.World())
m.NewWith(
    &tui.Position{X: 0, Y: 0},
    &tui.Glyph{Char: '*', Style: hudStyle},
    &tui.Layer{Z: 1},        // above all unlayered entities
)
```

Render order in `__spliti_render`:

1. **Unlayered fast path.** All entities with `(Position, Glyph)` but no `Layer` are drawn in arche's storage order with no allocation.
2. **Layered pass.** All entities with `(Position, Glyph, Layer)` are collected, stable-sorted by `Z` ascending, and drawn on top. Within a single `Z`, archetype-storage order breaks ties.

If no entity in the world has a `Layer`, the layered pass is skipped entirely — no slice allocation, no sort. So adding `Layer` to one HUD-style entity doesn't tax frames in a `Layer`-free game.

Note that `Layer` only orders against itself and against unlayered entities; it does **not** order against `tui.AddOverlay`-registered systems, which always run after the entity render and therefore always paint on top of layered glyphs. Layers are for ordering entities; overlays are for ordering systems.

## The render → present pipeline (and why)

The tui plugin registers two systems in `PostUpdate`:

1. **Render** (label `__spliti_render`):
   - Clears every cell of the back buffer to `ClearStyle`.
   - Iterates `(Position, Glyph)` entities and `SetContent`s their cells.
   - **Does not call `Show()`.**
2. **Present** (label `__spliti_present`, `.After(__spliti_render)`):
   - Calls `Screen.Show()` exactly once per frame.

Between them: any user overlay registered via `tui.AddOverlay(a, fn)` (which is constrained `.After(renderLabel).Before(presentLabel)`).

```
PostUpdate sequence:
  __spliti_render      → clears + draws entities to back buffer
  any AddOverlay-registered systems (HUDs, debug text, etc.)
  __spliti_present     → single Show() flushes the whole frame
```

### Why the split exists

The naive design — each system calls `SetContent` then `Show` — produces visible flicker on idle frames. Reason: `Show()` is a diff against the previous frame's screen state, and the render system's clear briefly puts the back buffer into a "no HUD" state. If the HUD overlay doesn't run before the next `Show`, the terminal repaints the cleared HUD area for one beat, then the overlay restores it on the second `Show`. Even at microsecond gaps the user can see the strip flash.

The fix: write everything to the back buffer first, then issue exactly one `Show`. The split-system design enforces that automatically — overlays slot between render and present, and the present is the only system that calls `Show`.

**The invariant: never call `Screen.Show()` from a user system.** Always write to the back buffer (via `SetContent` or helpers) and let the present system flush. An extra `Show` reintroduces the flicker.

## `tui.AddOverlay` — drawing on top

```go
tui.AddOverlay(a, drawHUD)
```

The function takes a `SystemFunc` (a plain `func(*app.Ctx)`) and registers it correctly between render and present. Inside, you draw straight to the screen:

```go
func drawHUD(c *app.Ctx) {
    s := tui.Screen(c)
    if s == nil { return }
    drawText(s, 1, 0, hudStyle, " score: 42 ")
}

func drawText(s tcell.Screen, x, y int, style tcell.Style, text string) {
    for i, r := range text {
        s.SetContent(x+i, y, r, nil, style)
    }
}
```

`tui.Screen(c)` is the convenience accessor — equivalent to `app.GetResource[terminal.Terminal](c).Screen` with a nil check. It's nil during very early frames if you call it before the terminal plugin has finished its `Build`; defend against that.

There's also `tui.AddPreRender(a, fn)` for systems that should run **before** the entity render (camera updates, transform propagation). These see the back buffer that the next render call will clobber, so don't draw — mutate world state.

## Reading the screen size

```go
w, h := tui.Screen(c).Size()
```

The terminal plugin enables resize events; if you want to react to window resizes:

```go
for _, ev := range app.ReadEvents[input.ResizeEvent](c) {
    // ev.Width, ev.Height are the new dimensions
}
```

## Input events

The input plugin runs a goroutine that polls tcell. Each frame, in `First`, it drains every pending event into typed buffers:

```go
type KeyEvent struct {
    Key  tcell.Key
    Rune rune
    Mod  tcell.ModMask
}

type ResizeEvent struct{ Width, Height int }

type MouseEvent struct {
    X, Y    int
    Buttons tcell.ButtonMask
    Mod     tcell.ModMask
}
```

Read them in `Update` (or wherever):

```go
for _, ev := range app.ReadEvents[input.KeyEvent](c) {
    if ev.Rune == 'q' || ev.Key == tcell.KeyCtrlC {
        c.App().Stop()
        return
    }
    // …
}
```

### Distinguishing keys

`KeyEvent` carries both `Key` (tcell's named-key enum, e.g. `KeyUp`, `KeyEnter`) and `Rune` (the literal character for typed keys). Check `Key` first for special keys, fall back to `Rune` for letters/digits/punctuation:

```go
switch {
case ev.Key == tcell.KeyUp || ev.Rune == 'w': // up
case ev.Key == tcell.KeyEscape:                // esc
case ev.Rune == 'a':                           // letter a
}
```

### Mouse

To enable mouse reporting:

```go
defaultplugins.Plugins{EnableMouse: true}
```

Or, if you're configuring plugins individually:

```go
terminal.Plugin{EnableMouse: true}
```

The terminal then sends `MouseEvent`s for clicks, drags, and motion (terminal-emulator dependent).

### Lifetime

Input events follow the standard event lifetime: any event captured in this frame's `First` is visible through the rest of the frame, then drained at end of `Last`. If a system in `Update` doesn't consume a key, no later frame will see it.

## Where the input plugin's goroutine fits

The poll goroutine is started in `Plugin.Build` and reads `screen.PollEvent()` in a loop, pushing into a channel. That channel is drained synchronously in the `First`-stage system. The goroutine exits cleanly via the `OnExit` hook the plugin installs (it `close`s the quit channel and posts a synthetic interrupt event so `PollEvent` returns).

You shouldn't need to touch this — the design's only externally visible quirk is that key events captured during the host-handshake stall in `network.Plugin` are buffered (up to 64) until the first `First` after handshake.

## A complete render loop, end to end

For one frame:

```
First:                  time tick; input drain → Events[KeyEvent]
PreUpdate:              network capture (if network.Plugin loaded)
StateTransition:        applyStateTransitions → OnExit/OnEnter systems run
[time pre-update hook]: 0..N iterations of FixedFirst → FixedUpdate → FixedLast
Update:                 user game logic, input handling, animations
PostUpdate:
  __spliti_render       clear back buffer + draw (Position, Glyph) entities
  user overlays         draw HUD, debug text, etc. via tui.AddOverlay
  __spliti_present      Screen.Show() — single flush
Last:                   anything genuinely last
[engine drains all Events[T] buffers]
[time post-update hook] frame pacing sleep
```

That's the canonical CLI-game frame in `spliti`.

## Next

- [docs/network.md](network.md) — making the game two-player.
- The `examples/snake/main.go` source — every concept above used in anger.
