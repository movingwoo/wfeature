package lgt

import (
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
)

// recordingSink counts what reached the audio device, which is the only way to
// tell a clip that played from one that was accepted and dropped — the
// difference this whole block exists to remove.
type recordingSink struct{ notes int }

func (sink *recordingSink) PlayWave(uint8, uint32, []int16) {}
func (sink *recordingSink) MIDINoteOn(_, _, _ uint8)        { sink.notes++ }
func (sink *recordingSink) MIDINoteOff(_, _, _ uint8)       {}
func (sink *recordingSink) MIDIProgramChange(_, _ uint8)    {}
func (sink *recordingSink) MIDIControlChange(_, _, _ uint8) {}
func (sink *recordingSink) MIDIPitchBend(_ uint8, _ uint16) {}
func (sink *recordingSink) MIDISysEx([]byte)                {}

func mediaClient(t *testing.T, sink backend.AudioSink) *Client {
	t.Helper()
	archive, err := Open(fixtureArchive(t))
	if err != nil {
		t.Fatal(err)
	}
	client, err := Load(archive, Options{Width: 16, Height: 8, AudioSink: sink})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

// oneNoteSound is the smallest sound the audio path decodes: a mobile-format
// score track holding one note.
func oneNoteSound(t *testing.T) []byte {
	t.Helper()
	sequence := []byte{
		0x05, 0x90, 60, 100, 0x05, // duration 5, note on ch0 note 60 velocity 100, gate 5
		0x00, 0xff, 0x2f, 0x00, // end of stream
	}
	track := append([]byte{
		2, 0, // mobile, no compression
		2, 2, // four-millisecond timebase
	}, make([]byte, 16)...) // channel status: all "no care"
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

// writeGuest puts bytes somewhere the guest can address and answers where.
func writeGuest(t *testing.T, client *Client, data []byte) uint32 {
	t.Helper()
	address, err := client.allocateBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	return address
}

// TestAClipPlaysWhatWasPutIntoIt is the contract the media block exists for.
// Every one of these calls used to answer success without playing, which a
// game cannot tell apart from silence it asked for.
func TestAClipPlaysWhatWasPutIntoIt(t *testing.T) {
	sink := &recordingSink{}
	client := mediaClient(t, sink)
	sound := oneNoteSound(t)

	clip := callSlot(t, client, slotClipCreate, 0, uint32(len(sound)), 0)
	if clip == 0 {
		t.Fatal("MC_mdaClipCreate answered a null clip")
	}
	data := writeGuest(t, client, sound)
	if taken := callSlot(t, client, slotClipPutData, clip, data, uint32(len(sound))); taken != uint32(len(sound)) {
		t.Fatalf("putData took %d of %d bytes", taken, len(sound))
	}
	if result := int32(callSlot(t, client, slotClipPlay, clip, 0)); result != wipiSuccess {
		t.Fatalf("play = %d, want success", result)
	}
	// Playback only reaches the sink as the guest's clock moves, which is what
	// serviceAudio does at the end of a tick.
	client.clock.advance(50 * time.Millisecond)
	client.serviceAudio()
	if sink.notes == 0 {
		t.Fatal("the clip was accepted and played nothing")
	}
}

// TestPlayingAnEmptyClipFails pins the answer for a clip that was never
// filled. Reporting success would have a game wait for a sound to end.
func TestPlayingAnEmptyClipFails(t *testing.T) {
	client := mediaClient(t, nil)
	clip := callSlot(t, client, slotClipCreate, 0, 0, 0)
	if result := int32(callSlot(t, client, slotClipPlay, clip, 0)); result != wipiError {
		t.Fatalf("playing an empty clip = %d, want an error", result)
	}
	if result := int32(callSlot(t, client, slotClipPlay, clip+0x1000, 0)); result != wipiError {
		t.Fatalf("playing a clip nobody created = %d, want an error", result)
	}
}

// TestClipDataIsReplacedNotAppended covers clearData, which a game calls
// between two sounds it plays through one clip.
func TestClipDataIsReplacedNotAppended(t *testing.T) {
	client := mediaClient(t, nil)
	sound := oneNoteSound(t)
	clip := callSlot(t, client, slotClipCreate, 0, uint32(len(sound)), 0)
	data := writeGuest(t, client, sound)

	callSlot(t, client, slotClipPutData, clip, data, uint32(len(sound)))
	callSlot(t, client, slotClipClearData, clip)
	if held := len(client.clips[clip].data); held != 0 {
		t.Fatalf("clearData left %d bytes", held)
	}
	callSlot(t, client, slotClipPutData, clip, data, uint32(len(sound)))
	if held := len(client.clips[clip].data); held != len(sound) {
		t.Fatalf("the clip holds %d bytes after a refill, want %d", held, len(sound))
	}
}

// TestVolumeReadsBackWhatWasWritten is what the global volume pair has to do:
// a title reads the level it found, sets its own, and restores what it read on
// the way out. A getter that ignored the setter would leave the handset at the
// game's volume for good.
func TestVolumeReadsBackWhatWasWritten(t *testing.T) {
	client := mediaClient(t, nil)
	before := callSlot(t, client, slotGetVolume)
	callSlot(t, client, slotSetVolume, 40)
	if got := callSlot(t, client, slotGetVolume); got != 40 {
		t.Fatalf("volume = %d after setting 40", got)
	}
	callSlot(t, client, slotSetVolume, before)
	if got := callSlot(t, client, slotGetVolume); got != before {
		t.Fatalf("volume = %d after restoring %d", got, before)
	}
	// Out of range is clamped rather than stored, because the level is handed
	// straight back to a caller that indexes with it.
	callSlot(t, client, slotSetVolume, 0xffffffff)
	if got := callSlot(t, client, slotGetVolume); got != 0 {
		t.Fatalf("a negative volume read back as %d", got)
	}
	callSlot(t, client, slotSetVolume, 1000)
	if got := callSlot(t, client, slotGetVolume); got != mediaMaxVolume {
		t.Fatalf("an over-range volume read back as %d", got)
	}
}

// TestSourceVolumeAndMuteAreKeptPerSource pins that the four source-keyed
// slots do not share one value. A title sets one source and reads another; a
// single shared field would answer with its own write.
func TestSourceVolumeAndMuteAreKeptPerSource(t *testing.T) {
	client := mediaClient(t, nil)
	callSlot(t, client, slotSetSourceVolume, 11, 30)
	if got := callSlot(t, client, slotGetSourceVolume, 11); got != 30 {
		t.Fatalf("source 11 volume = %d, want 30", got)
	}
	if got := callSlot(t, client, slotGetSourceVolume, 6); got != mediaMaxVolume {
		t.Fatalf("an untouched source read back as %d", got)
	}
	callSlot(t, client, slotSetMuteState, 6, 1)
	if got := callSlot(t, client, slotGetMuteState, 6); got != 1 {
		t.Fatalf("source 6 mute = %d, want 1", got)
	}
	if got := callSlot(t, client, slotGetMuteState, 11); got != 0 {
		t.Fatalf("source 11 mute = %d, want 0", got)
	}
}

// TestMutingASourceDoesNotSilenceEveryClip guards the reading that cost a
// title its sound: one title mutes source six during its own startup and then
// plays, so a per-source mute taken as a global one plays nothing at all.
func TestMutingASourceDoesNotSilenceEveryClip(t *testing.T) {
	sink := &recordingSink{}
	client := mediaClient(t, sink)
	sound := oneNoteSound(t)

	callSlot(t, client, slotSetMuteState, 6, 1)
	clip := callSlot(t, client, slotClipCreate, 0, uint32(len(sound)), 0)
	callSlot(t, client, slotClipPutData, clip, writeGuest(t, client, sound), uint32(len(sound)))
	if result := int32(callSlot(t, client, slotClipPlay, clip, 0)); result != wipiSuccess {
		t.Fatalf("play with a muted source = %d, want success", result)
	}
	client.clock.advance(50 * time.Millisecond)
	client.serviceAudio()
	if sink.notes == 0 {
		t.Fatal("muting one source silenced a clip")
	}
}

// TestClipTypeComesBackOutOfTheClip covers MC_mdaClipGetType, whose answer is
// how a game decides what it is holding.
func TestClipTypeComesBackOutOfTheClip(t *testing.T) {
	client := mediaClient(t, nil)
	const mediaType = "audio/mmf"
	name := writeGuest(t, client, append([]byte(mediaType), 0))
	clip := callSlot(t, client, slotClipCreate, name, 0, 0)

	buffer := writeGuest(t, client, make([]byte, 32))
	if result := int32(callSlot(t, client, slotClipGetType, clip, buffer, 32)); result != wipiSuccess {
		t.Fatalf("getType = %d, want success", result)
	}
	got, err := client.readCString(buffer)
	if err != nil {
		t.Fatal(err)
	}
	if got != mediaType {
		t.Fatalf("clip type = %q, want %q", got, mediaType)
	}
	// A buffer that cannot hold the name is reported rather than truncated.
	if result := int32(callSlot(t, client, slotClipGetType, clip, buffer, 2)); result != wipiShortBuffer {
		t.Fatalf("getType into a short buffer = %d, want the short-buffer code", result)
	}
}

// TestTheWaterMarkSlotLeavesTheClipAlone covers 0x4c2. A title sets the
// watermark to the 100% it already is between creating a clip and playing it,
// so the call has to be accepted and has to change nothing — a clip that came
// back at a different volume, or one that stopped playing, would be this slot
// being read as something it is not. A handle nobody created is still refused.
func TestTheWaterMarkSlotLeavesTheClipAlone(t *testing.T) {
	sink := &recordingSink{}
	client := mediaClient(t, sink)
	sound := oneNoteSound(t)

	clip := callSlot(t, client, slotClipCreate, 0, uint32(len(sound)), 0)
	callSlot(t, client, slotClipPutData, clip, writeGuest(t, client, sound), uint32(len(sound)))
	callSlot(t, client, slotClipSetVolume, clip, 40)

	if result := int32(callSlot(t, client, slotClipSetWaterMark, clip, 100)); result != wipiSuccess {
		t.Fatalf("the watermark slot = %d, want success", result)
	}
	if got := callSlot(t, client, slotClipGetVolume, clip); got != 40 {
		t.Fatalf("clip volume = %d after the watermark call, want 40", got)
	}
	if result := int32(callSlot(t, client, slotClipPlay, clip, 0)); result != wipiSuccess {
		t.Fatalf("play after the watermark call = %d, want success", result)
	}
	client.clock.advance(50 * time.Millisecond)
	client.serviceAudio()
	if sink.notes == 0 {
		t.Fatal("the watermark call silenced the clip")
	}

	if result := int32(callSlot(t, client, slotClipSetWaterMark, clip+0x1000, 100)); result == wipiSuccess {
		t.Fatal("the watermark slot accepted a handle nobody created")
	}
}

// The default volume of a system volume category is the level this platform is
// playing at. A title reads it once at sound setup and keeps it as its own
// volume, so answering zero — which is what an unknown slot answered — told
// the title it had been started muted.
func TestDefaultVolumeAnswersTheSystemVolume(t *testing.T) {
	client := fixtureClient(t)

	category, err := client.allocateBytes([]byte("GENERAL\x00"))
	if err != nil {
		t.Fatal(err)
	}
	if level := callSlot(t, client, slotGetDefaultVolume, category); level != uint32(mediaMaxVolume) {
		t.Fatalf("the default volume answered %d, want %d", level, mediaMaxVolume)
	}

	// It tracks the system volume rather than answering a constant, which is
	// what makes a title that reads it, lowers it, and reads it again agree
	// with itself.
	callSlot(t, client, slotSetVolume, 40)
	if level := callSlot(t, client, slotGetDefaultVolume, category); level != 40 {
		t.Fatalf("after setVolume(40) the default volume answered %d", level)
	}

	// An unlisted category is answered rather than refused: there is one
	// volume here, so the answer is the same one.
	other, err := client.allocateBytes([]byte("SOMETHING\x00"))
	if err != nil {
		t.Fatal(err)
	}
	if level := callSlot(t, client, slotGetDefaultVolume, other); level != 40 {
		t.Fatalf("an unlisted category answered %d", level)
	}
}

// The categories are the specification's own set, and "GENERAL" — the one
// every local caller asks for — has to be in it or the lookup would report
// every real call as unlisted.
func TestSystemVolumeCategoriesCarryTheSpecifiedSet(t *testing.T) {
	for _, name := range []string{"GENERAL", "VOICE", "RING", "KEY", "MESSAGE", "ALARM", "ALERT", "MMEDIA", "GAME", "OEM"} {
		if !systemVolumeCategories[name] {
			t.Errorf("%s is not a known system volume category", name)
		}
	}
	if len(systemVolumeCategories) != 10 {
		t.Errorf("there are %d categories, want the specification's 10", len(systemVolumeCategories))
	}
}
