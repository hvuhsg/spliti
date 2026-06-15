# Tile rules

How the game decides which tile may go where. You pick a cell; the game collapses
it to a tile that satisfies every rule below, then propagates the new constraint
to its neighbors.

All images here are rendered straight from the game's own tiles and rules (see
`../capture.go`), so they always match what actually ships.

---

## 1. The terrain gradient

Every tile has a **terrain** at its rim. The five terrains form a height
gradient, and two tiles may touch **only if their terrains are at most one step
apart** on it:

```
water  —  sand  —  grass  —  dirt  —  stone
  0        1         2        3        4
```

So beaches ring the sea, grass grows behind the beach, and bare rock and
mountains only rise deep inland. A mountain (stone) can never touch the sea
(water) because they are four steps apart.

### Adjacency matrix

A ✅ means tiles of those two terrains are allowed to be neighbors.

|            | water | sand | grass | dirt | stone |
| ---------- | :---: | :--: | :---: | :--: | :---: |
| **water**  |  ✅   |  ✅  |  ⬜   |  ⬜  |  ⬜   |
| **sand**   |  ✅   |  ✅  |  ✅   |  ⬜  |  ⬜   |
| **grass**  |  ⬜   |  ✅  |  ✅   |  ✅  |  ⬜   |
| **dirt**   |  ⬜   |  ⬜  |  ✅   |  ✅  |  ✅   |
| **stone**  |  ⬜   |  ⬜  |  ⬜   |  ✅  |  ✅   |

A legal run across the whole gradient — every adjacency here is allowed:

<img src="examples/gradient.png" width="520" alt="water, sand, grass, dirt, stone in a row">

---

## 2. The tiles

Tiles that share a terrain are interchangeable to the rules, so a single cell
often has several valid outcomes — that is where the variety comes from.

### 🌊 Water
| | | |
| :--: | :--: | :--: |
| <img src="tiles/water.png" width="150"> | <img src="tiles/water-rocks.png" width="150"> | <img src="tiles/water-island.png" width="150"> |
| `water` | `water-rocks` | `water-island` ⭐ |

### 🏖️ Sand
| | | |
| :--: | :--: | :--: |
| <img src="tiles/sand.png" width="150"> | <img src="tiles/sand-desert.png" width="150"> | <img src="tiles/sand-rocks.png" width="150"> |
| `sand` | `sand-desert` | `sand-rocks` |

### 🌳 Grass
| | | |
| :--: | :--: | :--: |
| <img src="tiles/grass.png" width="150"> | <img src="tiles/grass-forest.png" width="150"> | <img src="tiles/grass-hill.png" width="150"> |
| `grass` | `grass-forest` | `grass-hill` |

### 🟤 Dirt
| | |
| :--: | :--: |
| <img src="tiles/dirt.png" width="150"> | <img src="tiles/dirt-lumber.png" width="150"> |
| `dirt` | `dirt-lumber` |

### ⛰️ Stone
| | | | |
| :--: | :--: | :--: | :--: |
| <img src="tiles/stone.png" width="150"> | <img src="tiles/stone-hill.png" width="150"> | <img src="tiles/stone-mountain.png" width="150"> | <img src="tiles/stone-rocks.png" width="150"> |
| `stone` | `stone-hill` | `stone-mountain` | `stone-rocks` |

⭐ = has an extra placement rule (see §4).

---

## 3. What may sit around each terrain

Each tile below is ringed by examples of its **legal** neighbors.

| Center | Legal neighbor terrains | Example |
| ------ | ----------------------- | ------- |
| **water** | water, sand | <img src="examples/around-water.png" width="320"> |
| **sand**  | water, sand, grass | <img src="examples/around-sand.png" width="320"> |
| **grass** | sand, grass, dirt | <img src="examples/around-grass.png" width="320"> |
| **dirt**  | grass, dirt, stone | <img src="examples/around-dirt.png" width="320"> |
| **stone** | dirt, stone | <img src="examples/around-stone.png" width="320"> |

---

## 4. Special rule: islands need open water

Beyond the gradient, **`water-island` may only be placed when every one of its
neighbors is water** — a collapsed water tile, or the open ocean past the edge of
the board. The gradient alone would let an island sit against a beach; this rule
forbids that, so an island only appears out in the sea.

✅ Allowed — ringed entirely by water:

<img src="examples/island.png" width="400" alt="island surrounded by water">

---

## 5. Forbidden examples

These are **not** allowed; shown here only for contrast.

| | |
| :--: | :--: |
| <img src="examples/forbidden-mountain-sea.png" width="320"> | <img src="examples/forbidden-island-beach.png" width="320"> |
| ❌ mountain (stone) next to sea (water) — 4 steps apart | ❌ island next to a beach — not surrounded by water |

---

## Where this lives in the code

- Gradient + matrix: `wfc.Compatible` in `../game/wfc/wfc.go`.
- Tile catalog (which tile is which terrain): `wfc.Tiles`.
- Island open-water rule: `wfc.openWaterOnly` + `Board.allNeighboursWater`.
- Propagation that enforces it all across the board: `Board.Collapse` / `Board.propagate`.

To regenerate every image in this folder:

```sh
go build -o /tmp/hexisland-bin .
# one tile:
SPLITI_HEX_SHOT=rules/tiles/grass.png SPLITI_HEX_TILE=grass /tmp/hexisland-bin
# one example scene (see the scenes map in capture.go for names):
SPLITI_HEX_SHOT=rules/examples/island.png SPLITI_HEX_SCENE=island /tmp/hexisland-bin
```
