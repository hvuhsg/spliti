package app

// Plugin is the unit of engine composition. A plugin's Build method registers
// systems, resources, and other plugins on the App during the build phase.
type Plugin interface {
	Build(app *App)
}
