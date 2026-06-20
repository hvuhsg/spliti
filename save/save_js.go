//go:build js

package save

import (
	"errors"
	"sort"
	"strings"
	"syscall/js"
)

// lsBackend stores each slot as a localStorage entry keyed "<appID>/<slot>", so a
// game's saves persist across browser sessions and stay namespaced from other
// games served from the same origin.
type lsBackend struct {
	prefix  string // "<appID>/"
	storage js.Value
}

func openBackend(appID string) (backend, error) {
	storage := js.Global().Get("localStorage")
	if !storage.Truthy() {
		return nil, errors.New("save: localStorage is unavailable in this environment")
	}
	return &lsBackend{prefix: appID + "/", storage: storage}, nil
}

func (l *lsBackend) key(slot string) string { return l.prefix + slot }

func (l *lsBackend) read(slot string) ([]byte, error) {
	v := l.storage.Call("getItem", l.key(slot))
	if !v.Truthy() {
		return nil, ErrNotFound
	}
	return []byte(v.String()), nil
}

func (l *lsBackend) write(slot string, data []byte) error {
	// localStorage is synchronous and atomic per key; setItem can throw only when
	// the quota is exceeded, which surfaces as a JS exception → Go panic. Recover
	// it into an error so a full quota does not crash the game.
	return guard(func() { l.storage.Call("setItem", l.key(slot), string(data)) })
}

func (l *lsBackend) delete(slot string) error {
	return guard(func() { l.storage.Call("removeItem", l.key(slot)) })
}

func (l *lsBackend) has(slot string) bool {
	return l.storage.Call("getItem", l.key(slot)).Truthy()
}

func (l *lsBackend) list() ([]string, error) {
	var slots []string
	n := l.storage.Get("length").Int()
	for i := 0; i < n; i++ {
		k := l.storage.Call("key", i)
		if !k.Truthy() {
			continue
		}
		s := k.String()
		if strings.HasPrefix(s, l.prefix) {
			slots = append(slots, strings.TrimPrefix(s, l.prefix))
		}
	}
	sort.Strings(slots)
	return slots, nil
}

// guard runs fn, converting a thrown JS exception (delivered as a Go panic from
// syscall/js) into an error so storage failures (e.g. quota exceeded) are
// recoverable.
func guard(fn func()) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errFromRecover(r)
		}
	}()
	fn()
	return nil
}

func errFromRecover(r any) error {
	if e, ok := r.(error); ok {
		return e
	}
	return errors.New("save: localStorage operation failed")
}
