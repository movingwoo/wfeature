package ktf

import (
	"bytes"
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

func TestRuntimeControlContinuesClockCallbacksInputAndUnloadedClip(t *testing.T) {
	clock := NewManualClock(time.Unix(1700000000, 0))
	client, runtime := newPacedTestRuntime(t, clock, 1.25)
	probe, err := runtime.allocate(8)
	if err != nil {
		t.Fatal(err)
	}
	callback := fixtureARM(t, runtime, func(code *continuationARM) {
		code.literal(2, probe)
		code.emit(0xe5820000, 0xe5821004, 0xe12fff1e)
	})
	netCall(t, runtime, wipicNetConnect, callback, 0x1234)
	card := &jvm.Object{ClassName: jvm.ObjectClass}
	runtime.cInput = cInputState{card: card, owner: cInputOwner{address: probe}, active: true, mode: 3, revision: 17, calls: 12, activations: 3, discardCarrier: true, clearPending: true}
	runtime.cInput.pending = []byte{0xb0, 0xa1}
	runtime.repaintPending, runtime.guestFlushedOwnFrame, runtime.guestHasPainted = true, true, true
	runtime.roundsSinceGuestPaint, runtime.guestEventLoop = 3, true
	clip := mediaCall(t, runtime, wipicMediaClipCreate, 0, 0, 0)
	runtime.wipicClips[clip].state.data = oneNoteSMAF()
	mediaCall(t, runtime, wipicMediaSetMuteState, 3, 1)
	mediaCall(t, runtime, wipicMediaSetMuteState, 0, 0)
	mediaCall(t, runtime, wipicMediaClipSetVolume, clip, 45)
	clock.Advance(123456789 * time.Nanosecond)
	heap, err := runtime.captureHeapState(nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(heap)
	if err != nil {
		t.Fatal(err)
	}
	var decoded runtimeHeapState
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	metadata, err := runtime.captureMetadataState()
	if err != nil {
		t.Fatal(err)
	}
	core, err := client.core.CaptureState()
	if err != nil {
		t.Fatal(err)
	}
	fixture := newContinuationRestoreFixture(t, 1900000000, continuationFixtureOptions{})
	fresh := fixture.runtime
	fixture.client.SetSpeed(1.25)
	fixture.client.core, err = armcore.NewCoreFromState(core, armcore.CoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := fresh.restoreMetadataState(metadata); err != nil {
		t.Fatal(err)
	}
	if _, err := fresh.restoreHeapState(decoded); err != nil {
		t.Fatal(err)
	}
	for _, step := range []time.Duration{0, time.Nanosecond, time.Microsecond, 800 * time.Microsecond} {
		clock.Advance(step)
		fixture.clock.Advance(step)
		if got, want := fresh.guestElapsed(), runtime.guestElapsed(); got != want {
			t.Fatalf("restored guest elapsed = %s, want %s", got, want)
		}
		if got, want := fresh.guestMillis(), runtime.guestMillis(); got != want {
			t.Fatalf("restored guest date = %d, want %d", got, want)
		}
	}
	if !fresh.cInput.owner.current(fresh) || fresh.cInput.card == nil || fresh.cInput.card == card || fresh.cInput.revision != 17 || fresh.cInput.calls != 12 || fresh.cInput.activations != 3 || !fresh.cInput.active || fresh.cInput.mode != 3 || !fresh.cInput.discardCarrier || !fresh.cInput.clearPending {
		t.Fatal("C input ownership or pending control state differs")
	}
	if !fresh.repaintPending || !fresh.guestFlushedOwnFrame || !fresh.guestHasPainted || fresh.roundsSinceGuestPaint != 3 || !fresh.guestEventLoop {
		t.Fatal("guest painting or event ownership differs")
	}
	if mediaCall(t, fresh, wipicMediaGetMuteState, 3) != 1 || mediaCall(t, fresh, wipicMediaGetMuteState, 0) != 0 || len(fresh.wipicMutedSources) != 1 {
		t.Fatalf("restored C media mute state = %v, want source 3 alone", fresh.wipicMutedSources)
	}
	if level := mediaCall(t, fresh, wipicMediaClipGetVolume, clip); level != 45 {
		t.Fatalf("restored C clip volume = %d, want 45", level)
	}
	buffer, err := fresh.allocate(4)
	if err != nil {
		t.Fatal(err)
	}
	capacity, err := fresh.allocateWords([]uint32{4})
	if err != nil {
		t.Fatal(err)
	}
	for pass := 0; pass < 2; pass++ {
		writeWord(t, fresh, capacity, 4)
		if got, err := cInputCall(t, fresh, wipicIMHandleInput, uint32(cInputCarrier), cInputPressed, buffer, capacity, 0, 0); err != nil || got != 1 {
			t.Fatalf("restored C input = %d, %v", got, err)
		}
		if pass == 0 && (len(fresh.cInput.pending) != 2 || fresh.cInput.discardCarrier || readTestBytes(t, fixture.client, buffer, 1)[0] != 0) {
			t.Fatal("restored C input did not discard its canceled carrier")
		}
	}
	if len(fresh.cInput.pending) != 0 || !bytes.Equal(readTestBytes(t, fixture.client, buffer, 3), []byte{0xb0, 0xa1, 0}) || len(runtime.cInput.pending) != 2 {
		t.Fatal("restored C input did not deliver pending text independently")
	}
	if count, err := fixture.client.serviceNetCallbacks(t.Context()); err != nil || count != 1 {
		t.Fatalf("restored network callback = %d, %v", count, err)
	}
	words, err := fresh.readAOTWords(probe, 2, "restored network callback")
	if err != nil || words[0] != wipiErrorCode || words[1] != 0x1234 || len(fresh.pendingNetCallbacks) != 0 || fresh.cInput.owner.current(fresh) {
		t.Fatalf("restored callback arguments or ownership = %v, %v", words, err)
	}
	if len(runtime.pendingNetCallbacks) != 1 || !runtime.cInput.owner.current(runtime) || !bytes.Equal(fresh.wipicClips[clip].state.data, runtime.wipicClips[clip].state.data) {
		t.Fatal("restored control changed its source or clip data")
	}
	sink := &countingSink{}
	fixture.client.audio = backend.NewAudio(sink)
	if got := mediaCall(t, fresh, wipicMediaPlay, clip, 0); got != 0 {
		t.Fatalf("restored unloaded clip play = %#x", got)
	}
	fixture.client.audio.Advance(fresh.guestElapsed() + time.Second)
	if sink.noteOns != 1 {
		t.Fatalf("restored clip emitted %d notes", sink.noteOns)
	}
	mediaCall(t, fresh, wipicMediaClipFree, clip)
	if len(fresh.wipicClips) != 0 || len(fresh.wipicClipOrder) != 0 || runtime.wipicClips[clip] == nil {
		t.Fatal("restored clip deletion changed source ownership")
	}
}

func TestRuntimeControlRefusesHostRemaindersAndMalformedState(t *testing.T) {
	_, runtime := newTestRuntime(t)
	invoked := false
	runtime.cInput.pendingValid = func() bool { invoked = true; return true }
	if _, err := runtime.captureHeapState(nil); err == nil || invoked {
		t.Fatal("capture ran or ignored a pending Host input guard")
	}
	runtime.cInput.pendingValid = nil
	saved, err := runtime.captureHeapState(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, damage := range []func(*runtimeHeapState){
		func(s *runtimeHeapState) { s.Control.ClockAge = time.Duration(math.MinInt64) },
		func(s *runtimeHeapState) { s.Control.CInput.Pending = []byte{1, 2, 3} },
		func(s *runtimeHeapState) { s.Control.CInput.Mode = 99 },
		func(s *runtimeHeapState) { s.Control.CInput.OwnerAddress = 0xfffffffc },
		func(s *runtimeHeapState) { s.Control.RoundsSinceGuestPaint = -1 },
		func(s *runtimeHeapState) { s.Control.Network = []runtimeNetCallbackState{{Callback: 0}} },
		func(s *runtimeHeapState) { s.Control.ClipOrder = []uint32{1} },
		func(s *runtimeHeapState) { s.Control.MutedSources = []uint32{3, 3} },
		func(s *runtimeHeapState) {
			s.Control.Clips = []runtimeCClipState{{Address: 0x1000, Volume: wipicClipFullVolume + 1}}
			s.Control.ClipOrder = []uint32{0x1000}
		},
		func(s *runtimeHeapState) {
			s.Control.MutedSources = nil
			for source := uint32(0); source <= maxWIPICMutedSources; source++ {
				s.Control.MutedSources = append(s.Control.MutedSources, source)
			}
		},
	} {
		bad := saved
		damage(&bad)
		runtime.guestEventLoop = true
		if _, err := runtime.restoreHeapState(bad); err == nil || !runtime.guestEventLoop {
			t.Fatal("malformed control state was adopted")
		}
	}
}
