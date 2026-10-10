package ktf

import (
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/audio/smaf"
	"github.com/movingwoo/wfeature/internal/backend"
)

// oneNoteSMAF is the smallest file the sound path plays: a mobile score track
// with a single note. It is built here rather than shipped as a fixture for
// the reason every other fixture in this repository is authored — nothing with
// someone else's provenance goes in the tree.
func oneNoteSMAF() []byte {
	sequence := []byte{
		0x05, 0x90, 60, 100, 0x05,
		0x00, 0xff, 0x2f, 0x00,
	}
	track := append([]byte{2, 0, 2, 2}, make([]byte, 16)...)
	track = append(track, smafChunk("Mtsq", sequence)...)
	body := smafChunk("MTR\x00", track)
	file := make([]byte, 8)
	copy(file, "MMMD")
	length := uint32(len(body) + 2)
	file[4], file[5], file[6], file[7] = byte(length>>24), byte(length>>16), byte(length>>8), byte(length)
	return append(append(file, body...), 0, 0)
}

func smafChunk(tag string, payload []byte) []byte {
	header := make([]byte, 8)
	copy(header, tag)
	length := uint32(len(payload))
	header[4], header[5], header[6], header[7] = byte(length>>24), byte(length>>16), byte(length>>8), byte(length)
	return append(header, payload...)
}

// countingSink counts what reached the Host, which is the only way to tell a
// sound that played from one that was accepted and dropped.
type countingSink struct {
	noteOns int
	waves   int
}

func (sink *countingSink) PlayWave(uint8, uint32, []int16) { sink.waves++ }
func (sink *countingSink) MIDINoteOn(_, _, _ uint8)        { sink.noteOns++ }
func (sink *countingSink) MIDINoteOff(_, _, _ uint8)       {}
func (sink *countingSink) MIDIProgramChange(_, _ uint8)    {}
func (sink *countingSink) MIDIControlChange(_, _, _ uint8) {}
func (sink *countingSink) MIDIPitchBend(_ uint8, _ uint16) {}
func (sink *countingSink) MIDISysEx([]byte)                {}

// mediaCall drives one media function with the arguments a guest would pass.
func mediaCall(t *testing.T, runtime *initializationRuntime, function uint32, arguments ...uint32) uint32 {
	t.Helper()
	context := armcore.NewContext()
	for index, value := range arguments {
		context.Registers[index] = value
	}
	result, err := runtime.handleWIPICMediaCall(armcore.NewThread(context), function)
	if err != nil {
		t.Fatalf("media function %d error = %v", function, err)
	}
	return result
}

// guestBytes puts Host data where a guest pointer can reach it, which is what
// every one of these calls takes.
func guestBytes(t *testing.T, runtime *initializationRuntime, data []byte) uint32 {
	t.Helper()
	address, err := runtime.allocateBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	return address
}

// TestWIPICMediaPlaysAClipTheGuestFilled is the whole reason this block exists:
// a title that keeps its sound in C creates a clip, copies an SMAF file into it
// and plays it, and every one of those calls used to be accepted and thrown
// away. Counting what reached the sink is what tells the two apart — the calls
// answered success either way.
func TestWIPICMediaPlaysAClipTheGuestFilled(t *testing.T) {
	client, runtime := newTestRuntime(t)
	sink := &countingSink{}
	client.audio = backend.NewAudio(sink)

	sound := oneNoteSMAF()
	clip := mediaCall(t, runtime, wipicMediaClipCreate,
		guestBytes(t, runtime, append([]byte("Yamaha_MA2"), 0)), uint32(len(sound)), 0)
	if clip == 0 || runtime.wipicClips[clip] == nil {
		t.Fatalf("create answered %#x with no clip behind it", clip)
	}
	if runtime.wipicClips[clip].mediaType != "Yamaha_MA2" {
		t.Fatalf("clip media type = %q, want the type create was given", runtime.wipicClips[clip].mediaType)
	}

	taken := mediaCall(t, runtime, wipicMediaClipPutData, clip, guestBytes(t, runtime, sound), uint32(len(sound)))
	if taken != uint32(len(sound)) {
		t.Fatalf("putData took %d of %d bytes", taken, len(sound))
	}

	if result := mediaCall(t, runtime, wipicMediaPlay, clip, 0); result == wipiErrorCode {
		t.Fatal("play refused a clip holding a sound this build decodes")
	}
	client.audio.Advance(100 * time.Millisecond)
	if sink.noteOns == 0 {
		t.Fatal("nothing reached the sink after a play")
	}
}

// TestWIPICMediaRefusesAClipItCannotDecode covers the other half. A game whose
// sound this build cannot read has to be told, because it branches on the
// answer; failing the call instead would take the game down over a missing
// codec.
func TestWIPICMediaRefusesAClipItCannotDecode(t *testing.T) {
	client, runtime := newTestRuntime(t)
	client.audio = backend.NewAudio(&countingSink{})

	data := []byte("this is not a sound")
	clip := mediaCall(t, runtime, wipicMediaClipCreate, 0, uint32(len(data)), 0)
	mediaCall(t, runtime, wipicMediaClipPutData, clip, guestBytes(t, runtime, data), uint32(len(data)))
	if result := mediaCall(t, runtime, wipicMediaPlay, clip, 0); result != wipiErrorCode {
		t.Fatalf("play of undecodable data answered %#x, want the failure code", result)
	}
}

// TestWIPICMediaFreeDropsTheClip pins the teardown run four titles make —
// stop, clear, free — and the part that matters is the last one: a clip that
// stayed registered would hold its sound bytes for as long as the game ran.
func TestWIPICMediaFreeDropsTheClip(t *testing.T) {
	client, runtime := newTestRuntime(t)
	client.audio = backend.NewAudio(&countingSink{})

	sound := oneNoteSMAF()
	clip := mediaCall(t, runtime, wipicMediaClipCreate, 0, uint32(len(sound)), 0)
	mediaCall(t, runtime, wipicMediaClipPutData, clip, guestBytes(t, runtime, sound), uint32(len(sound)))
	mediaCall(t, runtime, wipicMediaPlay, clip, 0)

	mediaCall(t, runtime, wipicMediaStop, clip)
	mediaCall(t, runtime, wipicMediaClipClearData, clip)
	if len(runtime.wipicClips[clip].state.data) != 0 {
		t.Fatal("clearData left the clip's bytes behind")
	}
	mediaCall(t, runtime, wipicMediaClipFree, clip)
	if runtime.wipicClips[clip] != nil {
		t.Fatal("free left the clip registered")
	}
	// A handle nobody created takes no data rather than answering a byte count
	// of -1, which is what the caller would read a failure code as.
	if taken := mediaCall(t, runtime, wipicMediaClipPutData, clip, guestBytes(t, runtime, sound), uint32(len(sound))); taken != 0 {
		t.Fatalf("putData into a freed clip took %d bytes", taken)
	}
}

// TestWIPICMediaBoundsWhatOneTitleCanRetain covers the title that creates a
// clip per sound effect and frees none of them. The guest picks both the count
// and each clip's size, so without the cap a long session grows without end.
func TestWIPICMediaBoundsWhatOneTitleCanRetain(t *testing.T) {
	client, runtime := newTestRuntime(t)
	client.audio = backend.NewAudio(&countingSink{})

	for index := 0; index < maxWIPICMediaClips+8; index++ {
		clip := mediaCall(t, runtime, wipicMediaClipCreate, 0, 4, 0)
		mediaCall(t, runtime, wipicMediaClipPutData, clip, guestBytes(t, runtime, []byte("data")), 4)
	}
	if len(runtime.wipicClips) > maxWIPICMediaClips {
		t.Fatalf("%d clips retained, want at most %d", len(runtime.wipicClips), maxWIPICMediaClips)
	}
}

// TestWIPICMediaVolumeReachesTheSoundPath is the difference between storing the
// level and honouring it. A title fades its music out by walking this to zero,
// and a runtime that remembers the number and keeps playing at full has heard
// the request and disregarded it.
func TestWIPICMediaVolumeReachesTheSoundPath(t *testing.T) {
	client, runtime := newTestRuntime(t)
	client.audio = backend.NewAudio(&countingSink{})

	mediaCall(t, runtime, wipicMediaSetVolume, 40)
	if level := client.audio.Volume(); level != 40 {
		t.Fatalf("sound path volume = %d, want the level the guest set", level)
	}
	// Out of range on either side is clamped rather than wrapped, because the
	// value comes from the guest.
	mediaCall(t, runtime, wipicMediaSetVolume, 0xffffffff)
	if level := client.audio.Volume(); level != 0 {
		t.Fatalf("sound path volume = %d after a negative level, want 0", level)
	}
}

// TestWIPICMediaPauseAndResumeKeepTheClipsPlace covers MC_mdaPause and
// MC_mdaResume at 9 and 10, which a title reaches from its pauseApp and
// resumeApp with the clip it is playing. Before they were bound both were
// accepted and discarded, so a parked title's music ran on and its resume had
// nothing to pick up. Pause holds the clip where it is, resume continues from
// there, and every call the specification calls a failure answers M_E_ERROR.
func TestWIPICMediaPauseAndResumeKeepTheClipsPlace(t *testing.T) {
	client, runtime := newTestRuntime(t)
	clock := NewManualClock(runtime.clockBase)
	client.clock = clock
	sink := &audioPauseProbe{}
	client.audio = backend.NewAudioWithClock(sink, clock.Now)

	sound := oneNoteSMAF()
	clip := mediaCall(t, runtime, wipicMediaClipCreate, 0, uint32(len(sound)), 0)
	mediaCall(t, runtime, wipicMediaClipPutData, clip, guestBytes(t, runtime, sound), uint32(len(sound)))
	// An authored decoded score: a held note whose gate the pause has to keep.
	handle, err := client.audio.LoadEvents([]smaf.Event{
		{Type: smaf.EventNoteOn, Channel: 1, Note: 60, Velocity: 100},
		{Time: 500, Type: smaf.EventNoteOff, Channel: 1, Note: 60},
		{Time: 1000, Type: smaf.EventEnd},
	})
	if err != nil {
		t.Fatal(err)
	}
	state := &runtime.wipicClips[clip].state
	state.handle, state.loaded = handle, true

	if result := mediaCall(t, runtime, wipicMediaPause, clip); result != wipiErrorCode {
		t.Fatalf("pause of a clip that is not playing answered %#x, want M_E_ERROR", result)
	}
	if result := mediaCall(t, runtime, wipicMediaPlay, clip, 1); result != 0 {
		t.Fatalf("play answered %#x", result)
	}
	client.serviceAudio()
	clock.Advance(250 * time.Millisecond)
	if result := mediaCall(t, runtime, wipicMediaPause, clip); result != 0 {
		t.Fatalf("pause answered %#x, want 0", result)
	}
	if !client.audio.Paused(handle) || len(sink.stopped) == 0 || sink.stopped[len(sink.stopped)-1] != handle {
		t.Fatal("pause did not hold the clip and stop its output")
	}
	if result := mediaCall(t, runtime, wipicMediaPause, clip); result != wipiErrorCode {
		t.Fatalf("second pause answered %#x, want M_E_ERROR", result)
	}
	sink.events = nil
	clock.Advance(10 * time.Second)
	client.serviceAudio()
	if len(sink.events) != 0 {
		t.Fatal("a paused clip emitted output")
	}
	if result := mediaCall(t, runtime, wipicMediaResume, clip); result != 0 {
		t.Fatalf("resume answered %#x, want 0", result)
	}
	if len(sink.resumed) != 1 || sink.resumed[0].note != 60 || sink.resumed[0].age != 250*time.Millisecond {
		t.Fatalf("resumed notes = %+v, want the held note at age 250ms", sink.resumed)
	}
	if result := mediaCall(t, runtime, wipicMediaResume, clip); result != wipiErrorCode {
		t.Fatalf("resume of a playing clip answered %#x, want M_E_ERROR", result)
	}
	sink.events = nil
	clock.Advance(249 * time.Millisecond)
	client.serviceAudio()
	if len(sink.ofType(smaf.EventNoteOff)) != 0 {
		t.Fatal("the held note ended before the rest of its gate")
	}
	clock.Advance(time.Millisecond)
	client.serviceAudio()
	if offs := sink.ofType(smaf.EventNoteOff); len(offs) != 1 {
		t.Fatalf("note-offs after the remaining gate = %+v, want one", offs)
	}
	mediaCall(t, runtime, wipicMediaStop, clip)
	for _, function := range []uint32{wipicMediaPause, wipicMediaResume} {
		if result := mediaCall(t, runtime, function, clip); result != wipiErrorCode {
			t.Fatalf("function %d on a stopped clip answered %#x, want M_E_ERROR", function, result)
		}
		// One title pauses and resumes a clip it never created.
		if result := mediaCall(t, runtime, function, 0); result != wipiErrorCode {
			t.Fatalf("function %d on a null clip answered %#x, want M_E_ERROR", function, result)
		}
	}
}

// TestWIPICMediaMuteStateIsRememberedAndSilencesNothing pins MC_mdaSetMuteState
// and MC_mdaGetMuteState at 17 and 18. The local titles mute source 3 at startup
// and go on to play every sound they have, so the mute is not applied to a
// clip; what a title reads back is what it set, which is how one puts the
// handset's own setting back on the way out.
func TestWIPICMediaMuteStateIsRememberedAndSilencesNothing(t *testing.T) {
	client, runtime := newTestRuntime(t)
	sink := &countingSink{}
	client.audio = backend.NewAudio(sink)

	if muted := mediaCall(t, runtime, wipicMediaGetMuteState, 3); muted != 0 {
		t.Fatalf("source 3 reads muted = %d before anything set it", muted)
	}
	if result := mediaCall(t, runtime, wipicMediaSetMuteState, 3, 1); result != 0 {
		t.Fatalf("set mute state answered %#x", result)
	}
	if muted := mediaCall(t, runtime, wipicMediaGetMuteState, 3); muted != 1 {
		t.Fatalf("source 3 reads muted = %d after it was muted", muted)
	}
	if muted := mediaCall(t, runtime, wipicMediaGetMuteState, 1); muted != 0 {
		t.Fatalf("source 1 reads muted = %d; only source 3 was set", muted)
	}
	sound := oneNoteSMAF()
	clip := mediaCall(t, runtime, wipicMediaClipCreate, 0, uint32(len(sound)), 0)
	mediaCall(t, runtime, wipicMediaClipPutData, clip, guestBytes(t, runtime, sound), uint32(len(sound)))
	mediaCall(t, runtime, wipicMediaPlay, clip, 0)
	client.audio.Advance(100 * time.Millisecond)
	if sink.noteOns == 0 {
		t.Fatal("a source mute silenced a clip")
	}
	mediaCall(t, runtime, wipicMediaSetMuteState, 3, 0)
	if muted := mediaCall(t, runtime, wipicMediaGetMuteState, 3); muted != 0 {
		t.Fatalf("source 3 reads muted = %d after it was unmuted", muted)
	}
	// The guest picks the source number, so the set it can grow is bounded.
	for source := uint32(100); source < 100+maxWIPICMutedSources; source++ {
		mediaCall(t, runtime, wipicMediaSetMuteState, source, 1)
	}
	if result := mediaCall(t, runtime, wipicMediaSetMuteState, 0xffff, 1); result != wipiErrorCode {
		t.Fatalf("a mute state past the bound answered %#x, want M_E_ERROR", result)
	}
	if result := mediaCall(t, runtime, wipicMediaSetMuteState, 3, 1); result != 0 {
		t.Fatalf("a source already held answered %#x after the bound was reached", result)
	}
}

// TestWIPICMediaClipVolumeIsTheTitlesOwn pins 14, 25 and 26. A title reads the
// device level or a clip's level and hands it to every clip it fills, and one
// title's own volume setting arrives as that level in steps of twenty. The
// getters used to answer zero and the setter discarded what it was given, so
// the in-game setting did nothing and a title that copied the getter's answer
// was asking for silence.
func TestWIPICMediaClipVolumeIsTheTitlesOwn(t *testing.T) {
	client, runtime := newTestRuntime(t)
	client.audio = backend.NewAudio(&countingSink{})

	if level := mediaCall(t, runtime, wipicMediaGetVolume); level != 100 {
		t.Fatalf("device volume reads %d, want 100 before anything set it", level)
	}
	mediaCall(t, runtime, wipicMediaSetVolume, 40)
	if level := mediaCall(t, runtime, wipicMediaGetVolume); level != 40 {
		t.Fatalf("device volume reads %d, want the 40 just set", level)
	}

	sound := oneNoteSMAF()
	clip := mediaCall(t, runtime, wipicMediaClipCreate, 0, uint32(len(sound)), 0)
	mediaCall(t, runtime, wipicMediaClipPutData, clip, guestBytes(t, runtime, sound), uint32(len(sound)))
	if level := mediaCall(t, runtime, wipicMediaClipGetVolume, clip); level != 100 {
		t.Fatalf("a new clip reads %d, want 100", level)
	}
	// Set between putData and play, which is where the titles set it.
	mediaCall(t, runtime, wipicMediaClipSetVolume, clip, 60)
	if result := mediaCall(t, runtime, wipicMediaPlay, clip, 0); result != 0 {
		t.Fatalf("play answered %#x", result)
	}
	handle := runtime.wipicClips[clip].state.handle
	if level, _, err := client.audio.SoundVolume(handle); err != nil || level != 60 {
		t.Fatalf("loaded clip plays at %d (%v), want the 60 set before it loaded", level, err)
	}
	// And after, on the sound already playing; out of range is clamped.
	mediaCall(t, runtime, wipicMediaClipSetVolume, clip, 0xffffffff)
	if level, _, err := client.audio.SoundVolume(handle); err != nil || level != 0 {
		t.Fatalf("playing clip is at %d (%v) after a negative level, want 0", level, err)
	}
	mediaCall(t, runtime, wipicMediaClipSetVolume, clip, 250)
	if level := mediaCall(t, runtime, wipicMediaClipGetVolume, clip); level != 100 {
		t.Fatalf("clip reads %d after 250, want the clamped 100", level)
	}
	if level, _, err := client.audio.SoundVolume(handle); err != nil || level != 100 {
		t.Fatalf("playing clip is at %d (%v), want 100", level, err)
	}
	// One title sets the level of a clip it never created.
	if result := mediaCall(t, runtime, wipicMediaClipSetVolume, 0, 50); result != 0 {
		t.Fatalf("setting a null clip's volume answered %#x", result)
	}
	if result := mediaCall(t, runtime, wipicMediaClipGetVolume, 0); result != wipiErrorCode {
		t.Fatalf("reading a null clip's volume answered %#x, want M_E_ERROR", result)
	}
}
