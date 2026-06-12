// Package undo holds the editor's command stack. Every editor mutation —
// transform drags, inspector edits, structural scene changes — is a Command
// whose Do and Undo update both the live world and the scene source model, so
// the two can never drift apart across an undo/redo cycle.
package undo

import "github.com/hvuhsg/spliti/app"

// Command is one undoable editor operation. Do is called when the command is
// pushed and again on redo; it must be idempotent with respect to the live
// edits the UI may have already applied (e.g. a gizmo drag applies its
// transform live before the command is pushed).
type Command interface {
	Name() string
	Do(c *app.Ctx) error
	Undo(c *app.Ctx) error
}

// Stack is a bounded undo/redo history.
type Stack struct {
	done   []Command
	undone []Command
	limit  int
}

// NewStack returns a stack keeping at most limit commands (0 = 256).
func NewStack(limit int) *Stack {
	if limit <= 0 {
		limit = 256
	}
	return &Stack{limit: limit}
}

// Push executes the command and records it; the redo history is discarded.
// A command that fails to execute is not recorded.
func (s *Stack) Push(c *app.Ctx, cmd Command) error {
	if err := cmd.Do(c); err != nil {
		return err
	}
	s.done = append(s.done, cmd)
	if len(s.done) > s.limit {
		s.done = s.done[1:]
	}
	s.undone = s.undone[:0]
	return nil
}

// Undo reverts the most recent command. A command that fails to undo is
// dropped (the stacks must never hold a command in an unknown state).
func (s *Stack) Undo(c *app.Ctx) (string, error) {
	if len(s.done) == 0 {
		return "", nil
	}
	cmd := s.done[len(s.done)-1]
	s.done = s.done[:len(s.done)-1]
	if err := cmd.Undo(c); err != nil {
		return cmd.Name(), err
	}
	s.undone = append(s.undone, cmd)
	return cmd.Name(), nil
}

// Redo re-executes the most recently undone command.
func (s *Stack) Redo(c *app.Ctx) (string, error) {
	if len(s.undone) == 0 {
		return "", nil
	}
	cmd := s.undone[len(s.undone)-1]
	s.undone = s.undone[:len(s.undone)-1]
	if err := cmd.Do(c); err != nil {
		return cmd.Name(), err
	}
	s.done = append(s.done, cmd)
	return cmd.Name(), nil
}

// Clear empties both histories (external file edits invalidate them).
func (s *Stack) Clear() { s.done, s.undone = nil, nil }

// CanUndo returns the name of the next command to undo.
func (s *Stack) CanUndo() (string, bool) {
	if len(s.done) == 0 {
		return "", false
	}
	return s.done[len(s.done)-1].Name(), true
}

// CanRedo returns the name of the next command to redo.
func (s *Stack) CanRedo() (string, bool) {
	if len(s.undone) == 0 {
		return "", false
	}
	return s.undone[len(s.undone)-1].Name(), true
}
