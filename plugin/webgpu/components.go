package webgpu

// Transform places and sizes a sprite in world space. (X,Y) is the top-left
// corner, (W,H) the extent — both in the world units the Camera maps to the
// window. A zero W or H falls back to the source texture's pixel size at draw
// time, so a sprite drawn 1:1 only needs X,Y set.
//
// World space matches the terminal convention: (0,0) is top-left and Y grows
// downward. The ortho projection in camera.go flips Y for the GPU.
type Transform struct{ X, Y, W, H float32 }

// Color tints a sprite. The final fragment is texture * tint, so {1,1,1,1}
// (or the zero value, which the renderer treats as opaque white) leaves the
// texture untouched. Lower A for transparency.
type Color struct{ R, G, B, A float32 }

// opaqueWhite is substituted for a missing or zero Color so an untinted sprite
// renders at full texture color. A genuinely invisible sprite should set a
// non-zero-but-transparent Color (e.g. {0,0,0, 0} is indistinguishable from
// "no Color"; use {1,1,1, 0} to force full transparency).
var opaqueWhite = Color{1, 1, 1, 1}

func (c Color) orDefault() Color {
	if c == (Color{}) {
		return opaqueWhite
	}
	return c
}

// Sprite selects which uploaded texture to draw, by the key it was registered
// under in the TextureRegistry. An entity needs Transform + Sprite to render;
// Color and Layer are optional.
type Sprite struct{ Ref string }

// Layer overrides draw order. Higher Z draws on top of lower Z and on top of
// entities with no Layer (which are treated as Z=0). Mirrors tui.Layer.
type Layer struct{ Z int }
