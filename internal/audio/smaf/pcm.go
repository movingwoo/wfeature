package smaf

import (
	"encoding/binary"
	"fmt"
)

// PCMEventKind names a PCM audio track sequence event.
type PCMEventKind uint8

const (
	PCMEventNop PCMEventKind = iota
	PCMEventWave
	PCMEventPitchBend
	PCMEventVolume
	PCMEventPan
	PCMEventExpression
	PCMEventExclusive
)

// PCMEvent is one PCM audio track event and the silence before it.
type PCMEvent struct {
	Kind     PCMEventKind
	Duration uint32

	Channel    uint8
	WaveNumber uint8
	GateTime   uint32
	Value      uint8
	Exclusive  []byte
}

// PCMAudioTrack is one ATRx chunk: a stream of wave triggers plus the waves
// they name. Unlike a score track it carries no notes, only samples.
type PCMAudioTrack struct {
	FormatType   uint8
	SequenceType uint8
	Channels     Channels
	Format       PCMWaveFormat
	SamplingFreq uint32
	BaseBit      BaseBit
	TimebaseD    uint32
	TimebaseG    uint32

	Sequence []PCMEvent
	Waves    map[uint8][]byte
}

// samplingFrequencies indexes the four-bit rate field in a track's wave type.
var samplingFrequencies = [5]uint32{4000, 8000, 11000, 22050, 44100}

func parsePCMAudioTrack(data []byte) (*PCMAudioTrack, error) {
	if len(data) < 6 {
		return nil, fmt.Errorf("PCM audio track is %d bytes", len(data))
	}
	waveType := binary.BigEndian.Uint16(data[2:4])
	rateIndex := waveType >> 8 & 0x0f
	if int(rateIndex) >= len(samplingFrequencies) {
		return nil, fmt.Errorf("invalid PCM sampling frequency index %d", rateIndex)
	}
	track := &PCMAudioTrack{
		FormatType:   data[0],
		SequenceType: data[1],
		Channels:     Channels(waveType >> 15 & 1),
		Format:       PCMWaveFormat(waveType >> 12 & 7),
		SamplingFreq: samplingFrequencies[rateIndex],
		BaseBit:      BaseBit(waveType >> 4 & 0x0f),
		Waves:        map[uint8][]byte{},
	}
	var err error
	if track.TimebaseD, err = parseTimebase(data[4]); err != nil {
		return nil, err
	}
	if track.TimebaseG, err = parseTimebase(data[5]); err != nil {
		return nil, err
	}

	rest := data[6:]
	for len(rest) >= 8 {
		tag, payload, next, ok := splitChunk(rest)
		if !ok {
			break
		}
		switch {
		case string(tag[:]) == "Atsq":
			events, err := parsePCMSequence(payload)
			if err != nil {
				return track, nil
			}
			track.Sequence = events
		case string(tag[:3]) == "Awa":
			track.Waves[tag[3]] = payload
		}
		rest = next
	}
	return track, nil
}

// The PCM dialect abbreviates the same two controllers the score dialect does.
var shortPitchBendValues = [15]uint8{0x00, 0x08, 0x10, 0x18, 0x20, 0x28, 0x30, 0x38, 0x40, 0x48, 0x50, 0x58, 0x60, 0x68, 0x70}

func parsePCMSequence(data []byte) ([]PCMEvent, error) {
	r := &reader{data: data}
	var events []PCMEvent
	for {
		// Four zero bytes end the stream; the nop keeps the trailing silence.
		if r.remaining() == 4 {
			tail := r.data[r.offset:]
			if tail[0] == 0 && tail[1] == 0 && tail[2] == 0 && tail[3] == 0 {
				return append(events, PCMEvent{Kind: PCMEventNop}), nil
			}
		}
		duration, ok := r.variableNumber()
		if !ok {
			return events, nil
		}
		first, ok := r.byte()
		if !ok {
			return events, nil
		}
		event := PCMEvent{Duration: duration}
		switch {
		case first == 0xff:
			second, ok := r.byte()
			if !ok {
				return events, nil
			}
			if second != 0xf0 {
				events = append(events, event)
				continue
			}
			length, ok := r.byte()
			if !ok {
				return events, nil
			}
			payload, ok := r.take(int(length))
			if !ok {
				return events, nil
			}
			event.Kind, event.Exclusive = PCMEventExclusive, append([]byte(nil), payload...)
		case first != 0x00:
			gate, ok := r.variableNumber()
			if !ok {
				return events, nil
			}
			event.Kind = PCMEventWave
			event.Channel, event.WaveNumber, event.GateTime = first>>6, first&0x3f, gate
		default:
			second, ok := r.byte()
			if !ok {
				return events, nil
			}
			event.Channel = second >> 6 & 3
			eventType := second & 0x3f
			switch {
			case eventType == 0x00:
				event.Kind = PCMEventNop
			case eventType >= 0x01 && eventType <= 0x0e:
				event.Kind, event.Value = PCMEventExpression, shortExpressionValues[eventType]
			case eventType >= 0x11 && eventType <= 0x1e:
				event.Kind, event.Value = PCMEventPitchBend, shortPitchBendValues[eventType-0x10]
			case eventType == 0x34, eventType == 0x36, eventType == 0x37, eventType == 0x3a, eventType == 0x3b:
				value, ok := r.byte()
				if !ok {
					return events, nil
				}
				event.Value = value
				switch eventType {
				case 0x34:
					event.Kind = PCMEventPitchBend
				case 0x36, 0x3b:
					event.Kind = PCMEventExpression
				case 0x37:
					event.Kind = PCMEventVolume
				case 0x3a:
					event.Kind = PCMEventPan
				}
			default:
				return events, nil
			}
		}
		events = append(events, event)
	}
}
