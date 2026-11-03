package wipicrt_test

import (
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt/wipicrttest"
)

// TestWIPICMediaPlaysAClipTheGuestFilled is the whole reason this block exists:
// a title that keeps its sound in C creates a clip, copies an SMAF file into it
// and plays it, and every one of those calls used to be accepted and thrown
// away. Counting what reached the sink is what tells the two apart — the calls
// answered success either way.
func TestWIPICMediaPlaysAClipTheGuestFilled(t *testing.T) {
	client, runtime := wipicrttest.NewRuntime(t)
	sink := &wipicrttest.CountingSink{}
	client.Audio = backend.NewAudio(sink)

	sound := wipicrttest.OneNoteSMAF()
	clip := wipicrttest.MediaCall(t, runtime, wipicrt.WIPICMediaClipCreate,
		wipicrttest.GuestBytes(t, runtime, append([]byte("Yamaha_MA2"), 0)), uint32(len(sound)), 0)
	if clip == 0 || runtime.WIPICClips[clip] == nil {
		t.Fatalf("create answered %#x with no clip behind it", clip)
	}
	if runtime.WIPICClips[clip].MediaType != "Yamaha_MA2" {
		t.Fatalf("clip media type = %q, want the type create was given", runtime.WIPICClips[clip].MediaType)
	}

	taken := wipicrttest.MediaCall(t, runtime, wipicrt.WIPICMediaClipPutData, clip, wipicrttest.GuestBytes(t, runtime, sound), uint32(len(sound)))
	if taken != uint32(len(sound)) {
		t.Fatalf("putData took %d of %d bytes", taken, len(sound))
	}

	if result := wipicrttest.MediaCall(t, runtime, wipicrt.WIPICMediaPlay, clip, 0); result == wipicrt.WipiErrorCode {
		t.Fatal("play refused a clip holding a sound this build decodes")
	}
	client.Audio.Advance(100 * time.Millisecond)
	if sink.NoteOns == 0 {
		t.Fatal("nothing reached the sink after a play")
	}
}

// TestWIPICMediaRefusesAClipItCannotDecode covers the other half. A game whose
// sound this build cannot read has to be told, because it branches on the
// answer; failing the call instead would take the game down over a missing
// codec.
func TestWIPICMediaRefusesAClipItCannotDecode(t *testing.T) {
	client, runtime := wipicrttest.NewRuntime(t)
	client.Audio = backend.NewAudio(&wipicrttest.CountingSink{})

	data := []byte("this is not a sound")
	clip := wipicrttest.MediaCall(t, runtime, wipicrt.WIPICMediaClipCreate, 0, uint32(len(data)), 0)
	wipicrttest.MediaCall(t, runtime, wipicrt.WIPICMediaClipPutData, clip, wipicrttest.GuestBytes(t, runtime, data), uint32(len(data)))
	if result := wipicrttest.MediaCall(t, runtime, wipicrt.WIPICMediaPlay, clip, 0); result != wipicrt.WipiErrorCode {
		t.Fatalf("play of undecodable data answered %#x, want the failure code", result)
	}
}

// TestWIPICMediaFreeDropsTheClip pins the teardown run four titles make —
// stop, clear, free — and the part that matters is the last one: a clip that
// stayed registered would hold its sound bytes for as long as the game ran.
func TestWIPICMediaFreeDropsTheClip(t *testing.T) {
	client, runtime := wipicrttest.NewRuntime(t)
	client.Audio = backend.NewAudio(&wipicrttest.CountingSink{})

	sound := wipicrttest.OneNoteSMAF()
	clip := wipicrttest.MediaCall(t, runtime, wipicrt.WIPICMediaClipCreate, 0, uint32(len(sound)), 0)
	wipicrttest.MediaCall(t, runtime, wipicrt.WIPICMediaClipPutData, clip, wipicrttest.GuestBytes(t, runtime, sound), uint32(len(sound)))
	wipicrttest.MediaCall(t, runtime, wipicrt.WIPICMediaPlay, clip, 0)

	wipicrttest.MediaCall(t, runtime, wipicrt.WIPICMediaStop, clip)
	wipicrttest.MediaCall(t, runtime, wipicrt.WIPICMediaClipClearData, clip)
	if len(runtime.WIPICClips[clip].State.Data) != 0 {
		t.Fatal("clearData left the clip's bytes behind")
	}
	wipicrttest.MediaCall(t, runtime, wipicrt.WIPICMediaClipFree, clip)
	if runtime.WIPICClips[clip] != nil {
		t.Fatal("free left the clip registered")
	}
	// A handle nobody created takes no data rather than answering a byte count
	// of -1, which is what the caller would read a failure code as.
	if taken := wipicrttest.MediaCall(t, runtime, wipicrt.WIPICMediaClipPutData, clip, wipicrttest.GuestBytes(t, runtime, sound), uint32(len(sound))); taken != 0 {
		t.Fatalf("putData into a freed clip took %d bytes", taken)
	}
}

// TestWIPICMediaBoundsWhatOneTitleCanRetain covers the title that creates a
// clip per sound effect and frees none of them. The guest picks both the count
// and each clip's size, so without the cap a long session grows without end.
func TestWIPICMediaBoundsWhatOneTitleCanRetain(t *testing.T) {
	client, runtime := wipicrttest.NewRuntime(t)
	client.Audio = backend.NewAudio(&wipicrttest.CountingSink{})

	for index := 0; index < wipicrt.MaxWIPICMediaClips+8; index++ {
		clip := wipicrttest.MediaCall(t, runtime, wipicrt.WIPICMediaClipCreate, 0, 4, 0)
		wipicrttest.MediaCall(t, runtime, wipicrt.WIPICMediaClipPutData, clip, wipicrttest.GuestBytes(t, runtime, []byte("data")), 4)
	}
	if len(runtime.WIPICClips) > wipicrt.MaxWIPICMediaClips {
		t.Fatalf("%d clips retained, want at most %d", len(runtime.WIPICClips), wipicrt.MaxWIPICMediaClips)
	}
}

// TestWIPICMediaVolumeReachesTheSoundPath is the difference between storing the
// level and honouring it. A title fades its music out by walking this to zero,
// and a runtime that remembers the number and keeps playing at full has heard
// the request and disregarded it.
func TestWIPICMediaVolumeReachesTheSoundPath(t *testing.T) {
	client, runtime := wipicrttest.NewRuntime(t)
	client.Audio = backend.NewAudio(&wipicrttest.CountingSink{})

	wipicrttest.MediaCall(t, runtime, wipicrt.WIPICMediaSetVolume, 40)
	if level := client.Audio.Volume(); level != 40 {
		t.Fatalf("sound path volume = %d, want the level the guest set", level)
	}
	// Out of range on either side is clamped rather than wrapped, because the
	// value comes from the guest.
	wipicrttest.MediaCall(t, runtime, wipicrt.WIPICMediaSetVolume, 0xffffffff)
	if level := client.Audio.Volume(); level != 0 {
		t.Fatalf("sound path volume = %d after a negative level, want 0", level)
	}
}

// TestWIPICMediaMuteStateIsRememberedAndSilencesNothing pins MC_mdaSetMuteState
// and MC_mdaGetMuteState at 17 and 18. The local titles mute source 3 at startup
// and go on to play every sound they have, so the mute is not applied to a
// clip; what a title reads back is what it set, which is how one puts the
// handset's own setting back on the way out.
func TestWIPICMediaMuteStateIsRememberedAndSilencesNothing(t *testing.T) {
	client, runtime := wipicrttest.NewRuntime(t)
	sink := &wipicrttest.CountingSink{}
	client.Audio = backend.NewAudio(sink)

	if muted := wipicrttest.MediaCall(t, runtime, wipicrt.WIPICMediaGetMuteState, 3); muted != 0 {
		t.Fatalf("source 3 reads muted = %d before anything set it", muted)
	}
	if result := wipicrttest.MediaCall(t, runtime, wipicrt.WIPICMediaSetMuteState, 3, 1); result != 0 {
		t.Fatalf("set mute state answered %#x", result)
	}
	if muted := wipicrttest.MediaCall(t, runtime, wipicrt.WIPICMediaGetMuteState, 3); muted != 1 {
		t.Fatalf("source 3 reads muted = %d after it was muted", muted)
	}
	if muted := wipicrttest.MediaCall(t, runtime, wipicrt.WIPICMediaGetMuteState, 1); muted != 0 {
		t.Fatalf("source 1 reads muted = %d; only source 3 was set", muted)
	}
	sound := wipicrttest.OneNoteSMAF()
	clip := wipicrttest.MediaCall(t, runtime, wipicrt.WIPICMediaClipCreate, 0, uint32(len(sound)), 0)
	wipicrttest.MediaCall(t, runtime, wipicrt.WIPICMediaClipPutData, clip, wipicrttest.GuestBytes(t, runtime, sound), uint32(len(sound)))
	wipicrttest.MediaCall(t, runtime, wipicrt.WIPICMediaPlay, clip, 0)
	client.Audio.Advance(100 * time.Millisecond)
	if sink.NoteOns == 0 {
		t.Fatal("a source mute silenced a clip")
	}
	wipicrttest.MediaCall(t, runtime, wipicrt.WIPICMediaSetMuteState, 3, 0)
	if muted := wipicrttest.MediaCall(t, runtime, wipicrt.WIPICMediaGetMuteState, 3); muted != 0 {
		t.Fatalf("source 3 reads muted = %d after it was unmuted", muted)
	}
	// The guest picks the source number, so the set it can grow is bounded.
	for source := uint32(100); source < 100+wipicrt.MaxWIPICMutedSources; source++ {
		wipicrttest.MediaCall(t, runtime, wipicrt.WIPICMediaSetMuteState, source, 1)
	}
	if result := wipicrttest.MediaCall(t, runtime, wipicrt.WIPICMediaSetMuteState, 0xffff, 1); result != wipicrt.WipiErrorCode {
		t.Fatalf("a mute state past the bound answered %#x, want M_E_ERROR", result)
	}
	if result := wipicrttest.MediaCall(t, runtime, wipicrt.WIPICMediaSetMuteState, 3, 1); result != 0 {
		t.Fatalf("a source already held answered %#x after the bound was reached", result)
	}
}

// TestWIPICMediaSourceVolumeChangesNothing pins 19. One title hands it a
// source and a percentage from its sound option and gives the same level to
// the playing clip through 26 straight after; 19 has to be answered, and has
// to leave the clip's level and its sound alone.
func TestWIPICMediaSourceVolumeChangesNothing(t *testing.T) {
	client, runtime := wipicrttest.NewRuntime(t)
	sink := &wipicrttest.CountingSink{}
	client.Audio = backend.NewAudio(sink)
	sound := wipicrttest.OneNoteSMAF()
	clip := wipicrttest.MediaCall(t, runtime, wipicrt.WIPICMediaClipCreate, 0, uint32(len(sound)), 0)
	wipicrttest.MediaCall(t, runtime, wipicrt.WIPICMediaClipPutData, clip, wipicrttest.GuestBytes(t, runtime, sound), uint32(len(sound)))

	if result := wipicrttest.MediaCall(t, runtime, wipicrt.WIPICMediaSourceVolume, 7, 0); result != 0 {
		t.Fatalf("source volume answered %#x", result)
	}
	if level := wipicrttest.MediaCall(t, runtime, wipicrt.WIPICMediaClipGetVolume, clip); level != wipicrt.WIPICClipFullVolume {
		t.Fatalf("clip level after a source volume = %d, want %d", level, wipicrt.WIPICClipFullVolume)
	}
	wipicrttest.MediaCall(t, runtime, wipicrt.WIPICMediaPlay, clip, 0)
	client.Audio.Advance(100 * time.Millisecond)
	if sink.NoteOns == 0 {
		t.Fatal("a source volume of zero silenced a clip")
	}
}

// TestWIPICMediaClipVolumeIsTheTitlesOwn pins 14, 25 and 26. A title reads the
// device level or a clip's level and hands it to every clip it fills, and one
// title's own volume setting arrives as that level in steps of twenty. The
// getters used to answer zero and the setter discarded what it was given, so
// the in-game setting did nothing and a title that copied the getter's answer
// was asking for silence.
func TestWIPICMediaClipVolumeIsTheTitlesOwn(t *testing.T) {
	client, runtime := wipicrttest.NewRuntime(t)
	client.Audio = backend.NewAudio(&wipicrttest.CountingSink{})

	if level := wipicrttest.MediaCall(t, runtime, wipicrt.WIPICMediaGetVolume); level != 100 {
		t.Fatalf("device volume reads %d, want 100 before anything set it", level)
	}
	wipicrttest.MediaCall(t, runtime, wipicrt.WIPICMediaSetVolume, 40)
	if level := wipicrttest.MediaCall(t, runtime, wipicrt.WIPICMediaGetVolume); level != 40 {
		t.Fatalf("device volume reads %d, want the 40 just set", level)
	}

	sound := wipicrttest.OneNoteSMAF()
	clip := wipicrttest.MediaCall(t, runtime, wipicrt.WIPICMediaClipCreate, 0, uint32(len(sound)), 0)
	wipicrttest.MediaCall(t, runtime, wipicrt.WIPICMediaClipPutData, clip, wipicrttest.GuestBytes(t, runtime, sound), uint32(len(sound)))
	if level := wipicrttest.MediaCall(t, runtime, wipicrt.WIPICMediaClipGetVolume, clip); level != 100 {
		t.Fatalf("a new clip reads %d, want 100", level)
	}
	// Set between putData and play, which is where the titles set it.
	wipicrttest.MediaCall(t, runtime, wipicrt.WIPICMediaClipSetVolume, clip, 60)
	if result := wipicrttest.MediaCall(t, runtime, wipicrt.WIPICMediaPlay, clip, 0); result != 0 {
		t.Fatalf("play answered %#x", result)
	}
	handle := runtime.WIPICClips[clip].State.Handle
	if level, _, err := client.Audio.SoundVolume(handle); err != nil || level != 60 {
		t.Fatalf("loaded clip plays at %d (%v), want the 60 set before it loaded", level, err)
	}
	// And after, on the sound already playing; out of range is clamped.
	wipicrttest.MediaCall(t, runtime, wipicrt.WIPICMediaClipSetVolume, clip, 0xffffffff)
	if level, _, err := client.Audio.SoundVolume(handle); err != nil || level != 0 {
		t.Fatalf("playing clip is at %d (%v) after a negative level, want 0", level, err)
	}
	wipicrttest.MediaCall(t, runtime, wipicrt.WIPICMediaClipSetVolume, clip, 250)
	if level := wipicrttest.MediaCall(t, runtime, wipicrt.WIPICMediaClipGetVolume, clip); level != 100 {
		t.Fatalf("clip reads %d after 250, want the clamped 100", level)
	}
	if level, _, err := client.Audio.SoundVolume(handle); err != nil || level != 100 {
		t.Fatalf("playing clip is at %d (%v), want 100", level, err)
	}
	// One title sets the level of a clip it never created.
	if result := wipicrttest.MediaCall(t, runtime, wipicrt.WIPICMediaClipSetVolume, 0, 50); result != 0 {
		t.Fatalf("setting a null clip's volume answered %#x", result)
	}
	if result := wipicrttest.MediaCall(t, runtime, wipicrt.WIPICMediaClipGetVolume, 0); result != wipicrt.WipiErrorCode {
		t.Fatalf("reading a null clip's volume answered %#x, want M_E_ERROR", result)
	}
}
