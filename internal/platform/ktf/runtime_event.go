package ktf

import (
	"fmt"
	"strings"
	"time"

	"github.com/movingwoo/wfeature/internal/jvm"
)

// The WIPI event queue is the second way a game receives input and redraw
// requests: instead of letting the platform call into its cards, the game runs
// its own loop of getNextEvent/dispatchEvent. Both paths deliver the same
// events to the same cards, so the queue only decides who drives.
//
// A game that never calls getNextEvent keeps the Host-driven delivery it has
// today. The first getNextEvent call switches key delivery to the queue,
// because dispatching a key both directly and through the queue would deliver
// it twice.

const (
	runtimeEventQueueClass         = "org/kwis/msp/lcdui/EventQueue"
	runtimeInputMethodHandlerClass = "org/kwis/msp/lcdui/InputMethodHandler"
	runtimeJletListenerClass       = "org/kwis/msp/lcdui/JletEventListener"
	runtimeImageObserverClass      = "org/kwis/msp/lcdui/ImageObserver"
	runtimeMainClass               = "org/kwis/msp/lcdui/Main"
)

// Event kinds and keyboard event types match the values the original runtime
// puts in the guest's int[4]; games compare against their own copies of them.
const (
	eventKindKey     int32 = 1
	eventKindPointer int32 = 2
	eventKindRepaint int32 = 41
	eventKindNotify  int32 = 1000

	// eventQueueSlots is the int[] length one event occupies.
	eventQueueSlots = 4
	// maxQueuedEvents bounds queue growth when a game stops draining it.
	maxQueuedEvents = 256
	// eventLoopFrameMillis is how long an idle getNextEvent waits, matching the
	// original runtime's frame-paced event wait.
	eventLoopFrameMillis = 16
)

type guestEvent struct {
	kind   int32
	param1 int32
	param2 int32
	param3 int32
}

// postGuestEvent appends one event. A full queue drops its oldest entry: a game
// that stopped reading events must not grow Host memory without bound.
func (runtime *initializationRuntime) postGuestEvent(event guestEvent) {
	if len(runtime.events) >= maxQueuedEvents {
		copy(runtime.events, runtime.events[1:])
		runtime.events = runtime.events[:len(runtime.events)-1]
	}
	runtime.events = append(runtime.events, event)
}

func (runtime *initializationRuntime) nextGuestEvent() (guestEvent, bool) {
	if len(runtime.events) == 0 {
		return guestEvent{}, false
	}
	event := runtime.events[0]
	runtime.events = runtime.events[1:]
	return event, true
}

// postRepaintEvent queues a redraw request for a guest-driven event loop,
// coalescing it with one already waiting the way a frame-paced platform does.
func (runtime *initializationRuntime) postRepaintEvent() {
	if !runtime.guestEventLoop {
		return
	}
	for _, pending := range runtime.events {
		if pending.kind == eventKindRepaint {
			return
		}
	}
	runtime.postGuestEvent(guestEvent{kind: eventKindRepaint})
}

func runtimeEventQueueClassDefinition() runtimeJavaClass {
	const class = runtimeEventQueueClass
	return runtimeJavaClass{
		name:        class,
		superName:   "java/lang/Object",
		accessFlags: 0x0021,
		methods: []runtimeJavaMethod{
			{class: class, name: "<init>", descriptor: "(Lorg/kwis/msp/lcdui/Jlet;)V", accessFlags: 0x0001, implementation: runtimeEventQueueConstructor},
			{class: class, name: "getNextEvent", descriptor: "([I)V", accessFlags: 0x0001, implementation: runtimeEventQueueGetNextEvent},
			{class: class, name: "dispatchEvent", descriptor: "([I)V", accessFlags: 0x0001, implementation: runtimeEventQueueDispatchEvent},
			{class: class, name: "postEvent", descriptor: "([I)Z", accessFlags: 0x0001, implementation: runtimeEventQueuePostEvent},
			{class: class, name: "postEvent", descriptor: "(I[I)V", accessFlags: 0x0009, implementation: runtimeEventQueuePostEventStatic},
		},
	}
}

func runtimeInputMethodHandlerClassDefinition() runtimeJavaClass {
	const class = runtimeInputMethodHandlerClass
	return runtimeJavaClass{
		name:        class,
		superName:   "java/lang/Object",
		accessFlags: 0x0021,
		methods: []runtimeJavaMethod{
			{class: class, name: "<init>", descriptor: "(I)V", accessFlags: 0x0001, implementation: runtimeInputMethodConstructor},
			{class: class, name: "setCurrentMode", descriptor: "(I)Z", accessFlags: 0x0001, implementation: runtimeInputMethodSetMode},
		},
	}
}

func runtimeMainClassDefinition() runtimeJavaClass {
	const class = runtimeMainClass
	return runtimeJavaClass{
		name:        class,
		superName:   "java/lang/Object",
		accessFlags: 0x0021,
		methods: []runtimeJavaMethod{
			{class: class, name: "main", descriptor: "([Ljava/lang/String;)V", accessFlags: 0x0009, implementation: runtimeMainStart},
		},
	}
}

// runtimeInterfaceClass declares a runtime-owned interface. Its abstract
// methods carry a body that re-dispatches on the receiver's own class, so a
// guest that calls one through the interface record reaches the implementation
// by name instead of by an inherited vtable slot it never had.
func runtimeInterfaceClass(name string, methods ...runtimeJavaMethod) runtimeJavaClass {
	for index := range methods {
		methods[index].class = name
		methods[index].accessFlags = 0x0401
		methods[index].implementation = runtimeInterfaceDispatch(name, methods[index].name, methods[index].descriptor)
	}
	return runtimeJavaClass{name: name, accessFlags: 0x0601, methods: methods}
}

func runtimeInterfaceDispatch(class, name, descriptor string) runtimeJavaImplementation {
	return func(_ *initializationRuntime, vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		if len(arguments) == 0 {
			return jvm.VoidValue(), fmt.Errorf("interface method %s.%s%s expected a receiver", class, name, descriptor)
		}
		receiver, err := arguments[0].Reference()
		if err != nil {
			return jvm.VoidValue(), err
		}
		if receiver == nil {
			return jvm.VoidValue(), fmt.Errorf("interface method %s.%s%s receiver is null", class, name, descriptor)
		}
		if receiver.ClassName == class {
			// Re-dispatching on the interface itself would call this body
			// again; the receiver has to be an implementation.
			return jvm.VoidValue(), fmt.Errorf("interface method %s.%s%s has no implementation on its own interface", class, name, descriptor)
		}
		return vm.InvokeVirtual(receiver, name, descriptor, arguments[1:]...)
	}
}

// runtimeEventQueueConstructor records the queue as the Jlet's own and keeps
// the owning Jlet reachable from it.
func runtimeEventQueueConstructor(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) != 2 {
		return jvm.VoidValue(), fmt.Errorf("EventQueue constructor expected receiver and Jlet, got %d arguments", len(arguments))
	}
	receiver, err := arguments[0].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if receiver == nil {
		return jvm.VoidValue(), fmt.Errorf("EventQueue constructor receiver is null")
	}
	if receiver.Fields == nil {
		receiver.Fields = make(map[string]jvm.Value)
	}
	receiver.Fields["jlet:Lorg/kwis/msp/lcdui/Jlet;"] = arguments[1]
	if runtime.runtimeObjects[runtimeEventQueueClass] == nil {
		runtime.runtimeObjects[runtimeEventQueueClass] = receiver
	}
	return jvm.VoidValue(), nil
}

// runtimeEventQueueObject answers the process-wide event queue, creating it on
// first use so Jlet.getEventQueue works whether or not the guest constructed
// one itself.
func (runtime *initializationRuntime) runtimeEventQueueObject() *jvm.Object {
	queue := runtime.runtimeObjects[runtimeEventQueueClass]
	if queue == nil {
		queue = &jvm.Object{ClassName: runtimeEventQueueClass, Fields: make(map[string]jvm.Value)}
		runtime.runtimeObjects[runtimeEventQueueClass] = queue
	}
	return queue
}

// runtimeEventQueueGetNextEvent fills the guest's int[4] with the next event.
// The original runtime blocks here until one exists; a worker-hosted guest
// thread does the same by ending its slice, which lets the Host run timers,
// other threads, and the paint service before the loop continues. When nothing
// is queued the answer is the platform's own redraw request, so a game whose
// whole frame loop lives in this queue keeps drawing.
func runtimeEventQueueGetNextEvent(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) != 2 {
		return jvm.VoidValue(), fmt.Errorf("EventQueue.getNextEvent expected receiver and event array, got %d arguments", len(arguments))
	}
	array, err := arguments[1].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if array == nil {
		return jvm.VoidValue(), fmt.Errorf("EventQueue.getNextEvent event array is null")
	}
	runtime.guestEventLoop = true
	event, ok := runtime.nextGuestEvent()
	if !ok {
		// An empty queue is the frame-paced wait the original runtime blocks
		// in, so the worker parks for one frame rather than returning a redraw
		// request as fast as the Host can ask for one.
		if err := runtime.sleepCurrentWorker(eventLoopFrameMillis * time.Millisecond); err != nil {
			return jvm.VoidValue(), err
		}
		if event, ok = runtime.nextGuestEvent(); !ok {
			event = guestEvent{kind: eventKindRepaint}
		}
	}
	// Counted by kind rather than under a name composed here: this runs once
	// per event the game receives, which is the frequency diagEvent exists for.
	runtime.recordDiagnostic(diagEvent{kind: diagGuestEvent, nums: [5]uint32{uint32(event.kind)}})
	return jvm.VoidValue(), storeGuestEvent(array, event)
}

func storeGuestEvent(array *jvm.Object, event guestEvent) error {
	values := []jvm.Value{
		jvm.IntValue(event.kind),
		jvm.IntValue(event.param1),
		jvm.IntValue(event.param2),
		jvm.IntValue(event.param3),
	}
	if _, length, ok := jvm.ArrayComponent(array); ok && length < len(values) {
		values = values[:length]
	}
	return jvm.SetArrayRange(array, 0, values)
}

func loadGuestEvent(array *jvm.Object) (guestEvent, error) {
	_, values, err := jvm.ArraySnapshot(array)
	if err != nil {
		return guestEvent{}, err
	}
	if len(values) < eventQueueSlots {
		return guestEvent{}, fmt.Errorf("KTF event array holds %d slots, need %d", len(values), eventQueueSlots)
	}
	var event guestEvent
	fields := []*int32{&event.kind, &event.param1, &event.param2, &event.param3}
	for index, field := range fields {
		value, err := values[index].Int32()
		if err != nil {
			return guestEvent{}, err
		}
		*field = value
	}
	return event, nil
}

// runtimeEventQueueDispatchEvent delivers one event the game read with
// getNextEvent: keys traverse the card stack exactly as Host-driven delivery
// does, a redraw request paints the top card, and a notify event reaches the
// registered Jlet event listeners.
func runtimeEventQueueDispatchEvent(runtime *initializationRuntime, vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) != 2 {
		return jvm.VoidValue(), fmt.Errorf("EventQueue.dispatchEvent expected receiver and event array, got %d arguments", len(arguments))
	}
	array, err := arguments[1].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if array == nil {
		return jvm.VoidValue(), fmt.Errorf("EventQueue.dispatchEvent event array is null")
	}
	event, err := loadGuestEvent(array)
	if err != nil {
		return jvm.VoidValue(), err
	}
	switch event.kind {
	case eventKindKey:
		return jvm.VoidValue(), runtime.dispatchKeyToCards(event.param1, event.param2)
	case eventKindPointer:
		// The specification's table: event[1] is the pointer event type and
		// event[2] and event[3] are its screen coordinates.
		return jvm.VoidValue(), runtime.dispatchPointerToCards(event.param1, event.param2, event.param3)
	case eventKindRepaint:
		// The redraw request has been taken off the queue, so the dirty flag
		// the Host-driven path uses is satisfied by this paint.
		runtime.repaintPending = false
		_, err := runtime.paintTopCard()
		return jvm.VoidValue(), err
	case eventKindNotify:
		return jvm.VoidValue(), runtime.dispatchNotifyEvent(vm, event)
	default:
		message := fmt.Sprintf("invalid event queue event type %d", event.kind)
		return jvm.VoidValue(), &jvm.GuestException{
			Object:  &jvm.Object{ClassName: "java/lang/IllegalArgumentException", Native: message},
			Message: message,
		}
	}
}

func (runtime *initializationRuntime) dispatchNotifyEvent(vm *jvm.VM, event guestEvent) error {
	for _, listener := range runtime.jletListeners {
		if listener == nil {
			continue
		}
		if _, err := vm.InvokeVirtual(listener, "notifyEvent", "(III)V",
			jvm.IntValue(event.param1), jvm.IntValue(event.param2), jvm.IntValue(event.param3)); err != nil {
			return fmt.Errorf("notify KTF Jlet listener %s: %w", listener.ClassName, err)
		}
	}
	return nil
}

// runtimeEventQueuePostEvent accepts an event the game itself produced. The
// original runtime rejects unqueueable events with false; a queued event
// answers true.
func runtimeEventQueuePostEvent(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) != 2 {
		return jvm.VoidValue(), fmt.Errorf("EventQueue.postEvent expected receiver and event array, got %d arguments", len(arguments))
	}
	array, err := arguments[1].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if array == nil {
		return jvm.IntValue(0), nil
	}
	event, err := loadGuestEvent(array)
	if err != nil {
		return jvm.VoidValue(), err
	}
	runtime.postGuestEvent(event)
	return jvm.IntValue(1), nil
}

// runtimeEventQueuePostEventStatic posts to the program identified by id. The
// emulator runs exactly one program, so every identifier addresses this queue.
func runtimeEventQueuePostEventStatic(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) != 2 {
		return jvm.VoidValue(), fmt.Errorf("EventQueue.postEvent expected a program id and event array, got %d arguments", len(arguments))
	}
	array, err := arguments[1].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if array == nil {
		return jvm.VoidValue(), nil
	}
	event, err := loadGuestEvent(array)
	if err != nil {
		return jvm.VoidValue(), err
	}
	runtime.postGuestEvent(event)
	return jvm.VoidValue(), nil
}

// dispatchKeyToCards delivers one key event from the top pushed card down.
// A card's keyNotify returning true propagates to the card below it, matching
// the original runtime's card stack traversal.
func (runtime *initializationRuntime) dispatchKeyToCards(eventType, key int32) error {
	if listener := runtime.grabbedKeys[key]; listener != nil {
		if _, err := runtime.client.vm.InvokeVirtual(listener, "notifyEvent", "(III)V",
			jvm.IntValue(eventKindKey), jvm.IntValue(eventType), jvm.IntValue(key)); err != nil {
			return fmt.Errorf("notify KTF grabbed key listener %s: %w", listener.ClassName, err)
		}
		return nil
	}
	for index := len(runtime.displayCards) - 1; index >= 0; index-- {
		card := runtime.displayCards[index]
		result, err := runtime.client.vm.InvokeVirtual(card, "keyNotify", "(II)Z", jvm.IntValue(eventType), jvm.IntValue(key))
		if err != nil {
			return fmt.Errorf("notify KTF card %s key: %w", card.ClassName, err)
		}
		propagate, err := result.Int32()
		if err != nil {
			return err
		}
		if propagate == 0 {
			return nil
		}
	}
	return nil
}

// dispatchPointerToCards delivers one pointer event down the card stack, the
// same traversal keys take. The guest method takes the event type first, then
// the coordinates.
func (runtime *initializationRuntime) dispatchPointerToCards(eventType, x, y int32) error {
	for index := len(runtime.displayCards) - 1; index >= 0; index-- {
		card := runtime.displayCards[index]
		result, err := runtime.client.vm.InvokeVirtual(card, "pointerNotify", "(III)Z",
			jvm.IntValue(eventType), jvm.IntValue(x), jvm.IntValue(y))
		if err != nil {
			return fmt.Errorf("notify KTF card %s pointer: %w", card.ClassName, err)
		}
		propagate, err := result.Int32()
		if err != nil {
			return err
		}
		if propagate == 0 {
			return nil
		}
	}
	return nil
}

// paintTopCard paints the top pushed card into the screen framebuffer and
// presents the frame. It reports whether a card painted. Re-entrant paints are
// dropped: a card that repaints while painting would otherwise recurse.
func (runtime *initializationRuntime) paintTopCard() (bool, error) {
	if len(runtime.displayCards) == 0 {
		return false, nil
	}
	if runtime.repaintServicing {
		return false, nil
	}
	card := runtime.displayCards[len(runtime.displayCards)-1]
	graphics, err := runtime.newScreenGraphics()
	if err != nil {
		return false, err
	}
	if err := runtime.ensureResultBound(graphics); err != nil {
		return false, err
	}
	runtime.repaintServicing = true
	defer func() { runtime.repaintServicing = false }()
	if _, err := runtime.client.vm.InvokeVirtual(card, "paint", "(Lorg/kwis/msp/lcdui/Graphics;)V", jvm.ReferenceValue(graphics)); err != nil {
		return true, fmt.Errorf("paint KTF card %s: %w", card.ClassName, err)
	}
	return true, runtime.presentScreen()
}

func runtimeInputMethodConstructor(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) != 2 {
		return jvm.VoidValue(), fmt.Errorf("InputMethodHandler constructor expected receiver and constraint, got %d arguments", len(arguments))
	}
	receiver, err := arguments[0].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if receiver == nil {
		return jvm.VoidValue(), fmt.Errorf("InputMethodHandler constructor receiver is null")
	}
	if receiver.Fields == nil {
		receiver.Fields = make(map[string]jvm.Value)
	}
	receiver.Fields["mode:I"] = arguments[1]
	return jvm.VoidValue(), nil
}

// runtimeInputMethodSetMode records the requested input mode and accepts it.
// There is no on-device input method to switch: text arrives through the Host
// keypad, so every mode is equally available.
func runtimeInputMethodSetMode(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) != 2 {
		return jvm.VoidValue(), fmt.Errorf("InputMethodHandler.setCurrentMode expected receiver and mode, got %d arguments", len(arguments))
	}
	receiver, err := arguments[0].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if receiver != nil {
		if receiver.Fields == nil {
			receiver.Fields = make(map[string]jvm.Value)
		}
		receiver.Fields["mode:I"] = arguments[1]
	}
	return jvm.IntValue(1), nil
}

// runtimeMainStart is the original launcher entry point: it constructs the
// application class named by the first argument and starts it. Hosts normally
// drive that sequence themselves through ktf.Session; a guest that calls
// Main.main directly takes the same path.
func runtimeMainStart(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) != 1 {
		return jvm.VoidValue(), fmt.Errorf("Main.main expected an argument array, got %d arguments", len(arguments))
	}
	array, err := arguments[0].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if array == nil {
		return jvm.VoidValue(), fmt.Errorf("Main.main argument array is null")
	}
	_, values, err := jvm.ArraySnapshot(array)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if len(values) == 0 {
		return jvm.VoidValue(), fmt.Errorf("Main.main needs the application class name")
	}
	nameObject, err := values[0].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	name, ok := jvm.StringText(nameObject)
	if !ok {
		return jvm.VoidValue(), fmt.Errorf("Main.main application class name is not a string")
	}
	className := strings.ReplaceAll(name, ".", "/")
	if err := validateClassName(className); err != nil {
		return jvm.VoidValue(), err
	}
	if runtime.currentContext == nil {
		return jvm.VoidValue(), fmt.Errorf("Main.main has no active guest context")
	}
	loaded, err := runtime.client.loadClassLocked(runtime.currentContext, className)
	if err != nil {
		return jvm.VoidValue(), err
	}
	_, object, err := runtime.allocateAOTInstance(loaded.Metadata.Address)
	if err != nil {
		return jvm.VoidValue(), fmt.Errorf("allocate KTF application %s: %w", className, err)
	}
	if _, err := runtime.invokeAOTFromJVM(className, "<init>", "()V", []jvm.Value{jvm.ReferenceValue(object)}); err != nil {
		return jvm.VoidValue(), err
	}
	startArguments, err := runtime.client.newStringArrayObject()
	if err != nil {
		return jvm.VoidValue(), err
	}
	_, err = runtime.invokeAOTFromJVM(className, "startApp", "([Ljava/lang/String;)V",
		[]jvm.Value{jvm.ReferenceValue(object), jvm.ReferenceValue(startArguments)})
	return jvm.VoidValue(), err
}

// runtimeJletGetActive answers the Jlet the application constructed. WIPI
// applications reach their Display and event queue through it, so it is the
// most-used entry point in the class.
func runtimeJletGetActive(runtime *initializationRuntime, _ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	return jvm.ReferenceValue(runtime.activeJlet), nil
}

func runtimeJletGetEventQueue(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) != 1 {
		return jvm.VoidValue(), fmt.Errorf("Jlet.getEventQueue expected receiver, got %d arguments", len(arguments))
	}
	return jvm.ReferenceValue(runtime.runtimeEventQueueObject()), nil
}

// runtimeJletGetAppProperty answers a property of the application descriptor,
// which is where the original runtime reads its manifest values from. An
// unknown key answers null, as the WIPI API specifies.
func runtimeJletGetAppProperty(runtime *initializationRuntime, vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) != 2 {
		return jvm.VoidValue(), fmt.Errorf("Jlet.getAppProperty expected receiver and key, got %d arguments", len(arguments))
	}
	keyObject, err := arguments[1].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	key, ok := jvm.StringText(keyObject)
	if !ok {
		return jvm.ReferenceValue(nil), nil
	}
	value, found := runtime.client.appProperties[strings.ToUpper(strings.TrimSpace(key))]
	if !found {
		return jvm.ReferenceValue(nil), nil
	}
	return jvm.ReferenceValue(vm.NewString(value)), nil
}

// runtimeJletNotifyDestroyed ends the session the same way MC_knlExit does:
// the application declared itself finished, which is a normal Host shutdown
// rather than a failure.
func runtimeJletNotifyDestroyed(runtime *initializationRuntime, _ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	runtime.countDiagnostic("Jlet.notifyDestroyed")
	return jvm.VoidValue(), ErrGuestExited
}

// runtimeDisplayAddListener registers a Jlet event listener. Notify events and
// grabbed keys are delivered to these listeners.
func runtimeDisplayAddListener(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	listener, err := runtimeListenerArgument("Display.addJletEventListener", arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if listener == nil {
		return jvm.VoidValue(), nil
	}
	for _, existing := range runtime.jletListeners {
		if existing == listener {
			return jvm.VoidValue(), nil
		}
	}
	if len(runtime.jletListeners) >= maxJletListeners {
		return jvm.VoidValue(), fmt.Errorf("KTF Jlet event listener count exceeds %d", maxJletListeners)
	}
	runtime.jletListeners = append(runtime.jletListeners, listener)
	return jvm.VoidValue(), nil
}

const maxJletListeners = 64

func runtimeDisplayRemoveListener(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	listener, err := runtimeListenerArgument("Display.removeJletEventListener", arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	for index, existing := range runtime.jletListeners {
		if existing == listener {
			runtime.jletListeners = append(runtime.jletListeners[:index], runtime.jletListeners[index+1:]...)
			break
		}
	}
	return jvm.VoidValue(), nil
}

func runtimeListenerArgument(method string, arguments []jvm.Value) (*jvm.Object, error) {
	if len(arguments) != 2 {
		return nil, fmt.Errorf("%s expected receiver and listener, got %d arguments", method, len(arguments))
	}
	return arguments[1].Reference()
}

// runtimeDisplayGrabKey routes one key to a listener instead of the card
// stack, which is how a WIPI application takes over a hardware key.
func runtimeDisplayGrabKey(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) != 3 {
		return jvm.VoidValue(), fmt.Errorf("Display.grabKey expected receiver, key, and listener, got %d arguments", len(arguments))
	}
	key, err := arguments[1].Int32()
	if err != nil {
		return jvm.VoidValue(), err
	}
	listener, err := arguments[2].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if listener == nil {
		delete(runtime.grabbedKeys, key)
		return jvm.VoidValue(), nil
	}
	if len(runtime.grabbedKeys) >= maxJletListeners {
		return jvm.VoidValue(), fmt.Errorf("KTF grabbed key count exceeds %d", maxJletListeners)
	}
	runtime.grabbedKeys[key] = listener
	return jvm.VoidValue(), nil
}

func runtimeDisplayUngrabKey(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) != 2 {
		return jvm.VoidValue(), fmt.Errorf("Display.ungrabKey expected receiver and key, got %d arguments", len(arguments))
	}
	key, err := arguments[1].Int32()
	if err != nil {
		return jvm.VoidValue(), err
	}
	delete(runtime.grabbedKeys, key)
	return jvm.VoidValue(), nil
}

// runtimeDisplaySetDockedCard records the card docked below the annunciator.
// Nothing composites it yet, so it is only observable through getDockedCard.
func runtimeDisplaySetDockedCard(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) != 3 {
		return jvm.VoidValue(), fmt.Errorf("Display.setDockedCard expected receiver, card, and position, got %d arguments", len(arguments))
	}
	card, err := arguments[1].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	runtime.dockedCard = card
	return jvm.VoidValue(), nil
}

func runtimeDisplayDockedCard(runtime *initializationRuntime, _ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	return jvm.ReferenceValue(runtime.dockedCard), nil
}

// runtimeDisplayKeyName answers the printable name of a WIPI key code, using
// the same names the original runtime reports.
func runtimeDisplayKeyName(_ *initializationRuntime, vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) != 1 {
		return jvm.VoidValue(), fmt.Errorf("Display.getKeyName expected a key code, got %d arguments", len(arguments))
	}
	key, err := arguments[0].Int32()
	if err != nil {
		return jvm.VoidValue(), err
	}
	name := ""
	switch key {
	case KeyUp:
		name = "UP"
	case KeyDown:
		name = "DOWN"
	case KeyLeft:
		name = "LEFT"
	case KeyRight:
		name = "RIGHT"
	case KeyFire:
		name = "FIRE"
	case KeyLeftSoft:
		name = "SOFT1"
	case KeyRightSoft:
		name = "SOFT2"
	case KeyCall:
		name = "SEND"
	case KeyHangup:
		name = "END"
	case KeyClear:
		name = "CLEAR"
	case KeyVolumeUp:
		name = "VOLUP"
	case KeyVolumeDown:
		name = "VOLDOWN"
	default:
		if key >= KeyNum0 && key <= KeyNum9 || key == KeyHash || key == KeyStar {
			name = string(rune(key))
		}
	}
	return jvm.ReferenceValue(vm.NewString(name)), nil
}

// runtimeCardConstructorDisplayBounds initializes a card placed at explicit
// bounds on a given display, with the optional transparency flag of the wider
// overload.
func runtimeCardConstructorDisplayBounds(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) != 6 && len(arguments) != 7 {
		return jvm.VoidValue(), fmt.Errorf("Card constructor expected receiver, display, and bounds, got %d arguments", len(arguments))
	}
	bounds := make([]int32, 4)
	for index := range bounds {
		value, err := arguments[index+2].Int32()
		if err != nil {
			return jvm.VoidValue(), err
		}
		bounds[index] = value
	}
	transparent := int32(0)
	if len(arguments) == 7 {
		value, err := arguments[6].Int32()
		if err != nil {
			return jvm.VoidValue(), err
		}
		transparent = value
	}
	return runtimeCardInitialize(runtime, arguments, bounds[0], bounds[1], bounds[2], bounds[3], transparent)
}

func runtimeCardMove(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	return runtimeCardSetPair("Card.move", arguments, "x:I", "y:I")
}

func runtimeCardResize(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	return runtimeCardSetPair("Card.resize", arguments, "w:I", "h:I")
}

func runtimeCardSetPair(method string, arguments []jvm.Value, first, second string) (jvm.Value, error) {
	if len(arguments) != 3 {
		return jvm.VoidValue(), fmt.Errorf("%s expected receiver and two values, got %d arguments", method, len(arguments))
	}
	receiver, err := arguments[0].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if receiver == nil {
		return jvm.VoidValue(), fmt.Errorf("%s receiver is null", method)
	}
	if receiver.Fields == nil {
		receiver.Fields = make(map[string]jvm.Value)
	}
	receiver.Fields[first] = arguments[1]
	receiver.Fields[second] = arguments[2]
	return jvm.VoidValue(), nil
}

// runtimeCardIsShown reports whether the card is the top of the display stack,
// which is the card the paint service draws.
func runtimeCardIsShown(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) != 1 {
		return jvm.VoidValue(), fmt.Errorf("Card.isShown expected receiver, got %d arguments", len(arguments))
	}
	receiver, err := arguments[0].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if len(runtime.displayCards) == 0 || receiver == nil {
		return jvm.IntValue(0), nil
	}
	if runtime.displayCards[len(runtime.displayCards)-1] == receiver {
		return jvm.IntValue(1), nil
	}
	return jvm.IntValue(0), nil
}
