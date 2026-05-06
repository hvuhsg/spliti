package app

// Events is the typed buffer for messages of type T. One per (App, T).
// Sent events are readable for the remainder of the frame and cleared at the
// end of Last, before the next frame begins.
type Events[T any] struct {
	buf []T
}

// Send appends an event to the current frame's buffer.
func (e *Events[T]) Send(v T) { e.buf = append(e.buf, v) }

// Drain returns and clears the current buffer. Used by the App between frames.
func (e *Events[T]) drain() { e.buf = e.buf[:0] }

// Iter returns a snapshot of the current frame's events. Safe to range over.
func (e *Events[T]) Iter() []T { return e.buf }

type eventsDrainer interface{ drain() }

// SendEvent is a top-level helper that auto-registers the Events[T] resource.
func SendEvent[T any](ctx *Ctx, v T) {
	eventsResource[T](ctx.app).Send(v)
}

// ReadEvents returns a snapshot of the current-frame events of type T.
func ReadEvents[T any](ctx *Ctx) []T {
	return eventsResource[T](ctx.app).Iter()
}

// ClearEvents drops the current buffer of events of type T immediately.
//
// The engine already drains every Events[T] buffer at the end of each frame
// (after the Last stage). ClearEvents is for callers that need to drop the
// buffer between sub-stages of the same frame — most importantly, plugins
// that produce events at FixedFirst and need to discard the previous fixed
// iteration's events before producing this iteration's.
func ClearEvents[T any](ctx *Ctx) {
	eventsResource[T](ctx.app).drain()
}

func eventsResource[T any](a *App) *Events[T] {
	k := typeKey[Events[T]]()
	if v, ok := a.resources[k]; ok {
		return v.(*Events[T])
	}
	e := &Events[T]{}
	a.resources[k] = e
	a.eventsDrainers = append(a.eventsDrainers, e)
	return e
}

// drainAllEvents resets every registered Events[T] buffer.
func (a *App) drainAllEvents() {
	for _, d := range a.eventsDrainers {
		d.drain()
	}
}
