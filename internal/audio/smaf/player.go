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
	// EventPCMControl changes one PCM track channel independently of MIDI.
	EventPCMControl
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
	// PCMChannel scopes ATR controls within a sound. Zero retains the
	// ungrouped stream-wave and legacy authored-event behavior.
	PCMChannel uint16
	Wave       []int16
	SysEx      []byte
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
	events, _ := Decode(data)
	return events
}

// Decode returns the same events as Play, preserving parse and resource-limit
// errors for callers that report why a sound could not be loaded. A refused
// decode never returns a playable prefix.
func Decode(data []byte) ([]Event, error) {
	budget := newDecodeBudget()
	events := playWithBudget(data, budget)
	return events, budget.err
}

func playWithBudget(data []byte, budget *decodeBudget) []Event {
	file, err := parseWithBudget(data, budget)
	if err != nil {
		budget.err = err
		return nil
	}

	var events []Event
	handyChannelOffset := uint8(0)
	handyToneMap := newToneMap()
	pcmTracks := 0

	for _, chunk := range file.Chunks {
		switch chunk.Kind {
		case ChunkScoreTrack:
			trackEvents, nextOffset := scoreTrackEvents(chunk.ScoreTrack, handyChannelOffset, handyToneMap, budget)
			events = append(events, trackEvents...)
			handyChannelOffset = nextOffset
		case ChunkPCMAudioTrack:
			// Reserve four channels in file order, including unsupported tracks.
			// Chunk tags may repeat; zero is reserved for ungrouped stream waves.
			if budget.charge(&pcmTracks, 1, int(^uint16(0))/4, "PCM audio tracks") != nil {
				return nil
			}
			base := uint16((pcmTracks-1)*4 + 1)
			events = append(events, pcmTrackEvents(chunk.PCMAudioTrack, base, budget)...)
		case ChunkSoftbankSequence:
			tones := newToneMap()
			tones.initTrack(HandyPhoneStandard, nil, handyChannelOffset)
			trackEvents, nextOffset := sequenceEvents(chunk.SoftbankSequence, 20, 20, handyChannelOffset, true, nil, tones, budget)
			events = append(events, trackEvents...)
			handyChannelOffset = nextOffset
		}
		if budget.err != nil {
			return nil
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
	case EventControlChange, EventPitchBend, EventPCMControl:
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

func scoreTrackEvents(track *ScoreTrack, handyChannelOffset uint8, handyToneMap *toneMap, budget *decodeBudget) ([]Event, uint8) {
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
		events = append(events, setupSysExEvents(setup, isHandy, budget)...)
	}
	for _, sequence := range track.Sequences {
		if budget.err != nil {
			return nil, handyChannelOffset
		}
		tones.preclassify(sequence, isHandy, handyChannelOffset)
		trackEvents, _ := sequenceEvents(sequence, track.TimebaseD, track.TimebaseG, handyChannelOffset, isHandy, track.Waves, tones, budget)
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
	budget *decodeBudget,
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
		if budget.err != nil {
			return nil, channelOffset
		}
		// The duration precedes the event it introduces, so the clock advances
		// first and the event lands at the new time.
		now = budget.milliseconds(uint64(now) + uint64(event.Duration)*uint64(timebaseD))
		if budget.err != nil {
			return nil, channelOffset
		}
		time := now

		switch event.Kind {
		case SeqNote:
			channel := mapChannel(event.Channel)
			// Explicit velocity updates channel memory even when the stream
			// wave is absent or unsupported and this note produces no output.
			velocity := tones.noteVelocity(channel, event.Velocity, event.HasVelocity)
			if event.Note == 0 {
				// Note zero plays the score track's attached wave rather than
				// a pitch. The wave numbering is one-based against channels.
				wave, ok := waves[channel+1]
				if !ok {
					continue
				}
				expansion := streamWaveExpansion(wave)
				if expansion == 0 {
					continue
				}
				if !budget.pcm(len(wave.Data), expansion) {
					return nil, channelOffset
				}
				samples, playable := decodeStreamWave(wave)
				if !playable {
					continue
				}
				events = budget.emit(events, Event{
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
			midiChannel := tones.realChannel(channel)
			gate := uint64(event.GateTime) * uint64(timebaseG)
			duration := tones.noteDuration(channel, gate)
			// Validate the latest generated note-off before narrowing any gate
			// arithmetic, including the extra tails of an atmosphere voice.
			layerTail := uint64(0)
			pseudo := tones.pseudoChannel(channel)
			if tones.atmosphere[pseudo] {
				for layer, active := range tones.atmosLayerSet[pseudo] {
					if active {
						layerTail = max(layerTail, uint64(tones.atmosLayers[pseudo][layer].gateExtensionMS))
					}
				}
			}
			budget.milliseconds(uint64(time) + duration + layerTail)
			if budget.err != nil {
				return nil, channelOffset
			}
			events = budget.emit(events,
				Event{Time: time, Type: EventNoteOn, Channel: midiChannel, Note: note, Velocity: velocity},
				Event{Time: time + uint32(duration), Type: EventNoteOff, Channel: midiChannel, Note: note})
			events = budget.emit(events, tones.atmosphereNotes(time, uint32(duration), channel, note, velocity)...)

		case SeqControlChange:
			channel := mapChannel(event.Channel)
			tones.updateControl(channel, event.Control, event.Value)
			events = budget.emit(events, Event{
				Time: time, Type: EventControlChange,
				Channel: tones.realChannel(channel), Control: event.Control, Value: event.Value,
			})

		case SeqProgramChange:
			source := mapChannel(event.Channel)
			channel, program := tones.setProgram(source, event.Program)
			events = budget.emit(events, Event{Time: time, Type: EventProgramChange, Channel: channel, Program: program})
			events = budget.emit(events, tones.atmosphereSetup(time, source, event.Program)...)

		case SeqExclusive:
			events = budget.emit(events, Event{Time: time, Type: EventSysEx, SysEx: budget.sysEx(event.Exclusive)})

		case SeqPitchBend:
			channel := tones.realChannel(mapChannel(event.Channel))
			bend := event.BendValue
			if bend > 0x3fff {
				bend = 0x3fff
			}
			events = budget.emit(events, Event{Time: time, Type: EventPitchBend, Channel: channel, Bend: bend})

		case SeqVolume:
			events = budget.emit(events, tones.volumeEvents(time, mapChannel(event.Channel), event.Value)...)

		case SeqPan:
			channel := tones.realChannel(mapChannel(event.Channel))
			events = budget.emit(events, Event{Time: time, Type: EventControlChange, Channel: channel, Control: 10, Value: event.Value})

		case SeqExpression:
			events = budget.emit(events, tones.expressionEvents(time, mapChannel(event.Channel), event.Value)...)

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
			events = budget.emit(events, Event{Time: time, Type: EventControlChange, Channel: channel, Control: 1, Value: event.Value})

		case SeqBankSelect:
			channel := mapChannel(event.Channel)
			tones.updateBankSelect(channel, event.Value)
			events = budget.emit(events, Event{
				Time: time, Type: EventControlChange,
				Channel: tones.realChannel(channel), Control: 0, Value: event.Value & 0x7f,
			})
		}
	}
	events = budget.emit(events, Event{Time: now, Type: EventEnd})

	nextOffset := channelOffset
	if useChannelOffset {
		nextOffset = saturatingAdd(channelOffset, 4)
	}
	return events, nextOffset
}

func pcmTrackEvents(track *PCMAudioTrack, base uint16, budget *decodeBudget) []Event {
	// The decoder handles mono ADPCM, which is what these tracks are in
	// practice. Anything else is left silent rather than played as noise.
	if track.Format != PCMAdpcm || track.Channels != Mono {
		return nil
	}

	var events []Event
	var now uint32
	for _, event := range track.Sequence {
		if budget.err != nil {
			return nil
		}
		now = budget.milliseconds(uint64(now) + uint64(event.Duration)*uint64(track.TimebaseD))
		if budget.err != nil {
			return nil
		}
		switch event.Kind {
		case PCMEventVolume, PCMEventExpression, PCMEventPan:
			if event.Value >= 128 {
				continue
			}
			control := uint8(7)
			if event.Kind == PCMEventExpression {
				control = 11
			} else if event.Kind == PCMEventPan {
				control = 10
			}
			events = budget.emit(events, Event{Time: now, Type: EventPCMControl,
				PCMChannel: base + uint16(event.Channel), Control: control, Value: event.Value})
		case PCMEventWave:
			wave, ok := track.Waves[event.WaveNumber]
			if !ok {
				continue
			}
			if !budget.pcm(len(wave), 4) {
				return nil
			}
			events = budget.emit(events, Event{
				Time:         now,
				Type:         EventWave,
				PCMChannel:   base + uint16(event.Channel),
				WaveChannels: waveChannelCount(track.Channels),
				SamplingRate: track.SamplingFreq,
				Wave:         DecodeADPCM(wave),
			})
		}
	}
	return budget.emit(events, Event{Time: now, Type: EventEnd})
}

func waveChannelCount(channels Channels) uint8 {
	if channels == Stereo {
		return 2
	}
	return 1
}

// setupSysExEvents reads a setup chunk's concatenated sysex messages. HPS uses
// the same FF F0 header and byte size as its sequence, without a duration.
// Mobile uses F0 followed by a MIDI variable length quantity and its payload.
func setupSysExEvents(data []byte, handy bool, budget *decodeBudget) []Event {
	var events []Event
	offset := 0
	for offset < len(data) {
		if budget.err != nil {
			return nil
		}
		if handy {
			if len(data)-offset < 2 || data[offset] != 0xff || data[offset+1] != 0xf0 {
				break
			}
			r := reader{data: data, offset: offset + 2}
			payload, ok := readHandyExclusive(&r, false)
			if !ok {
				break
			}
			events = budget.emit(events, Event{Time: 0, Type: EventSysEx, SysEx: budget.sysEx(payload)})
			offset = r.offset
			continue
		}
		if data[offset] != 0xf0 {
			break
		}
		offset++
		length, next, ok := readMIDIVariableLength(data, offset)
		if !ok || length > len(data)-next {
			break
		}
		events = budget.emit(events, Event{Time: 0, Type: EventSysEx, SysEx: budget.sysEx(data[next : next+length])})
		offset = next + length
	}
	return events
}

func readMIDIVariableLength(data []byte, offset int) (value, next int, ok bool) {
	if offset < 0 || offset > len(data) {
		return 0, 0, false
	}
	r := reader{data: data, offset: offset}
	length, ok := r.variableNumber()
	if !ok || uint64(length) > uint64(^uint(0)>>1) {
		return 0, 0, false
	}
	return int(length), r.offset, true
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
	expansion := streamWaveExpansion(wave)
	if expansion == 0 || len(wave.Data) > maxDecodedPCMBytes/expansion {
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

// streamWaveExpansion is the number of decoded bytes per encoded byte. Zero
// marks an unsupported encoding; it must not consume the PCM decoding budget.
func streamWaveExpansion(wave WaveData) int {
	if wave.Channels != Mono {
		return 0
	}
	if wave.Format == YamahaADPCM && wave.BaseBit == Bit4 {
		return 4
	}
	if (wave.Format == OffsetBinaryPCM || wave.Format == TwosComplementPCM) && wave.BaseBit == Bit8 {
		return 2
	}
	return 0
}
