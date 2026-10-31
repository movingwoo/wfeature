package clet

import (
	"context"
	"fmt"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
)

// A Clet runs on the WIPI-C engine the KTF C titles run on
// (internal/platform/wipicrt): an SKT executable calls almost the same
// functions under almost the same numbers. What is a Clet's own is here and
// in the rest of this package: its root table and C library, its six entry
// points, its checkpoint codec, and the platform answers below.

// cletPlatform is what the engine is told about this platform. On SKT a
// memory identifier's first word points at the data itself, twelve bytes on,
// so MC_GETDPTR is that word and nothing more.
var cletPlatform = wipicrt.Platform{
	Name:            "SKT Clet",
	CName:           "SKT WIPI C",
	CallName:        "SKT Clet",
	ExitError:       ErrGuestExited,
	HandleFirstWord: wipicrt.WIPICAllocationOverhead,
	HandleData:      0,
}

// ImageBase is where a Clet's code is linked and loaded.
const ImageBase = wipicrt.ImageBase

// Diagnostics is what a Clet session reports of the boundaries it crossed.
type Diagnostics = wipicrt.Diagnostics

// Client is one Clet's client: the engine's, with what only a Clet has.
type Client struct {
	*wipicrt.Client
	authentication backend.AuthenticationStatus
	// image is the code alone: it is what a pixel operation's function
	// pointer has to lie in, what the cheat panel labels as the client, and
	// what a checkpoint is bound to.
	image ClientImage
	// clet is the Clet state LoadCletClient built; see clet.go.
	clet *cletState

	initializationStarted bool
	workersStopped        bool
	// runtime and prepared are this package's view of the engine's runtimes,
	// kept with them by setRuntimes.
	runtime  *initializationRuntime
	prepared *initializationRuntime
}

// initializationRuntime is the engine's runtime with the Clet's own calls on
// it: the root table, the C library and the checkpoint codec.
type initializationRuntime struct {
	*wipicrt.Runtime
	client *Client
}

// setRuntimes records the running and the prepared runtime, here and in the
// engine, which reads both.
func (client *Client) setRuntimes(runtime, prepared *initializationRuntime) {
	client.runtime, client.prepared = runtime, prepared
	client.Client.Runtime, client.Client.Prepared = runtime.engine(), prepared.engine()
}

func (runtime *initializationRuntime) engine() *wipicrt.Runtime {
	if runtime == nil {
		return nil
	}
	return runtime.Runtime
}

// newInitializationRuntime builds the engine's runtime for a Clet client.
// Nothing of a Java startup applies — there is no VM, no class context and no
// interface to initialise.
func newInitializationRuntime(client *Client) (*initializationRuntime, error) {
	runtime := &initializationRuntime{client: client}
	engine, err := wipicrt.New(client.Client, runtime.hooks())
	if err != nil {
		return nil, err
	}
	runtime.Runtime = engine
	return runtime, nil
}

// hooks are the Clet's answers to what the engine does not own. A Clet has no
// Java side, so all it supplies is its supervisor call handler.
func (runtime *initializationRuntime) hooks() wipicrt.RuntimeHooks {
	return wipicrt.RuntimeHooks{Supervisor: runtime.handleSupervisorCall}
}

// clientHooks are the Clet's part of a service round: the events the program
// posted and the repaint it asked for are due at once, because the next round
// is what delivers them.
func (client *Client) clientHooks() wipicrt.ClientHooks {
	return wipicrt.ClientHooks{Deadlines: func(consider func(time.Time)) {
		if engine := client.Client.Runtime; engine != nil && (len(engine.Events) > 0 || engine.RepaintPending) {
			consider(time.Time{})
		}
	}}
}

const (
	// svcCategoryClet is the immediate the root table's veneers carry, and
	// svcSemihosting the one semihosting call a title's C runtime makes.
	svcCategoryClet uint32 = 10
	svcSemihosting  uint32 = 0xab
)

// handleSupervisorCall answers the supervisor calls a Clet makes: the root
// table's veneers (svcCategoryClet) and the one semihosting immediate a title's
// C runtime carries. A Clet makes no other kind.
func (runtime *initializationRuntime) handleSupervisorCall(ctx context.Context, thread *armcore.Thread, call armcore.SupervisorCall) error {
	id, err := thread.Register(12)
	if err != nil {
		return err
	}
	previousThread, previousContext := runtime.CurrentThread, runtime.CurrentContext
	runtime.CurrentThread, runtime.CurrentContext = thread, ctx
	defer func() {
		runtime.CurrentThread, runtime.CurrentContext = previousThread, previousContext
	}()
	var result uint32
	switch call.Immediate {
	case svcCategoryClet:
		result, err = runtime.handleCletCall(thread, id)
	case svcSemihosting:
		result, err = runtime.handleCletSemihosting(thread)
	default:
		err = fmt.Errorf("unknown SKT Clet SVC category %d with id %#x", call.Immediate, id)
	}
	if err != nil {
		return err
	}
	return thread.SetRegister(0, result)
}
