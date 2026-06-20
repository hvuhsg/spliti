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

### ⚓ Harbours

Coastal buildings on a **sand** rim — they obey the sand gradient, but also need a
shoreline to dock against (see §5).

| | |
| :--: | :--: |
| <img src="tiles/building-dock.png" width="150"> | <img src="tiles/building-port.png" width="150"> |
| `building-dock` ⭐ | `building-port` ⭐ |

### 🌳 Grass
| | | |
| :--: | :--: | :--: |
| <img src="tiles/grass.png" width="150"> | <img src="tiles/grass-forest.png" width="150"> | <img src="tiles/grass-hill.png" width="150"> |
| `grass` | `grass-forest` | `grass-hill` |

### 🏰 Buildings

Settlements that ride on a **grass** base, so they follow the grass gradient.
They are rare — most grass cells stay wild — and two buildings may never sit
side by side (see §5), so each reads as its own landmark.

| | | | |
| :--: | :--: | :--: | :--: |
| <img src="tiles/building-cabin.png" width="120"> | <img src="tiles/building-house.png" width="120"> | <img src="tiles/building-village.png" width="120"> | <img src="tiles/building-farm.png" width="120"> |
| `building-cabin` ⭐ | `building-house` ⭐ | `building-village` ⭐ | `building-farm` ⭐ |
| <img src="tiles/building-sheep.png" width="120"> | <img src="tiles/building-market.png" width="120"> | <img src="tiles/building-mill.png" width="120"> | <img src="tiles/building-watermill.png" width="120"> |
| `building-sheep` ⭐ | `building-market` ⭐ | `building-mill` ⭐ | `building-watermill` ⭐ |
| <img src="tiles/building-smelter.png" width="120"> | <img src="tiles/building-mine.png" width="120"> | <img src="tiles/building-tower.png" width="120"> | <img src="tiles/building-archery.png" width="120"> |
| `building-smelter` ⭐ | `building-mine` ⭐ | `building-tower` ⭐ | `building-archery` ⭐ |
| <img src="tiles/building-wall.png" width="120"> | <img src="tiles/building-walls.png" width="120"> | <img src="tiles/building-castle.png" width="120"> | <img src="tiles/building-wizard-tower.png" width="120"> |
| `building-wall` ⭐ | `building-walls` ⭐ | `building-castle` ⭐ | `building-wizard-tower` ⭐ |

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

⭐ = has an extra placement rule (see §5).

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

## 4. Terrain clusters together (soft rule)

The gradient says which terrains *may* touch; on its own it would pick among the
legal options at near-random, giving a speckled, noisy island. So on top of it
there is one **soft** rule — it changes the odds, never the legality:

> **A cell leans toward the terrain its neighbours already have.** Every
> already-placed neighbour that shares a terrain multiplies that terrain's chance
> by **2.5×**. The more neighbours agree, the stronger the pull:

| Matching neighbours | Roughly how much likelier that terrain becomes |
| :-----------------: | ---------------------------------------------- |
| 1 | 2.5× — a gentle nudge |
| 2 | ~6× |
| 3 | ~16× — strongly favoured |
| 4 | ~39× |
| 6 (fully ringed) | ~240× — all but certain |

So a cell with four water neighbours almost always becomes water as well, and
water pools into open sea, sand spreads into broad beaches, grass into meadows,
and stone into mountain ranges — instead of every cell rolling its terrain in
isolation. Open ocean past the board edge counts as water, so islands keep a
natural watery rim.

Two things this rule does **not** do: it never overrides the gradient (a choice
the rules forbid stays forbidden, with or without the bias), and it only steers
which *terrain* a cell takes — the base tile weights still decide which variant
of that terrain you get, so buildings stay rare even in a large meadow.

A whole island grown under the bias — notice the contiguous regions:

<img src="examples/clusters.png" width="460" alt="island with coherent terrain regions">

The strength lives in one constant, `wfc.clusterBias` — raise it for blockier,
more segregated continents, lower it (toward 1) for a noisier, more mixed island.

---

## 5. Special placement rules

The gradient decides which **terrains** may touch. Three tiles want a little
more than that — each looks only at the neighbors already placed, so these rules
sit on top of the gradient without any extra propagation.

### 5a. Islands need open water

**`water-island` may only be placed when every one of its neighbors is water** —
a collapsed water tile, or the open ocean past the edge of the board. The
gradient alone would let an island sit against a beach; this holds it out at sea.

✅ Allowed — ringed entirely by water:

<img src="examples/island.png" width="400" alt="island surrounded by water">

### 5b. Harbours need a shore — and face the land

The harbour tiles, **`building-dock` and `building-port`, are water on one side
and grass on the other** — transition tiles. So two things apply:

- They may only be placed where at least one neighbor is **water** (a placed
  water tile, or open ocean off the board) **and** at least one neighbor is
  **grass**: a harbour bridges the sea and the shore.
- They are then **turned so their green half faces a grass tile** (and, where it
  can, their water half faces the open sea). A dock's pier never points at dry
  land, and its grassy back never faces the water. This is the one tile whose
  *rotation* the rules choose instead of leaving to chance.

✅ Allowed — a port with grass behind its green half and the sea before its dock:

<img src="examples/harbour.png" width="400" alt="port tile, green side to grass, water side to the sea">

### 5c. Buildings stand apart

**No building may be placed next to another building.** Buildings are already
rare, and this keeps them from clumping, so every settlement — `building-cabin`,
`building-village`, `building-castle` and the rest — reads as its own landmark
with open ground around it.

✅ Allowed — a castle and a mill kept apart by plain grass:

<img src="examples/settlements.png" width="400" alt="castle and mill separated by grass">

---

## 6. Forbidden examples

These are **not** allowed; shown here only for contrast.

| | |
| :--: | :--: |
| <img src="examples/forbidden-mountain-sea.png" width="320"> | <img src="examples/forbidden-island-beach.png" width="320"> |
| ❌ mountain (stone) next to sea (water) — 4 steps apart | ❌ island next to a beach — not surrounded by water |
| <img src="examples/forbidden-harbour-inland.png" width="320"> | <img src="examples/forbidden-buildings-adjacent.png" width="320"> |
| ❌ harbour ringed by dry land — no shore to dock against | ❌ two buildings side by side — settlements must stand apart |

---

## Where this lives in the code

- Gradient + matrix: `wfc.Compatible` in `../game/wfc/wfc.go`.
- Tile catalog (which tile is which terrain, and its pick weight): `wfc.Tiles`.
- Terrain-clustering bias: `wfc.clusterBias` + `Board.weightedPick` /
  `Board.neighbourTerrainCounts`.
- Special placement rules: `wfc.needsOpenWater`, `wfc.needsShore`, `wfc.IsBuilding`,
  gathered in `Board.allows` (with `Board.allNeighboursWater` / `touchesWater` /
  `touchesBuilding`).
- Harbour orientation (green half faces grass): `wfc.orientedGreenEdge`,
  `Board.canOrient`, `Board.RotationStep` (called from the spawn code).
- Propagation that enforces it all across the board: `Board.Collapse` / `Board.propagate`.

To regenerate every image in this folder:

```sh
go build -o /tmp/hexisland-bin .
# one tile:
SPLITI_HEX_SHOT=rules/tiles/grass.png SPLITI_HEX_TILE=grass /tmp/hexisland-bin
# one example scene (see the scenes map in capture.go for names):
SPLITI_HEX_SHOT=rules/examples/island.png SPLITI_HEX_SCENE=island /tmp/hexisland-bin
```
