// Command radio is an interactive teaching game about how information travels
// over radio waves. The main scene shows the whole pipeline — binary -> QPSK
// symbols -> I/Q wave -> over the air -> wave -> symbols -> binary — as a row of
// clickable stages; clicking a stage opens a scene that visualizes that step.
//
// Built on the spliti engine's WebGPU backend, so it needs cgo and a C toolchain:
//
//	CGO_ENABLED=1 go run ./examples/radio
//
// Controls: click a pipeline stage to open it; Esc goes back (and quits from the
// main scene). Detail scenes add their own keys (shown on screen).
package main

import (
	"os"
	"runtime"

	"github.com/hvuhsg/spliti/app"
	splititime "github.com/hvuhsg/spliti/plugin/time"
	"github.com/hvuhsg/spliti/plugin/webgpu"
	"github.com/hvuhsg/spliti/schedule"
)

// The GLFW window must live on the main OS thread (see gpu-demo).
func init() { runtime.LockOSThread() }

// World rectangle mapped to the window. 16:9 matches the window so circles and
// squares aren't stretched.
const (
	worldW = 160.0
	worldH = 90.0
)

// uiState holds the small bit of cross-frame UI state the detail scenes share:
// the selected QPSK symbol and the pseudo-3D view angle, plus the last values
// applied so a scene only re-bakes its readout texture when something changed.
type uiState struct {
	sym     int // selected QPSK symbol index 0..3
	lastSym int // last symbol whose readout texture was baked (-1 = force refresh)

	// Receive-side state, shared by the Channel/Demod/Decode scenes.
	noiseStep     int     // channel noise level 0..maxNoiseStep
	lastNoiseStep int     // last noise level a readout was baked for (-1 = force refresh)
	rxI, rxQ      float64 // current received vector (ideal symbol + noise)
	nextSample    float64 // elapsed time at which to draw a new received vector

	msgIdx int // selected preset message (Message scene)

	qamI, qamQ int // selected 16-QAM grid position 0..3 (QAM16 scene)

	compose string // the user-typed message (Compose scene)
	bps     int    // Compose scene bits per symbol: 2 (QPSK), 3 (8-PSK), 4 (16-QAM)

	mfN    int // MultiFreq scene: number of subcarriers
	mfSeed int // MultiFreq scene: data seed (changes the per-carrier symbols)
}

func main() {
	a := app.New()
	a.AddPlugins(
		splititime.Plugin{TargetFrameRate: 0},
		webgpu.Plugin{
			Width: 1280, Height: 720,
			Title:      "spliti — radio",
			WorldW:     worldW,
			WorldH:     worldH,
			ClearColor: webgpu.Color{R: 0.04, G: 0.05, B: 0.08, A: 1},
			Smooth:     true,
			VSync:      true,
		},
	)

	app.InsertResource(a, &uiState{})

	app.InitState(a, initialScene())
	app.OnEnter(a, MainFlow, setupMainFlow)
	app.OnExit(a, MainFlow, teardownScene)
	app.OnEnter(a, Constellation, setupConstellation)
	app.OnExit(a, Constellation, teardownScene)
	app.OnEnter(a, IQWave, setupIQWave)
	app.OnExit(a, IQWave, teardownScene)
	app.OnEnter(a, Channel, setupChannel)
	app.OnExit(a, Channel, teardownScene)
	app.OnEnter(a, Demod, setupDemod)
	app.OnExit(a, Demod, teardownScene)
	app.OnEnter(a, Decode, setupDecode)
	app.OnExit(a, Decode, teardownScene)
	app.OnEnter(a, Message, setupMessage)
	app.OnExit(a, Message, teardownScene)
	app.OnEnter(a, QAM16, setupQAM16)
	app.OnExit(a, QAM16, teardownScene)
	app.OnEnter(a, Compose, setupCompose)
	app.OnExit(a, Compose, teardownScene)
	app.OnEnter(a, MultiFreq, setupMultiFreq)
	app.OnExit(a, MultiFreq, teardownScene)

	a.AddSystems(schedule.Startup, loadSharedTextures)

	// Global input: clicks navigate, Escape goes back/quits.
	a.AddSystems(schedule.Update, app.System(clickSystem), app.System(escapeSystem))

	// Per-scene logic, gated to its state.
	a.AddSystems(schedule.Update,
		app.System(mainFlowUpdate).RunIf(stateIs(MainFlow)),
		app.System(constellationInput).RunIf(stateIs(Constellation)),
		app.System(constellationDraw).RunIf(stateIs(Constellation)),
		app.System(iqwaveInput).RunIf(stateIs(IQWave)),
		app.System(iqwaveDraw).RunIf(stateIs(IQWave)),
		app.System(channelInput).RunIf(stateIs(Channel)),
		app.System(channelDraw).RunIf(stateIs(Channel)),
		app.System(demodInput).RunIf(stateIs(Demod)),
		app.System(demodDraw).RunIf(stateIs(Demod)),
		app.System(decodeInput).RunIf(stateIs(Decode)),
		app.System(decodeDraw).RunIf(stateIs(Decode)),
		app.System(messageInput).RunIf(stateIs(Message)),
		app.System(messageDraw).RunIf(stateIs(Message)),
		app.System(qam16Input).RunIf(stateIs(QAM16)),
		app.System(qam16Draw).RunIf(stateIs(QAM16)),
		app.System(composeInput).RunIf(stateIs(Compose)),
		app.System(composeDraw).RunIf(stateIs(Compose)),
		app.System(multiFreqInput).RunIf(stateIs(MultiFreq)),
		app.System(multiFreqDraw).RunIf(stateIs(MultiFreq)),
	)

	a.Run()
}

// initialScene lets the RADIO_SCENE env var open a specific scene on launch
// (handy when working on one stage); defaults to the main flow.
func initialScene() Scene {
	switch os.Getenv("RADIO_SCENE") {
	case "constellation":
		return Constellation
	case "iqwave":
		return IQWave
	case "channel":
		return Channel
	case "demod":
		return Demod
	case "decode":
		return Decode
	case "message":
		return Message
	case "qam16":
		return QAM16
	case "compose":
		return Compose
	case "multifreq":
		return MultiFreq
	default:
		return MainFlow
	}
}

// elapsed returns wall-clock seconds since start, used to animate waves and the
// travelling packet as pure functions of time (no fixed-step interpolation
// needed because every animated position is recomputed from t each frame).
func elapsed(c *app.Ctx) float64 {
	t := app.GetResource[splititime.Time](c)
	if t == nil {
		return 0
	}
	return t.Elapsed().Seconds()
}
