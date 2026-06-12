// Command spliti is the spliti engine's project CLI:
//
//	spliti new <name> [--engine <path>]   scaffold a game project
//	spliti gen                            regenerate .spliti/editor from the game source
//	spliti edit                           gen + run the visual editor (needs cgo)
//	spliti run                            run the game natively
//	spliti build [--wasm]                 build the game binary (or wasm bundle)
//
// new/gen/edit/run/build all operate on the project in the current directory
// (except new, which creates <name>/).
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/hvuhsg/spliti/editor/gen"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "new":
		err = cmdNew(os.Args[2:])
	case "gen":
		_, err = cmdGen()
	case "edit":
		err = cmdEdit()
	case "run":
		err = run("go", "run", ".")
	case "build":
		err = cmdBuild(os.Args[2:])
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "spliti: unknown command %q\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "spliti:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage:
  spliti new <name> [--engine <path>]   scaffold a game project
  spliti gen                            regenerate the editor target
  spliti edit                           open the project in the visual editor
  spliti run                            run the game
  spliti build [--wasm]                 build the game`)
}

func cmdGen() (*gen.Project, error) {
	p, err := gen.Load(".")
	if err != nil {
		return nil, err
	}
	if err := p.Generate(); err != nil {
		return nil, err
	}
	// The target is its own module (the go tool ignores dot-directories);
	// resolve its dependency graph in place so the follow-up build works.
	if err := runDir(p.EditorDir(), nil, "go", "mod", "tidy"); err != nil {
		return nil, fmt.Errorf("gen: go mod tidy (.spliti/editor): %w", err)
	}
	fmt.Println("generated .spliti/editor")
	return p, nil
}

func cmdEdit() error {
	p, err := cmdGen()
	if err != nil {
		return err
	}
	return runDir(p.EditorDir(), []string{"CGO_ENABLED=1"}, "go", "run", ".")
}

func cmdBuild(args []string) error {
	wasm := len(args) > 0 && args[0] == "--wasm"
	if wasm {
		return runEnv([]string{"GOOS=js", "GOARCH=wasm"}, "go", "build", "-o", "game.wasm", ".")
	}
	return run("go", "build", ".")
}

func run(name string, args ...string) error { return runDir("", nil, name, args...) }

func runEnv(env []string, name string, args ...string) error {
	return runDir("", env, name, args...)
}

func runDir(dir string, env []string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = append(os.Environ(), env...)
	return cmd.Run()
}

// engineReplacePath resolves where a scaffolded project should point its
// `replace github.com/hvuhsg/spliti` directive: the --engine flag, the
// SPLITI_ENGINE env var, or (when the CLI runs inside the engine repo itself,
// the usual development setup) the enclosing module.
func engineReplacePath(flagVal string) string {
	if flagVal != "" {
		return flagVal
	}
	if env := os.Getenv("SPLITI_ENGINE"); env != "" {
		return env
	}
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Path}} {{.Dir}}").Output()
	if err == nil {
		fields := strings.Fields(strings.TrimSpace(string(out)))
		if len(fields) == 2 && fields[0] == "github.com/hvuhsg/spliti" {
			return fields[1]
		}
	}
	return ""
}

func cmdNew(args []string) error {
	var name, engine string
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--engine" && i+1 < len(args):
			i++
			engine = args[i]
		case strings.HasPrefix(args[i], "-"):
			return fmt.Errorf("new: unknown flag %s", args[i])
		case name == "":
			name = args[i]
		default:
			return fmt.Errorf("new: unexpected argument %s", args[i])
		}
	}
	if name == "" {
		return fmt.Errorf("new: project name required")
	}
	engine = engineReplacePath(engine)
	if engine != "" {
		if abs, err := filepath.Abs(engine); err == nil {
			engine = abs
		}
	}
	if err := scaffold(name, engine); err != nil {
		return err
	}
	fmt.Printf("created %s/\n", name)
	fmt.Printf("  cd %s && spliti edit\n", name)
	if engine == "" {
		fmt.Println("  (no local engine path found; go.mod expects a published github.com/hvuhsg/spliti)")
	}
	return nil
}
