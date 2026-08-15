package backend

import (
	"encoding/binary"
	"fmt"
	"os"
	"sort"
	"time"
)

// RecordingSink writes what a game plays to disk instead of to a speaker.
//
// It exists because the native CLI has no audio device this project is willing
// to take a dependency on — Rust's side reaches a system MIDI port and an ALSA
// or CoreAudio backend, neither of which has a cgo-free Go equivalent — and
// because "did the sound come out right" is a question a recording answers
// better than a speaker does. The MIDI file can be played by anything; the WAV
// holds the sampled sounds, which are the part no synthesiser is needed for.
type RecordingSink struct {
	// events are MIDI messages with the time they happened, which is what a
	// Standard MIDI File is; the times come from Clock rather than the wall
	// clock so a batched run records the guest's timeline, not the Host's.
	events []recordedMIDI
	// samples is every wave concatenated at the rate of the first one. Games
	// here play one sampled sound at a time, so mixing is not needed to hear
	// what was played.
	samples    []int16
	sampleRate uint32
	channels   uint8

	// Clock answers the current guest time. Nil records everything at zero,
	// which still produces a playable file with no timing.
	Clock func() time.Duration
}

type recordedMIDI struct {
	at      time.Duration
	message []byte
}

func NewRecordingSink(clock func() time.Duration) *RecordingSink {
	return &RecordingSink{Clock: clock}
}

func (sink *RecordingSink) now() time.Duration {
	if sink.Clock == nil {
		return 0
	}
	return sink.Clock()
}

func (sink *RecordingSink) message(bytes ...byte) {
	sink.events = append(sink.events, recordedMIDI{at: sink.now(), message: bytes})
}

func (sink *RecordingSink) PlayWave(channels uint8, samplingRate uint32, samples []int16) {
	if len(samples) == 0 || samplingRate == 0 {
		return
	}
	if sink.sampleRate == 0 {
		sink.sampleRate, sink.channels = samplingRate, channels
	}
	sink.samples = append(sink.samples, samples...)
}

func (sink *RecordingSink) MIDINoteOn(channel, note, velocity uint8) {
	sink.message(0x90|channel&0x0f, note&0x7f, velocity&0x7f)
}

func (sink *RecordingSink) MIDINoteOff(channel, note, velocity uint8) {
	sink.message(0x80|channel&0x0f, note&0x7f, velocity&0x7f)
}

func (sink *RecordingSink) MIDIProgramChange(channel, program uint8) {
	sink.message(0xc0|channel&0x0f, program&0x7f)
}

func (sink *RecordingSink) MIDIControlChange(channel, control, value uint8) {
	sink.message(0xb0|channel&0x0f, control&0x7f, value&0x7f)
}

func (sink *RecordingSink) MIDIPitchBend(channel uint8, value uint16) {
	sink.message(0xe0|channel&0x0f, byte(value&0x7f), byte(value>>7&0x7f))
}

func (sink *RecordingSink) MIDISysEx(data []byte) {
	// A file's sysex event carries its payload length and drops the leading
	// 0xf0, which the event type already implies.
	payload := data
	if len(payload) > 0 && payload[0] == 0xf0 {
		payload = payload[1:]
	}
	sink.events = append(sink.events, recordedMIDI{at: sink.now(), message: append([]byte{0xf0}, payload...)})
}

// Summary reports what was recorded, for a run that wants to say so without
// writing anything.
func (sink *RecordingSink) Summary() (messages, waveSamples int) {
	return len(sink.events), len(sink.samples)
}

// Write saves the recording as <prefix>.mid and <prefix>.wav, skipping either
// if nothing of that kind was played. It answers the paths it wrote.
func (sink *RecordingSink) Write(prefix string) ([]string, error) {
	var written []string
	if len(sink.events) > 0 {
		path := prefix + ".mid"
		if err := os.WriteFile(path, sink.standardMIDIFile(), 0o644); err != nil {
			return written, fmt.Errorf("write %s: %w", path, err)
		}
		written = append(written, path)
	}
	if len(sink.samples) > 0 {
		path := prefix + ".wav"
		if err := os.WriteFile(path, sink.waveFile(), 0o644); err != nil {
			return written, fmt.Errorf("write %s: %w", path, err)
		}
		written = append(written, path)
	}
	return written, nil
}

// standardMIDIFile builds a type 0 file at one tick per millisecond, which
// makes the recorded times the file's times with no tempo arithmetic.
func (sink *RecordingSink) standardMIDIFile() []byte {
	const ticksPerQuarter = 500 // at 500,000µs per quarter note, one tick is 1ms

	var track []byte
	previous := time.Duration(0)
	// A recording stops when the run does, which is rarely where the music
	// ends: whatever is sounding at that moment has no note off in the file,
	// and a player holds it after the last event forever. So the notes still
	// down are counted while the track is built and released at the end.
	sounding := map[[2]byte]int{}
	for _, event := range sink.events {
		delta := event.at - previous
		if delta < 0 {
			delta = 0
		}
		previous = event.at
		track = append(track, variableLength(uint32(delta/time.Millisecond))...)
		if event.message[0] == 0xf0 {
			track = append(track, 0xf0)
			track = append(track, variableLength(uint32(len(event.message)-1))...)
			track = append(track, event.message[1:]...)
			continue
		}
		if len(event.message) == 3 {
			key := [2]byte{event.message[0] & 0x0f, event.message[1]}
			switch {
			// A note on with zero velocity is a note off, which is how a
			// running-status stream says one and how these games do.
			case event.message[0]&0xf0 == 0x90 && event.message[2] != 0:
				sounding[key]++
			case event.message[0]&0xf0 == 0x80 || event.message[0]&0xf0 == 0x90:
				if sounding[key] > 0 {
					sounding[key]--
				}
			}
		}
		track = append(track, event.message...)
	}
	// Sorted, because a map's order is not one and two runs of the same game
	// have to produce the same file for a recording to be comparable.
	left := make([][2]byte, 0, len(sounding))
	for key, count := range sounding {
		if count > 0 {
			left = append(left, key)
		}
	}
	sort.Slice(left, func(i, j int) bool {
		if left[i][0] != left[j][0] {
			return left[i][0] < left[j][0]
		}
		return left[i][1] < left[j][1]
	})
	for _, key := range left {
		for count := sounding[key]; count > 0; count-- {
			track = append(track, 0x00, 0x80|key[0], key[1], 0x40)
		}
	}
	track = append(track, 0x00, 0xff, 0x2f, 0x00) // end of track

	file := append([]byte(nil), "MThd"...)
	file = binary.BigEndian.AppendUint32(file, 6)
	file = binary.BigEndian.AppendUint16(file, 0) // format 0: one track
	file = binary.BigEndian.AppendUint16(file, 1)
	file = binary.BigEndian.AppendUint16(file, ticksPerQuarter)
	file = append(file, "MTrk"...)
	file = binary.BigEndian.AppendUint32(file, uint32(len(track)))
	return append(file, track...)
}

func variableLength(value uint32) []byte {
	encoded := []byte{byte(value & 0x7f)}
	for value >>= 7; value > 0; value >>= 7 {
		encoded = append([]byte{byte(value&0x7f | 0x80)}, encoded...)
	}
	return encoded
}

// waveFile wraps the recorded samples in a 16-bit PCM RIFF header.
func (sink *RecordingSink) waveFile() []byte {
	channels := uint16(sink.channels)
	if channels == 0 {
		channels = 1
	}
	rate := sink.sampleRate
	if rate == 0 {
		rate = 8000
	}
	const bitsPerSample = 16
	blockAlign := channels * bitsPerSample / 8
	dataSize := uint32(len(sink.samples) * 2)

	file := append([]byte(nil), "RIFF"...)
	file = binary.LittleEndian.AppendUint32(file, 36+dataSize)
	file = append(file, "WAVEfmt "...)
	file = binary.LittleEndian.AppendUint32(file, 16)
	file = binary.LittleEndian.AppendUint16(file, 1) // PCM
	file = binary.LittleEndian.AppendUint16(file, channels)
	file = binary.LittleEndian.AppendUint32(file, rate)
	file = binary.LittleEndian.AppendUint32(file, rate*uint32(blockAlign))
	file = binary.LittleEndian.AppendUint16(file, blockAlign)
	file = binary.LittleEndian.AppendUint16(file, bitsPerSample)
	file = append(file, "data"...)
	file = binary.LittleEndian.AppendUint32(file, dataSize)
	for _, sample := range sink.samples {
		file = binary.LittleEndian.AppendUint16(file, uint16(sample))
	}
	return file
}
