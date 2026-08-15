package backend

import (
	"encoding/binary"
	"testing"
	"time"
)

// midiTrack pulls the one track out of a type 0 file, checking the header on
// the way: a recording nothing can parse is worth less than no recording.
func midiTrack(t *testing.T, file []byte) []byte {
	t.Helper()
	if len(file) < 22 || string(file[:4]) != "MThd" {
		t.Fatalf("not a MIDI file: % x", file[:min(len(file), 8)])
	}
	headerLength := binary.BigEndian.Uint32(file[4:8])
	if format := binary.BigEndian.Uint16(file[8:10]); format != 0 {
		t.Fatalf("format = %d, want 0", format)
	}
	if tracks := binary.BigEndian.Uint16(file[10:12]); tracks != 1 {
		t.Fatalf("tracks = %d, want 1", tracks)
	}
	// One tick per millisecond at the default tempo, so recorded times are the
	// file's times.
	if division := binary.BigEndian.Uint16(file[12:14]); division != 500 {
		t.Fatalf("division = %d, want 500", division)
	}
	chunk := file[8+headerLength:]
	if string(chunk[:4]) != "MTrk" {
		t.Fatalf("second chunk is %q, want MTrk", chunk[:4])
	}
	length := binary.BigEndian.Uint32(chunk[4:8])
	if int(length) != len(chunk)-8 {
		t.Fatalf("track length says %d, file holds %d", length, len(chunk)-8)
	}
	return chunk[8:]
}

// midiEvents walks a track and answers its events, so a test can ask what the
// file actually says rather than matching bytes.
type midiEvent struct {
	delta   uint32
	status  byte
	data    []byte
	metaTag byte
}

func midiEvents(t *testing.T, track []byte) []midiEvent {
	t.Helper()
	var events []midiEvent
	var running byte
	for index := 0; index < len(track); {
		delta := uint32(0)
		for {
			symbol := track[index]
			index++
			delta = delta<<7 | uint32(symbol&0x7f)
			if symbol&0x80 == 0 {
				break
			}
		}
		status := track[index]
		if status < 0x80 {
			status = running
		} else {
			index++
			running = status
		}
		switch {
		case status == 0xff:
			tag := track[index]
			index++
			length := int(track[index])
			index++
			events = append(events, midiEvent{delta: delta, status: status, metaTag: tag})
			index += length
		case status == 0xf0 || status == 0xf7:
			length := int(track[index])
			index++
			events = append(events, midiEvent{delta: delta, status: status, data: track[index : index+length]})
			index += length
		default:
			size := 2
			if status&0xf0 == 0xc0 || status&0xf0 == 0xd0 {
				size = 1
			}
			events = append(events, midiEvent{delta: delta, status: status, data: track[index : index+size]})
			index += size
		}
	}
	return events
}

// A run stops where it stops, which is rarely where the music ends. Every note
// still down at that moment used to reach the file with no note off after it,
// and a player holds those forever — four of five recordings taken from real
// titles ended that way.
func TestRecordedMIDIReleasesWhatWasStillSounding(t *testing.T) {
	moment := time.Duration(0)
	sink := NewRecordingSink(func() time.Duration { return moment })

	sink.MIDIProgramChange(0, 24)
	sink.MIDINoteOn(0, 60, 100)
	moment = 250 * time.Millisecond
	sink.MIDINoteOn(1, 64, 90)
	moment = 500 * time.Millisecond
	// One of the three is released by the game; the other two are not.
	sink.MIDINoteOff(0, 60, 64)
	sink.MIDINoteOn(9, 38, 110)

	events := midiEvents(t, midiTrack(t, sink.standardMIDIFile()))

	sounding := map[[2]byte]int{}
	var last midiEvent
	for _, event := range events {
		last = event
		if event.status&0xf0 == 0x90 && event.data[1] != 0 {
			sounding[[2]byte{event.status & 0x0f, event.data[0]}]++
		}
		if event.status&0xf0 == 0x80 || (event.status&0xf0 == 0x90 && event.data[1] == 0) {
			key := [2]byte{event.status & 0x0f, event.data[0]}
			if sounding[key] > 0 {
				sounding[key]--
			}
		}
	}
	for key, count := range sounding {
		if count > 0 {
			t.Fatalf("channel %d note %d is still sounding at the end of the file", key[0], key[1])
		}
	}
	if last.status != 0xff || last.metaTag != 0x2f {
		t.Fatalf("the file does not end with end-of-track: status %#x tag %#x", last.status, last.metaTag)
	}
	// The releases belong at the end, not at some invented later time.
	for _, event := range events[len(events)-3:] {
		if event.delta != 0 {
			t.Fatalf("a trailing release carries delta %d, want 0", event.delta)
		}
	}

	// Two runs of the same game have to produce the same bytes, which a map's
	// iteration order would break.
	again := NewRecordingSink(func() time.Duration { return moment })
	again.MIDIProgramChange(0, 24)
	again.MIDINoteOn(0, 60, 100)
	again.MIDINoteOn(1, 64, 90)
	again.MIDINoteOff(0, 60, 64)
	again.MIDINoteOn(9, 38, 110)
	first := again.standardMIDIFile()
	for round := 0; round < 8; round++ {
		if repeat := again.standardMIDIFile(); string(repeat) != string(first) {
			t.Fatal("the same recording produced two different files")
		}
	}
}

// A recording with nothing left down must not grow a release it does not need,
// and the messages a game sent have to reach the file unchanged.
func TestRecordedMIDIKeepsWhatTheGameSent(t *testing.T) {
	moment := time.Duration(0)
	sink := NewRecordingSink(func() time.Duration { return moment })
	sink.MIDIControlChange(3, 7, 100)
	sink.MIDINoteOn(3, 72, 80)
	moment = 40 * time.Millisecond
	// A note on with zero velocity is a note off, and counting it as anything
	// else would leave the file releasing a note that is already up.
	sink.MIDINoteOn(3, 72, 0)
	sink.MIDIPitchBend(3, 8192)

	events := midiEvents(t, midiTrack(t, sink.standardMIDIFile()))
	if len(events) != 5 {
		t.Fatalf("the file holds %d events, want the four sent plus end-of-track", len(events))
	}
	if events[3].status != 0xe3 || events[3].data[0] != 0x00 || events[3].data[1] != 0x40 {
		t.Fatalf("pitch bend reached the file as %#x % x", events[3].status, events[3].data)
	}
	if events[2].delta != 40 {
		t.Fatalf("the note off is at delta %d, want the 40ms the clock moved", events[2].delta)
	}
}
