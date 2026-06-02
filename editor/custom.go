package editor

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/editor/components"
	"github.com/hvuhsg/spliti/editor/project"
	"github.com/hvuhsg/spliti/editor/ui/panels"
)

// LoadCustomComponentsForProject reads <projectDir>/components.toml
// (if present) and re-registers synthetic ComponentDescs for the
// schemas it finds. Safe to call repeatedly; previous custom descs are
// removed before re-registration.
//
// Stores the loaded file as a resource so the schema editor can mutate
// it and SaveCustomComponentsForProject can write it back.
func LoadCustomComponentsForProject(c *app.Ctx, p *project.Project) error {
	f, err := project.LoadCustomComponents(p.Dir)
	if err != nil {
		return err
	}
	app.InsertResource(c.App(), f)
	components.RegisterCustom(f)
	return nil
}

// SaveCustomComponentsForProject writes the active custom-components
// file back to <projectDir>/components.toml. Errors surface to the
// status bar; callers don't get them.
func SaveCustomComponentsForProject(c *app.Ctx) {
	p := app.GetResource[project.Project](c)
	f := app.GetResource[components.CustomComponentsFile](c)
	if p == nil || f == nil {
		setStatus(c, "save custom: missing project/components", tcell.StyleDefault.Foreground(tcell.ColorRed))
		return
	}
	if err := project.SaveCustomComponents(p.Dir, f); err != nil {
		setStatus(c, "save custom: "+err.Error(), tcell.StyleDefault.Foreground(tcell.ColorRed))
		return
	}
	setStatus(c, "saved components.toml", tcell.StyleDefault.Foreground(tcell.ColorGreen))
}

// CreateCustomComponentType opens a one-shot prompt for a terse
// component-type spec like "Health: Value:int Max:int Speed:int". The
// spec parses to a CustomComponentSchema, which is appended to the
// file, the registry is rebuilt, and the file is saved.
//
// We use a single-line spec (rather than multi-step prompts) for v1
// because:
//   - It's faster to type for users who know what they want
//   - It's much less code than chained prompts
//   - Bad specs are rejected with an error in the status bar; users
//     simply re-press the shortcut and re-type
func CreateCustomComponentType(c *app.Ctx) {
	pr := app.GetResource[panels.Prompt](c)
	if pr == nil {
		return
	}
	pr.Open(
		"New component type",
		"Spec: \"Name: field1:kind field2:kind ...\"   kinds: int|string|bool|rune",
		func(c *app.Ctx, line string) {
			s, err := parseTypeSpec(line)
			if err != nil {
				setStatus(c, "new type: "+err.Error(), tcell.StyleDefault.Foreground(tcell.ColorRed))
				return
			}
			f := app.GetResource[components.CustomComponentsFile](c)
			if f == nil {
				// Lazily create — possible if no project was loaded
				// at startup. Won't persist without a project anyway.
				f = &components.CustomComponentsFile{}
				app.InsertResource(c.App(), f)
			}
			// Reject duplicate names against built-ins and existing customs.
			if components.ByName(s.Name) != nil {
				setStatus(c, fmt.Sprintf("type %q already exists", s.Name),
					tcell.StyleDefault.Foreground(tcell.ColorRed))
				return
			}
			f.Components = append(f.Components, *s)
			components.RegisterCustom(f)
			SaveCustomComponentsForProject(c)
			setStatus(c,
				fmt.Sprintf("created type %q with %d field(s)", s.Name, len(s.Fields)),
				tcell.StyleDefault.Foreground(tcell.ColorGreen))
		},
		nil,
	)
}

// parseTypeSpec parses "Name: f1:kind f2:kind ...". Strict: the colon
// after Name is required; field tokens must contain exactly one colon.
// Whitespace between fields is tolerant.
func parseTypeSpec(line string) (*components.CustomComponentSchema, error) {
	colon := strings.Index(line, ":")
	if colon < 0 {
		return nil, fmt.Errorf("missing ':' after type name")
	}
	name := strings.TrimSpace(line[:colon])
	if name == "" {
		return nil, fmt.Errorf("empty type name")
	}
	if !validIdent(name) {
		return nil, fmt.Errorf("invalid type name %q (use letters/digits/underscore)", name)
	}
	rest := strings.TrimSpace(line[colon+1:])
	if rest == "" {
		return nil, fmt.Errorf("type %q has no fields", name)
	}
	tokens := strings.Fields(rest)
	fields := make([]components.CustomFieldSchema, 0, len(tokens))
	for _, tok := range tokens {
		parts := strings.SplitN(tok, ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("bad field %q: expected name:kind", tok)
		}
		fname := strings.TrimSpace(parts[0])
		fkind := strings.TrimSpace(parts[1])
		if !validIdent(fname) {
			return nil, fmt.Errorf("invalid field name %q", fname)
		}
		if !validKind(fkind) {
			return nil, fmt.Errorf("unknown kind %q (must be int/string/bool/rune)", fkind)
		}
		fields = append(fields, components.CustomFieldSchema{Name: fname, Kind: fkind})
	}
	return &components.CustomComponentSchema{Name: name, Fields: fields}, nil
}

// validIdent allows a relaxed Go-style identifier: letters, digits,
// underscores, must start with a letter or underscore.
func validIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		ok := r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		if i > 0 {
			ok = ok || (r >= '0' && r <= '9')
		}
		if !ok {
			return false
		}
	}
	return true
}

func validKind(s string) bool {
	switch s {
	case "int", "string", "bool", "rune":
		return true
	}
	return false
}
