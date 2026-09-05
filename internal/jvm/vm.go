package jvm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/movingwoo/wfeature/internal/jvm/classfile"
)

var (
	ErrStepLimit  = errors.New("JVM instruction limit exceeded")
	ErrFrameLimit = errors.New("JVM frame limit exceeded")
)

type NativeMethod func(vm *VM, arguments []Value) (Value, error)

// AOTInvoker executes a platform-owned AOT method when interpreted bytecode
// dispatches into a class that exists only as registered AOT metadata. The
// receiver is the first argument for instance methods.
type AOTInvoker func(className, name, descriptor string, arguments []Value) (Value, error)

// SetAOTInvoker installs the platform bridge used when interpreted code calls
// into AOT-only classes.
func (vm *VM) SetAOTInvoker(invoker AOTInvoker) {
	vm.mu.Lock()
	vm.aotInvoker = invoker
	vm.mu.Unlock()
}

// invokeAOTFallback delegates a failed bytecode resolution to the platform
// AOT bridge when the lookup class is registered AOT metadata.
func (vm *VM) invokeAOTFallback(className, name, descriptor string, arguments []Value) (Value, bool, error) {
	vm.mu.RLock()
	invoker := vm.aotInvoker
	vm.mu.RUnlock()
	if invoker == nil {
		return VoidValue(), false, nil
	}
	if _, ok := vm.AOTClass(className); !ok {
		return VoidValue(), false, nil
	}
	result, err := invoker(className, name, descriptor, arguments)
	return result, true, err
}

type Options struct {
	MaxSteps       uint64
	MaxFrames      int
	MaxArrayLength int
	Logger         *slog.Logger
	// TraceInstructions writes one log line per bytecode instruction. It is a
	// tool rather than a log level, and it is off unless a Host asks for it:
	// the line is on the hottest path there is, and a title that runs for a
	// second writes millions of them, which costs more than the emulation and
	// moves every timing the trace was opened to look at. The other execution
	// core has no equivalent at all — its trace is `runlgt -trace N`, bounded
	// by a count — and this is the same bargain.
	TraceInstructions bool
	Clock             func() int64
	// Speed is how fast the guest's time runs against the wall, asked each
	// time a wait is taken so a Host can change it while a game is running.
	// Nil is the speed the game was written for. It scales what a guest wait
	// costs — Thread.sleep, a monitor's timed wait — and a platform that
	// installs it should scale Clock by the same factor, because a game that
	// sleeps on one clock and measures on another measures nonsense.
	Speed       func() float64
	AsyncError  func(error)
	ThreadYield func() error
	// ByteDecoder converts platform byte content to text for the String byte
	// constructors and ByteEncoder is its String.getBytes inverse. Platforms
	// with a non-UTF-8 default charset, such as KTF's EUC-KR, install both;
	// the default keeps UTF-8 with replacement characters.
	ByteDecoder func([]byte) string
	ByteEncoder func(string) []byte
	// GuestThreadStarter replaces the goroutine-backed Thread.start. Platforms
	// whose guest threads share one cooperative execution core queue the
	// thread object for their own service loop instead.
	GuestThreadStarter func(thread *Object) error
	// RenewSteps is asked whether an execution that has spent MaxSteps may have
	// another window. Without it MaxSteps is a ceiling on one execution, which
	// is the right answer for a Host call that should not run away and the
	// wrong one for a game's own thread: that thread is the game, and it spends
	// instructions for as long as the title is running. A platform that installs
	// this makes MaxSteps a window and keeps the stop condition its own — its
	// runtime being torn down — which is the only condition a spinning guest can
	// be stopped by. Returning an error ends the execution with that error.
	RenewSteps func() error
	// Exit is what System.exit(status) does. A MIDlet is not supposed to call
	// it — notifyDestroyed is the lifecycle's own way out — but titles of this
	// era ship it on the path out of an error dialog, and with no hook the
	// call ends the session as a failed method rather than as the shutdown the
	// title asked for. A platform installs the same teardown its destroy path
	// uses.
	Exit func(status int32) error
}

// decodePlatformBytes converts guest byte content to text using the
// platform's default charset when one is installed.
func (vm *VM) decodePlatformBytes(data []byte) string {
	if vm != nil && vm.config.ByteDecoder != nil {
		return vm.config.ByteDecoder(append([]byte(nil), data...))
	}
	return strings.ToValidUTF8(string(data), "�")
}

// encodePlatformString is the String.getBytes inverse of decodePlatformBytes.
func (vm *VM) encodePlatformString(value string) []byte {
	if vm != nil && vm.config.ByteEncoder != nil {
		return vm.config.ByteEncoder(value)
	}
	return []byte(value)
}

type contextNativeMethod func(vm *VM, state *execution, arguments []Value) (Value, error)

type methodKey struct {
	class      string
	name       string
	descriptor string
}

type fieldKey struct {
	class      string
	name       string
	descriptor string
}

type VM struct {
	// storeObserver is shown every guest store to a field, a static or an
	// array element. It is an atomic pointer because a Host installs it
	// between ticks while guest threads are the ones that fire it, and it is
	// nil whenever nobody is watching so a title that is not being
	// investigated pays one nil check per store and nothing else.
	storeObserver atomic.Pointer[func(StoreEvent)]

	loader *Loader
	config Options
	// traceInstructions is the answer to "is anyone listening to the per
	// instruction trace", settled once at construction because the logger and
	// its level are settled once too. See Options.TraceInstructions.
	traceInstructions bool
	aotMu             sync.RWMutex

	mu             sync.RWMutex
	natives        map[methodKey]NativeMethod
	contextNatives map[methodKey]contextNativeMethod
	// builtinNatives marks the entries of natives that came from the
	// runtime's own implementations. A platform may replace one of those —
	// KTF answers Class.getName from its guest class records — while two
	// platform registrations of the same method stay an error, because that
	// is a mistake rather than an override.
	builtinNatives map[methodKey]bool
	// definedConstants holds the static finals of classes declared in Go,
	// which have no constant pool for the class initializer to read them from.
	definedConstants map[string][]definedConstant
	statics          map[fieldKey]Value
	// declaringFields caches field resolution: which class in a reference's
	// chain actually declares the field it names. It is cleared whenever a
	// class is defined, because a class that arrives later can be the answer.
	declaringFields map[fieldKey]fieldResolution
	classMonitors   map[string]*monitor
	nextExecution   atomic.Uint64
	nextObject      atomic.Uint32
	arraycopyMu     sync.Mutex
	threadMu        sync.Mutex
	threads         map[*Object]*guestThread
	mainThread      *Object
	aotClasses      map[string]AOTClassMetadata
	aotAddresses    map[uint32]string
	aotObjects      map[uint32]aotBinding
	aotInvoker      AOTInvoker

	initMu       sync.Mutex
	initCond     *sync.Cond
	initializing map[string]bool
	initialized  map[string]bool
	initErrors   map[string]error

	// toStringDepth bounds how deeply objectText may re-enter guest code. A
	// title's toString may itself name another object, so one call can nest;
	// a class whose toString names itself would nest until the Go stack ran
	// out, which is a guest archive deciding how much host stack to use.
	toStringDepth atomic.Int32
}

type execution struct {
	steps        uint64
	frames       int
	id           uint64
	initializing map[string]bool
	thread       *Object
	// framePool is this execution's finished frames, waiting to be lent to the
	// next call it makes. One execution is one guest thread, so nothing here
	// needs a lock. See newFrame.
	framePool []*frame
}

type ExecutionError struct {
	Class      string
	Method     string
	Descriptor string
	PC         int
	Opcode     byte
	Cause      error
}

func (e *ExecutionError) Error() string {
	return fmt.Sprintf("execute %s.%s%s at pc %d (opcode 0x%02x): %v", e.Class, e.Method, e.Descriptor, e.PC, e.Opcode, e.Cause)
}

func (e *ExecutionError) Unwrap() error {
	return e.Cause
}

func New(source ClassSource, options Options) *VM {
	if options.MaxSteps == 0 {
		options.MaxSteps = 1_000_000
	}
	if options.MaxFrames == 0 {
		options.MaxFrames = 1024
	}
	if options.MaxArrayLength == 0 {
		options.MaxArrayLength = 16 * 1024 * 1024
	}
	vm := &VM{
		loader: NewLoader(source),
		config: options,
		// Asked once here rather than at every instruction. slog evaluates a
		// call's arguments before the handler decides whether to keep them, so
		// an ungated Debug on the interpreter loop formats an opcode and boxes
		// six attributes for every instruction a release build then throws
		// away. That was most of what this runtime cost.
		traceInstructions: options.TraceInstructions && options.Logger != nil &&
			options.Logger.Enabled(context.Background(), slog.LevelDebug),
		natives:         make(map[methodKey]NativeMethod),
		contextNatives:  make(map[methodKey]contextNativeMethod),
		builtinNatives:  make(map[methodKey]bool),
		statics:         make(map[fieldKey]Value),
		declaringFields: make(map[fieldKey]fieldResolution),
		classMonitors:   make(map[string]*monitor),
		threads:         make(map[*Object]*guestThread),
		aotClasses:      make(map[string]AOTClassMetadata),
		aotAddresses:    make(map[uint32]string),
		aotObjects:      make(map[uint32]aotBinding),
		initializing:    make(map[string]bool),
		initialized:     make(map[string]bool),
		initErrors:      make(map[string]error),
	}
	vm.initCond = sync.NewCond(&vm.initMu)
	vm.initialized["java/lang/Object"] = true
	// Core exception classes are currently runtime-owned native surfaces rather
	// than class files. Mark Exception initialized so runtime API exceptions can
	// extend it without requiring a java/lang/Exception class file.
	vm.initialized["java/lang/Exception"] = true
	vm.registerBuiltins()
	if err := vm.registerCoreLibrary(); err != nil {
		// The core library is a table in this package rather than anything the
		// caller supplied, so a rejected definition is a mistake in that table
		// and every VM in the process has it. There is nothing a caller could
		// do with the error, and continuing would hand a game a library with a
		// hole in it.
		panic(err)
	}
	return vm
}

// ClassCensus names every class this VM has loaded and every one it was asked
// for and could not find. See Loader.Census.
func (vm *VM) ClassCensus() (loaded []string, missing map[string]string) {
	if vm == nil || vm.loader == nil {
		return nil, nil
	}
	return vm.loader.Census()
}

func (vm *VM) RegisterNative(class, name, descriptor string, method NativeMethod) error {
	if method == nil {
		return fmt.Errorf("native method is nil")
	}
	if _, err := ParseMethodDescriptor(descriptor); err != nil {
		return err
	}
	key := methodKey{class: class, name: name, descriptor: descriptor}
	vm.mu.Lock()
	defer vm.mu.Unlock()
	_, plain := vm.natives[key]
	_, context := vm.contextNatives[key]
	if (plain || context) && !vm.builtinNatives[key] {
		// A body is already here and it is not the runtime's own, so it is a
		// library's — the method is implemented, and registering over it would
		// replace behavior a caller of this API did not know was there.
		return fmt.Errorf("native method already registered: %s.%s%s", class, name, descriptor)
	}
	// A platform registration overrides the built-in implementation,
	// including context builtins such as the real-time Thread.sleep.
	delete(vm.contextNatives, key)
	delete(vm.builtinNatives, key)
	vm.natives[key] = method
	return nil
}

// RegisterContextNative is RegisterNative for an implementation that needs the
// execution it was entered on rather than only the VM. What needs it is a wait:
// whether a platform call may block depends on whether the caller is a thread
// the game started or the Host's own pass — see Invocation.WaitAsGuestThread.
func (vm *VM) RegisterContextNative(class, name, descriptor string, method ContextMethod) error {
	if method == nil {
		return fmt.Errorf("native method is nil")
	}
	if _, err := ParseMethodDescriptor(descriptor); err != nil {
		return err
	}
	key := methodKey{class: class, name: name, descriptor: descriptor}
	vm.mu.Lock()
	defer vm.mu.Unlock()
	_, plain := vm.natives[key]
	_, context := vm.contextNatives[key]
	if (plain || context) && !vm.builtinNatives[key] {
		return fmt.Errorf("native method already registered: %s.%s%s", class, name, descriptor)
	}
	delete(vm.natives, key)
	delete(vm.builtinNatives, key)
	vm.contextNatives[key] = func(vm *VM, state *execution, arguments []Value) (Value, error) {
		return method(&Invocation{vm: vm, state: state}, arguments)
	}
	return nil
}

// HasMethodBody reports whether this VM can answer one method call, either
// from a registered native or from a class it can load. A platform that
// publishes a class to guest code without carrying its own body is promising
// the method exists; asking here lets that promise be checked once, rather
// than by the game that eventually calls it.
func (vm *VM) HasMethodBody(class, name, descriptor string) bool {
	key := methodKey{class: class, name: name, descriptor: descriptor}
	vm.mu.RLock()
	_, native := vm.natives[key]
	_, context := vm.contextNatives[key]
	vm.mu.RUnlock()
	if native || context {
		return true
	}
	if _, method, err := vm.resolveStaticMethod(class, name, descriptor); err == nil {
		return method.CodeAttribute() != nil
	}
	if _, method, err := vm.resolveInstanceMethod(class, name, descriptor); err == nil {
		return method.CodeAttribute() != nil
	}
	return false
}

func (vm *VM) InvokeStatic(class, name, descriptor string, arguments ...Value) (Value, error) {
	methodType, err := ParseMethodDescriptor(descriptor)
	if err != nil {
		return VoidValue(), err
	}
	if err := validateArguments(arguments, methodType.Parameters); err != nil {
		return VoidValue(), fmt.Errorf("invoke %s.%s%s: %w", class, name, descriptor, err)
	}
	state := vm.newExecution()
	result, err := vm.invokeStatic(state, class, name, descriptor, arguments)
	if err != nil {
		return VoidValue(), err
	}
	if err := validateValue(result, methodType.Return); err != nil {
		return VoidValue(), fmt.Errorf("invoke %s.%s%s returned invalid value: %w", class, name, descriptor, err)
	}
	return result, nil
}

func (vm *VM) InvokeVirtual(receiver *Object, name, descriptor string, arguments ...Value) (Value, error) {
	if receiver == nil {
		return VoidValue(), fmt.Errorf("invoke %s%s on null reference", name, descriptor)
	}
	methodType, err := ParseMethodDescriptor(descriptor)
	if err != nil {
		return VoidValue(), err
	}
	if err := validateArguments(arguments, methodType.Parameters); err != nil {
		return VoidValue(), fmt.Errorf("invoke %s.%s%s: %w", receiver.ClassName, name, descriptor, err)
	}
	state := vm.newExecution()
	result, err := vm.invokeInstance(state, receiver.ClassName, receiver, name, descriptor, arguments)
	if err != nil {
		return VoidValue(), err
	}
	if err := validateValue(result, methodType.Return); err != nil {
		return VoidValue(), fmt.Errorf("invoke %s.%s%s returned invalid value: %w", receiver.ClassName, name, descriptor, err)
	}
	return result, nil
}

// InvokeSpecial invokes an instance method using the explicitly named lookup
// class instead of virtual dispatch. Platform AOT bridges use this for
// invokespecial calls into runtime-owned superclass constructors.
func (vm *VM) InvokeSpecial(receiver *Object, className, name, descriptor string, arguments ...Value) (Value, error) {
	if receiver == nil {
		return VoidValue(), fmt.Errorf("invoke special %s.%s%s on null reference", className, name, descriptor)
	}
	if className == "" {
		return VoidValue(), fmt.Errorf("invoke special class name is empty")
	}
	methodType, err := ParseMethodDescriptor(descriptor)
	if err != nil {
		return VoidValue(), err
	}
	if err := validateArguments(arguments, methodType.Parameters); err != nil {
		return VoidValue(), fmt.Errorf("invoke special %s.%s%s: %w", className, name, descriptor, err)
	}
	state := vm.newExecution()
	result, err := vm.invokeInstance(state, className, receiver, name, descriptor, arguments)
	if err != nil {
		return VoidValue(), err
	}
	if err := validateValue(result, methodType.Return); err != nil {
		return VoidValue(), fmt.Errorf("invoke special %s.%s%s returned invalid value: %w", className, name, descriptor, err)
	}
	return result, nil
}

// NewObject allocates a guest object and invokes the constructor declared by
// that class. Class initialization and constructor execution share one set of
// instruction and frame limits.
func (vm *VM) NewObject(className, descriptor string, arguments ...Value) (*Object, error) {
	methodType, err := ParseMethodDescriptor(descriptor)
	if err != nil {
		return nil, err
	}
	if methodType.Return.Kind != TypeVoid {
		return nil, fmt.Errorf("constructor descriptor must return void: %s", descriptor)
	}
	if err := validateArguments(arguments, methodType.Parameters); err != nil {
		return nil, fmt.Errorf("construct %s%s: %w", className, descriptor, err)
	}

	class, err := vm.loader.Load(className)
	if err != nil {
		return nil, err
	}
	constructor := class.FindMethod("<init>", descriptor)
	if constructor == nil || constructor.AccessFlags&0x0008 != 0 {
		return nil, fmt.Errorf("constructor not found: %s%s", className, descriptor)
	}

	state := vm.newExecution()
	object, err := vm.newObject(state, className)
	if err != nil {
		return nil, err
	}
	if _, err := vm.invokeInstance(state, className, object, "<init>", descriptor, arguments); err != nil {
		return nil, fmt.Errorf("construct %s%s: %w", className, descriptor, err)
	}
	return object, nil
}

// IsSubclassOf reports whether className is superName itself or reaches it by
// following the guest class hierarchy. The traversal is bounded because class
// files are untrusted input.
func (vm *VM) IsSubclassOf(className, superName string) (bool, error) {
	if className == "" || superName == "" {
		return false, fmt.Errorf("class names must not be empty")
	}
	visited := make(map[string]bool)
	for depth, current := 0, className; current != ""; depth++ {
		if current == superName {
			return true, nil
		}
		if depth >= vm.config.MaxFrames {
			return false, fmt.Errorf("class hierarchy from %s exceeds limit %d", className, vm.config.MaxFrames)
		}
		if visited[current] {
			return false, fmt.Errorf("cyclic class hierarchy at %s", current)
		}
		visited[current] = true
		// java/lang/Object is runtime-owned and has no class-file source. It is
		// the terminal superclass when the requested type did not match above.
		if current == "java/lang/Object" {
			return false, nil
		}
		if parent := runtimeClassParent(current); parent != "" {
			current = parent
			continue
		}
		if class, ok := vm.AOTClass(current); ok {
			current = class.SuperName
			continue
		}
		class, err := vm.loader.Load(current)
		if err != nil {
			return false, err
		}
		current = class.SuperName
	}
	return false, nil
}

func (vm *VM) invokeStatic(state *execution, className, name, descriptor string, arguments []Value) (Value, error) {
	key := methodKey{class: className, name: name, descriptor: descriptor}
	vm.mu.RLock()
	contextNative := vm.contextNatives[key]
	native := vm.natives[key]
	vm.mu.RUnlock()
	if contextNative != nil {
		result, err := contextNative(vm, state, append([]Value(nil), arguments...))
		if err != nil {
			return VoidValue(), fmt.Errorf("native %s.%s%s: %w", className, name, descriptor, err)
		}
		methodType, _ := ParseMethodDescriptor(descriptor)
		if err := validateValue(result, methodType.Return); err != nil {
			return VoidValue(), fmt.Errorf("native %s.%s%s returned invalid value: %w", className, name, descriptor, err)
		}
		return result, nil
	}
	if native != nil {
		result, err := native(vm, append([]Value(nil), arguments...))
		if err != nil {
			return VoidValue(), fmt.Errorf("native %s.%s%s: %w", className, name, descriptor, err)
		}
		methodType, _ := ParseMethodDescriptor(descriptor)
		if err := validateValue(result, methodType.Return); err != nil {
			return VoidValue(), fmt.Errorf("native %s.%s%s returned invalid value: %w", className, name, descriptor, err)
		}
		return result, nil
	}

	class, method, err := vm.resolveStaticMethod(className, name, descriptor)
	if err != nil {
		if result, handled, aotErr := vm.invokeAOTFallback(className, name, descriptor, arguments); handled {
			return result, aotErr
		}
		return VoidValue(), err
	}
	if name != "<clinit>" {
		if err := vm.ensureInitialized(state, class.Name); err != nil {
			return VoidValue(), err
		}
	}
	code := method.CodeAttribute()
	if code == nil {
		return VoidValue(), fmt.Errorf("method has no code: %s.%s%s", class.Name, name, descriptor)
	}
	var classMonitor *monitor
	if method.AccessFlags&0x0020 != 0 {
		classMonitor = vm.classMonitor(class.Name)
		classMonitor.enter(state.id)
	}
	result, executeErr := vm.execute(state, class, method, code, arguments)
	if classMonitor != nil {
		if exitErr := classMonitor.exit(state.id); executeErr == nil && exitErr != nil {
			executeErr = exitErr
		}
	}
	return result, executeErr
}

func (vm *VM) invokeInstance(
	state *execution,
	lookupClass string,
	receiver *Object,
	name string,
	descriptor string,
	arguments []Value,
) (Value, error) {
	if receiver == nil {
		return VoidValue(), guestException("java/lang/NullPointerException", "invoke "+name+descriptor)
	}
	combined := make([]Value, 0, len(arguments)+1)
	combined = append(combined, ReferenceValue(receiver))
	combined = append(combined, arguments...)
	return vm.invokeInstanceReceived(state, lookupClass, receiver, name, descriptor, combined)
}

// invokeInstanceReceived is invokeInstance with the receiver already in front
// of the arguments, which is the shape every callee here wants: a bytecode
// frame's locals start with `this`, and a native's argument list does too.
//
// It exists because building that slice was the single largest allocation in
// this runtime — eighty per cent of everything a title allocated, one make per
// guest method call, on top of the one the interpreter had already made to pop
// the arguments off the stack. The interpreter now pops into a slice with the
// receiver's slot in front of it and calls this directly, so a call allocates
// once instead of twice. The wrapper above stays for the callers that hold the
// receiver and the arguments apart, which is every Host-side entry point.
func (vm *VM) invokeInstanceReceived(
	state *execution,
	lookupClass string,
	receiver *Object,
	name string,
	descriptor string,
	combined []Value,
) (Value, error) {
	class, method, resolveErr := vm.resolveInstanceMethod(lookupClass, name, descriptor)
	var contextNative contextNativeMethod
	var native NativeMethod
	// A synchronized method implemented in Go still holds the receiver's
	// monitor. The interpreter takes it below for a bytecode body, and a
	// library method that says synchronized — java/util/Vector says it on
	// nearly all of them — would quietly stop being one when its body moved
	// out of bytecode.
	synchronizedNative := false
	if resolveErr == nil {
		synchronizedNative = method.AccessFlags&0x0020 != 0
		vm.mu.RLock()
		contextNative = vm.contextNatives[methodKey{class: class.Name, name: name, descriptor: descriptor}]
		native = vm.natives[methodKey{class: class.Name, name: name, descriptor: descriptor}]
		vm.mu.RUnlock()
	} else {
		contextNative = vm.resolveContextNativeInstance(lookupClass, name, descriptor)
		native = vm.resolveNativeInstance(lookupClass, name, descriptor)
	}
	if contextNative != nil || native != nil {
		if synchronizedNative {
			receiver.monitor.enter(state.id)
		}
		var result Value
		var err error
		if contextNative != nil {
			result, err = contextNative(vm, state, combined)
		} else {
			result, err = native(vm, combined)
		}
		if synchronizedNative {
			if exitErr := receiver.monitor.exit(state.id); err == nil && exitErr != nil {
				err = exitErr
			}
		}
		if err != nil {
			return VoidValue(), fmt.Errorf("native %s.%s%s: %w", lookupClass, name, descriptor, err)
		}
		methodType, _ := ParseMethodDescriptor(descriptor)
		if err := validateValue(result, methodType.Return); err != nil {
			return VoidValue(), fmt.Errorf("native %s.%s%s returned invalid value: %w", lookupClass, name, descriptor, err)
		}
		return result, nil
	}
	if resolveErr != nil {
		// The receiver's dynamic class dispatches AOT virtual calls.
		dispatchClass := lookupClass
		if receiver.ClassName != "" {
			dispatchClass = receiver.ClassName
		}
		if result, handled, aotErr := vm.invokeAOTFallback(dispatchClass, name, descriptor, combined); handled {
			return result, aotErr
		}
		return VoidValue(), resolveErr
	}
	code := method.CodeAttribute()
	if code == nil {
		return VoidValue(), fmt.Errorf("method has no code: %s.%s%s", class.Name, name, descriptor)
	}
	if method.AccessFlags&0x0020 != 0 {
		receiver.monitor.enter(state.id)
	}
	result, executeErr := vm.execute(state, class, method, code, combined)
	if method.AccessFlags&0x0020 != 0 {
		if exitErr := receiver.monitor.exit(state.id); executeErr == nil && exitErr != nil {
			executeErr = exitErr
		}
	}
	return result, executeErr
}

func (vm *VM) resolveStaticMethod(className, name, descriptor string) (*classfile.Class, *classfile.Member, error) {
	referenced := className
	for className != "" {
		class, err := vm.loader.Load(className)
		if err != nil {
			return nil, nil, unresolvedMethod(referenced, className, name, descriptor, err)
		}
		if method := class.FindMethod(name, descriptor); method != nil {
			if method.AccessFlags&0x0008 == 0 {
				return nil, nil, fmt.Errorf("method is not static: %s.%s%s", class.Name, name, descriptor)
			}
			return class, method, nil
		}
		className = class.SuperName
	}
	return nil, nil, fmt.Errorf("static method not found: %s.%s%s", referenced, name, descriptor)
}

// unresolvedMethod reports a resolution that ran off the end of a class chain.
//
// Which class is worth naming depends on where the walk stopped. Failing on
// the referenced class itself is a missing class, and its name is the answer.
// Failing further up means the chain reached a class the runtime owns rather
// than one the game ships — java/lang/Object, almost always — and reporting
// that name describes the walk instead of the gap: the caller wanted a method
// nothing in the chain declares, and the method is what has to be named.
func unresolvedMethod(referenced, missing, name, descriptor string, err error) error {
	if referenced == missing {
		return err
	}
	return fmt.Errorf("method not found: %s.%s%s (chain reached %s, which is not in the archive)", referenced, name, descriptor, missing)
}

func (vm *VM) resolveInstanceMethod(className, name, descriptor string) (*classfile.Class, *classfile.Member, error) {
	referenced := className
	for className != "" {
		class, err := vm.loader.Load(className)
		if err != nil {
			return nil, nil, unresolvedMethod(referenced, className, name, descriptor, err)
		}
		if method := class.FindMethod(name, descriptor); method != nil {
			if method.AccessFlags&0x0008 != 0 {
				return nil, nil, fmt.Errorf("method is static: %s.%s%s", class.Name, name, descriptor)
			}
			return class, method, nil
		}
		className = class.SuperName
	}
	return nil, nil, fmt.Errorf("instance method not found: %s%s", name, descriptor)
}

func validateArguments(arguments []Value, parameters []Type) error {
	if len(arguments) != len(parameters) {
		return fmt.Errorf("expected %d arguments, got %d", len(parameters), len(arguments))
	}
	for index, parameter := range parameters {
		if err := validateValue(arguments[index], parameter); err != nil {
			return fmt.Errorf("argument %d: %w", index, err)
		}
	}
	return nil
}

func (vm *VM) newExecution() *execution {
	return &execution{
		id:           vm.nextExecution.Add(1),
		initializing: make(map[string]bool),
	}
}

// renewSteps answers an exhausted step budget: without a platform hook the
// limit stands, and with one a granted window starts the count again.
func (vm *VM) renewSteps(state *execution) error {
	if vm.config.RenewSteps == nil {
		return ErrStepLimit
	}
	if err := vm.config.RenewSteps(); err != nil {
		return err
	}
	state.steps = 0
	return nil
}

func (vm *VM) classMonitor(className string) *monitor {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	result := vm.classMonitors[className]
	if result == nil {
		result = &monitor{}
		vm.classMonitors[className] = result
	}
	return result
}

func (vm *VM) resolveNativeInstance(className, name, descriptor string) NativeMethod {
	for className != "" {
		key := methodKey{class: className, name: name, descriptor: descriptor}
		vm.mu.RLock()
		native := vm.natives[key]
		vm.mu.RUnlock()
		if native != nil {
			return native
		}
		class, err := vm.loader.Load(className)
		if err != nil {
			if parent := runtimeClassParent(className); parent != "" {
				className = parent
				continue
			}
			if className != "java/lang/Object" {
				className = "java/lang/Object"
				continue
			}
			return nil
		}
		className = class.SuperName
	}
	return nil
}

func (vm *VM) resolveContextNativeInstance(className, name, descriptor string) contextNativeMethod {
	for className != "" {
		key := methodKey{class: className, name: name, descriptor: descriptor}
		vm.mu.RLock()
		native := vm.contextNatives[key]
		vm.mu.RUnlock()
		if native != nil {
			return native
		}
		class, err := vm.loader.Load(className)
		if err != nil {
			if parent := runtimeClassParent(className); parent != "" {
				className = parent
				continue
			}
			if className != "java/lang/Object" {
				className = "java/lang/Object"
				continue
			}
			return nil
		}
		className = class.SuperName
	}
	return nil
}

// StaticField reads a static field from outside the interpreter, running the
// class initializer first if it has not run. A platform needs this to reach a
// constant a runtime-owned class publishes — MIDP's List.SELECT_COMMAND is
// created by <clinit>, so there is nowhere else to read it from.
func (vm *VM) StaticField(class, name, descriptor string) (Value, error) {
	return vm.staticValue(vm.newExecution(), classfile.Reference{
		Kind:       classfile.FieldReference,
		Class:      class,
		Name:       name,
		Descriptor: descriptor,
	})
}

// SetStaticField writes a static field from outside the interpreter, running
// the class initializer first for the reason StaticField does. It is how a
// platform publishes a device fact a runtime-owned class exposes as a field
// rather than as a method: the class file cannot know the screen it will run
// on, and a game reads the field directly.
func (vm *VM) SetStaticField(class, name, descriptor string, value Value) error {
	return vm.setStaticValue(vm.newExecution(), classfile.Reference{
		Kind:       classfile.FieldReference,
		Class:      class,
		Name:       name,
		Descriptor: descriptor,
	}, value)
}

func (vm *VM) staticValue(state *execution, reference classfile.Reference) (Value, error) {
	if err := vm.ensureInitialized(state, reference.Class); err != nil {
		return VoidValue(), err
	}
	typeInfo, err := ParseFieldDescriptor(reference.Descriptor)
	if err != nil {
		return VoidValue(), err
	}
	resolved := vm.resolveFieldReference(reference)
	key := fieldKey{class: resolved.Class, name: resolved.Name, descriptor: resolved.Descriptor}
	vm.mu.RLock()
	value, ok := vm.statics[key]
	vm.mu.RUnlock()
	if !ok {
		value = zeroValue(typeInfo)
	}
	return value, nil
}

func (vm *VM) setStaticValue(state *execution, reference classfile.Reference, value Value) error {
	if err := vm.ensureInitialized(state, reference.Class); err != nil {
		return err
	}
	typeInfo, err := ParseFieldDescriptor(reference.Descriptor)
	if err != nil {
		return err
	}
	if err := validateValue(value, typeInfo); err != nil {
		return err
	}
	resolved := vm.resolveFieldReference(reference)
	key := fieldKey{class: resolved.Class, name: resolved.Name, descriptor: resolved.Descriptor}
	vm.mu.Lock()
	vm.statics[key] = value
	vm.mu.Unlock()
	return nil
}

func (vm *VM) newObject(state *execution, className string) (*Object, error) {
	if err := vm.ensureInitialized(state, className); err != nil {
		return nil, err
	}
	// Object is provided by the runtime native boundary rather than a class
	// file. Guest bytecode may still instantiate it directly. The runtime's own
	// Throwable types are the same case: they have a parent and a pair of
	// constructors and no class file, and bytecode throws them by name —
	// runtime-owned library code that raises one is the caller here, not only
	// the game.
	if className != "java/lang/Object" {
		if _, err := vm.loader.Load(className); err != nil {
			if runtimeClassParent(className) == "" {
				return nil, err
			}
		}
	}
	object := &Object{ClassName: className, Fields: make(map[string]Value)}
	vm.objectIdentity(object)
	return object, nil
}

// Field reads one instance field of an object, the way getfield does. A method
// body written in Go needs it for the same reason bytecode needed the opcode:
// the object holds the state its class declared, including the fields a
// subclass in the game can see.
func (vm *VM) Field(object *Object, className, name, descriptor string) (Value, error) {
	return vm.instanceValue(object, classfile.Reference{
		Kind:       classfile.FieldReference,
		Class:      className,
		Name:       name,
		Descriptor: descriptor,
	})
}

// SetField writes one instance field, the putfield to Field's getfield.
func (vm *VM) SetField(object *Object, className, name, descriptor string, value Value) error {
	return vm.setInstanceValue(object, classfile.Reference{
		Kind:       classfile.FieldReference,
		Class:      className,
		Name:       name,
		Descriptor: descriptor,
	}, value)
}

func (vm *VM) instanceValue(object *Object, reference classfile.Reference) (Value, error) {
	if object == nil {
		return VoidValue(), guestException("java/lang/NullPointerException", "get field "+reference.Class+"."+reference.Name)
	}
	typeInfo, err := ParseFieldDescriptor(reference.Descriptor)
	if err != nil {
		return VoidValue(), err
	}
	key := vm.resolveField(reference.Class, reference.Name, reference.Descriptor).key
	object.fieldMu.RLock()
	value, ok := object.Fields[key]
	object.fieldMu.RUnlock()
	if !ok {
		value = zeroValue(typeInfo)
	}
	return value, nil
}

func (vm *VM) setInstanceValue(object *Object, reference classfile.Reference, value Value) error {
	if object == nil {
		return guestException("java/lang/NullPointerException", "put field "+reference.Class+"."+reference.Name)
	}
	typeInfo, err := ParseFieldDescriptor(reference.Descriptor)
	if err != nil {
		return err
	}
	if err := validateValue(value, typeInfo); err != nil {
		return err
	}
	key := vm.resolveField(reference.Class, reference.Name, reference.Descriptor).key
	object.fieldMu.Lock()
	if object.Fields == nil {
		object.Fields = make(map[string]Value)
	}
	object.Fields[key] = value
	object.fieldMu.Unlock()
	return nil
}

// fieldReferenceKey names a field the way an object's field map does and the
// way HeapField.Key does, so a tool reading the heap and the interpreter
// writing it agree on what a field is called. Statics are keyed by a struct
// rather than by this string, but a store observer is shown this form for both
// so that an observer has one thing to match on.
func fieldReferenceKey(reference classfile.Reference) string {
	return reference.Class + "." + reference.Name + ":" + reference.Descriptor
}

// resolveFieldReference answers the reference a field access is really about:
// the same field on the class that declares it.
//
// A compiler names a field reference after the type the source expression had,
// not after the class the field is declared on, so a subclass reading an
// inherited field emits its own name — `Sub.buf` for a field java/io's stream
// declares, `Sub.v` for one its own superclass declares. Storing under the name
// as written puts the subclass's read and the superclass's write in two
// different slots, and the read answers a zero that was never written. The
// specification's field resolution is this walk, and it is what makes the two
// meet.
func (vm *VM) resolveFieldReference(reference classfile.Reference) classfile.Reference {
	declaring := vm.declaringFieldClass(reference.Class, reference.Name, reference.Descriptor)
	if declaring == reference.Class {
		return reference
	}
	resolved := reference
	resolved.Class = declaring
	return resolved
}

// declaringFieldClass walks a reference's class chain for the class that
// declares the field, and answers the referenced class itself when nothing in
// the chain does — a field an object carries without any class declaring it is
// the arrangement a platform's own objects use, and it keeps the name it was
// written under.
// fieldResolution is what a field reference resolves to, cached together
// because the two are wanted together. The key is the string an object's field
// map is keyed by, and composing it is a five-part concatenation and an
// allocation — on a path that runs on every `getfield` and every `putfield`,
// which was 5% of a guest call loop on its own.
type fieldResolution struct {
	class string
	key   string
}

func (vm *VM) declaringFieldClass(className, name, descriptor string) string {
	return vm.resolveField(className, name, descriptor).class
}

func (vm *VM) resolveField(className, name, descriptor string) fieldResolution {
	key := fieldKey{class: className, name: name, descriptor: descriptor}
	vm.mu.RLock()
	cached, ok := vm.declaringFields[key]
	vm.mu.RUnlock()
	if ok {
		return cached
	}
	declaring := className
	// The chain is bounded by the loader, which refuses a cycle; the counter
	// bounds it again because this runs on every field access.
	for current, depth := className, 0; current != "" && depth < maxFieldResolutionDepth; depth++ {
		class, err := vm.loader.Load(current)
		if err != nil {
			break
		}
		if class.FindField(name, descriptor) != nil {
			declaring = current
			break
		}
		// A static final may be declared on an interface, which is why
		// resolution searches them before the superclass.
		if found, ok := vm.declaringInterfaceField(class, name, descriptor, depth); ok {
			declaring = found
			break
		}
		current = class.SuperName
	}
	resolution := fieldResolution{
		class: declaring,
		key:   declaring + "." + name + ":" + descriptor,
	}
	vm.mu.Lock()
	vm.declaringFields[key] = resolution
	vm.mu.Unlock()
	return resolution
}

// declaringInterfaceField searches a class's superinterfaces for the field.
func (vm *VM) declaringInterfaceField(class *classfile.Class, name, descriptor string, depth int) (string, bool) {
	if depth >= maxFieldResolutionDepth {
		return "", false
	}
	for _, interfaceName := range class.Interfaces {
		declared, err := vm.loader.Load(interfaceName)
		if err != nil {
			continue
		}
		if declared.FindField(name, descriptor) != nil {
			return interfaceName, true
		}
		if found, ok := vm.declaringInterfaceField(declared, name, descriptor, depth+1); ok {
			return found, true
		}
	}
	return "", false
}

// maxFieldResolutionDepth bounds the walk a field reference takes through a
// chain a game supplies.
const maxFieldResolutionDepth = 64

// NewArray creates a guest array of the given component type and length for a
// native service that has to hand one back. It applies the same limits the
// interpreter's own array creation does.
func (vm *VM) NewArray(component Type, length int32) (*Object, error) {
	return vm.newArray(component, length)
}

func (vm *VM) newArray(component Type, length int32) (*Object, error) {
	if length < 0 {
		return nil, guestException("java/lang/NegativeArraySizeException", fmt.Sprintf("length %d", length))
	}
	if int64(length) > int64(vm.config.MaxArrayLength) {
		return nil, fmt.Errorf("array size %d exceeds limit %d", length, vm.config.MaxArrayLength)
	}
	values := make([]Value, int(length))
	for index := range values {
		values[index] = zeroValue(component)
	}
	object := &Object{
		ClassName: "[" + component.Descriptor(),
		Native:    &Array{Component: component, storage: valueStorage(values)},
	}
	vm.objectIdentity(object)
	return object, nil
}

func (vm *VM) newMultiArray(arrayType Type, lengths []int32) (*Object, error) {
	if arrayType.Kind != TypeArray || arrayType.Component == nil || len(lengths) == 0 {
		return nil, fmt.Errorf("invalid multianewarray type or dimensions")
	}
	array, err := vm.newArray(*arrayType.Component, lengths[0])
	if err != nil {
		return nil, err
	}
	if len(lengths) == 1 {
		return array, nil
	}
	if arrayType.Component.Kind != TypeArray {
		return nil, fmt.Errorf("multianewarray has more dimensions than its type")
	}
	storage := array.Native.(*Array)
	for index := 0; index < storage.Length(); index++ {
		child, err := vm.newMultiArray(*arrayType.Component, lengths[1:])
		if err != nil {
			return nil, err
		}
		if err := storage.Store(index, ReferenceValue(child)); err != nil {
			return nil, err
		}
	}
	return array, nil
}

// IsInstance answers `instanceof` for a native method holding a reference it
// was handed. Unlike IsSubclassOf it reads the interfaces a class declares,
// which is what a platform surface needs when the parameter's type is an
// interface such as java/lang/Runnable.
func (vm *VM) IsInstance(object *Object, target string) bool {
	return vm.isInstance(object, target)
}

func (vm *VM) isInstance(object *Object, target string) bool {
	if object == nil {
		return false
	}
	if object.ClassName == target || target == "java/lang/Object" {
		return true
	}
	if len(object.ClassName) > 0 && object.ClassName[0] == '[' {
		return target == "java/lang/Cloneable" || target == "java/io/Serializable"
	}
	for current := object.ClassName; current != ""; {
		class, err := vm.loader.Load(current)
		if err != nil {
			return false
		}
		for _, interfaceName := range class.Interfaces {
			if interfaceName == target {
				return true
			}
		}
		if class.SuperName == target {
			return true
		}
		current = class.SuperName
	}
	return false
}

func (vm *VM) objectIdentity(object *Object) uint32 {
	if object == nil {
		return 0
	}
	if identity := object.identity.Load(); identity != 0 {
		return identity
	}
	candidate := vm.nextObject.Add(1)
	if candidate == 0 {
		candidate = vm.nextObject.Add(1)
	}
	if object.identity.CompareAndSwap(0, candidate) {
		return candidate
	}
	return object.identity.Load()
}

func (vm *VM) ensureInitialized(state *execution, className string) error {
	if state.initializing[className] {
		return nil
	}

	vm.initMu.Lock()
	for vm.initializing[className] {
		vm.initCond.Wait()
	}
	if vm.initialized[className] {
		vm.initMu.Unlock()
		return nil
	}
	if err := vm.initErrors[className]; err != nil {
		vm.initMu.Unlock()
		return fmt.Errorf("class initialization previously failed for %s: %w", className, err)
	}
	vm.initializing[className] = true
	vm.initMu.Unlock()

	state.initializing[className] = true
	err := vm.initializeClass(state, className)
	delete(state.initializing, className)

	vm.initMu.Lock()
	delete(vm.initializing, className)
	if err == nil {
		vm.initialized[className] = true
	} else {
		vm.initErrors[className] = err
	}
	vm.initCond.Broadcast()
	vm.initMu.Unlock()
	return err
}

func (vm *VM) initializeClass(state *execution, className string) error {
	class, err := vm.loader.Load(className)
	if err != nil {
		// A runtime-owned Throwable has no class file to initialize, and no
		// statics behind it either.
		if runtimeClassParent(className) != "" {
			return nil
		}
		return err
	}
	if class.SuperName != "" {
		if err := vm.ensureInitialized(state, class.SuperName); err != nil {
			return err
		}
	}
	if err := vm.initializeStaticFields(class); err != nil {
		return err
	}
	vm.seedDefinedConstants(className)
	initializer := class.FindMethod("<clinit>", "()V")
	if initializer == nil {
		return nil
	}
	code := initializer.CodeAttribute()
	if code == nil {
		// A class declared in Go writes its initializer as a method body like
		// any other, so the class initializer is a native here rather than
		// bytecode.
		if _, err := vm.invokeStatic(state, className, "<clinit>", "()V", nil); err != nil {
			return fmt.Errorf("initialize class %s: %w", className, err)
		}
		return nil
	}
	_, err = vm.execute(state, class, initializer, code, nil)
	if err != nil {
		return fmt.Errorf("initialize class %s: %w", className, err)
	}
	return nil
}

func (vm *VM) initializeStaticFields(class *classfile.Class) error {
	for _, field := range class.Fields {
		if field.AccessFlags&0x0008 == 0 {
			continue
		}
		typeInfo, err := ParseFieldDescriptor(field.Descriptor)
		if err != nil {
			return err
		}
		value := zeroValue(typeInfo)
		for _, attribute := range field.Attributes {
			if attribute.Name != "ConstantValue" {
				continue
			}
			if len(attribute.Info) != 2 {
				return fmt.Errorf("invalid ConstantValue on %s.%s", class.Name, field.Name)
			}
			index := uint16(attribute.Info[0])<<8 | uint16(attribute.Info[1])
			value, err = constantValue(class.ConstantPool, index, typeInfo.Slots() == 2)
			if err != nil {
				return err
			}
			if err := validateValue(value, typeInfo); err != nil {
				return err
			}
		}
		key := fieldKey{class: class.Name, name: field.Name, descriptor: field.Descriptor}
		vm.mu.Lock()
		vm.statics[key] = value
		vm.mu.Unlock()
	}
	return nil
}

// SetStoreObserver installs f as the observer of guest stores, or clears it
// when f is nil. A platform that watches writes installs one while it has a
// watch and takes it away when the last one goes.
func (vm *VM) SetStoreObserver(f func(StoreEvent)) {
	if vm == nil {
		return
	}
	if f == nil {
		vm.storeObserver.Store(nil)
		return
	}
	vm.storeObserver.Store(&f)
}

// observeStore shows one store to the observer if there is one.
func (vm *VM) observeStore(event StoreEvent) {
	if vm == nil {
		return
	}
	if observe := vm.storeObserver.Load(); observe != nil {
		(*observe)(event)
	}
}

// watchingStores answers whether building a StoreEvent is worth anything. A
// caller that has to compose the event's key first asks this instead of paying
// for a key nobody reads: the point of the observer being a nil pointer is that
// a title nobody is investigating pays a nil check, and a `putfield` that
// composed its own name on the way past was paying rather more.
func (vm *VM) watchingStores() bool {
	return vm != nil && vm.storeObserver.Load() != nil
}
