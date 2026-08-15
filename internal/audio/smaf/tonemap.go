package smaf

// toneMap converts SMAF's channel and program space into MIDI's.
//
// Three problems it solves. SMAF tracks each address four channels and a file
// may hold several, so more than sixteen logical channels can exist; melodic
// ones are allocated real MIDI channels on first use and rhythm ones all go to
// channel 10. SMAF programs come from Yamaha's MA banks, so bank 0x7d means
// "this is a drum kit" and a handful of MA voices have General MIDI stand-ins
// that sound closer than the raw program number would. And the handset dialect
// keeps volume and expression as separate scales that MIDI folds into one
// controller, and expresses drum hits as a program number rather than a note.
type toneMap struct {
	formatType FormatType

	channelKinds  [maxSMAFChannels]uint8
	programs      [maxSMAFChannels]uint8
	volumes       [maxSMAFChannels]uint8
	expressions   [maxSMAFChannels]uint8
	velocities    [maxSMAFChannels]uint8
	bankMSB       [maxSMAFChannels]uint8
	bankLSB       [maxSMAFChannels]uint8
	forcedRhythm  [maxSMAFChannels]bool
	realMap       [maxSMAFChannels]uint8
	realMapSet    [maxSMAFChannels]bool
	reserved      [16]bool
	atmosphere    [maxSMAFChannels]bool
	atmosLayers   [maxSMAFChannels][2]atmosphereLayer
	atmosLayerSet [maxSMAFChannels][2]bool
}

// atmosphereLayer is one of the two supporting voices laid under Yamaha's MA
// ambience voice, which General MIDI has no single equivalent for.
type atmosphereLayer struct {
	channel         uint8
	program         uint8
	velocityPercent uint8
	noteOffset      int8
	pan             uint8
	pitchBend       uint16
	gateExtensionMS uint32
}

// Channel kinds as this map stores them, which is the SMAF encoding rather
// than ChannelKind: 1 melody, 2 no-melody, 3 rhythm.
const (
	toneMelody  = 1
	toneNoMelry = 2
	toneRhythm  = 3
)

func newToneMap() *toneMap {
	tones := &toneMap{formatType: MobileStandardNoCompress}
	tones.reset()
	return tones
}

func (tones *toneMap) reset() {
	for index := 0; index < maxSMAFChannels; index++ {
		tones.channelKinds[index] = toneNoMelry
		tones.programs[index] = 0
		tones.volumes[index] = 100
		tones.expressions[index] = 127
		tones.velocities[index] = 64
		tones.bankMSB[index] = 0
		tones.bankLSB[index] = 0
		tones.forcedRhythm[index] = false
		tones.realMapSet[index] = false
		tones.atmosphere[index] = false
		tones.atmosLayerSet[index] = [2]bool{}
	}
	tones.reserved = [16]bool{}
}

// initTrack applies a track's channel declarations. Handset tracks only touch
// the four channels they own, because a later track continues the allocation
// the earlier one started; mobile tracks own the whole map.
func (tones *toneMap) initTrack(formatType FormatType, statuses []ChannelStatus, channelOffset uint8) {
	tones.formatType = formatType

	if formatType == HandyPhoneStandard {
		base := int(channelOffset)
		if base > maxSMAFChannels-4 {
			base = maxSMAFChannels - 4
		}
		for local := 0; local < 4; local++ {
			channel := base + local
			kind := uint8(toneMelody)
			if local < len(statuses) && statuses[local].Kind == ChannelRhythm {
				kind = toneRhythm
			}
			tones.channelKinds[channel] = kind
			tones.programs[channel] = 0
			tones.volumes[channel] = 100
			tones.expressions[channel] = 127
			// Handset velocity comes from volume and expression, so the
			// stored default is full scale rather than MIDI's mezzo-forte.
			tones.velocities[channel] = 127
			tones.bankMSB[channel] = 0
			tones.bankLSB[channel] = 0
			tones.forcedRhythm[channel] = false
			tones.atmosphere[channel] = false
			tones.atmosLayerSet[channel] = [2]bool{}
		}
		return
	}

	tones.reset()
	for channel, status := range statuses {
		if channel >= 16 {
			break
		}
		switch status.Kind {
		case ChannelMelody:
			tones.channelKinds[channel] = toneMelody
		case ChannelRhythm:
			tones.channelKinds[channel] = toneRhythm
		default:
			tones.channelKinds[channel] = toneNoMelry
		}
	}
}

// preclassify walks the sequence once before playing it, because whether a
// channel is a drum channel decides which MIDI channel its very first event
// goes to — and the bank select that says so can arrive after that event.
func (tones *toneMap) preclassify(sequence []SequenceEvent, useChannelOffset bool, channelOffset uint8) {
	bankMSB := tones.bankMSB
	for _, event := range sequence {
		channel := tones.logicalChannel(event.Channel, useChannelOffset, channelOffset)
		switch event.Kind {
		case SeqControlChange:
			if event.Control == 0 {
				bankMSB[channel] = event.Value & 0x7f
			}
		case SeqBankSelect:
			bankMSB[channel] = event.Value & 0x7f
			if event.Value&0x80 != 0 {
				tones.forcedRhythm[channel] = true
			}
		case SeqProgramChange:
			if bankMSB[channel] == 0x7d {
				tones.forcedRhythm[channel] = true
			}
		}
	}
}

func (tones *toneMap) logicalChannel(channel uint8, useChannelOffset bool, channelOffset uint8) uint8 {
	if !useChannelOffset {
		return channel & 0x0f
	}
	shifted := saturatingAdd(channel, channelOffset)
	if shifted > maxSMAFChannels-1 {
		return maxSMAFChannels - 1
	}
	return shifted
}

// pseudoChannel is the index into this map's per-channel state. Handset tracks
// address the full range; mobile tracks wrap into sixteen.
func (tones *toneMap) pseudoChannel(channel uint8) uint8 {
	if tones.formatType == HandyPhoneStandard {
		if channel > maxSMAFChannels-1 {
			return maxSMAFChannels - 1
		}
		return channel
	}
	return channel & 0x0f
}

func (tones *toneMap) isRhythm(channel uint8) bool {
	index := tones.pseudoChannel(channel)
	return tones.channelKinds[index] == toneRhythm || tones.forcedRhythm[index] || tones.bankMSB[index] == 0x7d
}

// realChannel answers the MIDI channel this SMAF channel plays on, claiming
// one on first use. Rhythm always answers the drum channel.
func (tones *toneMap) realChannel(channel uint8) uint8 {
	index := tones.pseudoChannel(channel)
	if tones.isRhythm(index) {
		return midiDrumChannel
	}
	if tones.realMapSet[index] {
		return tones.realMap[index]
	}

	var used [16]bool
	used[midiDrumChannel] = true
	for other := 0; other < maxSMAFChannels; other++ {
		if tones.realMapSet[other] {
			used[tones.realMap[other]] = true
		}
	}
	for reserved, isReserved := range tones.reserved {
		if isReserved {
			used[reserved] = true
		}
	}

	real := index
	for _, candidate := range melodyAllocationOrder {
		if !used[candidate] {
			real = candidate
			break
		}
	}
	tones.realMap[index], tones.realMapSet[index] = real, true
	return real
}

func (tones *toneMap) updateControl(channel, control, value uint8) {
	index := tones.pseudoChannel(channel)
	switch control {
	case 0:
		tones.bankMSB[index] = value & 0x7f
	case 32:
		tones.bankLSB[index] = value & 0x7f
	case 7:
		tones.volumes[index] = min(value, 0x7f)
	}
}

func (tones *toneMap) updateBankSelect(channel, value uint8) {
	index := tones.pseudoChannel(channel)
	if value&0x80 != 0 {
		tones.forcedRhythm[index] = true
	}
	tones.bankMSB[index] = value & 0x7f
}

// setProgram records a program change and answers the MIDI channel and program
// to send. A handset rhythm channel's program is the drum key it will play, so
// it is stored rather than sent.
func (tones *toneMap) setProgram(channel, program uint8) (uint8, uint8) {
	index := tones.pseudoChannel(channel)
	if tones.formatType == HandyPhoneStandard && tones.channelKinds[index] == toneRhythm {
		tones.programs[index] = min(program, 0x7f)
		return midiDrumChannel, 0
	}
	if tones.bankMSB[index] == 0x7d {
		tones.forcedRhythm[index] = true
	}

	mapped := uint8(0)
	if !tones.isRhythm(index) {
		mapped = tones.mapProgram(index, program)
	}
	tones.programs[index] = mapped
	return tones.realChannel(index), mapped
}

// mapProgram substitutes General MIDI voices for the Yamaha MA voices that
// have a recognisable stand-in. Everything else passes through.
func (tones *toneMap) mapProgram(channel, program uint8) uint8 {
	index := tones.pseudoChannel(channel)
	program &= 0x7f
	switch [3]uint8{tones.bankMSB[index], tones.bankLSB[index], program} {
	case [3]uint8{0x7c, 0x01, 0x22}:
		return 81
	case [3]uint8{0x7c, 0x01, 0x70}:
		return 30
	case [3]uint8{0x7c, 0x01, 0x46}:
		return 84
	case [3]uint8{0x7c, 0x01, 0x21}:
		return 33
	case [3]uint8{0x7c, 0x01, 0x6a}:
		return 87
	case [3]uint8{0x7c, 0x01, 0x62}:
		return 98
	case [3]uint8{0x7d, 0x00, 0x02}:
		return 0
	}
	return program
}

// mapNote answers the MIDI note. The handset dialect's notes sit three octaves
// below MIDI's, and its rhythm channels carry the drum key in the program
// rather than the note.
func (tones *toneMap) mapNote(channel uint8, note int16) uint8 {
	index := tones.pseudoChannel(channel)

	if tones.formatType == HandyPhoneStandard {
		if tones.isRhythm(index) {
			return min(tones.programs[index], 0x7f)
		}
		return clampNote(note + 36)
	}

	mapped := clampNote(note)
	if !tones.isRhythm(index) {
		return mapped
	}
	// A few MA drum keys land on General MIDI keys that are silent; these are
	// the ones worth moving.
	switch mapped {
	case 0x12:
		return 45
	case 0x1a:
		return 41
	case 0x1f:
		return 47
	case 0x4d:
		return 50
	case 0x54:
		return 43
	case 0x59:
		return 48
	}
	return mapped
}

func clampNote(note int16) uint8 {
	if note < 0 {
		return 0
	}
	if note > 127 {
		return 127
	}
	return uint8(note)
}

// noteVelocity answers the velocity to play at, remembering it: the mobile
// dialect has a note form that omits velocity and means "as before".
func (tones *toneMap) noteVelocity(channel, velocity uint8, hasVelocity bool) uint8 {
	index := tones.pseudoChannel(channel)
	if tones.formatType == HandyPhoneStandard && tones.channelKinds[index] == toneRhythm {
		return tones.handsetDrumVelocity(index)
	}
	if !hasVelocity {
		return tones.velocities[index]
	}
	value := min(velocity, 0x7f)
	tones.velocities[index] = value
	return value
}

// handsetDrumVelocity derives a drum hit's loudness from the channel's volume
// and expression, which is the only dynamic a handset rhythm channel has. The
// divisor is smaller below half expression so quiet passages stay audible.
func (tones *toneMap) handsetDrumVelocity(index uint8) uint8 {
	divisor := uint16(100)
	if tones.expressions[index] < 64 {
		divisor = 102
	}
	value := uint16(tones.volumes[index]) * uint16(tones.expressions[index]) / divisor
	if value < 1 {
		return 1
	}
	if value > 127 {
		return 127
	}
	return uint8(value)
}

func (tones *toneMap) setExpression(channel, value uint8) uint8 {
	index := tones.pseudoChannel(channel)
	tones.expressions[index] = min(value, 0x7f)
	return tones.effectiveVolume(index)
}

// effectiveVolume folds the handset dialect's separate volume and expression
// into the single MIDI channel volume.
func (tones *toneMap) effectiveVolume(channel uint8) uint8 {
	index := tones.pseudoChannel(channel)
	value := uint16(tones.volumes[index]) * uint16(tones.expressions[index]) / 127
	return uint8(min(value, 127))
}

func (tones *toneMap) noteDuration(channel uint8, duration uint32) uint32 {
	if tones.atmosphere[tones.pseudoChannel(channel)] {
		return duration + 120
	}
	return duration
}

// volumeEvents answers what a volume change becomes. Handset rhythm has no
// per-channel volume of its own — every track shares the drum channel — so it
// is pinned rather than tracking the last track to set it.
func (tones *toneMap) volumeEvents(time uint32, channel, value uint8) []Event {
	tones.updateControl(channel, 7, value)
	if tones.formatType != HandyPhoneStandard {
		return []Event{{Time: time, Type: EventControlChange, Channel: tones.realChannel(channel), Control: 7, Value: value}}
	}
	if tones.isRhythm(channel) {
		return []Event{{Time: time, Type: EventControlChange, Channel: midiDrumChannel, Control: 7, Value: 100}}
	}
	return []Event{{
		Time: time, Type: EventControlChange,
		Channel: tones.realChannel(channel), Control: 7, Value: tones.effectiveVolume(channel),
	}}
}

// expressionEvents answers what an expression change becomes. The mobile
// dialect has MIDI's expression controller; the handset one does not, so its
// expression is folded into channel volume instead, and rhythm takes it as
// velocity on the next hit rather than as a controller.
func (tones *toneMap) expressionEvents(time uint32, channel, value uint8) []Event {
	if tones.formatType != HandyPhoneStandard {
		return []Event{{Time: time, Type: EventControlChange, Channel: tones.realChannel(channel), Control: 11, Value: value}}
	}
	folded := tones.setExpression(channel, value)
	if tones.isRhythm(channel) {
		return nil
	}
	return []Event{{Time: time, Type: EventControlChange, Channel: tones.realChannel(channel), Control: 7, Value: folded}}
}

// atmosphereSpecs are the two supporting voices laid under Yamaha's MA
// ambience voice: one detuned slightly flat and panned left, one an octave
// down and panned right, both quieter and both released later than the note
// that triggered them.
var atmosphereSpecs = [2]atmosphereLayer{
	{channel: 15, program: 99, velocityPercent: 42, noteOffset: 0, pan: 52, pitchBend: 8192 - 170, gateExtensionMS: 220},
	{channel: 14, program: 94, velocityPercent: 30, noteOffset: -12, pan: 76, pitchBend: 8192 + 130, gateExtensionMS: 360},
}

// atmosphereSetup claims the layer channels and configures them, once per
// source channel. A layer is only taken if its MIDI channel is still free.
func (tones *toneMap) atmosphereSetup(time uint32, channel, program uint8) []Event {
	if !tones.isAtmosphereVoice(channel, program) {
		return nil
	}
	index := tones.pseudoChannel(channel)
	tones.atmosphere[index] = true

	if !tones.atmosLayerSet[index][0] {
		var used [16]bool
		used[midiDrumChannel] = true
		for other := 0; other < maxSMAFChannels; other++ {
			if tones.realMapSet[other] {
				used[tones.realMap[other]] = true
			}
		}
		for reserved, isReserved := range tones.reserved {
			if isReserved {
				used[reserved] = true
			}
		}
		next := 0
		for _, spec := range atmosphereSpecs {
			if !used[spec.channel] && next < len(tones.atmosLayers[index]) {
				tones.reserved[spec.channel] = true
				tones.atmosLayers[index][next] = spec
				tones.atmosLayerSet[index][next] = true
				next++
			}
		}
	}

	source := tones.realChannel(channel)
	events := []Event{
		{Time: time, Type: EventControlChange, Channel: source, Control: 91, Value: 92},
		{Time: time, Type: EventControlChange, Channel: source, Control: 93, Value: 76},
		{Time: time, Type: EventControlChange, Channel: source, Control: 72, Value: 18},
	}

	volume := uint16(tones.volumes[index]) * 70 / 100
	if volume < 1 {
		volume = 1
	} else if volume > 127 {
		volume = 127
	}
	for layer, isSet := range tones.atmosLayerSet[index] {
		if !isSet {
			continue
		}
		spec := tones.atmosLayers[index][layer]
		events = append(events,
			Event{Time: time, Type: EventControlChange, Channel: spec.channel, Control: 7, Value: uint8(volume)},
			Event{Time: time, Type: EventControlChange, Channel: spec.channel, Control: 10, Value: spec.pan},
			Event{Time: time, Type: EventControlChange, Channel: spec.channel, Control: 11, Value: 110},
			Event{Time: time, Type: EventControlChange, Channel: spec.channel, Control: 91, Value: 104},
			Event{Time: time, Type: EventControlChange, Channel: spec.channel, Control: 93, Value: 84},
			Event{Time: time, Type: EventControlChange, Channel: spec.channel, Control: 72, Value: 22},
			Event{Time: time, Type: EventProgramChange, Channel: spec.channel, Program: spec.program},
			Event{Time: time, Type: EventPitchBend, Channel: spec.channel, Bend: spec.pitchBend})
	}
	return events
}

func (tones *toneMap) atmosphereNotes(time, duration uint32, channel, note, velocity uint8) []Event {
	index := tones.pseudoChannel(channel)
	if !tones.atmosphere[index] {
		return nil
	}
	var events []Event
	for layer, isSet := range tones.atmosLayerSet[index] {
		if !isSet {
			continue
		}
		spec := tones.atmosLayers[index][layer]
		layerNote := clampNote(int16(note) + int16(spec.noteOffset))
		layerVelocity := uint16(velocity) * uint16(spec.velocityPercent) / 100
		if layerVelocity < 1 {
			layerVelocity = 1
		} else if layerVelocity > 127 {
			layerVelocity = 127
		}
		events = append(events,
			Event{Time: time, Type: EventNoteOn, Channel: spec.channel, Note: layerNote, Velocity: uint8(layerVelocity)},
			Event{Time: time + duration + spec.gateExtensionMS, Type: EventNoteOff, Channel: spec.channel, Note: layerNote})
	}
	return events
}

func (tones *toneMap) isAtmosphereVoice(channel, program uint8) bool {
	index := tones.pseudoChannel(channel)
	return tones.bankMSB[index] == 0x7c && tones.bankLSB[index] == 0x01 && program&0x7f == 0x62
}
