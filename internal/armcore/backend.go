package armcore

import (
	"slices"
	"sync"
)

// The seam a second execution strategy would arrive through.
//
// Until now Engine was not an implementation of anything — Core held one by its
// concrete type and called it, so there was no place to put a second strategy
// and, more to the point, no place to stand a second one next to the first and
// ask whether they agree. That is what this is for. The interface is small on
// purpose: everything a strategy has to be able to do is run a bounded number
// of instructions from a context against a memory and say why it stopped.
//
// Three rules come with it.
//
// The interpreter is the oracle and stays. Whatever else arrives, the
// instruction semantics this package is trusted for are the ones in engine.go,
// arm.go and thumb.go, and the conformance corpus under conformance/ is what a
// newcomer has to match before anything runs on it.
//
// A backend is a black box only between synchronisation points. At the point it
// returns — a supervisor call, a spent budget, a fault, the end address — all
// seventeen guest-visible words and every byte of mapped memory have to be
// exactly what the interpreter would have left, and so does RunResult. Steps is
// part of that contract rather than a diagnostic: it is the unit a Host paces
// frames on and detects a runaway guest with, so a strategy that retires ten
// instructions and reports nine moves the guest's own clock.
//
// Nothing outside this package may assert a Backend's concrete type. A runtime
// that reaches through the seam for something only the interpreter has is a
// runtime the second strategy cannot run.
type Backend interface {
	// Run executes at most count instructions, starting from the context's PC,
	// and stops early at end, at a supervisor call the memory's fast handler
	// declines, or on a fault. It answers how it stopped and how many
	// instructions it retired.
	Run(context *Context, memory *Memory, end uint32, count uint32) (RunResult, error)
	// Name identifies the strategy in diagnostics and in the conformance
	// corpus's failure messages.
	Name() string
}

// InterpreterBackend is the name the precise interpreter is registered under.
const InterpreterBackend = "interpreter"

// Name identifies the interpreter. Engine is the Backend this package ships,
// and the one every other one is measured against.
func (Engine) Name() string { return InterpreterBackend }

var (
	backendsMutex sync.RWMutex
	backends      = map[string]func() Backend{
		InterpreterBackend: func() Backend { return Engine{} },
	}
)

// RegisterBackend adds an execution strategy under a name, replacing any
// previous registration of that name. It is a process-wide registry so that a
// strategy which only builds on some targets can register itself from a
// build-tagged file, and a Host can then select it by name without importing
// anything that does not exist elsewhere.
func RegisterBackend(name string, factory func() Backend) {
	if name == "" || factory == nil {
		return
	}
	backendsMutex.Lock()
	defer backendsMutex.Unlock()
	backends[name] = factory
}

// BackendNames lists the registered strategies in sorted order.
func BackendNames() []string {
	backendsMutex.RLock()
	defer backendsMutex.RUnlock()
	names := make([]string, 0, len(backends))
	for name := range backends {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// NewBackend builds one registered strategy, and reports whether the name was
// one. A caller that wants the fallback rather than the answer should use
// CoreOptions.BackendName instead.
func NewBackend(name string) (Backend, bool) {
	backendsMutex.RLock()
	factory, ok := backends[name]
	backendsMutex.RUnlock()
	if !ok {
		return nil, false
	}
	return factory(), true
}

// backendFor resolves what a Core should run through. An explicit Backend wins,
// then a registered name, and an unregistered name falls back to the
// interpreter rather than failing: a build that does not carry the strategy a
// configuration asks for still has to run the game.
func backendFor(options CoreOptions) Backend {
	if options.Backend != nil {
		return options.Backend
	}
	if options.BackendName != "" {
		if backend, ok := NewBackend(options.BackendName); ok {
			return backend
		}
	}
	return Engine{}
}
