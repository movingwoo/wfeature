package ktf

import (
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/audio/smaf"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
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
	result, err := runtime.HandleWIPICMediaCall(armcore.NewThread(context), function)
	if err != nil {
		t.Fatalf("media function %d error = %v", function, err)
	}
	return result
}

// guestBytes puts Host data where a guest pointer can reach it, which is what
// every one of these calls takes.
func guestBytes(t *testing.T, runtime *initializationRuntime, data []byte) uint32 {
	t.Helper()
	address, err := runtime.AllocateBytes(data)
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
	client.Audio = backend.NewAudio(sink)

	sound := oneNoteSMAF()
	clip := mediaCall(t, runtime, wipicrt.WIPICMediaClipCreate,
		guestBytes(t, runtime, append([]byte("Yamaha_MA2"), 0)), uint32(len(sound)), 0)
	if clip == 0 || runtime.WIPICClips[clip] == nil {
		t.Fatalf("create answered %#x with no clip behind it", clip)
	}
	if runtime.WIPICClips[clip].MediaType != "Yamaha_MA2" {
		t.Fatalf("clip media type = %q, want the type create was given", runtime.WIPICClips[clip].MediaType)
	}

	taken := mediaCall(t, runtime, wipicrt.WIPICMediaClipPutData, clip, guestBytes(t, runtime, sound), uint32(len(sound)))
	if taken != uint32(len(sound)) {
		t.Fatalf("putData took %d of %d bytes", taken, len(sound))
	}

	if result := mediaCall(t, runtime, wipicrt.WIPICMediaPlay, clip, 0); result == wipicrt.WipiErrorCode {
		t.Fatal("play refused a clip holding a sound this build decodes")
	}
	client.Audio.Advance(100 * time.Millisecond)
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
	client.Audio = backend.NewAudio(&countingSink{})

	data := []byte("this is not a sound")
	clip := mediaCall(t, runtime, wipicrt.WIPICMediaClipCreate, 0, uint32(len(data)), 0)
	mediaCall(t, runtime, wipicrt.WIPICMediaClipPutData, clip, guestBytes(t, runtime, data), uint32(len(data)))
	if result := mediaCall(t, runtime, wipicrt.WIPICMediaPlay, clip, 0); result != wipicrt.WipiErrorCode {
		t.Fatalf("play of undecodable data answered %#x, want the failure code", result)
	}
}

// TestWIPICMediaFreeDropsTheClip pins the teardown run four titles make —
// stop, clear, free — and the part that matters is the last one: a clip that
// stayed registered would hold its sound bytes for as long as the game ran.
func TestWIPICMediaFreeDropsTheClip(t *testing.T) {
	client, runtime := newTestRuntime(t)
	client.Audio = backend.NewAudio(&countingSink{})

	sound := oneNoteSMAF()
	clip := mediaCall(t, runtime, wipicrt.WIPICMediaClipCreate, 0, uint32(len(sound)), 0)
	mediaCall(t, runtime, wipicrt.WIPICMediaClipPutData, clip, guestBytes(t, runtime, sound), uint32(len(sound)))
	mediaCall(t, runtime, wipicrt.WIPICMediaPlay, clip, 0)

	mediaCall(t, runtime, wipicrt.WIPICMediaStop, clip)
	mediaCall(t, runtime, wipicrt.WIPICMediaClipClearData, clip)
	if len(runtime.WIPICClips[clip].State.Data) != 0 {
		t.Fatal("clearData left the clip's bytes behind")
	}
	mediaCall(t, runtime, wipicrt.WIPICMediaClipFree, clip)
	if runtime.WIPICClips[clip] != nil {
		t.Fatal("free left the clip registered")
	}
	// A handle nobody created takes no data rather than answering a byte count
	// of -1, which is what the caller would read a failure code as.
	if taken := mediaCall(t, runtime, wipicrt.WIPICMediaClipPutData, clip, guestBytes(t, runtime, sound), uint32(len(sound))); taken != 0 {
		t.Fatalf("putData into a freed clip took %d bytes", taken)
	}
}

// TestWIPICMediaBoundsWhatOneTitleCanRetain covers the title that creates a
// clip per sound effect and frees none of them. The guest picks both the count
// and each clip's size, so without the cap a long session grows without end.
func TestWIPICMediaBoundsWhatOneTitleCanRetain(t *testing.T) {
	client, runtime := newTestRuntime(t)
	client.Audio = backend.NewAudio(&countingSink{})

	for index := 0; index < wipicrt.MaxWIPICMediaClips+8; index++ {
		clip := mediaCall(t, runtime, wipicrt.WIPICMediaClipCreate, 0, 4, 0)
		mediaCall(t, runtime, wipicrt.WIPICMediaClipPutData, clip, guestBytes(t, runtime, []byte("data")), 4)
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
	client, runtime := newTestRuntime(t)
	client.Audio = backend.NewAudio(&countingSink{})

	mediaCall(t, runtime, wipicrt.WIPICMediaSetVolume, 40)
	if level := client.Audio.Volume(); level != 40 {
		t.Fatalf("sound path volume = %d, want the level the guest set", level)
	}
	// Out of range on either side is clamped rather than wrapped, because the
	// value comes from the guest.
	mediaCall(t, runtime, wipicrt.WIPICMediaSetVolume, 0xffffffff)
	if level := client.Audio.Volume(); level != 0 {
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
	clock := wipicrt.NewManualClock(runtime.ClockBase)
	client.Clock = clock
	sink := &audioPauseProbe{}
	client.Audio = backend.NewAudioWithClock(sink, clock.Now)

	sound := oneNoteSMAF()
	clip := mediaCall(t, runtime, wipicrt.WIPICMediaClipCreate, 0, uint32(len(sound)), 0)
	mediaCall(t, runtime, wipicrt.WIPICMediaClipPutData, clip, guestBytes(t, runtime, sound), uint32(len(sound)))
	// An authored decoded score: a held note whose gate the pause has to keep.
	handle, err := client.Audio.LoadEvents([]smaf.Event{
		{Type: smaf.EventNoteOn, Channel: 1, Note: 60, Velocity: 100},
		{Time: 500, Type: smaf.EventNoteOff, Channel: 1, Note: 60},
		{Time: 1000, Type: smaf.EventEnd},
	})
	if err != nil {
		t.Fatal(err)
	}
	state := &runtime.WIPICClips[clip].State
	state.Handle, state.Loaded = handle, true

	if result := mediaCall(t, runtime, wipicrt.WIPICMediaPause, clip); result != wipicrt.WipiErrorCode {
		t.Fatalf("pause of a clip that is not playing answered %#x, want M_E_ERROR", result)
	}
	if result := mediaCall(t, runtime, wipicrt.WIPICMediaPlay, clip, 1); result != 0 {
		t.Fatalf("play answered %#x", result)
	}
	client.ServiceAudio()
	clock.Advance(250 * time.Millisecond)
	if result := mediaCall(t, runtime, wipicrt.WIPICMediaPause, clip); result != 0 {
		t.Fatalf("pause answered %#x, want 0", result)
	}
	if !client.Audio.Paused(handle) || len(sink.stopped) == 0 || sink.stopped[len(sink.stopped)-1] != handle {
		t.Fatal("pause did not hold the clip and stop its output")
	}
	if result := mediaCall(t, runtime, wipicrt.WIPICMediaPause, clip); result != wipicrt.WipiErrorCode {
		t.Fatalf("second pause answered %#x, want M_E_ERROR", result)
	}
	sink.events = nil
	clock.Advance(10 * time.Second)
	client.ServiceAudio()
	if len(sink.events) != 0 {
		t.Fatal("a paused clip emitted output")
	}
	if result := mediaCall(t, runtime, wipicrt.WIPICMediaResume, clip); result != 0 {
		t.Fatalf("resume answered %#x, want 0", result)
	}
	if len(sink.resumed) != 1 || sink.resumed[0].note != 60 || sink.resumed[0].age != 250*time.Millisecond {
		t.Fatalf("resumed notes = %+v, want the held note at age 250ms", sink.resumed)
	}
	if result := mediaCall(t, runtime, wipicrt.WIPICMediaResume, clip); result != wipicrt.WipiErrorCode {
		t.Fatalf("resume of a playing clip answered %#x, want M_E_ERROR", result)
	}
	sink.events = nil
	clock.Advance(249 * time.Millisecond)
	client.ServiceAudio()
	if len(sink.ofType(smaf.EventNoteOff)) != 0 {
		t.Fatal("the held note ended before the rest of its gate")
	}
	clock.Advance(time.Millisecond)
	client.ServiceAudio()
	if offs := sink.ofType(smaf.EventNoteOff); len(offs) != 1 {
		t.Fatalf("note-offs after the remaining gate = %+v, want one", offs)
	}
	mediaCall(t, runtime, wipicrt.WIPICMediaStop, clip)
	for _, function := range []uint32{wipicrt.WIPICMediaPause, wipicrt.WIPICMediaResume} {
		if result := mediaCall(t, runtime, function, clip); result != wipicrt.WipiErrorCode {
			t.Fatalf("function %d on a stopped clip answered %#x, want M_E_ERROR", function, result)
		}
		// One title pauses and resumes a clip it never created.
		if result := mediaCall(t, runtime, function, 0); result != wipicrt.WipiErrorCode {
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
	client.Audio = backend.NewAudio(sink)

	if muted := mediaCall(t, runtime, wipicrt.WIPICMediaGetMuteState, 3); muted != 0 {
		t.Fatalf("source 3 reads muted = %d before anything set it", muted)
	}
	if result := mediaCall(t, runtime, wipicrt.WIPICMediaSetMuteState, 3, 1); result != 0 {
		t.Fatalf("set mute state answered %#x", result)
	}
	if muted := mediaCall(t, runtime, wipicrt.WIPICMediaGetMuteState, 3); muted != 1 {
		t.Fatalf("source 3 reads muted = %d after it was muted", muted)
	}
	if muted := mediaCall(t, runtime, wipicrt.WIPICMediaGetMuteState, 1); muted != 0 {
		t.Fatalf("source 1 reads muted = %d; only source 3 was set", muted)
	}
	sound := oneNoteSMAF()
	clip := mediaCall(t, runtime, wipicrt.WIPICMediaClipCreate, 0, uint32(len(sound)), 0)
	mediaCall(t, runtime, wipicrt.WIPICMediaClipPutData, clip, guestBytes(t, runtime, sound), uint32(len(sound)))
	mediaCall(t, runtime, wipicrt.WIPICMediaPlay, clip, 0)
	client.Audio.Advance(100 * time.Millisecond)
	if sink.noteOns == 0 {
		t.Fatal("a source mute silenced a clip")
	}
	mediaCall(t, runtime, wipicrt.WIPICMediaSetMuteState, 3, 0)
	if muted := mediaCall(t, runtime, wipicrt.WIPICMediaGetMuteState, 3); muted != 0 {
		t.Fatalf("source 3 reads muted = %d after it was unmuted", muted)
	}
	// The guest picks the source number, so the set it can grow is bounded.
	for source := uint32(100); source < 100+wipicrt.MaxWIPICMutedSources; source++ {
		mediaCall(t, runtime, wipicrt.WIPICMediaSetMuteState, source, 1)
	}
	if result := mediaCall(t, runtime, wipicrt.WIPICMediaSetMuteState, 0xffff, 1); result != wipicrt.WipiErrorCode {
		t.Fatalf("a mute state past the bound answered %#x, want M_E_ERROR", result)
	}
	if result := mediaCall(t, runtime, wipicrt.WIPICMediaSetMuteState, 3, 1); result != 0 {
		t.Fatalf("a source already held answered %#x after the bound was reached", result)
	}
}

// TestWIPICMediaSourceVolumeChangesNothing pins 19. One title hands it a
// source and a percentage from its sound option and gives the same level to
// the playing clip through 26 straight after; 19 has to be answered, and has
// to leave the clip's level and its sound alone.
func TestWIPICMediaSourceVolumeChangesNothing(t *testing.T) {
	client, runtime := newTestRuntime(t)
	sink := &countingSink{}
	client.Audio = backend.NewAudio(sink)
	sound := oneNoteSMAF()
	clip := mediaCall(t, runtime, wipicrt.WIPICMediaClipCreate, 0, uint32(len(sound)), 0)
	mediaCall(t, runtime, wipicrt.WIPICMediaClipPutData, clip, guestBytes(t, runtime, sound), uint32(len(sound)))

	if result := mediaCall(t, runtime, wipicrt.WIPICMediaSourceVolume, 7, 0); result != 0 {
		t.Fatalf("source volume answered %#x", result)
	}
	if level := mediaCall(t, runtime, wipicrt.WIPICMediaClipGetVolume, clip); level != wipicrt.WIPICClipFullVolume {
		t.Fatalf("clip level after a source volume = %d, want %d", level, wipicrt.WIPICClipFullVolume)
	}
	mediaCall(t, runtime, wipicrt.WIPICMediaPlay, clip, 0)
	client.Audio.Advance(100 * time.Millisecond)
	if sink.noteOns == 0 {
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
	client, runtime := newTestRuntime(t)
	client.Audio = backend.NewAudio(&countingSink{})

	if level := mediaCall(t, runtime, wipicrt.WIPICMediaGetVolume); level != 100 {
		t.Fatalf("device volume reads %d, want 100 before anything set it", level)
	}
	mediaCall(t, runtime, wipicrt.WIPICMediaSetVolume, 40)
	if level := mediaCall(t, runtime, wipicrt.WIPICMediaGetVolume); level != 40 {
		t.Fatalf("device volume reads %d, want the 40 just set", level)
	}

	sound := oneNoteSMAF()
	clip := mediaCall(t, runtime, wipicrt.WIPICMediaClipCreate, 0, uint32(len(sound)), 0)
	mediaCall(t, runtime, wipicrt.WIPICMediaClipPutData, clip, guestBytes(t, runtime, sound), uint32(len(sound)))
	if level := mediaCall(t, runtime, wipicrt.WIPICMediaClipGetVolume, clip); level != 100 {
		t.Fatalf("a new clip reads %d, want 100", level)
	}
	// Set between putData and play, which is where the titles set it.
	mediaCall(t, runtime, wipicrt.WIPICMediaClipSetVolume, clip, 60)
	if result := mediaCall(t, runtime, wipicrt.WIPICMediaPlay, clip, 0); result != 0 {
		t.Fatalf("play answered %#x", result)
	}
	handle := runtime.WIPICClips[clip].State.Handle
	if level, _, err := client.Audio.SoundVolume(handle); err != nil || level != 60 {
		t.Fatalf("loaded clip plays at %d (%v), want the 60 set before it loaded", level, err)
	}
	// And after, on the sound already playing; out of range is clamped.
	mediaCall(t, runtime, wipicrt.WIPICMediaClipSetVolume, clip, 0xffffffff)
	if level, _, err := client.Audio.SoundVolume(handle); err != nil || level != 0 {
		t.Fatalf("playing clip is at %d (%v) after a negative level, want 0", level, err)
	}
	mediaCall(t, runtime, wipicrt.WIPICMediaClipSetVolume, clip, 250)
	if level := mediaCall(t, runtime, wipicrt.WIPICMediaClipGetVolume, clip); level != 100 {
		t.Fatalf("clip reads %d after 250, want the clamped 100", level)
	}
	if level, _, err := client.Audio.SoundVolume(handle); err != nil || level != 100 {
		t.Fatalf("playing clip is at %d (%v), want 100", level, err)
	}
	// One title sets the level of a clip it never created.
	if result := mediaCall(t, runtime, wipicrt.WIPICMediaClipSetVolume, 0, 50); result != 0 {
		t.Fatalf("setting a null clip's volume answered %#x", result)
	}
	if result := mediaCall(t, runtime, wipicrt.WIPICMediaClipGetVolume, 0); result != wipicrt.WipiErrorCode {
		t.Fatalf("reading a null clip's volume answered %#x, want M_E_ERROR", result)
	}
}
