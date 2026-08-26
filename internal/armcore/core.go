package armcore

import (
	"context"
	"encoding/binary"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
)

const (
	defaultQuantum   = uint32(1000)
	defaultMaxSteps  = uint64(1_000_000)
	maxCallArguments = 64
)

type ThreadState uint8

const (
	ThreadReady ThreadState = iota
	ThreadRunning
	ThreadSuspended
	ThreadHalted
	ThreadFaulted
)

func (state ThreadState) String() string {
	switch state {
	case ThreadReady:
		return "ready"
	case ThreadRunning:
		return "running"
	case ThreadSuspended:
		return "suspended"
	case ThreadHalted:
		return "halted"
	case ThreadFaulted:
		return "faulted"
	default:
		return fmt.Sprintf("thread-state-%d", state)
	}
}

type CoreOptions struct {
	Quantum  uint32
	MaxSteps uint64
}

type Core struct {
	debug debugState

	memory   *Memory
	engine   Engine
	quantum  uint32
	maxSteps uint64
	execute  sync.Mutex
	steps    atomic.Uint64

	// profile is nil unless sampling was switched on, so the cost on an
	// unprofiled run is one atomic-free read guarded by profileMu per quantum.
	profileMu sync.RWMutex
	profile   *profiler
}

func NewCore(options CoreOptions) *Core {
	if options.Quantum == 0 {
		options.Quantum = defaultQuantum
	}
	if options.MaxSteps == 0 {
		options.MaxSteps = defaultMaxSteps
	}
	return &Core{
		memory:   NewMemory(),
		quantum:  options.Quantum,
		maxSteps: options.MaxSteps,
	}
}

// SetFastSupervisorCall installs a handler for supervisor calls that can be
// answered inside the quantum. See FastSupervisorCall for the contract it runs
// under. It must be set before the core runs; nil removes it.
func (core *Core) SetFastSupervisorCall(handler FastSupervisorCall) {
	core.memory.fastSupervisor = handler
}

func (core *Core) Memory() *Memory {
	return core.memory
}

func (core *Core) Steps() uint64 {
	return core.steps.Load()
}

// MaxSteps reports the step ceiling one run of a thread without its own
// budget receives. A limit hook that renews windows uses it to account for
// how much execution each renewal grants.
func (core *Core) MaxSteps() uint64 {
	return core.maxSteps
}

type Thread struct {
	mu          sync.Mutex
	context     Context
	state       ThreadState
	threadLocal *threadLocalState
	stepBudget  uint64
	limitHook   func(context.Context) error
	// entryStack is the stack pointer this thread's run started from. A
	// derived call inherits the guest stack rather than getting one of its
	// own, so this is the only way to tell a frame that belongs to the run
	// from one that belongs to a caller further out.
	entryStack uint32
	entryKnown bool
}

func NewThread(initial Context) *Thread {
	return &Thread{context: initial, state: ThreadReady, threadLocal: newThreadLocalState()}
}

// SetStepBudget overrides the core step ceiling for runs of this thread and
// for calls derived from it. Zero keeps the core ceiling. It must be set
// before the thread runs.
func (thread *Thread) SetStepBudget(budget uint64) {
	thread.stepBudget = budget
}

// SetLimitHook installs a callback invoked when a run of this thread (or a
// call derived from it) reaches its step ceiling. Returning nil grants a
// fresh budget window and execution resumes; the hook may block, which parks
// the goroutine mid-run with the guest context saved. Returning an error
// stops the run with that error. Without a hook the run fails with
// ErrStepLimit. It must be set before the thread runs.
func (thread *Thread) SetLimitHook(hook func(context.Context) error) {
	thread.limitHook = hook
}

// EntryStackPointer reports the stack pointer a derived call started from, and
// whether this thread is such a call. Guest frames at or below it belong to
// this run; anything above it belongs to a caller the Host is still inside, so
// a long jump that lands there cannot be resumed here.
func (thread *Thread) EntryStackPointer() (uint32, bool) {
	thread.mu.Lock()
	defer thread.mu.Unlock()
	return thread.entryStack, thread.entryKnown
}

func (thread *Thread) State() ThreadState {
	thread.mu.Lock()
	defer thread.mu.Unlock()
	return thread.state
}

func (thread *Thread) Context() Context {
	thread.mu.Lock()
	defer thread.mu.Unlock()
	return thread.context
}

// SetRegister is available before a run and while a supervisor-call handler
// has the thread suspended. Running state cannot be mutated concurrently.
func (thread *Thread) SetRegister(index int, value uint32) error {
	thread.mu.Lock()
	defer thread.mu.Unlock()
	if thread.state != ThreadReady && thread.state != ThreadSuspended {
		return fmt.Errorf("set r%d while thread is %s: %w", index, thread.state, ErrThreadState)
	}
	return thread.context.SetRegister(index, value)
}

func (thread *Thread) Register(index int) (uint32, error) {
	thread.mu.Lock()
	defer thread.mu.Unlock()
	return thread.context.Register(index)
}

// SetContext replaces the whole guest-visible state, in the same states
// SetRegister is available in. **A long jump is not fifteen register writes**:
// it restores the condition flags and the instruction set the saved point ran
// in as well, and a handler that reconstructs a context register by register
// leaves those behind.
func (thread *Thread) SetContext(saved Context) error {
	thread.mu.Lock()
	defer thread.mu.Unlock()
	if thread.state != ThreadReady && thread.state != ThreadSuspended {
		return fmt.Errorf("set the context while thread is %s: %w", thread.state, ErrThreadState)
	}
	thread.context = saved
	return nil
}

// RegisterThreadLocalWord makes one mapped read/write word private to each
// logical ARM Thread. It is used for guest runtime globals that are actually
// thread state, such as KTF's current Java exception-handler pointer.
func (core *Core) RegisterThreadLocalWord(address uint32) error {
	if core == nil || core.memory == nil {
		return fmt.Errorf("ARM core is nil")
	}
	return core.memory.registerThreadLocalWord(address)
}

func (core *Core) ThreadLocalWord(thread *Thread, address uint32) (uint32, error) {
	if core == nil || core.memory == nil {
		return 0, fmt.Errorf("ARM core is nil")
	}
	if thread == nil {
		return 0, fmt.Errorf("ARM thread is nil")
	}
	return core.memory.threadLocalWord(thread.threadLocal, address)
}

// ThreadLocalWords returns one thread's value for every registered
// thread-local word. A scanner walking guest memory would otherwise miss
// them: their per-thread values live outside the memory image.
func (core *Core) ThreadLocalWords(thread *Thread) []uint32 {
	if core == nil || core.memory == nil || thread == nil {
		return nil
	}
	return core.memory.threadLocalWords(thread.threadLocal)
}

func (core *Core) SetThreadLocalWord(thread *Thread, address, value uint32) error {
	if core == nil || core.memory == nil {
		return fmt.Errorf("ARM core is nil")
	}
	if thread == nil {
		return fmt.Errorf("ARM thread is nil")
	}
	return core.memory.setThreadLocalWord(thread.threadLocal, address, value)
}

type SupervisorCallHandler func(context.Context, *Thread, SupervisorCall) error

type RunSummary struct {
	Steps   uint64
	Context Context
}

// Call derives a temporary function context from parent, applies the ARM
// procedure-call argument layout, and restores the parent by leaving it
// untouched. A suspended SVC handler may use Call for a nested guest/JVM bridge
// invocation without losing the outer register context.
func (core *Core) Call(
	ctx context.Context,
	parent *Thread,
	address uint32,
	end uint32,
	arguments []uint32,
	handler SupervisorCallHandler,
) (RunSummary, error) {
	if parent == nil {
		return RunSummary{}, fmt.Errorf("parent ARM thread is nil")
	}
	if len(arguments) > maxCallArguments {
		return RunSummary{}, fmt.Errorf("%w (limit %d)", ErrCallArgumentLimit, maxCallArguments)
	}
	callContext, derived, err := parent.contextForCall()
	if err != nil {
		return RunSummary{}, err
	}
	for index := 0; index < len(arguments) && index < 4; index++ {
		callContext.Registers[index] = arguments[index]
	}
	if len(arguments) > 4 {
		stackBytes := uint32(len(arguments)-4) * 4
		stackPointer := callContext.Registers[RegisterSP]
		if stackPointer < stackBytes {
			return RunSummary{}, &AccessError{Operation: "write call arguments", Address: stackPointer, Size: uint64(stackBytes), Cause: ErrAddressOverflow}
		}
		stackPointer -= stackBytes
		stackData := make([]byte, stackBytes)
		for index, argument := range arguments[4:] {
			binary.LittleEndian.PutUint32(stackData[index*4:], argument)
		}
		if err := core.memory.Write(stackPointer, stackData); err != nil {
			return RunSummary{}, fmt.Errorf("write call arguments: %w", err)
		}
		callContext.Registers[RegisterSP] = stackPointer
	}
	if err := callContext.SetPC(address); err != nil {
		return RunSummary{}, err
	}
	callContext.Registers[RegisterLR] = end
	derived.context = callContext
	derived.entryStack = callContext.Registers[RegisterSP]
	derived.entryKnown = true
	return core.Run(ctx, derived, end, handler)
}

// Run executes one cooperative guest thread until its PC reaches end. A
// bounded quantum releases execution to other goroutines. Supervisor-call
// handlers run without the execution lock, so they may wait for a browser or
// native Host event while another guest thread makes progress.
func (core *Core) Run(ctx context.Context, thread *Thread, end uint32, handler SupervisorCallHandler) (RunSummary, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if thread == nil {
		return RunSummary{}, fmt.Errorf("ARM thread is nil")
	}

	local, err := thread.begin()
	if err != nil {
		return RunSummary{}, err
	}
	var executed uint64
	// Why a run failed travels back with it rather than being kept on the
	// thread: the caller is the only one who ever asked.
	fail := func(cause error) (RunSummary, error) {
		thread.fault(local)
		return RunSummary{Steps: executed, Context: local}, cause
	}

	budget := core.maxSteps
	if thread.stepBudget != 0 {
		budget = thread.stepBudget
	}
	// window counts the steps of the current budget window; executed keeps
	// the run total across hook-granted windows for the summary.
	var window uint64
	for {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		if window >= budget {
			if thread.limitHook == nil {
				return fail(fmt.Errorf("%w (limit %d)", ErrStepLimit, budget))
			}
			// Save the running context so the parked thread is observable,
			// then ask the hook for a fresh budget window. The hook may block.
			thread.saveRunning(local)
			if err := thread.limitHook(ctx); err != nil {
				return fail(err)
			}
			window = 0
		}
		count := uint64(core.quantum)
		if remaining := budget - window; count > remaining {
			count = remaining
		}
		profile := core.currentProfiler()
		if profile != nil {
			count = profile.chunkForSample(count)
		}
		// A debugger has to stop exactly at a breakpoint, and the only way to
		// do that without a check in the interpreter's inner loop — where it
		// would cost every session that is not being debugged — is to run one
		// instruction at a time while one is attached.
		if core.debugAttached() {
			count = 1
			if hook, stop, halt := core.debugCheck(local.PC()); halt {
				// Suspend rather than save: a debugger writes registers at a
				// stop, and only a suspended thread accepts that — the same
				// state a supervisor-call handler works in.
				thread.suspend(local)
				if err := hook(ctx, core, thread, stop); err != nil {
					local = thread.Context()
					return fail(err)
				}
				local, err = thread.resume()
				if err != nil {
					return fail(err)
				}
			}
		}

		core.execute.Lock()
		core.memory.activateThreadLocal(thread.threadLocal)
		result, runErr := core.engine.Run(&local, core.memory, end, uint32(count))
		if profile != nil {
			core.sampleProfile(profile, &local, uint64(result.Steps))
		}
		core.memory.activateThreadLocal(nil)
		core.execute.Unlock()
		window += uint64(result.Steps)
		executed += uint64(result.Steps)
		core.steps.Add(uint64(result.Steps))
		if runErr != nil {
			return fail(runErr)
		}

		switch result.Reason {
		case StopEnd:
			thread.halt(local)
			return RunSummary{Steps: executed, Context: local}, nil
		case StopCountExhausted:
			thread.saveRunning(local)
			runtime.Gosched()
		case StopSupervisorCall:
			thread.suspend(local)
			if handler == nil {
				return fail(fmt.Errorf("%w %#x at %#x", ErrUnhandledSupervisorCall, result.SupervisorCall.Immediate, result.SupervisorCall.Address))
			}
			if err := handler(ctx, thread, result.SupervisorCall); err != nil {
				local = thread.Context()
				return fail(fmt.Errorf("handle supervisor call %#x at %#x: %w", result.SupervisorCall.Immediate, result.SupervisorCall.Address, err))
			}
			local, err = thread.resume()
			if err != nil {
				return fail(err)
			}
		default:
			return fail(fmt.Errorf("unknown ARM stop reason %d", result.Reason))
		}
	}
}

func (thread *Thread) begin() (Context, error) {
	thread.mu.Lock()
	defer thread.mu.Unlock()
	if thread.state != ThreadReady {
		return Context{}, fmt.Errorf("run thread while it is %s: %w", thread.state, ErrThreadState)
	}
	thread.state = ThreadRunning
	return thread.context, nil
}

// contextForCall snapshots the parent's context and derives a thread for one
// nested call. The derived thread shares the parent's thread-local state and
// inherits its step budget and limit hook, so a parked worker parks at any
// nesting depth.
func (thread *Thread) contextForCall() (Context, *Thread, error) {
	thread.mu.Lock()
	defer thread.mu.Unlock()
	if thread.state != ThreadReady && thread.state != ThreadSuspended {
		return Context{}, nil, fmt.Errorf("call from thread while it is %s: %w", thread.state, ErrThreadState)
	}
	derived := &Thread{
		state:       ThreadReady,
		threadLocal: thread.threadLocal,
		stepBudget:  thread.stepBudget,
		limitHook:   thread.limitHook,
	}
	return thread.context, derived, nil
}

func (thread *Thread) saveRunning(context Context) {
	thread.mu.Lock()
	thread.context = context
	thread.mu.Unlock()
}

func (thread *Thread) suspend(context Context) {
	thread.mu.Lock()
	thread.context = context
	thread.state = ThreadSuspended
	thread.mu.Unlock()
}

func (thread *Thread) resume() (Context, error) {
	thread.mu.Lock()
	defer thread.mu.Unlock()
	if thread.state != ThreadSuspended {
		return Context{}, fmt.Errorf("resume thread while it is %s: %w", thread.state, ErrThreadState)
	}
	thread.state = ThreadRunning
	return thread.context, nil
}

func (thread *Thread) halt(context Context) {
	thread.mu.Lock()
	thread.context = context
	thread.state = ThreadHalted
	thread.mu.Unlock()
}

func (thread *Thread) fault(context Context) {
	thread.mu.Lock()
	thread.context = context
	thread.state = ThreadFaulted
	thread.mu.Unlock()
}
