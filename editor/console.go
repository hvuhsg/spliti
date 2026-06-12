package editor

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/AllenDang/cimgui-go/imgui"
	"github.com/hvuhsg/spliti/app"
)

// The Console panel collects the editor's status messages, anything the game
// writes through the standard log package, and the streamed output of rebuild
// runs (whose file:line errors are clickable). Lines arrive from goroutines
// (the log writer, the rebuild runner), so the buffer is mutex-guarded; the UI
// reads a copy each frame.

type logKind int

const (
	logInfo logKind = iota
	logWarn
	logError
	logBuild // rebuild output, scanned for file:line locations
)

type consoleLine struct {
	kind logKind
	text string
	file string // non-empty = clickable source location
	line int
}

type console struct {
	mu    sync.Mutex
	lines []consoleLine
	// partial accumulates an unterminated trailing fragment from Write calls
	// until its newline arrives.
	partial string
}

const consoleMax = 2000

func (co *console) add(kind logKind, text string) {
	co.mu.Lock()
	defer co.mu.Unlock()
	for _, ln := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		cl := consoleLine{kind: kind, text: ln}
		if kind == logBuild || kind == logError {
			cl.file, cl.line = parseSourceLocation(ln)
		}
		co.lines = append(co.lines, cl)
	}
	if over := len(co.lines) - consoleMax; over > 0 {
		co.lines = append(co.lines[:0], co.lines[over:]...)
	}
}

// Write lets the console capture the standard log package's output.
func (co *console) Write(p []byte) (int, error) {
	co.mu.Lock()
	s := co.partial + string(p)
	co.partial = ""
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		co.partial = s[i+1:]
		s = s[:i]
	} else {
		co.partial = s
		s = ""
	}
	co.mu.Unlock()
	if s != "" {
		co.add(logInfo, s)
	}
	return len(p), nil
}

func (co *console) snapshot() []consoleLine {
	co.mu.Lock()
	defer co.mu.Unlock()
	out := make([]consoleLine, len(co.lines))
	copy(out, co.lines)
	return out
}

func (co *console) clear() {
	co.mu.Lock()
	co.lines = nil
	co.mu.Unlock()
}

// captureLog tees the standard log output into the console (the terminal
// still gets everything).
func (st *state) captureLog() {
	log.SetOutput(teeWriter{a: log.Writer(), b: &st.console})
}

type teeWriter struct {
	a, b interface{ Write([]byte) (int, error) }
}

func (t teeWriter) Write(p []byte) (int, error) {
	t.b.Write(p)
	return t.a.Write(p)
}

// srcLocRe matches "path/file.go:12:5:" / "path/file.go:12:" prefixes the Go
// toolchain emits.
var srcLocRe = regexp.MustCompile(`(?:^|\s)((?:[\w./~-]+/)?[\w.-]+\.go):(\d+)(?::\d+)?:`)

func parseSourceLocation(line string) (string, int) {
	m := srcLocRe.FindStringSubmatch(line)
	if m == nil {
		return "", 0
	}
	n, _ := strconv.Atoi(m[2])
	return m[1], n
}

// drawConsole renders the Console panel.
func drawConsole(c *app.Ctx, st *state) {
	if !imgui.Begin("Console") {
		imgui.End()
		return
	}
	if imgui.Button("Clear") {
		st.console.clear()
	}
	imgui.SameLine()
	imgui.Checkbox("auto-scroll", &st.consoleAutoScroll)

	imgui.Separator()
	if imgui.BeginChildStr("##console-lines") {
		lines := st.console.snapshot()
		for i, ln := range lines {
			imgui.PushIDInt(int32(i))
			switch ln.kind {
			case logError:
				imgui.PushStyleColorVec4(imgui.ColText, imgui.Vec4{X: 1, Y: 0.4, Z: 0.35, W: 1})
			case logWarn:
				imgui.PushStyleColorVec4(imgui.ColText, imgui.Vec4{X: 1, Y: 0.8, Z: 0.4, W: 1})
			case logBuild:
				imgui.PushStyleColorVec4(imgui.ColText, imgui.Vec4{X: 0.75, Y: 0.8, Z: 1, W: 1})
			default:
				imgui.PushStyleColorVec4(imgui.ColText, imgui.Vec4{X: 0.85, Y: 0.85, Z: 0.85, W: 1})
			}
			if ln.file != "" {
				if imgui.SelectableBool(ln.text) {
					st.openSource(ln.file, ln.line)
				}
				imgui.SetItemTooltip(fmt.Sprintf("open %s:%d", ln.file, ln.line))
			} else {
				imgui.TextUnformatted(ln.text)
			}
			imgui.PopStyleColor()
			imgui.PopID()
		}
		if st.consoleAutoScroll && imgui.ScrollY() >= imgui.ScrollMaxY()-1 {
			imgui.SetScrollHereYV(1)
		}
	}
	imgui.EndChild()
	imgui.End()
}

// openSource opens file:line in the user's editor: $SPLITI_OPEN (run as
// "<cmd> file:line"), VS Code when on PATH, else the platform opener.
func (st *state) openSource(file string, line int) {
	if !filepath.IsAbs(file) {
		// Build errors are relative to the rebuild dir (.spliti/editor) or the
		// project root; try both.
		for _, base := range []string{editorTargetDir(st.cfg.ProjectRoot), st.cfg.ProjectRoot} {
			if p := filepath.Join(base, file); fileExists(p) {
				file = p
				break
			}
		}
	}
	loc := fmt.Sprintf("%s:%d", file, line)
	var cmd *exec.Cmd
	switch {
	case os.Getenv("SPLITI_OPEN") != "":
		parts := strings.Fields(os.Getenv("SPLITI_OPEN"))
		cmd = exec.Command(parts[0], append(parts[1:], loc)...)
	case lookPathOK("code"):
		cmd = exec.Command("code", "-g", loc)
	default:
		cmd = exec.Command("open", file)
	}
	if err := cmd.Start(); err != nil {
		st.status(fmt.Sprintf("open %s: %v", loc, err))
	}
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func lookPathOK(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
