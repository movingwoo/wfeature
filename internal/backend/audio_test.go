package backend

import (
	"testing"
	"time"
)

// recordingSink is the sink shape a Host implements, kept as a log so the
// tests can assert on the order sound actually reaches a device in.
type recordingSink struct{ calls []string }

func (sink *recordingSink) PlayWave(channels uint8, rate uint32, samples []int16) {
	sink.record("wave", int(channels), int(rate), len(samples))
}
func (sink *recordingSink) MIDINoteOn(channel, note, velocity uint8) {
	sink.record("on", int(channel), int(note), int(velocity))
}
func (sink *recordingSink) MIDINoteOff(channel, note, velocity uint8) {
	sink.record("off", int(channel), int(note), int(velocity))
}
func (sink *recordingSink) MIDIProgramChange(channel, program uint8) {
	sink.record("program", int(channel), int(program))
}
func (sink *recordingSink) MIDIControlChange(channel, control, value uint8) {
	sink.record("control", int(channel), int(control), int(value))
}
func (sink *recordingSink) MIDIPitchBend(channel uint8, value uint16) {
	sink.record("bend", int(channel), int(value))
}
func (sink *recordingSink) MIDISysEx(data []byte) { sink.record("sysex", len(data)) }

func (sink *recordingSink) record(name string, values ...int) {
	entry := name
	for _, value := range values {
		entry += " " + itoa(value)
	}
	sink.calls = append(sink.calls, entry)
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := ""
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	return digits
}

func (sink *recordingSink) count(prefix string) int {
	total := 0
	for _, call := range sink.calls {
		if len(call) >= len(prefix) && call[:len(prefix)] == prefix {
			total++
		}
	}
	return total
}

// oneNoteSMAF builds the smallest file that plays: a mobile score track with a
// single note that starts at 20ms and lasts 20ms.
func oneNoteSMAF() []byte {
	sequence := []byte{
		0x05, 0x90, 60, 100, 0x05, // duration 5, note on ch0 note 60 vel 100, gate 5
		0x00, 0xff, 0x2f, 0x00, // end of stream
	}
	track := append([]byte{
		2, 0, // mobile no-compress, sequence type
		2, 2, // timebase 4ms both
	}, make([]byte, 16)...) // channel status: all "no care"
	track = append(track, taggedChunk("Mtsq", sequence)...)
	return smafFile(taggedChunk("MTR\x00", track))
}

func taggedChunk(tag string, payload []byte) []byte {
	header := make([]byte, 8)
	copy(header, tag)
	length := uint32(len(payload))
	header[4], header[5], header[6], header[7] = byte(length>>24), byte(length>>16), byte(length>>8), byte(length)
	return append(header, payload...)
}

func smafFile(body []byte) []byte {
	file := make([]byte, 8)
	copy(file, "MMMD")
	length := uint32(len(body) + 2)
	file[4], file[5], file[6], file[7] = byte(length>>24), byte(length>>16), byte(length>>8), byte(length)
	return append(append(file, body...), 0, 0)
}

func TestAudioRejectsDataThatIsNotASound(t *testing.T) {
	audio := NewAudio(&recordingSink{})
	if _, err := audio.Load([]byte("this is not a sound")); err == nil {
		t.Fatal("Load accepted data that is not a sound")
	}
}

func TestAudioEmitsEventsAsTheClockReachesThem(t *testing.T) {
	sink := &recordingSink{}
	audio := NewAudio(sink)
	handle, err := audio.Load(oneNoteSMAF())
	if err != nil {
		t.Fatal(err)
	}
	if err := audio.Play(handle, 0, false); err != nil {
		t.Fatal(err)
	}

	// Nothing is due before the note's own timestamp.
	audio.Advance(10 * time.Millisecond)
	if got := sink.count("on"); got != 0 {
		t.Fatalf("note started early (%d note ons at 10ms)", got)
	}
	audio.Advance(25 * time.Millisecond)
	if got := sink.count("on"); got != 1 {
		t.Fatalf("%d note ons at 25ms, want 1: %v", got, sink.calls)
	}
	if got := sink.count("off"); got != 0 {
		t.Fatalf("note stopped early: %v", sink.calls)
	}
	audio.Advance(60 * time.Millisecond)
	if got := sink.count("off"); got != 1 {
		t.Fatalf("%d note offs at 60ms, want 1: %v", got, sink.calls)
	}
	if audio.Playing(handle) {
		t.Fatal("one-shot sound is still playing after its last event")
	}
}

// A Host batching ticks can jump the clock past a whole phrase. Every event
// still has to be emitted, because a skipped note off never stops.
func TestAudioClockJumpEmitsSkippedEvents(t *testing.T) {
	sink := &recordingSink{}
	audio := NewAudio(sink)
	handle, err := audio.Load(oneNoteSMAF())
	if err != nil {
		t.Fatal(err)
	}
	if err := audio.Play(handle, 0, false); err != nil {
		t.Fatal(err)
	}
	audio.Advance(10 * time.Second)

	if sink.count("on") != 1 || sink.count("off") != 1 {
		t.Fatalf("a clock jump lost events: %v", sink.calls)
	}
}

func TestAudioStopReleasesWhatItLeftSounding(t *testing.T) {
	sink := &recordingSink{}
	audio := NewAudio(sink)
	handle, err := audio.Load(oneNoteSMAF())
	if err != nil {
		t.Fatal(err)
	}
	if err := audio.Play(handle, 0, false); err != nil {
		t.Fatal(err)
	}
	audio.Advance(25 * time.Millisecond) // note on, no note off yet
	audio.Stop(handle)

	if sink.count("off") != 1 {
		t.Fatalf("stop left a note sounding: %v", sink.calls)
	}
	// Sustain, all sound off, and all notes off on the channel it used.
	for _, want := range []string{"control 0 64 0", "control 0 120 0", "control 0 123 0"} {
		found := false
		for _, call := range sink.calls {
			if call == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("stop did not send %q: %v", want, sink.calls)
		}
	}
}

func TestAudioRepeatRestartsOnTheTrackGrid(t *testing.T) {
	sink := &recordingSink{}
	audio := NewAudio(sink)
	handle, err := audio.Load(oneNoteSMAF())
	if err != nil {
		t.Fatal(err)
	}
	if err := audio.Play(handle, 0, true); err != nil {
		t.Fatal(err)
	}
	// The track is 40ms long, so a second and third pass fit inside 130ms.
	audio.Advance(130 * time.Millisecond)

	if got := sink.count("on"); got < 3 {
		t.Fatalf("%d note ons in three track lengths, want at least 3: %v", got, sink.calls)
	}
	if !audio.Playing(handle) {
		t.Fatal("a repeating sound stopped on its own")
	}
	audio.Stop(handle)
	if audio.Playing(handle) {
		t.Fatal("stop did not stop a repeating sound")
	}
}

func TestAudioCloseForgetsTheHandle(t *testing.T) {
	audio := NewAudio(&recordingSink{})
	handle, err := audio.Load(oneNoteSMAF())
	if err != nil {
		t.Fatal(err)
	}
	if err := audio.Close(handle); err != nil {
		t.Fatal(err)
	}
	if err := audio.Play(handle, 0, false); err == nil {
		t.Fatal("Play accepted a closed handle")
	}
	if err := audio.Close(handle); err == nil {
		t.Fatal("Close accepted an already closed handle")
	}
}

func TestAudioWithoutASinkStaysSilentRatherThanFailing(t *testing.T) {
	audio := NewAudio(nil)
	handle, err := audio.Load(oneNoteSMAF())
	if err != nil {
		t.Fatal(err)
	}
	if err := audio.Play(handle, 0, false); err != nil {
		t.Fatal(err)
	}
	audio.Advance(time.Second)
}

// TestVolumeScalesWhatReachesTheSink pins the device level a guest sets
// through its platform's media API. The level is not the Host's volume
// control — that one is the user's — so it has to change what is emitted
// rather than being remembered for a getter.
func TestVolumeScalesWhatReachesTheSink(t *testing.T) {
	sink := &recordingSink{}
	audio := NewAudio(sink)
	handle, err := audio.Load(oneNoteSMAF())
	if err != nil {
		t.Fatal(err)
	}
	audio.SetVolume(50)
	if err := audio.Play(handle, 0, false); err != nil {
		t.Fatal(err)
	}
	audio.Advance(100 * time.Millisecond)

	var noteOn string
	for _, call := range sink.calls {
		if len(call) >= 3 && call[:3] == "on " {
			noteOn = call
			break
		}
	}
	// The file's note carries velocity 100, so half of it is 50.
	if noteOn != "on 0 60 50" {
		t.Fatalf("note on at half volume = %q, want the velocity halved", noteOn)
	}
}

// TestZeroVolumeStopsWhatIsAlreadySounding is the fade a game ends on. Scaling
// the next note is not enough: a note started at full volume rings on under a
// volume the game has already set to nothing.
func TestZeroVolumeStopsWhatIsAlreadySounding(t *testing.T) {
	sink := &recordingSink{}
	audio := NewAudio(sink)
	handle, err := audio.Load(oneNoteSMAF())
	if err != nil {
		t.Fatal(err)
	}
	if err := audio.Play(handle, 0, false); err != nil {
		t.Fatal(err)
	}
	audio.Advance(20 * time.Millisecond)
	if sink.count("on") == 0 {
		t.Fatal("the note never started")
	}
	before := sink.count("off")
	audio.SetVolume(0)
	if sink.count("off") <= before {
		t.Fatal("setting the volume to zero left the sounding note alone")
	}
}

// TestVolumeIsClampedToItsScale keeps a guest number out of the arithmetic.
func TestVolumeIsClampedToItsScale(t *testing.T) {
	audio := NewAudio(&recordingSink{})
	audio.SetVolume(-40)
	if level := audio.Volume(); level != 0 {
		t.Fatalf("volume = %d after a negative level, want 0", level)
	}
	audio.SetVolume(400)
	if level := audio.Volume(); level != maxAudioVolume {
		t.Fatalf("volume = %d after an over-range level, want %d", level, maxAudioVolume)
	}
}
