package smaf

import "sort"

// Translating SMAF into MIDI is not a relabelling. SMAF addresses up to four
// channels per track and as many tracks as it likes, while MIDI has sixteen
// with the tenth reserved for drums; SMAF's programs live in Yamaha's MA banks
// rather than General MIDI; and the handset dialect expresses volume,
// velocity, and note numbers on different scales than MIDI does. toneMap holds
// the state those conversions need.

// EventType names one playable event.
type EventType uint8

const (
	// EventWave is a decoded PCM sample to play immediately.
	EventWave EventType = iota
	EventNoteOn
	EventNoteOff
	EventProgramChange
	EventControlChange
	EventPitchBend
	EventSysEx
	// EventEnd marks the end of a track, which is where a repeat restarts.
	EventEnd
)

// Event is one thing to do at Time milliseconds after the start of playback.
type Event struct {
	Time uint32
	Type EventType

	Channel  uint8
	Note     uint8
	Velocity uint8
	Program  uint8
	Control  uint8
	Value    uint8
	Bend     uint16

	SamplingRate uint32
	WaveChannels uint8
	Wave         []int16
	SysEx        []byte
}

const (
	midiDrumChannel = 9
	maxSMAFChannels = 64
)

// melodyAllocationOrder is every MIDI channel except the drum one, in the
// order melodic SMAF channels claim them.
var melodyAllocationOrder = [15]uint8{0, 1, 2, 3, 4, 5, 6, 7, 8, 10, 11, 12, 13, 14, 15}

// Play parses a SMAF file and answers its events, ordered by time. Events at
// the same instant are ordered so that a channel is configured before it is
// played: sysex, then controllers and bends, then program changes, then note
// offs, then note ons, then waves.
//
// A file that does not parse answers no events rather than an error — a game
// asking to play a sound it packaged wrongly should stay silent, not stop.
func Play(data []byte) []Event {
	file, err := Parse(data)
	if err != nil {
		return nil
	}

	var events []Event
	handyChannelOffset := uint8(0)
	handyToneMap := newToneMap()

	for _, chunk := range file.Chunks {
		switch chunk.Kind {
		case ChunkScoreTrack:
			trackEvents, nextOffset := scoreTrackEvents(chunk.ScoreTrack, handyChannelOffset, handyToneMap)
			events = append(events, trackEvents...)
			handyChannelOffset = nextOffset
		case ChunkPCMAudioTrack:
			events = append(events, pcmTrackEvents(chunk.PCMAudioTrack)...)
		case ChunkSoftbankSequence:
			tones := newToneMap()
			tones.initTrack(HandyPhoneStandard, nil, handyChannelOffset)
			trackEvents, nextOffset := sequenceEvents(chunk.SoftbankSequence, 20, 20, handyChannelOffset, true, nil, tones)
			events = append(events, trackEvents...)
			handyChannelOffset = nextOffset
		}
	}

	sort.SliceStable(events, func(left, right int) bool {
		if events[left].Time != events[right].Time {
			return events[left].Time < events[right].Time
		}
		return eventOrder(events[left]) < eventOrder(events[right])
	})
	return events
}

func eventOrder(event Event) int {
	switch event.Type {
	case EventSysEx:
		return 4
	case EventControlChange, EventPitchBend:
		return 5
	case EventProgramChange:
		return 6
	case EventNoteOff:
		return 20
	case EventNoteOn:
		return 30
	case EventWave:
		return 40
	default:
		return 99
	}
}

func scoreTrackEvents(track *ScoreTrack, handyChannelOffset uint8, handyToneMap *toneMap) ([]Event, uint8) {
	isHandy := track.FormatType == HandyPhoneStandard
	tones := handyToneMap
	if isHandy {
		tones.initTrack(track.FormatType, track.ChannelStatus, handyChannelOffset)
	} else {
		tones = newToneMap()
		tones.initTrack(track.FormatType, track.ChannelStatus, 0)
	}

	var events []Event
	for _, setup := range track.SetupData {
		events = append(events, setupSysExEvents(setup)...)
	}
	for _, sequence := range track.Sequences {
		tones.preclassify(sequence, isHandy, handyChannelOffset)
		trackEvents, _ := sequenceEvents(sequence, track.TimebaseD, track.TimebaseG, handyChannelOffset, isHandy, track.Waves, tones)
		events = append(events, trackEvents...)
	}

	nextOffset := handyChannelOffset
	if isHandy {
		nextOffset = saturatingAdd(handyChannelOffset, 4)
	}
	return events, nextOffset
}

func sequenceEvents(
	sequence []SequenceEvent,
	timebaseD, timebaseG uint32,
	channelOffset uint8,
	useChannelOffset bool,
	waves map[uint8]WaveData,
	tones *toneMap,
) ([]Event, uint8) {
	var events []Event
	var now uint32
	var octaveShift [maxSMAFChannels]int8

	mapChannel := func(channel uint8) uint8 {
		if useChannelOffset {
			return saturatingAdd(channel, channelOffset)
		}
		return channel
	}

	for _, event := range sequence {
		// The duration precedes the event it introduces, so the clock advances
		// first and the event lands at the new time.
		now += event.Duration * timebaseD
		time := now

		switch event.Kind {
		case SeqNote:
			channel := mapChannel(event.Channel)
			if event.Note == 0 {
				// Note zero plays the score track's attached wave rather than
				// a pitch. The wave numbering is one-based against channels.
				wave, ok := waves[channel+1]
				if !ok {
					continue
				}
				samples, playable := decodeStreamWave(wave)
				if !playable {
					continue
				}
				events = append(events, Event{
					Time:         time,
					Type:         EventWave,
					WaveChannels: waveChannelCount(wave.Channels),
					SamplingRate: uint32(wave.SamplingFreq),
					Wave:         samples,
				})
				continue
			}

			index := int(channel)
			if index >= len(octaveShift) {
				index = len(octaveShift) - 1
			}
			note := tones.mapNote(channel, int16(event.Note)+int16(octaveShift[index])*12)
			velocity := tones.noteVelocity(channel, event.Velocity, event.HasVelocity)
			midiChannel := tones.realChannel(channel)
			duration := tones.noteDuration(channel, event.GateTime*timebaseG)
			events = append(events,
				Event{Time: time, Type: EventNoteOn, Channel: midiChannel, Note: note, Velocity: velocity},
				Event{Time: time + duration, Type: EventNoteOff, Channel: midiChannel, Note: note})
			events = append(events, tones.atmosphereNotes(time, duration, channel, note, velocity)...)

		case SeqControlChange:
			channel := mapChannel(event.Channel)
			tones.updateControl(channel, event.Control, event.Value)
			events = append(events, Event{
				Time: time, Type: EventControlChange,
				Channel: tones.realChannel(channel), Control: event.Control, Value: event.Value,
			})

		case SeqProgramChange:
			source := mapChannel(event.Channel)
			channel, program := tones.setProgram(source, event.Program)
			events = append(events, Event{Time: time, Type: EventProgramChange, Channel: channel, Program: program})
			events = append(events, tones.atmosphereSetup(time, source, event.Program)...)

		case SeqExclusive:
			events = append(events, Event{Time: time, Type: EventSysEx, SysEx: sysExMessage(event.Exclusive)})

		case SeqPitchBend:
			channel := tones.realChannel(mapChannel(event.Channel))
			bend := event.BendValue
			if bend > 0x3fff {
				bend = 0x3fff
			}
			events = append(events, Event{Time: time, Type: EventPitchBend, Channel: channel, Bend: bend})

		case SeqVolume:
			events = append(events, tones.volumeEvents(time, mapChannel(event.Channel), event.Value)...)

		case SeqPan:
			channel := tones.realChannel(mapChannel(event.Channel))
			events = append(events, Event{Time: time, Type: EventControlChange, Channel: channel, Control: 10, Value: event.Value})

		case SeqExpression:
			events = append(events, tones.expressionEvents(time, mapChannel(event.Channel), event.Value)...)

		case SeqOctaveShift:
			channel := mapChannel(event.Channel)
			if shift, ok := parseOctaveShift(event.Value); ok {
				index := int(channel)
				if index >= len(octaveShift) {
					index = len(octaveShift) - 1
				}
				octaveShift[index] = shift
			}

		case SeqModulation:
			channel := tones.realChannel(mapChannel(event.Channel))
			events = append(events, Event{Time: time, Type: EventControlChange, Channel: channel, Control: 1, Value: event.Value})

		case SeqBankSelect:
			channel := mapChannel(event.Channel)
			tones.updateBankSelect(channel, event.Value)
			events = append(events, Event{
				Time: time, Type: EventControlChange,
				Channel: tones.realChannel(channel), Control: 0, Value: event.Value & 0x7f,
			})
		}
	}
	events = append(events, Event{Time: now, Type: EventEnd})

	nextOffset := channelOffset
	if useChannelOffset {
		nextOffset = saturatingAdd(channelOffset, 4)
	}
	return events, nextOffset
}

func pcmTrackEvents(track *PCMAudioTrack) []Event {
	// The decoder handles mono ADPCM, which is what these tracks are in
	// practice. Anything else is left silent rather than played as noise.
	if track.Format != PCMAdpcm || track.Channels != Mono {
		return nil
	}

	var events []Event
	var now uint32
	for _, event := range track.Sequence {
		now += event.Duration * track.TimebaseD
		if event.Kind != PCMEventWave {
			continue
		}
		wave, ok := track.Waves[event.WaveNumber]
		if !ok {
			continue
		}
		events = append(events, Event{
			Time:         now,
			Type:         EventWave,
			WaveChannels: waveChannelCount(track.Channels),
			SamplingRate: track.SamplingFreq,
			Wave:         DecodeADPCM(wave),
		})
	}
	return append(events, Event{Time: now, Type: EventEnd})
}

func waveChannelCount(channels Channels) uint8 {
	if channels == Stereo {
		return 2
	}
	return 1
}

// setupSysExEvents reads a setup chunk's concatenated sysex messages, each an
// 0xf0 followed by a MIDI variable length quantity and that many bytes.
func setupSysExEvents(data []byte) []Event {
	var events []Event
	offset := 0
	for offset < len(data) {
		if data[offset] != 0xf0 {
			break
		}
		offset++
		length, next, ok := readMIDIVariableLength(data, offset)
		if !ok || next+length > len(data) {
			break
		}
		events = append(events, Event{Time: 0, Type: EventSysEx, SysEx: sysExMessage(data[next : next+length])})
		offset = next + length
	}
	return events
}

func readMIDIVariableLength(data []byte, offset int) (value, next int, ok bool) {
	for offset < len(data) {
		current := data[offset]
		offset++
		value = value<<7 | int(current&0x7f)
		if current&0x80 == 0 {
			return value, offset, true
		}
	}
	return 0, 0, false
}

// sysExMessage wraps a payload in the 0xf0/0xf7 framing a MIDI device expects,
// leaving framing the file already carried alone.
func sysExMessage(data []byte) []byte {
	message := make([]byte, 0, len(data)+2)
	if len(data) == 0 || data[0] != 0xf0 {
		message = append(message, 0xf0)
	}
	message = append(message, data...)
	if message[len(message)-1] != 0xf7 {
		message = append(message, 0xf7)
	}
	return message
}

func parseOctaveShift(value uint8) (int8, bool) {
	switch {
	case value <= 0x04:
		return int8(value), true
	case value >= 0x81 && value <= 0x84:
		return -int8(value - 0x80), true
	}
	return 0, false
}

func saturatingAdd(value, increment uint8) uint8 {
	if int(value)+int(increment) > 0xff {
		return 0xff
	}
	return value + increment
}

// decodeStreamWave expands the wave a score track attached to a channel, and
// reports whether this runtime can play it at all.
//
// **The two uncompressed forms are here because one title's are the only wave
// its songs carry.** A rhythm title packages four songs whose score tracks each
// hang an eight-bit sample off a channel, and the gate that let only ADPCM
// through dropped all four: the melody played and the sample the beat is built
// on did not. Offset binary is what the format field says and what the bytes
// say — its silence is 0x80, and read as two's complement the same sample sits
// at a mean of -9,221 against a rail instead of -115 centred.
//
// Sixteen-bit and stereo waves stay unplayable rather than guessed at: no local
// archive carries one, so there is nothing to check a reading against, and a
// wave played wrong is worse than a wave not played.
func decodeStreamWave(wave WaveData) ([]int16, bool) {
	if wave.Channels != Mono {
		return nil, false
	}
	switch {
	case wave.Format == YamahaADPCM && wave.BaseBit == Bit4:
		return DecodeADPCM(wave.Data), true
	case wave.Format == OffsetBinaryPCM && wave.BaseBit == Bit8:
		samples := make([]int16, len(wave.Data))
		for index, encoded := range wave.Data {
			samples[index] = int16(int32(encoded)-128) << 8
		}
		return samples, true
	case wave.Format == TwosComplementPCM && wave.BaseBit == Bit8:
		samples := make([]int16, len(wave.Data))
		for index, encoded := range wave.Data {
			samples[index] = int16(int8(encoded)) << 8
		}
		return samples, true
	}
	return nil, false
}
