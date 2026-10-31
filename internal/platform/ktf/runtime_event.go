package ktf

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/movingwoo/wfeature/internal/jvm"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
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

	// eventQueueSlots is the int[] length one event occupies.
	eventQueueSlots = 4
	// eventLoopFrameMillis is how long an idle getNextEvent waits, matching the
	// original runtime's frame-paced event wait.
	eventLoopFrameMillis = 16
)

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
			{class: class, name: "getCurrentMode", descriptor: "()I", accessFlags: 0x0001, implementation: runtimeComponentField(inputMethodModeField)},
			{class: class, name: "setCurrentMode", descriptor: "(I)Z", accessFlags: 0x0001, implementation: runtimeInputMethodSetMode},
			// The listener the handler hands its characters to, and the key
			// that makes it hand one over. A title that draws its own field
			// forwards its keys here; see runtime_input_method.go. A text
			// component's own keys do not come this way — its card hands them
			// to keyNotify, which edits the component's string — and the
			// specification says a handler with no listener refuses every key,
			// so a title that could not register one has been told its own
			// input will never work.
			{class: class, name: "setInputMethodListener", descriptor: "(Lorg/kwis/msp/lcdui/InputMethodListener;)V", accessFlags: 0x0001, implementation: runtimeInputMethodSetListener},
			{class: class, name: "notifyKeyInput", descriptor: "(II)Z", accessFlags: 0x0011, implementation: runtimeInputMethodNotifyKeyInput},
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
	runtime.GuestEventLoop = true
	event, ok := runtime.NextGuestEvent()
	if !ok {
		// An empty queue is the frame-paced wait the original runtime blocks
		// in, so the worker parks for one frame rather than returning a redraw
		// request as fast as the Host can ask for one.
		if err := runtime.sleepCurrentWorker(eventLoopFrameMillis * time.Millisecond); err != nil {
			return jvm.VoidValue(), err
		}
		if event, ok = runtime.NextGuestEvent(); !ok {
			event = wipicrt.GuestEvent{Kind: wipicrt.EventKindRepaint}
		}
	}
	// Counted by kind rather than under a name composed here: this runs once
	// per event the game receives, which is the frequency diagEvent exists for.
	runtime.RecordDiagnostic(wipicrt.DiagEvent{Kind: wipicrt.DiagGuestEvent, Nums: [5]uint32{uint32(event.Kind)}})
	return jvm.VoidValue(), storeGuestEvent(array, event)
}

func storeGuestEvent(array *jvm.Object, event wipicrt.GuestEvent) error {
	values := []jvm.Value{
		jvm.IntValue(event.Kind),
		jvm.IntValue(event.Param1),
		jvm.IntValue(event.Param2),
		jvm.IntValue(event.Param3),
	}
	if _, length, ok := jvm.ArrayComponent(array); ok && length < len(values) {
		values = values[:length]
	}
	return jvm.SetArrayRange(array, 0, values)
}

func loadGuestEvent(array *jvm.Object) (wipicrt.GuestEvent, error) {
	_, values, err := jvm.ArraySnapshot(array)
	if err != nil {
		return wipicrt.GuestEvent{}, err
	}
	if len(values) < eventQueueSlots {
		return wipicrt.GuestEvent{}, fmt.Errorf("KTF event array holds %d slots, need %d", len(values), eventQueueSlots)
	}
	var event wipicrt.GuestEvent
	fields := []*int32{&event.Kind, &event.Param1, &event.Param2, &event.Param3}
	for index, field := range fields {
		value, err := values[index].Int32()
		if err != nil {
			return wipicrt.GuestEvent{}, err
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
	return jvm.VoidValue(), runtime.dispatchGuestEvent(vm, event)
}

// dispatchGuestEvent delivers one queued event, whoever took it off the queue.
// A game that drives its own loop reaches it through EventQueue.dispatchEvent
// and a Host-driven one through Client.ServiceEvents; both owe the event the
// same delivery.
func (runtime *initializationRuntime) dispatchGuestEvent(vm *jvm.VM, event wipicrt.GuestEvent) error {
	switch event.Kind {
	case wipicrt.EventKindKey:
		return runtime.dispatchKeyToCards(event.Param1, event.Param2)
	case wipicrt.EventKindPointer:
		// The specification's table: event[1] is the pointer event type and
		// event[2] and event[3] are its screen coordinates.
		return runtime.dispatchPointerToCards(event.Param1, event.Param2, event.Param3)
	case wipicrt.EventKindRepaint:
		// The redraw request has been taken off the queue, so the dirty flag
		// the Host-driven path uses is satisfied by this paint.
		runtime.RepaintPending = false
		_, err := runtime.paintTopCard()
		return err
	case wipicrt.EventKindNotify:
		return runtime.dispatchNotifyEvent(vm, event)
	default:
		if event.Kind >= 0x5000 {
			return runtime.dispatchNotifyEvent(vm, wipicrt.GuestEvent{Kind: wipicrt.EventKindNotify, Param1: event.Kind, Param2: event.Param1, Param3: event.Param2})
		}
		message := fmt.Sprintf("invalid event queue event type %d", event.Kind)
		return &jvm.GuestException{
			Object:  &jvm.Object{ClassName: "java/lang/IllegalArgumentException", Native: message},
			Message: message,
		}
	}
}

func (runtime *initializationRuntime) dispatchNotifyEvent(vm *jvm.VM, event wipicrt.GuestEvent) error {
	// A listener may add or remove listeners while it is notified, and a
	// removal shifts the list in place, so the event goes to the listeners
	// registered when it was dispatched.
	for _, listener := range slices.Clone(runtime.jletListeners) {
		if listener == nil {
			continue
		}
		if _, err := vm.InvokeVirtual(listener, "notifyEvent", "(III)V",
			jvm.IntValue(event.Param1), jvm.IntValue(event.Param2), jvm.IntValue(event.Param3)); err != nil {
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
	runtime.PostGuestEvent(event)
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
	runtime.PostGuestEvent(event)
	return jvm.VoidValue(), nil
}

// dispatchKeyToCards delivers one key event from the top pushed card down.
// A card's keyNotify returning true propagates to the card below it, matching
// the original runtime's card stack traversal.
func (runtime *initializationRuntime) dispatchKeyToCards(eventType, key int32) error {
	// A title's own field is open while the title hands it the presses it
	// receives. A press it does not see — fire that confirms, clear on an empty
	// field — closed it. The Host's own carriers are judged by their senders.
	if eventType == wipicrt.KeyPressed && len(runtime.CInput.Pending) == 0 && runtime.javaInput.pending == nil {
		// A press the player made can bring a field up; see
		// cInputState.unreached.
		runtime.CInput.Unreached = false
		runtime.javaInput.touched, runtime.javaInput.keyTaken = false, false
		defer func() {
			if !runtime.javaInput.touched {
				runtime.unseenPress()
			}
			runtime.javaInput.keyTaken = false
		}()
	}
	// External key actions invalidate snapshots even if the C widget does not
	// call its input method (for example, dismissing the name dialog).
	if len(runtime.CInput.Pending) == 0 && eventType != wipicrt.KeyReleased {
		runtime.CInput.DiscardCarrier = false
		runtime.CInput.ClearPending = key == wipicrt.KeyClear && (runtime.CInput.Active || runtime.CInput.ClearPending) && runtime.CInput.TimerCallback != 0
		runtime.CInput.Revision++
		activations, calls := runtime.CInput.Activations, runtime.CInput.Calls
		defer func() {
			// CLR edits the current field even when its only IM call flushes
			// composition. A recognized controller can also prove that an
			// ignored confirmation left the editor open. Other callers still
			// need input-method activity to establish continued editing.
			if runtime.CInput.Activations == activations && !(key == wipicrt.KeyClear && runtime.CInput.Calls > calls) {
				runtime.CInput.Active = runtime.CInput.Owner.Current(runtime.Runtime)
			}
		}()
	}

	if listener := runtime.grabbedKeys[key]; listener != nil {
		if _, err := runtime.client.vm.InvokeVirtual(listener, "notifyEvent", "(III)V",
			jvm.IntValue(wipicrt.EventKindKey), jvm.IntValue(eventType), jvm.IntValue(key)); err != nil {
			return fmt.Errorf("notify KTF grabbed key listener %s: %w", listener.ClassName, err)
		}
		return nil
	}
	if handled, err := runtime.dispatchKeyToVendorForm(eventType, key); err != nil || handled {
		return err
	}
	// A handler can pop, remove or push cards before it asks for the event to
	// go on, so the walk is over the stack as the event found it, and a card
	// already taken off it is passed over.
	cards := slices.Clone(runtime.displayCards)
	for index := len(cards) - 1; index >= 0; index-- {
		card := cards[index]
		if !slices.Contains(runtime.displayCards, card) {
			continue
		}
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
	// The walk keys take; see dispatchKeyToCards.
	cards := slices.Clone(runtime.displayCards)
	for index := len(cards) - 1; index >= 0; index-- {
		card := cards[index]
		if !slices.Contains(runtime.displayCards, card) {
			continue
		}
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

// repaintQueued reports whether the guest has asked for a repaint that this
// Host still owes it. A serial Runnable defers to one, because on the original
// event loop the two share a queue and the repaint was posted first.
func (runtime *initializationRuntime) repaintQueued() bool {
	return runtime != nil && runtime.RepaintPending && len(runtime.displayCards) > 0
}

// guestPaintOwnershipRounds delays automatic painting after a guest paint
// when no live worker owns the card. Worker ownership has no round timeout.
const guestPaintOwnershipRounds = 8

// paintTopCard paints the top pushed card into the screen framebuffer and
// presents the frame. It reports whether a card painted. Re-entrant paints are
// dropped: a card that repaints while painting would otherwise recurse.
func (runtime *initializationRuntime) paintTopCard() (bool, error) {
	if len(runtime.displayCards) == 0 {
		return false, nil
	}
	if runtime.RepaintServicing {
		return false, nil
	}
	// **A title that published its own frame keeps it for that round.** One
	// title draws its screen from its own loop — three pictures and a flush —
	// and its `paintClet` does nothing but fill and flush, which is what a
	// handset would run only when something asked for a repaint. Painting its
	// card every tick regardless put that fill over the picture once a frame,
	// and the title showed a uniform screen for as long as it was left running.
	//
	// The flag is cleared here rather than left standing, so this skips the one
	// round after a flush the title made rather than deciding for the rest of
	// the run. A title that publishes every frame is skipped every round; a
	// title that flushed once during its start loses one paint and gets the
	// next. Nothing has to be classified in advance, which is what makes this a
	// rule rather than a switch.
	if !runtime.RepaintPending && runtime.GuestFlushedOwnFrame {
		runtime.GuestFlushedOwnFrame = false
		return false, nil
	}
	// **A title painting its own frames gets the frames it asked for and no
	// others.** A title pairing `Card.repaint` with `Card.serviceRepaints`
	// every frame is naming its own cadence, and the Host's unconditional
	// round paint is then a second frame per frame. That is not just a wasted
	// paint: a title whose frame loop steps the world inside `paint` takes a
	// step it never asked for. See runtimeCardServiceRepaints.
	//
	// A live worker that requests or services this card owns its frame cadence.
	// A rearmed C timer that requests repaint has the same ownership. Counting
	// idle Host rounds cannot establish that it stopped: a slow scene can
	// leave a long gap between requested paints. An extra paint in that gap
	// can clear the dirty flag before the worker advances the next scene stage.
	// Explicit repaint requests remain eligible, as do other cards and cards
	// whose worker has returned.
	if !runtime.RepaintPending {
		card := runtime.topCard()
		for _, runnable := range runtime.pendingSerial {
			if runtime.serialPaintOwners[runnable] == card {
				return false, nil
			}
		}
		for _, timer := range runtime.PendingTimers {
			if guestObject(timer.PaintedCard) == card {
				return false, nil
			}
		}
		for _, worker := range runtime.client.workers {
			if worker.paintedCard == card {
				return false, nil
			}
		}
	}
	// **The round paint is still the only thing driving some titles.** When
	// no live worker owns the card, a bootstrap paint only delays this fallback.
	// Some titles service a load screen two or three times, then depend on
	// automatic painting. Keep their existing eight-round grace period here;
	// a long gap alone must not revoke a live worker's ownership above.
	runtime.roundsSinceGuestPaint++
	if !runtime.RepaintPending && runtime.guestHasPainted &&
		runtime.roundsSinceGuestPaint <= guestPaintOwnershipRounds {
		return false, nil
	}
	// The paint satisfies whatever repaint request is outstanding, whichever
	// path asked for it. Leaving the flag set would hold the serial queue for
	// ever, because the only other things that clear it are the guest's own
	// event drain and serviceRepaints.
	runtime.RepaintPending = false
	card := runtime.displayCards[len(runtime.displayCards)-1]
	graphics, err := runtime.newScreenGraphics()
	if err != nil {
		return false, err
	}
	if err := runtime.ensureResultBound(graphics); err != nil {
		return false, err
	}
	runtime.RepaintServicing = true
	defer func() { runtime.RepaintServicing = false }()
	finish, err := runtime.BeginCardPaint()
	if err != nil {
		return false, err
	}
	runtime.beginTextReads()
	_, err = runtime.client.vm.InvokeVirtual(card, "paint", "(Lorg/kwis/msp/lcdui/Graphics;)V", jvm.ReferenceValue(graphics))
	runtime.endTextReads(card, err == nil)
	if err != nil {
		return true, fmt.Errorf("paint KTF card %s: %w", card.ClassName, err)
	}
	return true, finish()
}

// inputMethodListenerField is the listener a handler was given.
const inputMethodListenerField = "listener:Lorg/kwis/msp/lcdui/InputMethodListener;"

// Host-only history invalidates edits even when a listener is restored to null.
const inputMethodListenerRevisionField = "host:input-listener-revision"

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
	receiver.Fields[inputMethodModeField] = arguments[1]
	receiver.Fields[inputMethodConstraintField] = arguments[1]
	receiver.Fields[inputMethodTitleOwnedField] = jvm.IntValue(1)
	return jvm.VoidValue(), nil
}

// runtimeInputMethodSetMode records the requested input mode and accepts it.
// There is no on-device input method to switch: text arrives through the Host's
// text input, so every mode is equally available. A title switches the mode of
// an open field from a soft key it does not otherwise forward, so the switch
// is also the field still being open.
func runtimeInputMethodSetMode(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
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
		receiver.Fields[inputMethodModeField] = arguments[1]
		runtime.touchInputMethod(receiver, false)
		if owner, _ := receiver.Fields[inputMethodOwnerField].Reference(); owner != nil {
			owner.Fields[componentModeChangedField] = jvm.IntValue(1)
			if err := runtime.publishTextComponent(owner); err != nil {
				return jvm.VoidValue(), err
			}
		}
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
	if runtime.CurrentContext == nil {
		return jvm.VoidValue(), fmt.Errorf("Main.main has no active guest context")
	}
	loaded, err := runtime.client.loadClassLocked(runtime.CurrentContext, className)
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
	runtime.CountDiagnostic("Jlet.notifyDestroyed")
	// Which of the two endings this was is worth carrying: a Java title
	// declaring itself finished and a native one calling MC_knlExit reach a
	// Host as the same error, and the two are looked into differently.
	return jvm.VoidValue(), fmt.Errorf("Jlet.notifyDestroyed: %w", ErrGuestExited)
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
	case wipicrt.KeyUp:
		name = "UP"
	case wipicrt.KeyDown:
		name = "DOWN"
	case wipicrt.KeyLeft:
		name = "LEFT"
	case wipicrt.KeyRight:
		name = "RIGHT"
	case wipicrt.KeyFire:
		name = "FIRE"
	case wipicrt.KeyLeftSoft:
		name = "SOFT1"
	case wipicrt.KeyRightSoft:
		name = "SOFT2"
	case wipicrt.KeyCall:
		name = "SEND"
	case wipicrt.KeyHangup:
		name = "END"
	case wipicrt.KeyClear:
		name = "CLEAR"
	case wipicrt.KeyVolumeUp:
		name = "VOLUP"
	case wipicrt.KeyVolumeDown:
		name = "VOLDOWN"
	default:
		if key >= wipicrt.KeyNum0 && key <= wipicrt.KeyNum9 || key == wipicrt.KeyHash || key == wipicrt.KeyStar {
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
	if runtime.cardIsShown(receiver) {
		return jvm.IntValue(1), nil
	}
	return jvm.IntValue(0), nil
}

// cardIsShown reports whether a card is the one the display is showing, which
// is the top of the pushed stack. It is what `Card.isShown` answers and what
// decides whether a paint of that card would reach the screen.
func (runtime *initializationRuntime) cardIsShown(card *jvm.Object) bool {
	if card == nil || len(runtime.displayCards) == 0 {
		return false
	}
	return runtime.displayCards[len(runtime.displayCards)-1] == card
}
