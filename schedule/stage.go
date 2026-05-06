// Package schedule defines the named execution stages of a spliti App.
//
// Stages mirror Bevy's frame schedule. Startup stages run once before the loop;
// Main stages run every frame in order.
package schedule

type Stage string

const (
	PreStartup  Stage = "PreStartup"
	Startup     Stage = "Startup"
	PostStartup Stage = "PostStartup"

	First           Stage = "First"
	PreUpdate       Stage = "PreUpdate"
	StateTransition Stage = "StateTransition"
	Update          Stage = "Update"
	PostUpdate      Stage = "PostUpdate"
	Last            Stage = "Last"

	// FixedUpdate runs zero or more times between PreUpdate-group and Update.
	// Driven by the time plugin's accumulator.
	FixedFirst  Stage = "FixedFirst"
	FixedUpdate Stage = "FixedUpdate"
	FixedLast   Stage = "FixedLast"
)

// StartupStages is the in-order list of stages executed once at App.Run.
var StartupStages = []Stage{PreStartup, Startup, PostStartup}

// MainStages is the in-order list of stages executed every frame, in order.
// FixedUpdate stages are run by the App between StateTransition and Update via
// a plugin-installed hook, not as part of this list.
var MainStages = []Stage{First, PreUpdate, StateTransition, Update, PostUpdate, Last}
