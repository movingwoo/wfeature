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
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
)

func TestRuntimeControlContinuesClockCallbacksInputAndUnloadedClip(t *testing.T) {
	clock := wipicrt.NewManualClock(time.Unix(1700000000, 0))
	client, runtime := newPacedTestRuntime(t, clock, 1.25)
	probe, err := runtime.Allocate(8)
	if err != nil {
		t.Fatal(err)
	}
	callback := fixtureARM(t, runtime, func(code *continuationARM) {
		code.literal(2, probe)
		code.emit(0xe5820000, 0xe5821004, 0xe12fff1e)
	})
	netCall(t, runtime, wipicrt.WIPICNetConnect, callback, 0x1234)
	card := &jvm.Object{ClassName: jvm.ObjectClass}
	runtime.cInputCardOwner = card
	runtime.CInput = wipicrt.CInputState{Owner: wipicrt.CInputOwner{Address: probe}, Active: true, Mode: 3, Revision: 17, Calls: 12, Activations: 3, DiscardCarrier: true, ClearPending: true}
	runtime.CInput.Pending = []byte{0xb0, 0xa1}
	runtime.RepaintPending, runtime.GuestFlushedOwnFrame, runtime.guestHasPainted = true, true, true
	runtime.roundsSinceGuestPaint, runtime.GuestEventLoop = 3, true
	clip := mediaCall(t, runtime, wipicrt.WIPICMediaClipCreate, 0, 0, 0)
	runtime.WIPICClips[clip].State.Data = oneNoteSMAF()
	mediaCall(t, runtime, wipicrt.WIPICMediaSetMuteState, 3, 1)
	mediaCall(t, runtime, wipicrt.WIPICMediaSetMuteState, 0, 0)
	mediaCall(t, runtime, wipicrt.WIPICMediaClipSetVolume, clip, 45)
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
	core, err := client.Core().CaptureState()
	if err != nil {
		t.Fatal(err)
	}
	fixture := newContinuationRestoreFixture(t, 1900000000, continuationFixtureOptions{})
	fresh := fixture.runtime
	fixture.client.SetSpeed(1.25)
	restoredCore, err := armcore.NewCoreFromState(core, armcore.CoreOptions{})
	fixture.client.AttachCore(restoredCore, fixture.client.Thread)
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
		if got, want := fresh.GuestElapsed(), runtime.GuestElapsed(); got != want {
			t.Fatalf("restored guest elapsed = %s, want %s", got, want)
		}
		if got, want := fresh.GuestMillis(), runtime.GuestMillis(); got != want {
			t.Fatalf("restored guest date = %d, want %d", got, want)
		}
	}
	if !fresh.CInput.Owner.Current(fresh.Runtime) || fresh.cInputCardOwner == nil || fresh.cInputCardOwner == card || fresh.CInput.Revision != 17 || fresh.CInput.Calls != 12 || fresh.CInput.Activations != 3 || !fresh.CInput.Active || fresh.CInput.Mode != 3 || !fresh.CInput.DiscardCarrier || !fresh.CInput.ClearPending {
		t.Fatal("C input ownership or pending control state differs")
	}
	if !fresh.RepaintPending || !fresh.GuestFlushedOwnFrame || !fresh.guestHasPainted || fresh.roundsSinceGuestPaint != 3 || !fresh.GuestEventLoop {
		t.Fatal("guest painting or event ownership differs")
	}
	if mediaCall(t, fresh, wipicrt.WIPICMediaGetMuteState, 3) != 1 || mediaCall(t, fresh, wipicrt.WIPICMediaGetMuteState, 0) != 0 || len(fresh.WIPICMutedSources) != 1 {
		t.Fatalf("restored C media mute state = %v, want source 3 alone", fresh.WIPICMutedSources)
	}
	if level := mediaCall(t, fresh, wipicrt.WIPICMediaClipGetVolume, clip); level != 45 {
		t.Fatalf("restored C clip volume = %d, want 45", level)
	}
	buffer, err := fresh.Allocate(4)
	if err != nil {
		t.Fatal(err)
	}
	capacity, err := fresh.AllocateWords([]uint32{4})
	if err != nil {
		t.Fatal(err)
	}
	for pass := 0; pass < 2; pass++ {
		writeWord(t, fresh, capacity, 4)
		if got, err := cInputCall(t, fresh, wipicrt.WIPICIMHandleInput, uint32(wipicrt.CInputCarrier), wipicrt.CInputPressed, buffer, capacity, 0, 0); err != nil || got != 1 {
			t.Fatalf("restored C input = %d, %v", got, err)
		}
		if pass == 0 && (len(fresh.CInput.Pending) != 2 || fresh.CInput.DiscardCarrier || readTestBytes(t, fixture.client, buffer, 1)[0] != 0) {
			t.Fatal("restored C input did not discard its canceled carrier")
		}
	}
	if len(fresh.CInput.Pending) != 0 || !bytes.Equal(readTestBytes(t, fixture.client, buffer, 3), []byte{0xb0, 0xa1, 0}) || len(runtime.CInput.Pending) != 2 {
		t.Fatal("restored C input did not deliver pending text independently")
	}
	if count, err := fixture.client.ServiceNetCallbacks(t.Context()); err != nil || count != 1 {
		t.Fatalf("restored network callback = %d, %v", count, err)
	}
	words, err := fresh.ReadAOTWords(probe, 2, "restored network callback")
	if err != nil || words[0] != wipicrt.WipiErrorCode || words[1] != 0x1234 || len(fresh.PendingNetCallbacks) != 0 || fresh.CInput.Owner.Current(fresh.Runtime) {
		t.Fatalf("restored callback arguments or ownership = %v, %v", words, err)
	}
	if len(runtime.PendingNetCallbacks) != 1 || !runtime.CInput.Owner.Current(runtime.Runtime) || !bytes.Equal(fresh.WIPICClips[clip].State.Data, runtime.WIPICClips[clip].State.Data) {
		t.Fatal("restored control changed its source or clip data")
	}
	sink := &countingSink{}
	fixture.client.Audio = backend.NewAudio(sink)
	if got := mediaCall(t, fresh, wipicrt.WIPICMediaPlay, clip, 0); got != 0 {
		t.Fatalf("restored unloaded clip play = %#x", got)
	}
	fixture.client.Audio.Advance(fresh.GuestElapsed() + time.Second)
	if sink.noteOns != 1 {
		t.Fatalf("restored clip emitted %d notes", sink.noteOns)
	}
	mediaCall(t, fresh, wipicrt.WIPICMediaClipFree, clip)
	if len(fresh.WIPICClips) != 0 || len(fresh.WIPICClipOrder) != 0 || runtime.WIPICClips[clip] == nil {
		t.Fatal("restored clip deletion changed source ownership")
	}
}

func TestRuntimeControlRefusesHostRemaindersAndMalformedState(t *testing.T) {
	_, runtime := newTestRuntime(t)
	invoked := false
	runtime.CInput.PendingValid = func() bool { invoked = true; return true }
	if _, err := runtime.captureHeapState(nil); err == nil || invoked {
		t.Fatal("capture ran or ignored a pending Host input guard")
	}
	runtime.CInput.PendingValid = nil
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
			s.Control.Clips = []runtimeCClipState{{Address: 0x1000, Volume: wipicrt.WIPICClipFullVolume + 1}}
			s.Control.ClipOrder = []uint32{0x1000}
		},
		func(s *runtimeHeapState) {
			s.Control.MutedSources = nil
			for source := uint32(0); source <= wipicrt.MaxWIPICMutedSources; source++ {
				s.Control.MutedSources = append(s.Control.MutedSources, source)
			}
		},
	} {
		bad := saved
		damage(&bad)
		runtime.GuestEventLoop = true
		if _, err := runtime.restoreHeapState(bad); err == nil || !runtime.GuestEventLoop {
			t.Fatal("malformed control state was adopted")
		}
	}
}
