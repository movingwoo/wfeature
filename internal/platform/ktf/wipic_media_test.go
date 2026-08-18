package ktf

import (
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
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
