# hexisland

Build an island by collapsing a wave-function-collapse hex grid.

You choose **where** to place a tile; the game chooses **what** — it collapses
the cell to a tile that obeys the terrain rules, then propagates that constraint
across the board so neighbouring cells narrow their own possibilities. Tiles are
from Kenney's [hexagon kit](https://kenney.nl/assets/hexagon-kit) (CC0).

## How it plays

- The board is a radius-5 hex grid that starts empty. Every cell glows faintly —
  those are the cells you can build on.
- Hover a buildable cell: it brightens and lifts. **Left-click** to place.
- The first tile can go anywhere; after that the island must grow outward, so
  you can only build next to tiles already placed.
- What you get is random among whatever the rules allow, so a grass cell might
  come up as plain grass, a forest, or a hill — and an island never repeats.

**See [`rules/RULES.md`](rules/RULES.md) for the full illustrated rules** — every
tile, what may sit next to it, and the allowed/forbidden combinations.

### The rules (terrain adjacency)

Five terrains form a height gradient and may only sit next to neighbours within
one step of themselves:

```
water — sand — grass — dirt — stone
```

So water rings into beaches, beaches into grass, grass climbs to dirt and bare
rock. That single rule (`game/wfc/wfc.go`, `Compatible`) is what makes the
output look like an island. Box yourself in with water on one side and stone on
the other and a cell can run out of legal tiles — then it stays a hole.

## Controls

| Input                   | Action               |
| ----------------------- | -------------------- |
| Mouse hover             | Highlight a cell     |
| Left-click              | Place a tile         |
| Right-drag              | Orbit the camera     |
| Middle-drag             | Pan across the board |
| Scroll wheel            | Zoom in / out        |
| `A` / `D`, `W` / `S`    | Orbit (keyboard)     |
| Arrow keys              | Pan (keyboard)       |
| `Q` / `E`               | Zoom (keyboard)      |

## Run

```sh
go run .        # or: spliti run
```

## Layout

- `game/wfc/` — engine-free WFC model: hex math, tile catalog, adjacency rules,
  constraint propagation (unit-tested in `wfc_test.go`).
- `game/systems/` — orbit camera, hover/place interaction, per-cell markers.
- `game/game.go` — loads the tile models + marker mesh, registers systems.
- `game/scenes/main.go` — spawns the sun and the board of markers.
- `capture.go` — optional headless screenshot driver (`SPLITI_HEX_SHOT=out.png`).
