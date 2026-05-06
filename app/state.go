package app

import "reflect"

// stateMachine is the type-erased internal representation of a state of type T.
// User-facing access goes through State[T], InitState[T], OnEnter/OnExit.
type stateMachine struct {
	typeKey reflect.Type
	current any
	next    any // non-nil = pending transition queued
	onEnter map[any][]SystemFunc
	onExit  map[any][]SystemFunc
}

// State is the user-facing handle to a state machine of type T.
type State[T comparable] struct{ m *stateMachine }

// Get returns the current state value.
func (s *State[T]) Get() T { return s.m.current.(T) }

// Set queues a transition. The transition runs at the next StateTransition
// stage, firing OnExit(prev) followed by OnEnter(next).
func (s *State[T]) Set(v T) { s.m.next = v }

// InitState registers a state machine of type T with the given initial value.
// OnEnter systems for the initial value run during PostStartup.
func InitState[T comparable](a *App, initial T) {
	k := typeKey[T]()
	if _, exists := a.stateByType[k]; exists {
		panic("state already initialized: " + k.String())
	}
	sm := &stateMachine{
		typeKey: k,
		current: initial,
		onEnter: map[any][]SystemFunc{},
		onExit:  map[any][]SystemFunc{},
	}
	a.stateByType[k] = sm
	a.stateMachines = append(a.stateMachines, sm)
}

// GetState returns the State[T] handle for an initialized state machine.
// Panics if InitState[T] has not been called.
func GetState[T comparable](ctx *Ctx) *State[T] {
	sm, ok := ctx.app.stateByType[typeKey[T]()]
	if !ok {
		panic("state not initialized: " + typeKey[T]().String())
	}
	return &State[T]{m: sm}
}

// OnEnter registers a system to run when the state machine of type T enters
// the given value.
func OnEnter[T comparable](a *App, value T, sys SystemFunc) {
	sm := requireMachine[T](a)
	sm.onEnter[value] = append(sm.onEnter[value], sys)
}

// OnExit registers a system to run when the state machine of type T leaves
// the given value.
func OnExit[T comparable](a *App, value T, sys SystemFunc) {
	sm := requireMachine[T](a)
	sm.onExit[value] = append(sm.onExit[value], sys)
}

func requireMachine[T comparable](a *App) *stateMachine {
	sm, ok := a.stateByType[typeKey[T]()]
	if !ok {
		panic("OnEnter/OnExit: state not initialized: " + typeKey[T]().String())
	}
	return sm
}

// applyStateTransitions runs queued transitions in registration order.
func (a *App) applyStateTransitions() {
	for _, sm := range a.stateMachines {
		if sm.next == nil {
			continue
		}
		for _, sys := range sm.onExit[sm.current] {
			sys(a.ctx)
		}
		sm.current = sm.next
		sm.next = nil
		for _, sys := range sm.onEnter[sm.current] {
			sys(a.ctx)
		}
	}
}

// fireInitialOnEnter runs OnEnter systems for the current value of every
// machine. Called once at end of startup before the main loop begins.
func (a *App) fireInitialOnEnter() {
	for _, sm := range a.stateMachines {
		for _, sys := range sm.onEnter[sm.current] {
			sys(a.ctx)
		}
	}
}
