package lgt

import (
	"encoding/binary"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/testfixture"
)

type lgtWIPIListenerEvent struct {
	receiver, clip uint32
	event, parm    int32
}

type lgtWIPIListenerFixture struct {
	t         *testing.T
	archive   []byte
	session   *Session
	client    *Client
	store     backend.SaveStore
	clip      uint32
	listeners [2]uint32
}

func newLGTWIPIListenerFixture(t *testing.T) *lgtWIPIListenerFixture {
	t.Helper()
	archive, err := testfixture.LGTListenerArchive()
	if err != nil {
		t.Fatal(err)
	}
	store, err := backend.NewMemorySaveStore(nil)
	if err != nil {
		t.Fatal(err)
	}
	session, err := StartSession(t.Context(), archive, SessionOptions{
		SaveStore: store, Width: 16, Height: 8, Tick: 5 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close(t.Context()) })
	fixture := &lgtWIPIListenerFixture{t: t, archive: archive, session: session, client: session.client, store: store}
	if count := fixture.word(testfixture.LGTListenerStartupCounter); count != 1 {
		t.Fatalf("fixture startup count = %d, want 1", count)
	}
	class := fixture.client.javaRun.byName[testfixture.LGTListenerClass]
	if class == nil || len(class.Record.Interfaces) != 1 ||
		class.Record.Interfaces[0].Name != "org/kwis/msp/media/PlayListener" {
		t.Fatal("authored listener class has no declared PlayListener interface")
	}
	for index := range fixture.listeners {
		fixture.listeners[index], err = fixture.client.allocateJavaObject(class)
		if err != nil {
			t.Fatal(err)
		}
		constructor, _, ok := fixture.client.findJavaMethod(class.Record, "<init>")
		if !ok {
			t.Fatal("authored listener has no constructor")
		}
		if _, err := fixture.client.call(t.Context(), constructor.Body, []uint32{fixture.listeners[index]}); err != nil {
			t.Fatal(err)
		}
	}
	fixture.clip = fixture.newClip()
	return fixture
}

func (fixture *lgtWIPIListenerFixture) newClip() uint32 {
	fixture.t.Helper()
	client := fixture.client
	class, err := client.preparePlatformJavaClass(javaClipClass)
	if err != nil {
		fixture.t.Fatal(err)
	}
	clip, err := client.allocateJavaObject(class)
	if err != nil {
		fixture.t.Fatal(err)
	}
	kind, err := client.newJavaString("audio/mmf")
	if err != nil {
		fixture.t.Fatal(err)
	}
	array, err := client.newJavaByteArray(oneNoteSound(fixture.t))
	if err != nil {
		fixture.t.Fatal(err)
	}
	fixture.javaCall(javaClipClass+".<init>(Ljava/lang/String;[B)V", clip, kind, array)
	return clip
}

// javaCall crosses the registered guest ABI, including its argument decoding
// and r0 result, instead of calling a media implementation directly.
func (fixture *lgtWIPIListenerFixture) javaCall(member string, arguments ...uint32) uint32 {
	fixture.t.Helper()
	method, ok := javaPlatformMethods[member]
	if !ok || method.Words != len(arguments) {
		fixture.t.Fatalf("Java media method %s has no matching guest entry", member)
	}
	thread := armcore.NewThread(armcore.NewContext())
	for index, value := range arguments {
		if err := thread.SetRegister(index, value); err != nil {
			fixture.t.Fatal(err)
		}
	}
	class, called, _ := strings.Cut(member, ".")
	if err := fixture.client.callJavaMethod(fixture.t.Context(), thread, class, called, method); err != nil {
		fixture.t.Fatal(err)
	}
	answer, err := thread.Register(0)
	if err != nil {
		fixture.t.Fatal(err)
	}
	return answer
}

func (fixture *lgtWIPIListenerFixture) setListener(listener uint32) {
	fixture.t.Helper()
	fixture.javaCall(javaClipClass+".setListener(Lorg/kwis/msp/media/PlayListener;)V", fixture.clip, listener)
}

func (fixture *lgtWIPIListenerFixture) call(method string, want bool, repeat ...bool) {
	fixture.t.Helper()
	descriptor := "(Lorg/kwis/msp/media/Clip;)Z"
	arguments := []uint32{fixture.clip}
	if method == "play" {
		descriptor = "(Lorg/kwis/msp/media/Clip;Z)Z"
		arguments = append(arguments, 0)
		if len(repeat) != 0 && repeat[0] {
			arguments[1] = 1
		}
	}
	expected := uint32(0)
	if want {
		expected = 1
	}
	if got := fixture.javaCall("org/kwis/msp/media/Player."+method+descriptor, arguments...); got != expected {
		fixture.t.Fatalf("Player.%s = %d, want %d", method, got, expected)
	}
}

func (fixture *lgtWIPIListenerFixture) drain() int {
	fixture.t.Helper()
	before := len(fixture.history())
	if err := fixture.client.serviceMediaCallbacks(fixture.t.Context()); err != nil {
		fixture.t.Fatal(err)
	}
	return len(fixture.history()) - before
}

func (fixture *lgtWIPIListenerFixture) advance(duration time.Duration) {
	fixture.t.Helper()
	fixture.client.clock.advance(duration)
	fixture.client.serviceAudio()
}

func (fixture *lgtWIPIListenerFixture) duration() time.Duration {
	fixture.t.Helper()
	clip := fixture.client.clips[fixture.clip]
	if clip == nil || !clip.loaded {
		fixture.t.Fatal("authored clip has not loaded")
	}
	progress, err := fixture.client.audio.Playback(clip.handle, fixture.client.clock.now())
	if err != nil || progress.Length <= 0 {
		fixture.t.Fatalf("authored clip duration = %v, %v", progress.Length, err)
	}
	return progress.Length
}

func (fixture *lgtWIPIListenerFixture) word(address uint32) uint32 {
	fixture.t.Helper()
	word, err := fixture.client.readWord(address)
	if err != nil {
		fixture.t.Fatal(err)
	}
	return word
}

func (fixture *lgtWIPIListenerFixture) writeWord(address, word uint32) {
	fixture.t.Helper()
	if err := fixture.client.writeWord(address, word); err != nil {
		fixture.t.Fatal(err)
	}
}

func (fixture *lgtWIPIListenerFixture) history() []lgtWIPIListenerEvent {
	fixture.t.Helper()
	count := fixture.word(testfixture.LGTListenerHistoryCount)
	if count > testfixture.LGTListenerHistoryCapacity {
		fixture.t.Fatalf("callback history count %d exceeds its authored bound", count)
	}
	data := make([]byte, int(count)*16)
	if err := fixture.client.core.Memory().Read(testfixture.LGTListenerHistory, data); err != nil {
		fixture.t.Fatal(err)
	}
	events := make([]lgtWIPIListenerEvent, count)
	for index := range events {
		record := data[index*16:]
		events[index] = lgtWIPIListenerEvent{binary.LittleEndian.Uint32(record), binary.LittleEndian.Uint32(record[4:]),
			int32(binary.LittleEndian.Uint32(record[8:])), int32(binary.LittleEndian.Uint32(record[12:]))}
	}
	return events
}

func (fixture *lgtWIPIListenerFixture) event(listener int, event int32) lgtWIPIListenerEvent {
	return lgtWIPIListenerEvent{receiver: fixture.listeners[listener], clip: fixture.clip, event: event}
}

func (fixture *lgtWIPIListenerFixture) wantHistory(want ...lgtWIPIListenerEvent) {
	fixture.t.Helper()
	if got := fixture.history(); !slices.Equal(got, want) {
		fixture.t.Fatalf("callback history = %+v, want %+v", got, want)
	}
}

func TestAuthoredLGTWIPIListenerCallbackABI(t *testing.T) {
	fixture := newLGTWIPIListenerFixture(t)
	callback := fixture.word(testfixture.LGTListenerCallbackAddress)
	var want []lgtWIPIListenerEvent
	for index := range testfixture.LGTListenerHistoryCapacity + 2 {
		listener := index % len(fixture.listeners)
		event, parm := int32(index+1), -int32(index+17)
		if _, err := fixture.client.call(t.Context(), callback,
			[]uint32{fixture.listeners[listener], fixture.clip, uint32(event), uint32(parm)}); err != nil {
			t.Fatal(err)
		}
		if index < testfixture.LGTListenerHistoryCapacity {
			record := fixture.event(listener, event)
			record.parm = parm
			want = append(want, record)
		}
	}
	fixture.wantHistory(want...)
}

func TestAuthoredLGTWIPIListenerReentrantPlayerEntry(t *testing.T) {
	fixture := newLGTWIPIListenerFixture(t)
	fixture.call("play", true)
	fixture.call("stop", true)
	fixture.writeWord(testfixture.LGTListenerRestartOnce, 1)
	if _, err := fixture.client.call(t.Context(), fixture.word(testfixture.LGTListenerCallbackAddress),
		[]uint32{fixture.listeners[0], fixture.clip, 1, 0}); err != nil {
		t.Fatal(err)
	}
	fixture.wantHistory(fixture.event(0, 1))
	if fixture.word(testfixture.LGTListenerRestartOnce) != 0 ||
		fixture.word(testfixture.LGTListenerRestartResult) != javaTrue ||
		!fixture.client.audio.Playing(fixture.client.clips[fixture.clip].handle) {
		t.Fatal("authored callback did not consume its hook and restart through Player.play")
	}
}

func TestLGTWIPIListenerDeliversAfterNativeCall(t *testing.T) {
	fixture := newLGTWIPIListenerFixture(t)
	fixture.setListener(fixture.listeners[0])
	fixture.call("play", true, false)
	fixture.wantHistory()
	if err := fixture.session.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	fixture.wantHistory(fixture.event(0, 2))
	if got := fixture.drain(); got != 0 {
		t.Fatalf("second drain delivered %d callbacks, want none", got)
	}
}

func TestLGTWIPIListenerTransitionsAndNoops(t *testing.T) {
	fixture := newLGTWIPIListenerFixture(t)
	fixture.setListener(fixture.listeners[0])
	for _, method := range []string{"pause", "resume", "stop"} {
		fixture.call(method, false)
	}
	fixture.call("play", true, true)
	fixture.call("play", false, false)
	fixture.advance(fixture.duration() / 4)
	fixture.call("resume", false)
	fixture.call("pause", true)
	fixture.call("pause", false)
	fixture.advance(time.Hour)
	fixture.call("resume", true)
	fixture.call("resume", false)
	fixture.call("stop", true)
	fixture.call("stop", true) // The existing boolean contract accepts a loaded idle clip.
	fixture.call("pause", false)
	fixture.call("resume", false)
	fixture.wantHistory()
	fixture.drain()
	want := []lgtWIPIListenerEvent{fixture.event(0, 2), fixture.event(0, 4), fixture.event(0, 5), fixture.event(0, 3)}
	fixture.wantHistory(want...)
	fixture.call("play", true)
	fixture.call("pause", true)
	fixture.call("stop", true)
	fixture.call("resume", false)
	fixture.wantHistory(want...)
	fixture.drain()
	want = append(want, fixture.event(0, 2), fixture.event(0, 4), fixture.event(0, 3))
	fixture.wantHistory(want...)
	fixture.advance(time.Hour)
	if got := fixture.drain(); got != 0 {
		t.Fatalf("stopped clip delivered %d later callbacks", got)
	}
}

func TestLGTWIPIListenerSnapshotsReplacementAndNull(t *testing.T) {
	fixture := newLGTWIPIListenerFixture(t)
	fixture.setListener(fixture.listeners[0])
	fixture.call("play", true)
	fixture.setListener(fixture.listeners[1])
	fixture.call("pause", true)
	fixture.setListener(0)
	fixture.call("resume", true)
	fixture.call("stop", true)
	fixture.wantHistory()
	fixture.drain()
	fixture.wantHistory(fixture.event(0, 2), fixture.event(1, 4))
	fixture.call("play", true)
	fixture.advance(fixture.duration() * 2)
	if got := fixture.drain(); got != 0 {
		t.Fatalf("null listener received %d callbacks", got)
	}
}

func TestLGTWIPIListenerNaturalEndForEachPass(t *testing.T) {
	for _, repeat := range []bool{false, true} {
		t.Run(map[bool]string{false: "once", true: "repeat"}[repeat], func(t *testing.T) {
			fixture := newLGTWIPIListenerFixture(t)
			fixture.setListener(fixture.listeners[0])
			fixture.call("play", true, repeat)
			length := fixture.duration()
			fixture.advance(3*length + time.Millisecond)
			fixture.wantHistory()
			fixture.drain()
			want := []lgtWIPIListenerEvent{fixture.event(0, 2), fixture.event(0, 1)}
			if repeat {
				want = append(want, fixture.event(0, 1), fixture.event(0, 1))
			}
			fixture.wantHistory(want...)
			if got := fixture.drain(); got != 0 {
				t.Fatalf("unchanged playback delivered %d duplicate callbacks", got)
			}
			if repeat {
				fixture.call("stop", true)
				fixture.drain()
				want = append(want, fixture.event(0, 3))
			} else {
				fixture.call("stop", true)
				fixture.drain()
			}
			fixture.wantHistory(want...)
		})
	}
}

func TestLGTWIPIListenerReentrantEndQueuesStartForNextDrain(t *testing.T) {
	fixture := newLGTWIPIListenerFixture(t)
	fixture.setListener(fixture.listeners[0])
	fixture.writeWord(testfixture.LGTListenerRestartOnce, 1)
	fixture.call("play", true)
	fixture.drain()
	fixture.wantHistory(fixture.event(0, 2))
	fixture.advance(fixture.duration() + time.Millisecond)
	fixture.drain()
	fixture.wantHistory(fixture.event(0, 2), fixture.event(0, 1))
	if got := fixture.word(testfixture.LGTListenerRestartResult); got != javaTrue {
		t.Fatalf("listener's Player.play result = %d, want true", got)
	}
	clip := fixture.client.clips[fixture.clip]
	if !fixture.client.audio.Playing(clip.handle) {
		t.Fatal("listener did not restart its clip")
	}
	fixture.drain()
	fixture.wantHistory(fixture.event(0, 2), fixture.event(0, 1), fixture.event(0, 2))
	fixture.advance(fixture.duration() + time.Millisecond)
	fixture.drain()
	fixture.wantHistory(fixture.event(0, 2), fixture.event(0, 1), fixture.event(0, 2), fixture.event(0, 1))
	if fixture.client.audio.Playing(clip.handle) {
		t.Fatal("one-shot restart hook repeated again")
	}
}
