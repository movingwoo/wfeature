package webhost

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
)

// A protocol 2 sound message is binary. It starts with "WFA2" and holds the
// tick's operations in the order the guest made the calls, each a byte and its
// operands, big-endian:
//
//	0x01 note on         channel, note, velocity
//	0x02 note off        channel, note, velocity
//	0x03 program change  channel, program
//	0x04 control change  channel, control, value
//	0x05 pitch bend      channel, value (16 bits)
//	0x06 all off
//	0x10 define          id (32 bits), length (32 bits), the bytes
//	0x11 play wave       id, channels, sampling rate (32 bits)
//	0x12 SysEx           id
//	0x13 forget          drop every definition held
//
// A sampled sound or a SysEx message travels once as a definition and is named
// by its id afterwards. A game plays the same few effects over and over, and
// the first protocol sent each one again as base64 text every time: one title
// measured 2.4 KB/s of samples that were four distinct sounds.
const (
	audioOpNoteOn        = 0x01
	audioOpNoteOff       = 0x02
	audioOpProgramChange = 0x03
	audioOpControlChange = 0x04
	audioOpPitchBend     = 0x05
	audioOpAllOff        = 0x06
	audioOpDefine        = 0x10
	audioOpPlayWave      = 0x11
	audioOpSysEx         = 0x12
	audioOpForget        = 0x13
)

var audioMagic = []byte("WFA2")

// audioDefinitionBudget bounds what a page is asked to hold. A title that
// streams long, never-repeated samples would otherwise grow the page's store
// for as long as it is played; past the budget the store starts over.
const audioDefinitionBudget = 8 << 20

// audioDefinitions is what one connection's page holds. It belongs to the
// connection rather than the game: a page that reconnects has nothing.
type audioDefinitions struct {
	ids  map[[sha256.Size]byte]uint32
	last uint32
	held int
	// forget says the page's store is not known any more — a message that
	// defined something was dropped — so the next message clears it first.
	forget bool
}

// encode writes one batch as a protocol 2 message.
func (d *audioDefinitions) encode(events []audioEvent) []byte {
	out := append([]byte(nil), audioMagic...)
	if d.forget {
		out = append(out, audioOpForget)
		d.forget = false
	}
	for _, event := range events {
		switch event.Kind {
		case audioNoteOn:
			out = append(out, audioOpNoteOn, event.Channel, event.Note, event.Velocity)
		case audioNoteOff:
			out = append(out, audioOpNoteOff, event.Channel, event.Note, event.Velocity)
		case audioProgramChange:
			out = append(out, audioOpProgramChange, event.Channel, event.Program)
		case audioControlChange:
			out = append(out, audioOpControlChange, event.Channel, event.Control, byte(event.Value))
		case audioPitchBend:
			out = append(out, audioOpPitchBend, event.Channel)
			out = binary.BigEndian.AppendUint16(out, event.Value)
		case audioAllOff:
			out = append(out, audioOpAllOff)
		case audioPlayWave:
			var id uint32
			out, id = d.reference(out, event.pcm)
			out = append(out, audioOpPlayWave)
			out = binary.BigEndian.AppendUint32(out, id)
			out = append(out, event.Channels)
			out = binary.BigEndian.AppendUint32(out, event.Rate)
		case audioSysEx:
			var id uint32
			out, id = d.reference(out, event.raw)
			out = append(out, audioOpSysEx)
			out = binary.BigEndian.AppendUint32(out, id)
		}
	}
	return out
}

// reference answers the id the page knows data by, defining it first when the
// page does not hold it yet. Ids are never reused, so a page that missed a
// definition finds nothing under an id rather than the wrong sound.
func (d *audioDefinitions) reference(out, data []byte) ([]byte, uint32) {
	digest := sha256.Sum256(data)
	if id, ok := d.ids[digest]; ok {
		return out, id
	}
	if d.ids == nil {
		d.ids = make(map[[sha256.Size]byte]uint32)
	}
	if d.held > 0 && d.held+len(data) > audioDefinitionBudget {
		out = append(out, audioOpForget)
		clear(d.ids)
		d.held = 0
	}
	d.last++
	d.ids[digest] = d.last
	d.held += len(data)
	out = append(out, audioOpDefine)
	out = binary.BigEndian.AppendUint32(out, d.last)
	out = binary.BigEndian.AppendUint32(out, uint32(len(data)))
	return append(out, data...), d.last
}

// dropped records that a message this encoded never reached the queue.
func (d *audioDefinitions) dropped() {
	clear(d.ids)
	d.held = 0
	d.forget = true
}

// textAudio fills in the base64 fields the first protocol's JSON carries.
func textAudio(events []audioEvent) []audioEvent {
	for index := range events {
		event := &events[index]
		switch {
		case event.pcm != nil:
			event.Samples = base64.StdEncoding.EncodeToString(event.pcm)
		case event.raw != nil:
			event.Data = base64.StdEncoding.EncodeToString(event.raw)
		}
	}
	return events
}
