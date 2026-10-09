package ktf

import (
	"encoding/binary"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
	"github.com/movingwoo/wfeature/internal/testfixture"
)

type ktfWIPIListenerEvent struct {
	receiver, clip uint32
	event, parm    int32
}

type ktfWIPIListenerFixture struct {
	t         *testing.T
	archive   []byte
	session   *Session
	client    *Client
	runtime   *initializationRuntime
	clock     *ManualClock
	store     backend.SaveStore
	sink      *audioPauseProbe
	clip      *jvm.Object
	listeners [2]*jvm.Object
}

func newKTFWIPIListenerFixture(t *testing.T) *ktfWIPIListenerFixture {
	t.Helper()
	archive, err := testfixture.KTFListenerArchive()
	if err != nil {
		t.Fatal(err)
	}
	store, err := backend.NewMemorySaveStore(nil)
	if err != nil {
		t.Fatal(err)
	}
	clock := NewManualClock(time.Unix(1800000000, 0))
	sink := &audioPauseProbe{}
	session, err := StartSession(t.Context(), archive, SessionOptions{Clock: clock, SaveStore: store, AudioSink: sink})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(session.Close)
	fixture := &ktfWIPIListenerFixture{t: t, archive: archive, session: session, client: session.Client,
		runtime: session.Client.runtime, clock: clock, store: store, sink: sink}
	if count := binary.LittleEndian.Uint32(readTestBytes(t, fixture.client, testfixture.KTFListenerStartupCounter, 4)); count != 1 {
		t.Fatalf("fixture startup count = %d, want 1", count)
	}
	for index := range fixture.listeners {
		fixture.listeners[index], _, err = fixture.client.NewObject(t.Context(), testfixture.KTFListenerClass, "()V")
		if err != nil {
			t.Fatal(err)
		}
	}
	fixture.clip = newJavaAudioPauseClip(t, fixture.client, fixture.runtime)
	return fixture
}

func (fixture *ktfWIPIListenerFixture) setListener(listener *jvm.Object) {
	fixture.t.Helper()
	if _, err := fixture.client.JVM().InvokeVirtual(fixture.clip, "setListener", "(Lorg/kwis/msp/media/PlayListener;)V", jvm.ReferenceValue(listener)); err != nil {
		fixture.t.Fatal(err)
	}
}

func (fixture *ktfWIPIListenerFixture) call(method string, want bool, repeat ...bool) {
	fixture.t.Helper()
	var argument []int32
	if len(repeat) != 0 {
		argument = []int32{0}
		if repeat[0] {
			argument[0] = 1
		}
	}
	expected := int32(0)
	if want {
		expected = 1
	}
	javaAudioPauseCall(fixture.t, fixture.client, fixture.clip, "Clip", method, expected, argument...)
}

func (fixture *ktfWIPIListenerFixture) drain() int {
	fixture.t.Helper()
	delivered, err := fixture.client.ServiceEvents(fixture.t.Context())
	if err != nil {
		fixture.t.Fatal(err)
	}
	return delivered
}

func (fixture *ktfWIPIListenerFixture) advance(duration time.Duration) {
	fixture.t.Helper()
	fixture.clock.Advance(duration)
	if err := fixture.client.serviceAudio(); err != nil {
		fixture.t.Fatal(err)
	}
}

func (fixture *ktfWIPIListenerFixture) duration() time.Duration {
	fixture.t.Helper()
	progress, err := fixture.client.audio.Playback(fixture.runtime.clip(fixture.clip).handle, fixture.runtime.guestElapsed())
	if err != nil || progress.Length <= 0 {
		fixture.t.Fatalf("authored clip duration = %v, %v", progress.Length, err)
	}
	return progress.Length
}

func (fixture *ktfWIPIListenerFixture) history() []ktfWIPIListenerEvent {
	fixture.t.Helper()
	count := binary.LittleEndian.Uint32(readTestBytes(fixture.t, fixture.client, testfixture.KTFListenerHistoryCount, 4))
	if count > testfixture.KTFListenerHistoryCapacity {
		fixture.t.Fatalf("callback history count %d exceeds its authored bound", count)
	}
	data := readTestBytes(fixture.t, fixture.client, testfixture.KTFListenerHistory, int(count)*16)
	events := make([]ktfWIPIListenerEvent, count)
	for index := range events {
		record := data[index*16:]
		events[index] = ktfWIPIListenerEvent{binary.LittleEndian.Uint32(record), binary.LittleEndian.Uint32(record[4:]),
			int32(binary.LittleEndian.Uint32(record[8:])), int32(binary.LittleEndian.Uint32(record[12:]))}
	}
	return events
}

func (fixture *ktfWIPIListenerFixture) event(listener int, event int32) ktfWIPIListenerEvent {
	fixture.t.Helper()
	receiver, ok := fixture.client.JVM().AOTAddress(fixture.listeners[listener])
	if !ok {
		fixture.t.Fatal("listener has no guest address")
	}
	clip, ok := fixture.client.JVM().AOTAddress(fixture.clip)
	if !ok {
		fixture.t.Fatal("clip has no guest address")
	}
	return ktfWIPIListenerEvent{receiver: receiver, clip: clip, event: event}
}

func (fixture *ktfWIPIListenerFixture) wantHistory(want ...ktfWIPIListenerEvent) {
	fixture.t.Helper()
	if got := fixture.history(); !slices.Equal(got, want) {
		fixture.t.Fatalf("callback history = %+v, want %+v", got, want)
	}
}

func TestAuthoredKTFWIPIListenerCallbackABI(t *testing.T) {
	fixture := newKTFWIPIListenerFixture(t)
	var want []ktfWIPIListenerEvent
	for index := range testfixture.KTFListenerHistoryCapacity + 2 {
		listener := index % len(fixture.listeners)
		event, parm := int32(index+1), -int32(index+17)
		if _, err := fixture.client.InvokeVirtual(t.Context(), fixture.listeners[listener], "playUpdate", "(Lorg/kwis/msp/media/Clip;II)V",
			jvm.ReferenceValue(fixture.clip), jvm.IntValue(event), jvm.IntValue(parm)); err != nil {
			t.Fatal(err)
		}
		if index < testfixture.KTFListenerHistoryCapacity {
			record := fixture.event(listener, event)
			record.parm = parm
			want = append(want, record)
		}
	}
	fixture.wantHistory(want...)
}

func TestKTFWIPIListenerDeliversAfterNativeCall(t *testing.T) {
	fixture := newKTFWIPIListenerFixture(t)
	fixture.setListener(fixture.listeners[0])
	fixture.call("play", true, false)
	fixture.wantHistory()
	if got := fixture.drain(); got != 1 {
		t.Fatalf("ServiceEvents delivered %d callbacks, want 1", got)
	}
	fixture.wantHistory(fixture.event(0, 2))
	if got := fixture.drain(); got != 0 {
		t.Fatalf("second drain delivered %d callbacks, want none", got)
	}
}

func TestKTFWIPIListenerTransitionsAndNoops(t *testing.T) {
	fixture := newKTFWIPIListenerFixture(t)
	fixture.setListener(fixture.listeners[0])
	for _, method := range []string{"pause", "resume", "stop"} {
		fixture.call(method, false)
	}
	fixture.call("play", true, false)
	fixture.advance(fixture.duration() / 4)
	fixture.call("resume", false)
	fixture.call("pause", true)
	fixture.call("pause", false)
	fixture.advance(time.Hour)
	fixture.call("resume", true)
	fixture.call("resume", false)
	fixture.call("pause", true)
	fixture.call("stop", true)
	// A previously loaded idle clip still acknowledges stop; no STOP event
	// accompanies it because playback has already been cancelled.
	fixture.call("stop", true)
	fixture.call("pause", false)
	fixture.call("resume", false)
	fixture.wantHistory()
	fixture.drain()
	fixture.wantHistory(fixture.event(0, 2), fixture.event(0, 4), fixture.event(0, 5), fixture.event(0, 4), fixture.event(0, 3))
	fixture.advance(time.Hour)
	if got := fixture.drain(); got != 0 {
		t.Fatalf("cancelled clip emitted %d callbacks", got)
	}
	// Missing and undecodable data must not report a successful START.
	for _, bytes := range [][]byte{nil, []byte("authored invalid sound")} {
		if _, err := fixture.client.JVM().InvokeVirtual(fixture.clip, "setBuffer", "([BI)V",
			jvm.ReferenceValue(newByteArray(t, fixture.client, bytes)), jvm.IntValue(int32(len(bytes)))); err != nil {
			t.Fatal(err)
		}
		fixture.call("play", false, false)
	}
	if got := fixture.drain(); got != 0 {
		t.Fatalf("failed playback emitted %d callbacks", got)
	}
}

func TestKTFWIPIListenerNaturalPassesAndRestart(t *testing.T) {
	fixture := newKTFWIPIListenerFixture(t)
	fixture.setListener(fixture.listeners[0])
	fixture.call("play", true, true)
	length := fixture.duration()
	fixture.drain()
	fixture.advance(length - time.Millisecond)
	if got := fixture.drain(); got != 0 {
		t.Fatalf("pass ended early with %d callbacks", got)
	}
	fixture.advance(time.Millisecond)
	fixture.wantHistory(fixture.event(0, 2))
	fixture.drain()
	fixture.advance(2*length + length/4)
	fixture.drain()
	fixture.wantHistory(fixture.event(0, 2), fixture.event(0, 1), fixture.event(0, 1), fixture.event(0, 1))
	// A successful explicit play restarts even an already playing clip.
	fixture.call("play", true, false)
	fixture.call("play", true, false)
	fixture.drain()
	fixture.advance(length)
	fixture.drain()
	fixture.call("pause", false)
	fixture.call("resume", false)
	fixture.call("stop", true)
	fixture.advance(3 * length)
	if got := fixture.drain(); got != 0 {
		t.Fatalf("finished clip emitted %d duplicate callbacks", got)
	}
	fixture.wantHistory(fixture.event(0, 2), fixture.event(0, 1), fixture.event(0, 1), fixture.event(0, 1),
		fixture.event(0, 2), fixture.event(0, 2), fixture.event(0, 1))
}

func TestKTFWIPIListenerReplacementSnapshotsAndNullRemoval(t *testing.T) {
	fixture := newKTFWIPIListenerFixture(t)
	fixture.setListener(fixture.listeners[0])
	fixture.call("play", true, false)
	fixture.setListener(fixture.listeners[1])
	fixture.call("pause", true)
	fixture.setListener(nil)
	fixture.call("resume", true)
	fixture.call("stop", true)
	fixture.drain()
	fixture.wantHistory(fixture.event(0, 2), fixture.event(1, 4))
	fixture.call("play", true, false)
	fixture.advance(fixture.duration())
	fixture.setListener(fixture.listeners[1])
	if got := fixture.drain(); got != 0 {
		t.Fatalf("late listener received %d old transitions", got)
	}
	fixture.call("play", true, false)
	fixture.advance(fixture.duration())
	fixture.setListener(nil)
	fixture.drain()
	fixture.wantHistory(fixture.event(0, 2), fixture.event(1, 4), fixture.event(1, 2), fixture.event(1, 1))
}

func TestKTFWIPIListenerReentrantEndStartsOnNextDrain(t *testing.T) {
	fixture := newKTFWIPIListenerFixture(t)
	fixture.setListener(fixture.listeners[0])
	player, ok := fixture.client.JVM().AOTClass("org/kwis/msp/media/Player")
	if !ok {
		t.Fatal("Player class was not loaded")
	}
	play, found, err := fixture.client.JVM().FindAOTMethod(player.Address, "play", "(Lorg/kwis/msp/media/Clip;Z)Z")
	if err != nil || !found || play.Body == 0 {
		t.Fatalf("Player.play direct entry unavailable: %+v, %v", play, err)
	}
	writeTestWords(t, fixture.client, testfixture.KTFListenerRestartOnce, []uint32{1, play.Body, 0})
	fixture.call("play", true, false)
	fixture.drain()
	fixture.advance(fixture.duration())
	if got := fixture.drain(); got != 1 {
		t.Fatalf("END drain delivered %d callbacks, want only END", got)
	}
	fixture.wantHistory(fixture.event(0, 2), fixture.event(0, 1))
	if got := binary.LittleEndian.Uint32(readTestBytes(t, fixture.client, testfixture.KTFListenerRestartResult, 4)); got != 1 {
		t.Fatalf("callback's Player.play result = %d, want success", got)
	}
	if got := fixture.drain(); got != 1 {
		t.Fatalf("next drain delivered %d callbacks, want restarted START", got)
	}
	fixture.advance(fixture.duration())
	fixture.drain()
	fixture.wantHistory(fixture.event(0, 2), fixture.event(0, 1), fixture.event(0, 2), fixture.event(0, 1))
	if got := fixture.drain(); got != 0 {
		t.Fatalf("one-shot reentrant restart left %d callbacks", got)
	}
}

func TestKTFWIPIListenerPostedGenericEventsWaitForNextDrain(t *testing.T) {
	for _, test := range []struct {
		name    string
		pending int
	}{{"empty generic queue", 0}, {"pending generic event", 1}, {"full generic queue", maxQueuedEvents}} {
		t.Run(test.name, func(t *testing.T) {
			client, runtime := newTestRuntime(t)
			var seen []int32
			listener := &jvm.Object{ClassName: "fixture/Listener"}
			if err := client.JVM().RegisterNative(listener.ClassName, "playUpdate", clipUpdateSignature,
				func(_ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
					runtime.postGuestEvent(guestEvent{kind: eventKindNotify, param1: 2})
					return jvm.VoidValue(), nil
				}); err != nil {
				t.Fatal(err)
			}
			if err := client.JVM().RegisterNative(listener.ClassName, "notifyEvent", "(III)V",
				func(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
					value, err := arguments[1].Int32()
					seen = append(seen, value)
					return jvm.VoidValue(), err
				}); err != nil {
				t.Fatal(err)
			}
			runtime.jletListeners = []*jvm.Object{listener}
			runtime.mediaEvents = []clipEvent{{clip: &jvm.Object{ClassName: "org/kwis/msp/media/Clip"}, listener: listener, code: 2}}
			want := 1 + test.pending
			var first []int32
			for range test.pending {
				runtime.postGuestEvent(guestEvent{kind: eventKindNotify, param1: 1})
				first = append(first, 1)
			}
			if delivered, err := client.ServiceEvents(t.Context()); err != nil || delivered != want || !slices.Equal(seen, first) {
				t.Fatalf("first drain delivered=%d history=%v err=%v; want %d and %v", delivered, seen, err, want, first)
			}
			if delivered, err := client.ServiceEvents(t.Context()); err != nil || delivered != 1 || !slices.Equal(seen, append(first, 2)) {
				t.Fatalf("second drain delivered=%d history=%v err=%v", delivered, seen, err)
			}
		})
	}
}

func TestKTFWIPIListenerFromGenericEventWaitsForNextDrain(t *testing.T) {
	client, runtime := newTestRuntime(t)
	listener := &jvm.Object{ClassName: "fixture/Listener"}
	updates := 0
	if err := client.JVM().RegisterNative(listener.ClassName, "playUpdate", clipUpdateSignature,
		func(_ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
			updates++
			return jvm.VoidValue(), nil
		}); err != nil {
		t.Fatal(err)
	}
	if err := client.JVM().RegisterNative(listener.ClassName, "notifyEvent", "(III)V",
		func(_ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
			runtime.mediaEvents = append(runtime.mediaEvents, clipEvent{
				clip: &jvm.Object{ClassName: "org/kwis/msp/media/Clip"}, listener: listener, code: 2,
			})
			return jvm.VoidValue(), nil
		}); err != nil {
		t.Fatal(err)
	}
	runtime.jletListeners = []*jvm.Object{listener}
	runtime.postGuestEvent(guestEvent{kind: eventKindNotify})
	if delivered, err := client.ServiceEvents(t.Context()); err != nil || delivered != 1 || updates != 0 {
		t.Fatalf("first drain delivered=%d updates=%d err=%v", delivered, updates, err)
	}
	if delivered, err := client.ServiceEvents(t.Context()); err != nil || delivered != 1 || updates != 1 {
		t.Fatalf("second drain delivered=%d updates=%d err=%v", delivered, updates, err)
	}
}

func TestKTFWIPIListenerSurvivesGenericCallbackException(t *testing.T) {
	client, runtime := newTestRuntime(t)
	listener := &jvm.Object{ClassName: "fixture/Listener"}
	updates := 0
	if err := client.JVM().RegisterNative(listener.ClassName, "playUpdate", clipUpdateSignature,
		func(_ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
			updates++
			return jvm.VoidValue(), nil
		}); err != nil {
		t.Fatal(err)
	}
	runtime.mediaEvents = []clipEvent{{clip: &jvm.Object{ClassName: "org/kwis/msp/media/Clip"}, listener: listener, code: 2}}
	// The session absorbs this invalid generic event's GuestException and
	// continues. A waiting media notification must survive that failed drain.
	runtime.postGuestEvent(guestEvent{kind: 0})
	var exception *jvm.GuestException
	if delivered, err := client.ServiceEvents(t.Context()); !errors.As(err, &exception) || delivered != 1 || updates != 0 {
		t.Fatalf("failed generic drain delivered=%d updates=%d err=%v", delivered, updates, err)
	}
	if delivered, err := client.ServiceEvents(t.Context()); err != nil || delivered != 1 || updates != 1 {
		t.Fatalf("next drain delivered=%d updates=%d err=%v", delivered, updates, err)
	}
}

func TestKTFWIPIListenerRefusesWrongListenerWithoutReplacement(t *testing.T) {
	fixture := newKTFWIPIListenerFixture(t)
	fixture.setListener(fixture.listeners[0])
	for _, invalid := range []*jvm.Object{fixture.clip, fixture.client.JVM().NewString("listener")} {
		if _, err := fixture.client.JVM().InvokeVirtual(fixture.clip, "setListener", "(Lorg/kwis/msp/media/PlayListener;)V", jvm.ReferenceValue(invalid)); err == nil {
			t.Fatalf("setListener accepted %s without a playback callback", invalid.ClassName)
		}
	}
	fixture.call("play", true, false)
	fixture.drain()
	fixture.wantHistory(fixture.event(0, 2))
}

func TestKTFWIPIListenerChecksDeclaredGuestInterface(t *testing.T) {
	fixture := newKTFWIPIListenerFixture(t)
	fixture.setListener(fixture.listeners[0])
	metadata, ok := fixture.client.JVM().AOTClass(testfixture.KTFListenerClass)
	if !ok {
		t.Fatal("authored listener class was not loaded")
	}
	word := func(address uint32) uint32 {
		return binary.LittleEndian.Uint32(readTestBytes(t, fixture.client, address, 4))
	}
	descriptor := word(metadata.Address + 8)
	interfaces := word(descriptor + 16)
	interfaceClass := word(interfaces)
	interfaceDescriptor := word(interfaceClass + 8)
	name := word(interfaceDescriptor)
	original := readTestBytes(t, fixture.client, name, len("org/kwis/msp/media/PlayListener")+1)
	unrelated := make([]byte, len(original))
	copy(unrelated, "fixture/UnrelatedListener")
	if err := fixture.client.Core().Memory().Write(name, unrelated); err != nil {
		t.Fatal(err)
	}
	_, rejected := fixture.client.JVM().InvokeVirtual(fixture.clip, "setListener", "(Lorg/kwis/msp/media/PlayListener;)V",
		jvm.ReferenceValue(fixture.listeners[1]))
	if err := fixture.client.Core().Memory().Write(name, original); err != nil {
		t.Fatal(err)
	}
	if rejected == nil {
		t.Fatal("setListener accepted a concrete callback whose declared guest interface is unrelated")
	}
	fixture.call("play", true, false)
	fixture.drain()
	fixture.wantHistory(fixture.event(0, 2))
}

func TestKTFWIPIListenerRequiresExecutableInstanceCallback(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*jvm.AOTMethodMetadata)
	}{
		{"static", func(method *jvm.AOTMethodMetadata) { method.AccessFlags |= jvm.AccessStatic }},
		{"abstract", func(method *jvm.AOTMethodMetadata) { method.AccessFlags |= jvm.AccessAbstract }},
		{"no body", func(method *jvm.AOTMethodMetadata) { method.Body = 0 }},
		{"unmapped body", func(method *jvm.AOTMethodMetadata) { method.Body = 0xfffffff1 }},
		{"unaligned ARM body", func(method *jvm.AOTMethodMetadata) { method.Body = (method.Body &^ 3) | 2 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newKTFWIPIListenerFixture(t)
			fixture.setListener(fixture.listeners[0])
			original, ok := fixture.client.JVM().AOTClass(testfixture.KTFListenerClass)
			if !ok {
				t.Fatal("authored listener class was not loaded")
			}
			malformed, _ := fixture.client.JVM().AOTClass(testfixture.KTFListenerClass)
			for index := range malformed.Methods {
				if malformed.Methods[index].Name == "playUpdate" {
					test.mutate(&malformed.Methods[index])
				}
			}
			if err := fixture.client.JVM().RegisterAOTClass(malformed); err != nil {
				t.Fatal(err)
			}
			_, rejected := fixture.client.JVM().InvokeVirtual(fixture.clip, "setListener", "(Lorg/kwis/msp/media/PlayListener;)V",
				jvm.ReferenceValue(fixture.listeners[1]))
			if err := fixture.client.JVM().RegisterAOTClass(original); err != nil {
				t.Fatal(err)
			}
			if rejected == nil {
				t.Fatal("setListener accepted a non-executable instance callback")
			}
			fixture.call("play", true, false)
			fixture.drain()
			fixture.wantHistory(fixture.event(0, 2))
		})
	}
}

func TestKTFWIPIListenerQueueBoundAndAudioServiceError(t *testing.T) {
	t.Run("explicit transitions", func(t *testing.T) {
		fixture := newKTFWIPIListenerFixture(t)
		fixture.setListener(fixture.listeners[0])
		for range 4096 {
			fixture.call("play", true, false)
		}
		if _, err := fixture.client.JVM().InvokeStatic("org/kwis/msp/media/Player", "play", "(Lorg/kwis/msp/media/Clip;Z)Z",
			jvm.ReferenceValue(fixture.clip), jvm.IntValue(0)); err == nil {
			t.Fatal("pending media queue accepted transition 4097")
		}
		fixture.wantHistory()
		if got := fixture.drain(); got != 4096 {
			t.Fatalf("bounded queue delivered %d callbacks, want 4096", got)
		}
		if got := fixture.drain(); got != 0 {
			t.Fatalf("overflowed queue left %d extra callbacks", got)
		}
	})
	t.Run("completion batch", func(t *testing.T) {
		fixture := newKTFWIPIListenerFixture(t)
		fixture.setListener(fixture.listeners[0])
		fixture.call("play", true, true)
		fixture.drain()
		fixture.clock.Advance(4097 * fixture.duration())
		if err := fixture.client.serviceAudio(); err == nil {
			t.Fatal("audio service hid a completion notification overflow")
		}
		if got := fixture.drain(); got != 0 {
			t.Fatalf("refused completion batch emitted %d partial callbacks", got)
		}
		fixture.wantHistory(fixture.event(0, 2))
	})
}
