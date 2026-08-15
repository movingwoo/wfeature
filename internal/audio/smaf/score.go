package smaf

import (
	"encoding/binary"
	"fmt"
)

// EventKind names one sequence event. The three dialects decode to the same
// set, which is what lets the player translate them through one path.
type EventKind uint8

const (
	SeqNop EventKind = iota
	SeqNote
	SeqControlChange
	SeqProgramChange
	SeqBankSelect
	SeqOctaveShift
	SeqModulation
	SeqPitchBend
	SeqVolume
	SeqPan
	SeqExpression
	SeqExclusive
)

// SequenceEvent is one event and the silence that precedes it. Duration is in
// timebase units, not milliseconds — the track's timebase converts it.
type SequenceEvent struct {
	Kind     EventKind
	Duration uint32

	Channel  uint8
	Note     uint8
	Velocity uint8
	// HasVelocity distinguishes the mobile note form that carries a velocity
	// from the one that reuses the channel's previous one.
	HasVelocity bool
	GateTime    uint32
	Control     uint8
	Program     uint8
	Value       uint8
	// BendValue is the 14-bit pitch bend; Value carries the 7-bit payloads.
	BendValue uint16
	Exclusive []byte
}

// ChannelStatus is a score track's declaration of what a channel is for.
type ChannelStatus struct {
	KeyControl uint8
	Vibration  uint8
	LED        uint8
	Kind       ChannelKind
}

// WaveData is a PCM sample attached to a score track, played by a note whose
// note number is zero.
type WaveData struct {
	Channels     Channels
	Format       StreamWaveFormat
	BaseBit      BaseBit
	SamplingFreq uint16
	Data         []byte
}

// ScoreTrack is one MTRx chunk: channel declarations, setup sysex, the
// sequence itself, and any wave data the sequence triggers.
type ScoreTrack struct {
	FormatType    FormatType
	SequenceType  uint8
	TimebaseD     uint32
	TimebaseG     uint32
	ChannelStatus []ChannelStatus

	SetupData [][]byte
	Sequences [][]SequenceEvent
	// Waves is keyed by the wave number the sequence refers to.
	Waves map[uint8]WaveData
}

func parseScoreTrack(data []byte) (*ScoreTrack, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("score track is %d bytes", len(data))
	}
	track := &ScoreTrack{FormatType: FormatType(data[0]), SequenceType: data[1], Waves: map[uint8]WaveData{}}
	if track.FormatType > MobileStandardNoCompress {
		return nil, fmt.Errorf("invalid score track format type %d", data[0])
	}
	var err error
	if track.TimebaseD, err = parseTimebase(data[2]); err != nil {
		return nil, err
	}
	if track.TimebaseG, err = parseTimebase(data[3]); err != nil {
		return nil, err
	}
	rest := data[4:]

	// The handset format declares four channels in one 16-bit word, the
	// mobile formats one byte per channel for sixteen.
	if track.FormatType == HandyPhoneStandard {
		if len(rest) < 2 {
			return nil, fmt.Errorf("handset score track has no channel status")
		}
		track.ChannelStatus = parseHandyChannelStatus(binary.BigEndian.Uint16(rest))
		rest = rest[2:]
	} else {
		if len(rest) < 16 {
			return nil, fmt.Errorf("mobile score track has no channel status")
		}
		for _, raw := range rest[:16] {
			track.ChannelStatus = append(track.ChannelStatus, parseMobileChannelStatus(raw))
		}
		rest = rest[16:]
	}

	for len(rest) >= 8 {
		tag, payload, next, ok := splitChunk(rest)
		if !ok {
			break
		}
		if err := track.readChunk(tag, payload); err != nil {
			break
		}
		rest = next
	}
	return track, nil
}

func (track *ScoreTrack) readChunk(tag [4]byte, payload []byte) error {
	switch {
	case string(tag[:]) == "Mtsu":
		track.SetupData = append(track.SetupData, payload)
	case string(tag[:]) == "Mtsq":
		events, err := track.parseSequence(payload)
		if err != nil {
			return err
		}
		track.Sequences = append(track.Sequences, events)
	case string(tag[:]) == "SEQU":
		events, err := parseSequenceHandyLike(payload, true)
		if err != nil {
			return err
		}
		track.Sequences = append(track.Sequences, events)
	case string(tag[:]) == "Mtsp":
		return track.readPCMChunks(payload)
	}
	return nil
}

func (track *ScoreTrack) parseSequence(payload []byte) ([]SequenceEvent, error) {
	switch track.FormatType {
	case MobileStandardNoCompress:
		return parseSequenceMobile(payload)
	case MobileStandardCompress:
		if len(payload) < 4 {
			return nil, fmt.Errorf("compressed sequence is %d bytes", len(payload))
		}
		decoded, err := huffmanDecode(int(binary.BigEndian.Uint32(payload)), payload[4:])
		if err != nil {
			return nil, err
		}
		return parseSequenceMobile(decoded)
	default:
		return parseSequenceHandyLike(payload, false)
	}
}

func (track *ScoreTrack) readPCMChunks(payload []byte) error {
	for len(payload) >= 8 {
		tag, data, next, ok := splitChunk(payload)
		if !ok {
			return fmt.Errorf("truncated score track PCM chunk")
		}
		if string(tag[:3]) != "Mwa" {
			return fmt.Errorf("unexpected score track PCM chunk %q", tag)
		}
		wave, err := parseWaveData(data)
		if err != nil {
			return err
		}
		track.Waves[tag[3]] = wave
		payload = next
	}
	return nil
}

func parseWaveData(data []byte) (WaveData, error) {
	if len(data) < 3 {
		return WaveData{}, fmt.Errorf("wave data is %d bytes", len(data))
	}
	waveType := data[0]
	return WaveData{
		Channels:     Channels(waveType >> 7 & 1),
		Format:       StreamWaveFormat(waveType >> 4 & 7),
		BaseBit:      BaseBit(waveType & 0x0f),
		SamplingFreq: binary.BigEndian.Uint16(data[1:3]),
		Data:         data[3:],
	}, nil
}

func parseMobileChannelStatus(raw byte) ChannelStatus {
	return ChannelStatus{
		KeyControl: raw >> 6 & 3,
		Vibration:  raw >> 5 & 1,
		LED:        raw >> 4 & 1,
		Kind:       ChannelKind(raw & 3),
	}
}

// parseHandyChannelStatus splits the handset format's four nibbles. The first
// data byte holds channels 0 and 1 in its upper and lower nibble, the second
// holds 2 and 3, so read as a big-endian word the channels run downward from
// the top.
func parseHandyChannelStatus(raw uint16) []ChannelStatus {
	statuses := make([]ChannelStatus, 0, 4)
	for index := 0; index < 4; index++ {
		nibble := raw >> ((3 - index) * 4) & 0xf
		statuses = append(statuses, ChannelStatus{
			KeyControl: uint8(nibble >> 3 & 1),
			Vibration:  uint8(nibble >> 2 & 1),
			Kind:       ChannelKind(nibble & 3),
		})
	}
	return statuses
}

// The handset dialect abbreviates common controller values into the event type
// itself; these are the values those short forms stand for.
var (
	shortModulationValues = [15]uint8{0x00, 0x00, 0x08, 0x10, 0x18, 0x20, 0x28, 0x30, 0x38, 0x40, 0x48, 0x50, 0x60, 0x70, 0x7f}
	shortExpressionValues = [15]uint8{0x00, 0x00, 0x1f, 0x27, 0x2f, 0x37, 0x3f, 0x47, 0x4f, 0x57, 0x5f, 0x67, 0x6f, 0x77, 0x7f}
)

// parseSequenceMobile reads the MIDI-shaped dialect, where the status byte is
// a MIDI status byte and durations are MIDI variable length quantities.
func parseSequenceMobile(data []byte) ([]SequenceEvent, error) {
	r := &reader{data: data}
	var events []SequenceEvent
	for {
		duration, ok := r.variableNumber()
		if !ok {
			return events, nil
		}
		status, ok := r.byte()
		if !ok {
			return events, nil
		}
		event := SequenceEvent{Duration: duration, Channel: status & 0x0f}
		switch {
		case status >= 0x80 && status <= 0x8f: // note without velocity
			note, ok1 := r.byte()
			gate, ok2 := r.variableNumber()
			if !ok1 || !ok2 {
				return events, nil
			}
			event.Kind, event.Note, event.GateTime = SeqNote, note, gate
		case status >= 0x90 && status <= 0x9f: // note with velocity
			note, ok1 := r.byte()
			velocity, ok2 := r.byte()
			gate, ok3 := r.variableNumber()
			if !ok1 || !ok2 || !ok3 {
				return events, nil
			}
			event.Kind, event.Note, event.Velocity, event.HasVelocity, event.GateTime = SeqNote, note, velocity, true, gate
		case status >= 0xa0 && status <= 0xaf:
			if _, ok := r.take(2); !ok {
				return events, nil
			}
		case status >= 0xb0 && status <= 0xbf:
			control, ok1 := r.byte()
			value, ok2 := r.byte()
			if !ok1 || !ok2 {
				return events, nil
			}
			event.Kind, event.Control, event.Value = SeqControlChange, control, value
		case status >= 0xc0 && status <= 0xcf:
			program, ok := r.byte()
			if !ok {
				return events, nil
			}
			event.Kind, event.Program = SeqProgramChange, program
		case status >= 0xd0 && status <= 0xdf:
			if _, ok := r.byte(); !ok {
				return events, nil
			}
		case status >= 0xe0 && status <= 0xef:
			low, ok1 := r.byte()
			high, ok2 := r.byte()
			if !ok1 || !ok2 {
				return events, nil
			}
			event.Kind = SeqPitchBend
			event.BendValue = uint16(high&0x7f)<<7 | uint16(low&0x7f)
		case status == 0xf0:
			length, ok := r.variableNumber()
			if !ok {
				return events, nil
			}
			payload, ok := r.take(int(length))
			if !ok {
				return events, nil
			}
			event.Kind, event.Exclusive = SeqExclusive, append([]byte(nil), payload...)
		case status == 0xff:
			second, ok := r.byte()
			if !ok {
				return events, nil
			}
			if second == 0x2f { // end of stream
				if _, ok := r.byte(); !ok {
					return events, nil
				}
				// A trailing nop keeps the track's total length, so a repeat
				// waits out the silence after the last note instead of
				// restarting on top of it.
				return append(events, SequenceEvent{Kind: SeqNop, Duration: duration}), nil
			}
		}
		events = append(events, event)
	}
}

// parseSequenceHandyLike reads the handset dialect, and Softbank's variant of
// it. They differ only in how an exclusive message is delimited: Softbank
// length-prefixes it, the handset format terminates it with 0xf7.
func parseSequenceHandyLike(data []byte, softbank bool) ([]SequenceEvent, error) {
	r := &reader{data: data}
	var events []SequenceEvent
	for {
		if r.remaining() == 0 {
			return events, nil
		}
		// Four zero bytes end the stream.
		if r.remaining() >= 4 {
			tail := r.data[r.offset : r.offset+4]
			if tail[0] == 0 && tail[1] == 0 && tail[2] == 0 && tail[3] == 0 {
				return events, nil
			}
		}
		duration, ok := r.handyVariableNumber()
		if !ok {
			return events, nil
		}
		status, ok := r.byte()
		if !ok {
			return events, nil
		}
		event := SequenceEvent{Duration: duration}
		switch {
		case status == 0x00:
			next, ok := r.byte()
			if !ok {
				return events, nil
			}
			event.Channel = next >> 6 & 3
			if !decodeHandyControlEvent(r, &event, next&0x3f) {
				continue
			}
		case status == 0xff:
			next, ok := r.byte()
			if !ok {
				return events, nil
			}
			if next != 0xf0 {
				// 0x00 is a nop; anything else is unknown and treated as one.
				events = append(events, event)
				continue
			}
			payload, ok := readHandyExclusive(r, softbank)
			if !ok {
				return events, nil
			}
			event.Kind, event.Exclusive = SeqExclusive, payload
		default: // 0x01..=0xfe is a note packed into the status byte
			event.Channel = status >> 6 & 3
			octave := status >> 4 & 3
			voice := status & 0x0f
			gate, ok := r.handyVariableNumber()
			if !ok {
				return events, nil
			}
			event.Kind, event.Note, event.GateTime = SeqNote, octave*12+voice, gate
		}
		events = append(events, event)
	}
}

// decodeHandyControlEvent fills in the event for a 0x00-status control message
// and reports whether it produced one at all; an unrecognised type is skipped.
func decodeHandyControlEvent(r *reader, event *SequenceEvent, eventType uint8) bool {
	readValue := func() (uint8, bool) { return r.byte() }
	switch {
	case eventType == 0x00:
		if _, ok := r.byte(); !ok {
			return false
		}
		event.Kind = SeqNop
	case eventType >= 0x01 && eventType <= 0x0e:
		event.Kind, event.Value = SeqExpression, shortExpressionValues[eventType]
	case eventType >= 0x11 && eventType <= 0x1e:
		event.Kind = SeqPitchBend
		if bend := (uint32(eventType) - 0x10) * 16384 / 16; bend > 0x3fff {
			event.BendValue = 0x3fff
		} else {
			event.BendValue = uint16(bend)
		}
	case eventType >= 0x21 && eventType <= 0x2e:
		event.Kind, event.Value = SeqModulation, shortModulationValues[eventType-0x20]
	case eventType == 0x30, eventType == 0x31, eventType == 0x32, eventType == 0x33,
		eventType == 0x34, eventType == 0x36, eventType == 0x37, eventType == 0x3a, eventType == 0x3b:
		value, ok := readValue()
		if !ok {
			return false
		}
		switch eventType {
		case 0x30:
			event.Kind, event.Program = SeqProgramChange, value
		case 0x31:
			event.Kind, event.Value = SeqBankSelect, value
		case 0x32:
			event.Kind, event.Value = SeqOctaveShift, value
		case 0x33:
			event.Kind, event.Value = SeqModulation, value
		case 0x34:
			event.Kind, event.BendValue = SeqPitchBend, pitchBendByteToMIDI(value)
		case 0x36, 0x3b:
			event.Kind, event.Value = SeqExpression, value
		case 0x37:
			event.Kind, event.Value = SeqVolume, value
		case 0x3a:
			event.Kind, event.Value = SeqPan, value
		}
	default:
		return false
	}
	return true
}

func readHandyExclusive(r *reader, softbank bool) ([]byte, bool) {
	if softbank {
		length, ok := r.byte()
		if !ok {
			return nil, false
		}
		payload, ok := r.take(int(length))
		if !ok {
			return nil, false
		}
		return append([]byte(nil), payload...), true
	}
	rest := r.data[r.offset:]
	end := len(rest)
	for index, value := range rest {
		if value == 0xf7 {
			end = index
			break
		}
	}
	payload := append([]byte(nil), rest[:end]...)
	if end < len(rest) {
		r.offset += end + 1
	} else {
		r.offset += end
	}
	return payload, true
}

// pitchBendByteToMIDI recentres the handset format's byte-sized bend onto
// MIDI's 14-bit range around 8192.
func pitchBendByteToMIDI(value uint8) uint16 {
	bend := (int32(value)-128)*64 + 8192
	if bend < 0 {
		return 0
	}
	if bend > 16383 {
		return 16383
	}
	return uint16(bend)
}
