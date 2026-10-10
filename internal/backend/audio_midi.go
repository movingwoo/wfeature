package backend

import "slices"

// controlChange records the supported MIDI channel state. Parameter selection
// is independent of parameter values: selecting another RPN or NRPN must not
// let subsequent Data Entry overwrite the previously selected sensitivity.
func (state *AudioChannelState) controlChange(control, value uint8) {
	switch control {
	case 7:
		state.Volume = value
	case 10:
		state.Pan = value
	case 11:
		state.Expression = value
	case 64:
		state.Sustain = value
	case 98:
		state.NRPNLSB, state.NRPN = value, true
	case 99:
		state.NRPNMSB, state.NRPN = value, true
	case 100:
		state.RPNLSB, state.NRPN = value, false
	case 101:
		state.RPNMSB, state.NRPN = value, false
	case 121:
		// Reset controller positions, not instrument/mix configuration or
		// registered parameter values (including pitch sensitivity).
		state.Expression, state.Sustain, state.Bend = 127, 0, 8192
		state.RPNMSB, state.RPNLSB, state.NRPNMSB, state.NRPNLSB = 127, 127, 127, 127
		state.NRPN = false
	case 6, 38, 96, 97:
		if state.NRPN || state.RPNMSB != 0 || state.RPNLSB != 0 {
			return
		}
		switch control {
		case 6:
			state.BendSemitones, state.BendCents = value, 0
		case 38:
			// RPN 0 defines cents 0..99; clamp out-of-range data bytes.
			state.BendCents = min(value, 99)
		case 96, 97:
			step := 1
			if control == 97 {
				step = -1
			}
			cents := min(12799, max(0, int(state.BendSemitones)*100+int(state.BendCents)+step))
			state.BendSemitones, state.BendCents = uint8(cents/100), uint8(cents%100)
		}
	}
}

func (output *audioOutput) releaseNote(channel, note uint8) {
	if channel == 9 {
		return // One-shot percussion keeps its natural duration.
	}
	if output.currentChannels()[channel].Sustain < 64 {
		output.removeNote(channel, note)
		return
	}
	for index := range output.notes {
		current := &output.notes[index]
		if current.Sound == output.sound && current.Channel == channel && current.Note == note {
			current.Released = true
		}
	}
}

func (output *audioOutput) releaseSustained(channel uint8) {
	output.notes = slices.DeleteFunc(output.notes, func(note audioOutputNote) bool {
		return note.Sound == output.sound && note.Channel == channel && note.Released
	})
}

func (output *audioOutput) stopChannel(channel uint8, immediate bool) {
	deferRelease := !immediate && channel != 9 && output.currentChannels()[channel].Sustain >= 64
	output.notes = slices.DeleteFunc(output.notes, func(note audioOutputNote) bool {
		return note.Sound == output.sound && note.Channel == channel && !deferRelease
	})
	if deferRelease {
		for index := range output.notes {
			note := &output.notes[index]
			if note.Sound == output.sound && note.Channel == channel {
				note.Released = true
			}
		}
	}
}

func replayAudioParameters(sink AudioSink, channel uint8, state AudioChannelState) {
	// Set sensitivity before applying bend or starting any reconstructed note.
	sink.MIDIControlChange(channel, 101, 0)
	sink.MIDIControlChange(channel, 100, 0)
	sink.MIDIControlChange(channel, 6, state.BendSemitones)
	sink.MIDIControlChange(channel, 38, state.BendCents)
	rpn := func() {
		sink.MIDIControlChange(channel, 101, state.RPNMSB)
		sink.MIDIControlChange(channel, 100, state.RPNLSB)
	}
	nrpn := func() {
		sink.MIDIControlChange(channel, 99, state.NRPNMSB)
		sink.MIDIControlChange(channel, 98, state.NRPNLSB)
	}
	// Retain both selector pairs and leave the most recently selected kind
	// active, so a later one-byte selector change has its original meaning.
	if state.NRPN {
		rpn()
		nrpn()
	} else {
		nrpn()
		rpn()
	}
}
