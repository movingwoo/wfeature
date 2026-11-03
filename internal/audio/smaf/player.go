package smaf

import (
	"cmp"
	"slices"
)

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
	// EventVoice defines one FM voice of the file's own: Program is its slot,
	// one or more, and SysEx its Voice.Encode bytes. A note-on whose Program
	// names the slot plays that voice at the pitch in its Value.
	EventVoice
)

// Event is one thing to do at Time milliseconds after the start of playback.
//
// A note-on's Program and Value are its voice: the slot of an EventVoice the
// same sound defined, and the pitch that voice sounds. Slot zero means the
// file defines no voice for the note, and Value is then unused. The pitch can
// differ from Note: a rhythm note's Note is the drum key a General MIDI
// synthesizer plays, while the chip sounds the file's drum voice at the pitch
// the score wrote.
type Event struct {
	Time uint32
	Type EventType

	Channel uint8
	Note    uint8
	// Velocity is a note's, or for a wave that of the stream note that
	// triggered it, 1 to 127. A wave's zero means it has none and plays at
	// its full level: a PCM audio track's waves, and events made before
	// waves carried one.
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
	// noMIDIChannel is what a logical channel past the sixteenth maps to when
	// every MIDI channel is taken. Its events are dropped before they leave
	// the track, so a malformed file never names a channel MIDI lacks.
	noMIDIChannel = 0xff
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

	voices, events := collectVoices(file, budget)
	if budget.err != nil {
		return nil
	}
	handyChannelOffset := uint8(0)
	handyToneMap := newToneMap()
	handyToneMap.voices = voices
	pcmTracks := 0
	scored := false

	for _, chunk := range file.Chunks {
		switch chunk.Kind {
		case ChunkScoreTrack:
			scored = true
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
			scored = true
			tones := newToneMap()
			tones.voices = voices
			tones.initTrack(HandyPhoneStandard, nil, handyChannelOffset)
			trackEvents, nextOffset := sequenceEvents(chunk.SoftbankSequence, softbankTimebase, softbankTimebase, handyChannelOffset, true, nil, tones, budget)
			events = append(events, trackEvents...)
			handyChannelOffset = nextOffset
		}
		if budget.err != nil {
			return nil
		}
	}

	// **A sound made only of PCM tracks lasts until its last sample has
	// played.** A wave plays to the end of its sample whatever its gate says,
	// and about 2,400 PCM-only sounds across the local KTF, LGT and SKT
	// archives end their sequence about half a second before their sample
	// does, with sound rather than padding in that half second. Ending them
	// with the sequence reported them finished early, and a one-shot one was
	// stopped there. A sound with a score keeps the score's timeline: that is
	// where a looping piece starts again, and its samples sound past it as
	// they always have. See docs/audio.md.
	if !scored {
		var last uint32
		for _, event := range events {
			last = max(last, event.Time)
		}
		if end := pcmSampleEnd(events, budget); budget.err == nil && end > last {
			events = budget.emit(events, Event{Time: end, Type: EventEnd})
		}
		if budget.err != nil {
			return nil
		}
	}

	slices.SortStableFunc(events, func(left, right Event) int {
		if left.Time != right.Time {
			return cmp.Compare(left.Time, right.Time)
		}
		return cmp.Compare(eventOrder(left), eventOrder(right))
	})
	return events
}

func eventOrder(event Event) int {
	switch event.Type {
	case EventVoice:
		return 3
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
		voices := tones.voices
		tones = newToneMap()
		tones.voices = voices
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
	var sounding [maxSMAFChannels]handsetNote

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
			// Note zero is a stream note even when the wave it names is
			// missing; it then plays nothing rather than a pitch.
			if wave, ok := streamWave(waves, channel, event.Note, tones.formatType); ok || event.Note == 0 {
				// A stream note's level follows its velocity, and at zero
				// it is silent, like a pitched note-on of zero.
				if !ok || velocity == 0 {
					continue
				}
				expansion := streamWaveExpansion(wave)
				if expansion == 0 {
					continue
				}
				if !budget.pcm(len(wave.Data), expansion) {
					return nil, channelOffset
				}
				samples, playable := budget.expand(wave, decodeStreamWave)
				if !playable {
					continue
				}
				events = budget.emit(events, Event{
					Time:         time,
					Type:         EventWave,
					Velocity:     velocity,
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
			// A note-off at its note-on's instant sorts before it and would
			// leave the note held, so a zero gate keys off a millisecond on.
			duration = max(duration, 1)
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
			noteOn := Event{Time: time, Type: EventNoteOn, Channel: midiChannel, Note: note, Velocity: velocity}
			noteOn.Program, noteOn.Value = tones.voiceFor(channel, int16(event.Note)+int16(octaveShift[index])*12, time)
			handset := tones.formatType == HandyPhoneStandard
			if handset {
				releaseHandsetNote(events, &sounding[pseudo], time)
			}
			emitted := len(events)
			events = budget.emit(events, noteOn,
				Event{Time: time + uint32(duration), Type: EventNoteOff, Channel: midiChannel, Note: note})
			layers := len(events)
			events = budget.emit(events, tones.atmosphereNotes(time, uint32(duration), channel, note, velocity)...)
			if handset && layers == emitted+2 {
				// The note's note-off, then each ambience layer's: every
				// layer is a note-on followed by its note-off.
				current := handsetNote{start: time}
				for off := emitted + 1; off < len(events) && current.count < len(current.offs); off += 2 {
					current.offs[current.count] = off
					current.count++
				}
				sounding[pseudo] = current
			}

		case SeqControlChange:
			// MIDI data bytes are seven bits. A control carrying a wider one
			// is ignored, as the PCM track's controls are, and the clock that
			// already advanced keeps the rest of the timeline in place.
			if event.Control > 0x7f || event.Value > 0x7f {
				continue
			}
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
			// The ambience layers stand in for a voice General MIDI lacks.
			// A file that defines that voice itself needs no stand-in, even
			// when an earlier selection of the voice set one up.
			if tones.definesVoice(source, time) {
				tones.atmosphere[tones.pseudoChannel(source)] = false
			} else {
				events = budget.emit(events, tones.atmosphereSetup(time, source, event.Program)...)
			}

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
			if event.Value > 0x7f {
				continue
			}
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
			if event.Value > 0x7f {
				continue
			}
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
	events = slices.DeleteFunc(events, func(event Event) bool { return event.Channel == noMIDIChannel })

	nextOffset := channelOffset
	if useChannelOffset {
		nextOffset = saturatingAdd(channelOffset, 4)
	}
	return events, nextOffset
}

// streamWave answers the attached wave a mobile score note triggers. Note zero
// plays the wave numbered one past its channel. Channel 9 also numbers waves
// by note: its note n plays wave n+1 whenever the track carries that wave in
// a form this decoder plays, and is a rhythm note otherwise; its note zero
// then falls back to wave 10. That is how the 21 local files whose only
// stream notes are on channel 9 sound at all (docs/audio.md, "Stream notes on
// channel 9"). Handset tracks attach no waves.
func streamWave(waves map[uint8]WaveData, channel, note uint8, format FormatType) (WaveData, bool) {
	if format != HandyPhoneStandard && channel == waveNoteChannel && note < 0xff {
		if wave, ok := waves[note+1]; ok && streamWaveExpansion(wave) != 0 {
			return wave, true
		}
	}
	if note != 0 {
		return WaveData{}, false
	}
	wave, ok := waves[channel+1]
	return wave, ok
}

// waveNoteChannel is the mobile score channel whose notes name waves.
const waveNoteChannel = 9

// handsetNote is the last note a handset channel started: where its note-off
// and its ambience layers' note-offs sit in the events being built, and when
// it began.
type handsetNote struct {
	offs  [1 + len(atmosphereSpecs)]int
	count int
	start uint32
}

// releaseHandsetNote ends a handset channel's previous note when the next one
// starts. A handset channel sounds one note at a time, rhythm channels too: a
// new note-on keys off the note still sounding there, whatever its key, and
// the ambience layers laid under it. A note-off moves forward rather than a
// second one being added, so the old one cannot cut a later note of the same
// key. A note replaced at its own start never sounds and is dropped with its
// note-off and its layers: at one instant every note-off sorts before every
// note-on, which would leave it held.
func releaseHandsetNote(events []Event, previous *handsetNote, time uint32) {
	for _, off := range previous.offs[:previous.count] {
		if off >= len(events) || events[off].Time <= time {
			continue
		}
		if previous.start == time {
			events[off-1].Channel = noMIDIChannel
			events[off].Channel = noMIDIChannel
		} else {
			events[off].Time = time
		}
	}
	previous.count = 0
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
			samples, _ := budget.expand(WaveData{Channels: Mono, Format: YamahaADPCM, BaseBit: Bit4, Data: wave}, func(wave WaveData) ([]int16, bool) {
				return DecodeADPCM(wave.Data), true
			})
			events = budget.emit(events, Event{
				Time:         now,
				Type:         EventWave,
				PCMChannel:   base + uint16(event.Channel),
				WaveChannels: waveChannelCount(track.Channels),
				SamplingRate: track.SamplingFreq,
				Wave:         samples,
			})
		}
	}
	return budget.emit(events, Event{Time: now, Type: EventEnd})
}

// pcmSampleEnd is when the last sample a PCM track triggered has played out:
// its start plus its length at its own rate. Stream waves a score attaches
// (group zero) are not counted.
func pcmSampleEnd(events []Event, budget *decodeBudget) uint32 {
	var end uint32
	for _, event := range events {
		if event.Type != EventWave || event.PCMChannel == 0 || event.SamplingRate == 0 || event.WaveChannels == 0 {
			continue
		}
		frames := uint64(len(event.Wave) / int(event.WaveChannels))
		played := (frames*1000 + uint64(event.SamplingRate) - 1) / uint64(event.SamplingRate)
		end = max(end, budget.milliseconds(uint64(event.Time)+played))
	}
	return end
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
	forEachSetupExclusive(data, handy, func(payload []byte) bool {
		if budget.err != nil {
			events = nil
			return false
		}
		events = budget.emit(events, Event{Time: 0, Type: EventSysEx, SysEx: budget.sysEx(payload)})
		return true
	})
	if budget.err != nil {
		return nil
	}
	return events
}

// forEachSetupExclusive calls visit with each message's payload, from its
// maker ID, until a message does not parse or visit answers false.
func forEachSetupExclusive(data []byte, handy bool, visit func([]byte) bool) {
	offset := 0
	for offset < len(data) {
		if handy {
			if len(data)-offset < 2 || data[offset] != 0xff || data[offset+1] != 0xf0 {
				return
			}
			r := reader{data: data, offset: offset + 2}
			payload, ok := readHandyExclusive(&r, false)
			if !ok || !visit(payload) {
				return
			}
			offset = r.offset
			continue
		}
		if data[offset] != 0xf0 {
			return
		}
		offset++
		length, next, ok := readMIDIVariableLength(data, offset)
		if !ok || length > len(data)-next || !visit(data[next:next+length]) {
			return
		}
		offset = next + length
	}
}

// softbankTimebase is the millisecond unit of a Softbank sequence's durations
// and gates, which declare none of their own.
const softbankTimebase = 20

// collectVoices reads every FM voice the file defines before any track plays,
// because a voice set up by one track is the chip's, not the track's: a later
// track's channels select it too. Setup voices are in force from the start;
// an inline one from its own time, which the same duration arithmetic as
// sequenceEvents places. The answered events carry each definition.
func collectVoices(file *File, budget *decodeBudget) (*voiceTable, []Event) {
	table := &voiceTable{}
	var events []Event
	define := func(time uint32, message []byte) bool {
		slot, encoded, ok := table.define(time, message)
		if !ok {
			return budget.err == nil
		}
		if !budget.voice(len(encoded)) {
			return false
		}
		events = budget.emit(events, Event{Time: time, Type: EventVoice, Program: slot, SysEx: encoded})
		return budget.err == nil
	}
	for _, chunk := range file.Chunks {
		if chunk.Kind != ChunkScoreTrack {
			continue
		}
		for _, setup := range chunk.ScoreTrack.SetupData {
			forEachSetupExclusive(setup, chunk.ScoreTrack.FormatType == HandyPhoneStandard, func(payload []byte) bool {
				return define(0, payload)
			})
		}
	}
	inline := func(sequence []SequenceEvent, timebase uint32) {
		var now uint64
		for _, event := range sequence {
			now += uint64(event.Duration) * uint64(timebase)
			if now > uint64(^uint32(0)) || budget.err != nil {
				return
			}
			if event.Kind == SeqExclusive && !define(uint32(now), event.Exclusive) {
				return
			}
		}
	}
	for _, chunk := range file.Chunks {
		switch chunk.Kind {
		case ChunkScoreTrack:
			for _, sequence := range chunk.ScoreTrack.Sequences {
				inline(sequence, chunk.ScoreTrack.TimebaseD)
			}
		case ChunkSoftbankSequence:
			inline(chunk.SoftbankSequence, softbankTimebase)
		}
	}
	if budget.err != nil {
		return nil, nil
	}
	return table, events
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

// waveSource names one encoded wave in the file being decoded: its bytes, by
// where they sit, and how they are read.
type waveSource struct {
	data    *byte
	length  int
	format  StreamWaveFormat
	baseBit BaseBit
}

type decodedWave struct {
	samples  []int16
	playable bool
}

// expand answers what a wave decodes to, decoding each wave once per Decode.
// A score strikes the same wave again and again, a drum wave on every beat;
// every strike's event holds the same samples, which nothing downstream
// writes to. Each strike is still
// charged to the PCM budget on its own.
func (b *decodeBudget) expand(wave WaveData, decode func(WaveData) ([]int16, bool)) ([]int16, bool) {
	if len(wave.Data) == 0 {
		return decode(wave)
	}
	key := waveSource{data: &wave.Data[0], length: len(wave.Data), format: wave.Format, baseBit: wave.BaseBit}
	if known, ok := b.samples[key]; ok {
		return known.samples, known.playable
	}
	samples, playable := decode(wave)
	if b.samples == nil {
		b.samples = make(map[waveSource]decodedWave)
	}
	b.samples[key] = decodedWave{samples: samples, playable: playable}
	return samples, playable
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
