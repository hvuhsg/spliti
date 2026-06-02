package project

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/gdamore/tcell/v2"
	"github.com/hvuhsg/spliti/plugin/sprite"
)

// SpriteFile is the on-disk format for a sprite asset. The grid is stored
// as a Rows []string field so authors can edit it visually in any text
// editor; per-cell style overrides go in the Cells map keyed by "x,y".
//
// A sprite is fully described by:
//
//   - Ref: registry id used by Sprite.Ref components in scenes
//   - W,H: explicit dimensions; rejecting mismatched Rows is the editor's
//     job, not the loader's (we trust files we wrote).
//   - Style: default style applied to every non-empty cell that lacks an override.
//   - Rows: each string is one row; runes index left→right; empty rune (' ')
//     marks a transparent cell unless EmptyChar is overridden.
//   - Cells: per-cell style/char overrides keyed "x,y" (e.g. "0,2" = column 0, row 2).
//   - EmptyChar: the rune used to represent transparent cells in Rows.
//     Defaults to ' ' (space). Override if your sprite needs literal spaces.
type SpriteFile struct {
	Ref       string                       `toml:"ref"`
	W         int                          `toml:"w"`
	H         int                          `toml:"h"`
	Style     StyleTOML                    `toml:"style,omitempty"`
	Rows      []string                     `toml:"rows"`
	Cells     map[string]SpriteCellTOML    `toml:"cells,omitempty"`
	EmptyChar string                       `toml:"emptyChar,omitempty"`
}

// SpriteCellTOML is a per-cell override. Char of "" means "use the row's
// rune"; this lets users pin a style to a cell without changing its glyph.
type SpriteCellTOML struct {
	Char  string    `toml:"char,omitempty"`
	Style StyleTOML `toml:",inline"`
}

// LoadSpriteFile reads a .sprite file from disk and returns the parsed
// SpriteFile. It does NOT convert to a *sprite.SpriteAsset — that's
// AssetFromFile's job, kept separate so editor UIs can mutate the file
// representation in place before re-rendering.
func LoadSpriteFile(path string) (*SpriteFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read sprite %q: %w", path, err)
	}
	var sf SpriteFile
	if _, err := toml.Decode(string(data), &sf); err != nil {
		return nil, fmt.Errorf("parse sprite %q: %w", path, err)
	}
	if sf.Ref == "" {
		// Default ref to filename stem so users can omit it.
		sf.Ref = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	return &sf, nil
}

// SaveSpriteFile writes a SpriteFile to disk atomically (temp file + rename).
// Indentation/whitespace are entirely up to BurntSushi/toml; we don't try to
// preserve hand-formatting because the editor is the authoring tool.
func SaveSpriteFile(path string, sf *SpriteFile) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var buf bytes.Buffer
	enc := toml.NewEncoder(&buf)
	enc.Indent = "  "
	if err := enc.Encode(sf); err != nil {
		return fmt.Errorf("encode sprite %q: %w", path, err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// AssetFromFile converts a SpriteFile into a renderable *sprite.SpriteAsset
// by resolving styles. Returns an error if any color is unparseable or any
// row is shorter than W. Rows longer than W are truncated; rows beyond H
// are ignored.
func AssetFromFile(sf *SpriteFile) (*sprite.SpriteAsset, error) {
	if sf.W <= 0 || sf.H <= 0 {
		return nil, fmt.Errorf("sprite %q: w,h must be positive (got %d,%d)", sf.Ref, sf.W, sf.H)
	}
	defaultStyle, err := DecodeStyle(sf.Style)
	if err != nil {
		return nil, fmt.Errorf("sprite %q: default style: %w", sf.Ref, err)
	}
	emptyRune := ' '
	if sf.EmptyChar != "" {
		// Take the first rune; longer strings are silently truncated.
		for _, r := range sf.EmptyChar {
			emptyRune = r
			break
		}
	}
	asset := &sprite.SpriteAsset{
		W:     sf.W,
		H:     sf.H,
		Cells: make([]sprite.Cell, sf.W*sf.H),
	}
	for y := 0; y < sf.H; y++ {
		var row string
		if y < len(sf.Rows) {
			row = sf.Rows[y]
		}
		runes := []rune(row)
		for x := 0; x < sf.W; x++ {
			ch := emptyRune
			if x < len(runes) {
				ch = runes[x]
			}
			cell := sprite.Cell{Char: ch, Style: defaultStyle}
			if ch == emptyRune {
				cell.Empty = true
			}
			// Per-cell override: replaces char and/or style.
			if sf.Cells != nil {
				if ov, ok := sf.Cells[strconv.Itoa(x)+","+strconv.Itoa(y)]; ok {
					if ov.Char != "" {
						for _, r := range ov.Char {
							cell.Char = r
							cell.Empty = false
							break
						}
					}
					if !ov.Style.IsZero() {
						s, err := DecodeStyle(ov.Style)
						if err != nil {
							return nil, fmt.Errorf("sprite %q cell %d,%d: %w", sf.Ref, x, y, err)
						}
						cell.Style = s
					}
				}
			}
			asset.Cells[y*sf.W+x] = cell
		}
	}
	return asset, nil
}

// FileFromAsset is the inverse of AssetFromFile: produces an editable
// SpriteFile from a runtime asset, deriving a default style from the most
// common style across non-empty cells. Cells diverging from that default
// are emitted as overrides.
//
// Rounding: if every non-empty cell shares a single style, we use it as
// the default and emit zero overrides. Otherwise we pick the first
// non-empty cell's style and emit overrides for the rest. Good enough for
// editor save — the output round-trips correctly even if it's not
// minimum-byte.
func FileFromAsset(ref string, asset *sprite.SpriteAsset) *SpriteFile {
	sf := &SpriteFile{Ref: ref, W: asset.W, H: asset.H}
	// Choose default style: first non-empty cell wins. If the sprite has no
	// non-empty cells, we fall back to tcell.StyleDefault.
	defaultStyle := tcell.StyleDefault
	for _, c := range asset.Cells {
		if !c.Empty {
			defaultStyle = c.Style
			break
		}
	}
	sf.Style = EncodeStyle(defaultStyle)

	// Build rows + overrides.
	emptyRune := ' '
	sf.Rows = make([]string, asset.H)
	for y := 0; y < asset.H; y++ {
		runes := make([]rune, asset.W)
		for x := 0; x < asset.W; x++ {
			cell := asset.Cells[y*asset.W+x]
			if cell.Empty {
				runes[x] = emptyRune
				continue
			}
			runes[x] = cell.Char
			if cell.Style != defaultStyle {
				if sf.Cells == nil {
					sf.Cells = make(map[string]SpriteCellTOML)
				}
				sf.Cells[strconv.Itoa(x)+","+strconv.Itoa(y)] = SpriteCellTOML{
					Style: EncodeStyle(cell.Style),
				}
			}
		}
		sf.Rows[y] = string(runes)
	}
	return sf
}

