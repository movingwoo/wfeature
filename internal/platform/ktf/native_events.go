package ktf

import (
	"context"
	"encoding/binary"
	"fmt"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// The title does not run in a loop of its own. Its start event ends by handing
// the platform an interval, a function and a context, and everything after
// that happens inside that function: it reads the clock, steps its own state
// machine, draws, and asks to be called again. So a Host that only starts the
// title sees it load its data and stop; the frame is what makes it run.
//
// Beside the frame it registers two listeners, one on each of the two event
// sources it queries. One of them clears the flag its own modal wait spins on,
// which is what says those sources carry events the title waits for rather
// than events it merely observes. What each source carries is not established,
// so nothing is delivered through them yet. See docs/ktf.md.

// The title's own object is what takes input. Its handler switches on the
// event number, and reading that switch is what named the numbers: 0, 1, 2 and
// 3 are small, and above them it compares against 0x101, 0x102 and 0x404. The
// first two store a code and hand it to the current screen, so they are a key
// pressed and a key released.
//
// The code space came out of the screen that acts on it. The title's own key
// handler compares against 0xe030 and 0xe035, and driving those two moves its
// menus, so a key is 0xE000 with a character in the low byte: the keypad's
// digits are their own characters, and a handset's five-way pad is the digits
// around the 5 — 2 up, 8 down, 4 left, 6 right, 5 the one that selects. That
// is what a run proves rather than what a table says: pressing 0xe035 on the
// title screen opens its menu, 0xe034 moves the selection along it, and 0xe035
// again enters the game.
const (
	// nativeEventStart runs the title's own start-up.
	nativeEventStart = 0
	// nativeEventKeyDown and nativeEventKeyUp carry a key code in the event's
	// first parameter.
	nativeEventKeyDown = 0x101
	nativeEventKeyUp   = 0x102
	// nativeEventKeyTyped carries the same code and is a **third** event, not
	// another name for the first. A later module's handler keeps all three
	// apart and puts them in different places: the down sets a pressed flag and
	// a code, the up clears them, and this one writes a code into a slot of its
	// own. Which one a screen reads is the screen's business — the title screen
	// of one local title reads only this one, so a platform that sends the pair
	// and not this one has a title that takes every key and acts on none of
	// them. The 2005 module does not handle it at all, which costs nothing: an
	// event its dispatch does not know answers zero and does nothing.
	nativeEventKeyTyped = 0x100

	// nativeKeyBase is the high half of every key code the title acts on.
	nativeKeyBase = 0xe000
)

// A later module does not register a frame at all. Its start-up ends by asking
// the object the entry was handed to **post an event to itself**:
//
//	ldr  r2, [r0, #8]         ; a parameter of its own
//	adds r3, r6, #0           ; 0x7009, an event number of the title's own
//	ldr  r0, [r0, #0x10]      ; the object
//	ldr  r4, [r1, #0x54]
//	movs r1, #0               ; no flags
//	bl   veneer               ; (this, flags, class, event, w, dw)
//
// The class it names is **its own** — the ClassID its information file carries
// — and the event is delivered back to the same handler a key is delivered to.
// So this generation drives itself by posting rather than by being called back
// on an interval, and a platform that drops the post leaves a title that has
// finished starting up and has nothing to do next.
//
// The post is a queue rather than a call: BREW's own is asynchronous, the
// module posts from inside the handler that is running, and delivering it
// there would re-enter the handler on its own stack. Tick drains it.
const (
	// nativeObjectPostEvent takes flags, a class, an event and two parameters.
	nativeObjectPostEvent = 0x54
	// maxNativePostedEvents bounds the queue. A title that posts to itself
	// every time it handles a post is a title in a loop, and a bound is what
	// turns that into a report rather than memory that never comes back.
	maxNativePostedEvents = 1024
)

// nativeObjectResume takes a function and a context and no interval, which is
// what separates it from the schedule beside it: the title asks to be called
// once more rather than every so many milliseconds. It is delivered on the
// next pass and not re-armed — a title that wants another asks again, and the
// local one does.
const nativeObjectResume = 0x30

// nativeResume is one of those calls.
type nativeResume struct {
	Function uint32
	Context  uint32
}

// resume queues one.
func (platform *NativePlatform) resume(thread *armcore.Thread) (uint32, error) {
	arguments, err := nativeArguments(thread, 3)
	if err != nil {
		return 0, err
	}
	if arguments[1] == 0 {
		return 0, nil
	}
	if len(platform.resumes) >= maxNativePostedEvents {
		return 0, fmt.Errorf("KTF native title has asked to be resumed %d times without being run", len(platform.resumes))
	}
	platform.resumes = append(platform.resumes, nativeResume{Function: arguments[1], Context: arguments[2]})
	return 1, nil
}

// deliverResumes runs what the title asked to have run once more. Like the
// posted events the queue is taken whole first, because the function it runs
// is the one that asks for the next one.
func (platform *NativePlatform) deliverResumes(ctx context.Context) error {
	if len(platform.resumes) == 0 {
		return nil
	}
	queued := platform.resumes
	platform.resumes = nil
	for _, call := range queued {
		if _, err := platform.client.CallExport(ctx, call.Function, []uint32{call.Context}); err != nil {
			return fmt.Errorf("KTF native resume at %#x: %w", call.Function, err)
		}
	}
	return nil
}

// nativePostedEvent is one event the title sent itself.
type nativePostedEvent struct {
	Class uint32
	Event uint32
	First uint32
	Secon uint32
}

// postEvent queues one. The class is carried rather than checked: a module of
// this generation names its own, and one that names another is a fact worth
// having in the report rather than a call to drop silently.
func (platform *NativePlatform) postEvent(thread *armcore.Thread) (uint32, error) {
	arguments, err := nativeArguments(thread, 4)
	if err != nil {
		return 0, err
	}
	// The fifth and sixth arguments are on the guest stack, where the calling
	// standard puts everything past the fourth.
	stack, err := thread.Register(armcore.RegisterSP)
	if err != nil {
		return 0, err
	}
	spilled := make([]byte, 8)
	if err := platform.client.core.Memory().Read(stack, spilled); err != nil {
		return 0, fmt.Errorf("read the parameters of a posted event at %#x: %w", stack, err)
	}
	if len(platform.posted) >= maxNativePostedEvents {
		return 0, fmt.Errorf("KTF native title has posted %d events without handling them", len(platform.posted))
	}
	platform.posted = append(platform.posted, nativePostedEvent{
		Class: arguments[2],
		Event: arguments[3],
		First: binary.LittleEndian.Uint32(spilled),
		Secon: binary.LittleEndian.Uint32(spilled[4:]),
	})
	return 1, nil
}

// deliverPosted hands the title back what it posted to itself, in order.
//
// The queue is taken whole before anything is delivered: a handler that posts
// while it is being run is asking to be run again, and running it inside this
// loop would be a frame that never ends rather than a title that carries on.
func (platform *NativePlatform) deliverPosted(ctx context.Context) error {
	if len(platform.posted) == 0 || platform.application == 0 {
		return nil
	}
	queued := platform.posted
	platform.posted = nil
	for _, event := range queued {
		if _, err := platform.client.SendEvent(ctx, platform.application, event.Event, event.First, event.Secon); err != nil {
			return fmt.Errorf("KTF native posted event %#x: %w", event.Event, err)
		}
	}
	return nil
}

// NativeKey is the code for one handset character. The digits are the keypad,
// and the five-way pad is the ring around the 5.
func NativeKey(character byte) uint32 { return nativeKeyBase | uint32(character) }

// The keys a run has proved the title acts on.
const (
	NativeKeyUp     = nativeKeyBase | '2'
	NativeKeyDown   = nativeKeyBase | '8'
	NativeKeyLeft   = nativeKeyBase | '4'
	NativeKeyRight  = nativeKeyBase | '6'
	NativeKeySelect = nativeKeyBase | '5'
	// NativeKeyClear is what the title's menus label CLR and treat as back.
	// Three codes do that — this one, 0xE021 and 0xE02B — and this is the one
	// the module names most often.
	NativeKeyClear = nativeKeyBase | '0'
)

// nativeKeyDigitBase is the code for the 0 key; the digits run up from it.
// The title's menu is what establishes it: its numbered items answer to
// consecutive codes from 0xE022 upwards, and the item numbered 1 is the first
// of them.
const nativeKeyDigitBase = 0xe021

// Key delivers one key to the title. A Host sends the press when a key goes
// down and the release when it comes up; a probe driving a route sends both.
func (platform *NativePlatform) Key(ctx context.Context, code uint32, pressed bool) error {
	if platform.application == 0 {
		return fmt.Errorf("KTF native key %#x before the application was created", code)
	}
	events := []uint32{nativeEventKeyUp}
	if pressed {
		// The press first and the typed key after it, which is the order the
		// two mean: one says the key is down and the other that it was struck.
		events = []uint32{nativeEventKeyDown, nativeEventKeyTyped}
	}
	for _, event := range events {
		if _, err := platform.client.SendEvent(ctx, platform.application, event, code, 0); err != nil {
			return fmt.Errorf("KTF native key %#x: %w", code, err)
		}
	}
	return nil
}

// nativeKeyCode translates the WIPI key values every Host here already speaks
// into the codes this title acts on.
//
// The pad is what a run proves: the select key opens the title's menu, and the
// two keys that move a selection along it are the ones a phone keypad puts
// beside the 5. The digits are a second block, and the menu is what names it —
// each of its numbered items answers to one code, in order, so the digit keys
// run from 0 at 0xE021 rather than from their own characters.
//
// A value with no code here is dropped by the caller rather than sent as
// itself; see NativeSession.SendKey.
func nativeKeyCode(key int32) (uint32, bool) {
	switch key {
	case KeyUp:
		return NativeKeyUp, true
	case KeyDown:
		return NativeKeyDown, true
	case KeyLeft:
		return NativeKeyLeft, true
	case KeyRight:
		return NativeKeyRight, true
	case KeyFire:
		return NativeKeySelect, true
	case KeyClear:
		return NativeKeyClear, true
	}
	if key >= KeyNum0 && key <= KeyNum9 {
		return nativeKeyDigitBase + uint32(key-KeyNum0), true
	}
	return 0, false
}

// NativeListener is one function the module registered on an event source,
// with the context it wants handed back and the interface it registered on.
// Which source a listener belongs to is what decides who it hears from: the
// module registers one on the player and one on the timed service, and each
// only recognises the events of its own.
type NativeListener struct {
	Source   uint32
	Function uint32
	Context  uint32
}

// listenerFor finds what the module registered on one source.
func (platform *NativePlatform) listenerFor(source uint32) (NativeListener, bool) {
	for _, listener := range platform.listeners {
		if listener.Source == source {
			return listener, true
		}
	}
	return NativeListener{}, false
}

// The timed service the title starts with a duration and then waits on. Its
// listener clears the wait when it reports 2 or 3, so the platform has to
// report one of them when the time is up; a platform that never does leaves
// the title waiting for something that already finished.
const (
	// nativeTimedStart takes a duration in milliseconds.
	nativeTimedStart = 0x28
	// nativeTimedStop ends it early, which is what the module calls when it
	// gives up waiting.
	nativeTimedStop = 0x2c
	// nativeTimedEventKind and nativeTimedEventEnd are what its listener
	// recognises. The kind is the same one the player uses; the code is the
	// lower of the two the listener accepts.
	nativeTimedEventKind = 1
	nativeTimedEventEnd  = 2
)

// installTimed registers the service the title waits on.
func (platform *NativePlatform) installTimed() {
	surface := nativeInterfaceSurface(nativeInterfaceTimed)
	platform.client.Serve(surface, nativeTimedStart, platform.startTimed)
	platform.client.Serve(surface, nativeTimedStop, platform.stopTimed)
}

// startTimed records when the title should be told the time is up.
func (platform *NativePlatform) startTimed(thread *armcore.Thread) (uint32, error) {
	milliseconds, err := thread.Register(1)
	if err != nil {
		return 0, err
	}
	platform.timedDue = platform.clock.Now().Add(time.Duration(milliseconds) * time.Millisecond)
	platform.timedRunning = true
	return 1, nil
}

// stopTimed cancels it.
func (platform *NativePlatform) stopTimed(*armcore.Thread) (uint32, error) {
	platform.timedRunning = false
	return 1, nil
}

// advanceTimed tells the title when its wait is over.
func (platform *NativePlatform) advanceTimed(ctx context.Context) error {
	if !platform.timedRunning || platform.clock.Now().Before(platform.timedDue) {
		return nil
	}
	platform.timedRunning = false
	listener, ok := platform.listenerFor(nativeInterfaceTimed)
	if !ok {
		return nil
	}
	if _, err := platform.Deliver(ctx, listener, nativeTimedEventKind, nativeTimedEventEnd); err != nil {
		return fmt.Errorf("KTF native timed service ended: %w", err)
	}
	return nil
}

// Listeners reports what the module registered, in order.
func (platform *NativePlatform) Listeners() []NativeListener { return platform.listeners }

// addListener records a registration against the source it was made on. The
// module registers before anything is delivered, so recording is all this has
// to do to be correct.
func (platform *NativePlatform) addListener(source uint32) NativeSlotHandler {
	return func(thread *armcore.Thread) (uint32, error) {
		arguments, err := nativeArguments(thread, 3)
		if err != nil {
			return 0, err
		}
		platform.listeners = append(platform.listeners, NativeListener{
			Source:   source,
			Function: arguments[1],
			Context:  arguments[2],
		})
		return 1, nil
	}
}

// Deliver calls one registered listener with two values, which is how a probe
// finds out what a source carries.
func (platform *NativePlatform) Deliver(ctx context.Context, listener NativeListener, first, second uint32) (uint32, error) {
	if listener.Function == 0 {
		// A source with nothing registered on it is not an error: the module
		// registers what it wants to hear from and leaves the rest alone.
		return 0, nil
	}
	return platform.client.CallExport(ctx, listener.Function, []uint32{listener.Context, first, second})
}

// nativeSchedule is the frame the title registered.
type nativeSchedule struct {
	interval time.Duration
	function uint32
	context  uint32
	due      time.Time
}

// schedule answers the platform object's scheduling call. The title re-arms it
// at the end of every frame, so this replaces rather than accumulates.
func (platform *NativePlatform) schedule(thread *armcore.Thread) (uint32, error) {
	arguments, err := nativeArguments(thread, 4)
	if err != nil {
		return 0, err
	}
	interval := time.Duration(arguments[1]) * time.Millisecond
	if interval <= 0 {
		// A zero interval is "as often as you can", which for a Host that
		// paces itself is one call per pass rather than a spin.
		interval = time.Millisecond
	}
	platform.frame = &nativeSchedule{
		interval: interval,
		function: arguments[2],
		context:  arguments[3],
		due:      platform.clock.Now().Add(interval),
	}
	return 1, nil
}

// Tick runs the title's frame if it is due, and reports whether it ran. A Host
// calls it in a loop; the interval the title asked for is what decides how
// often the frame actually runs, so a Host on a manual clock advances that
// clock rather than calling faster.
func (platform *NativePlatform) Tick(ctx context.Context) (bool, error) {
	// What the title posted to itself is delivered whether or not it has a
	// frame: a module of the later generation has no frame at all and this is
	// the whole of how it runs on.
	if err := platform.deliverPosted(ctx); err != nil {
		return false, err
	}
	if err := platform.deliverResumes(ctx); err != nil {
		return false, err
	}
	if platform.frame == nil || platform.frame.function == 0 {
		return len(platform.posted) > 0 || len(platform.resumes) > 0, nil
	}
	// The sound and the timed service are answered before the frame, so a
	// title that is waiting on one hears about it and then runs the frame that
	// acts on it, rather than a frame later.
	if err := platform.advanceSound(ctx); err != nil {
		return false, err
	}
	if err := platform.advanceTimed(ctx); err != nil {
		return false, err
	}
	// The schedule is read after those two and not before: both run the
	// title's own listeners, and a listener that re-arms replaces the schedule
	// rather than editing it. Holding the earlier pointer across them would
	// stamp the new schedule's due date onto a struct nothing points at any
	// more and then call the function the title has just moved on from.
	frame := platform.frame
	if frame == nil || frame.function == 0 {
		return false, nil
	}
	now := platform.clock.Now()
	if now.Before(frame.due) {
		return false, nil
	}
	frame.due = now.Add(frame.interval)
	if _, err := platform.client.CallExport(ctx, frame.function, []uint32{frame.context}); err != nil {
		return true, fmt.Errorf("KTF native frame at %#x: %w", frame.function, err)
	}
	// The end of a frame is a boundary a title's writes can be carried over:
	// one that keeps its save open never closes it, and waiting for a close
	// that does not come would leave the save in memory only.
	platform.FlushSaves()
	return true, nil
}

// SetSpeed scales how fast the title runs. The title paces itself by asking to
// be called back and then reading the clock, so what moves is that clock —
// see backend.SpeedClock, which every platform here scales time with.
func (platform *NativePlatform) SetSpeed(multiplier float64) {
	if platform == nil || platform.pace == nil {
		return
	}
	platform.pace.SetSpeed(multiplier)
}

// Speed reports the multiplier in force.
func (platform *NativePlatform) Speed() float64 {
	if platform == nil || platform.pace == nil {
		return 1
	}
	return platform.pace.Speed()
}

// NextFrame reports when the title's frame is next due, on the title's own
// clock. A Host waiting for it wants NativeSession.NextDeadline, which is the
// same instant on the Host's.
func (platform *NativePlatform) NextFrame() (time.Time, bool) {
	if platform.frame == nil || platform.frame.function == 0 {
		return time.Time{}, false
	}
	return platform.frame.due, true
}

// UntilNextFrame reports how long a Host should wait before ticking again. It
// is what is left of the interval the title asked for, and never negative: a
// frame that is already due is what a tick is for.
func (platform *NativePlatform) UntilNextFrame() time.Duration {
	due, ok := platform.NextFrame()
	if !ok {
		return 0
	}
	if wait := due.Sub(platform.clock.Now()); wait > 0 {
		return platform.pace.SourceDuration(wait)
	}
	return 0
}

// FrameInterval reports the interval the title asked to be called back at, or
// zero if it has not asked.
func (platform *NativePlatform) FrameInterval() time.Duration {
	if platform.frame == nil {
		return 0
	}
	return platform.frame.interval
}
