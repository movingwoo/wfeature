package smaf

// The MA chips are FM synthesizers, and a SMAF file that wants its own
// instruments rather than the chip's built-in ones carries them as Yamaha
// exclusive messages: in a score track's setup chunk, or inline in its
// sequence. Almost every score in the local corpus does. This file reads those
// messages into operator parameters a Host can synthesize, and binds each note
// to the voice its channel selected.
//
// Two layouts exist. The handset one (`43 03`) is a two-operator voice: an
// index, the bank and program it defines, a global byte (LFO, feedback,
// algorithm), a constant byte, then five bytes per operator. The mobile one
// (`43 79 06|07 7F 01`) names bank MSB, bank LSB, program and drum key, a voice
// type, then three global bytes and seven bytes per operator for two or four
// operators. The `06` form stores that body in the Yamaha seven-bit packing,
// where every eighth byte carries the top bits of the seven after it; the `07`
// form stores it directly.

const (
	// maxVoiceSlots bounds the voices one file may define. Slot zero means
	// "no voice of the file's own", so a slot fits a note event's program byte.
	maxVoiceSlots = 255
	// voiceEncodingVersion leads the canonical encoding, so a Host can refuse
	// a layout it does not know rather than misread one.
	voiceEncodingVersion = 1
	voiceHeaderBytes     = 4
	voiceOperatorBytes   = 17
)

// Voice is one FM instrument in the parameters the MA chips take. The values
// are the chips' own register units, not physical ones; a Host converts them.
type Voice struct {
	// Algorithm is the operator connection, 0 through 7. Zero and one use
	// two operators (serial and parallel); two through seven use four.
	Algorithm uint8
	// LFO selects the voice's vibrato and tremolo rate, 0 through 3.
	LFO uint8
	// BasicOctave transposes the voice: 0 is up an octave, 1 none, 2 down
	// one and 3 down two.
	BasicOctave uint8
	Operators   [4]Operator
}

// Operator is one FM operator. Rates are 0 through 15, where zero never
// moves; levels are attenuation, where zero is loudest.
type Operator struct {
	// Multiple scales the note's frequency; zero means one half.
	Multiple uint8
	Detune   uint8
	// TotalLevel is the operator's attenuation in 0.75 dB steps, 0 to 63.
	TotalLevel uint8
	Attack     uint8
	Decay      uint8
	// SustainRate is the decay after the sustain level is reached; zero
	// holds the level until key-off.
	SustainRate uint8
	Release     uint8
	// SustainLevel is the attenuation decay stops at, in 3 dB steps.
	SustainLevel  uint8
	KeyScaleLevel uint8
	KeyScaleRate  bool
	// Wave selects the operator waveform, 0 through 31.
	Wave uint8
	// Feedback is the operator's self-modulation, 0 through 7.
	Feedback     uint8
	Tremolo      bool
	TremoloDepth uint8
	Vibrato      bool
	VibratoDepth uint8
	// IgnoreKeyOff lets the envelope run to its end after a key-off.
	IgnoreKeyOff bool
}

// OperatorCount answers how many operators the algorithm connects.
func (voice Voice) OperatorCount() int {
	if voice.Algorithm < 2 {
		return 2
	}
	return 4
}

// Encode answers the canonical encoding a Host receives: a version byte, the
// algorithm, LFO and basic octave, then seventeen bytes per operator.
func (voice Voice) Encode() []byte {
	count := voice.OperatorCount()
	data := make([]byte, 0, voiceHeaderBytes+count*voiceOperatorBytes)
	data = append(data, voiceEncodingVersion, voice.Algorithm, voice.LFO, voice.BasicOctave)
	for _, op := range voice.Operators[:count] {
		data = append(data, op.Multiple, op.Detune, op.TotalLevel, op.Attack, op.Decay,
			op.SustainRate, op.Release, op.SustainLevel, op.KeyScaleLevel, flag(op.KeyScaleRate),
			op.Wave, op.Feedback, flag(op.Tremolo), op.TremoloDepth, flag(op.Vibrato), op.VibratoDepth,
			flag(op.IgnoreKeyOff))
	}
	return data
}

// DecodeVoice reads Encode's output, refusing anything out of range: the
// bytes cross into a checkpoint and a Host, so a reader cannot trust them.
func DecodeVoice(data []byte) (Voice, bool) {
	if len(data) < voiceHeaderBytes || data[0] != voiceEncodingVersion || data[1] > 7 || data[2] > 3 || data[3] > 3 {
		return Voice{}, false
	}
	voice := Voice{Algorithm: data[1], LFO: data[2], BasicOctave: data[3]}
	count := voice.OperatorCount()
	if len(data) != voiceHeaderBytes+count*voiceOperatorBytes {
		return Voice{}, false
	}
	limits := [voiceOperatorBytes]uint8{15, 7, 63, 15, 15, 15, 15, 15, 3, 1, 31, 7, 1, 3, 1, 3, 1}
	for index := range count {
		field := data[voiceHeaderBytes+index*voiceOperatorBytes:][:voiceOperatorBytes]
		for position, value := range field {
			if value > limits[position] {
				return Voice{}, false
			}
		}
		voice.Operators[index] = Operator{
			Multiple: field[0], Detune: field[1], TotalLevel: field[2], Attack: field[3], Decay: field[4],
			SustainRate: field[5], Release: field[6], SustainLevel: field[7], KeyScaleLevel: field[8],
			KeyScaleRate: field[9] == 1, Wave: field[10], Feedback: field[11], Tremolo: field[12] == 1,
			TremoloDepth: field[13], Vibrato: field[14] == 1, VibratoDepth: field[15], IgnoreKeyOff: field[16] == 1,
		}
	}
	return voice, true
}

func flag(value bool) uint8 {
	if value {
		return 1
	}
	return 0
}

// voiceKey is what a channel's selection has to equal for a defined voice to
// play. Handset voices name one bank byte, whose top bit marks the drum bank;
// mobile voices name both bank bytes and, for a drum kit, the key.
type voiceKey struct {
	handset          bool
	bankMSB, bankLSB uint8
	program          uint8
	drumNote         uint8
}

// parseVoiceExclusive reads one exclusive message, from its maker ID; a
// leading 0xf0 or a final 0xf7 is ignored. It answers false for anything that
// is not an FM voice definition: device commands, sampled voices, other makers.
func parseVoiceExclusive(message []byte) (voiceKey, Voice, bool) {
	if len(message) > 0 && message[0] == 0xf0 {
		message = message[1:]
	}
	if len(message) > 0 && message[len(message)-1] == 0xf7 {
		message = message[:len(message)-1]
	}
	if len(message) < 2 || message[0] != 0x43 {
		return voiceKey{}, Voice{}, false
	}
	switch {
	case message[1] == 0x03:
		return parseHandsetVoice(message)
	case len(message) >= 10 && message[1] == 0x79 && (message[2] == 0x06 || message[2] == 0x07) && message[3] == 0x7f && message[4] == 0x01:
		return parseMobileVoice(message)
	}
	return voiceKey{}, Voice{}, false
}

// parseHandsetVoice reads `43 03 index bank program global 01` and two
// five-byte operators: multiple, vibrato, envelope type, sustain and key-scale
// rate; release and decay; attack and sustain level; total level and key-scale
// level; vibrato depth, tremolo depth, tremolo and wave.
func parseHandsetVoice(message []byte) (voiceKey, Voice, bool) {
	const operators = 7
	if len(message) < operators+2*5 {
		return voiceKey{}, Voice{}, false
	}
	global := message[5]
	voice := Voice{Algorithm: global & 0x07, LFO: global >> 6, BasicOctave: 1}
	if voice.Algorithm > 1 {
		return voiceKey{}, Voice{}, false
	}
	for index := range 2 {
		field := message[operators+index*5:][:5]
		release := field[1] >> 4
		op := Operator{
			Multiple: field[0] >> 4, Vibrato: field[0]&0x08 != 0, KeyScaleRate: field[0]&0x01 != 0,
			Release: release, Decay: field[1] & 0x0f, Attack: field[2] >> 4, SustainLevel: field[2] & 0x0f,
			TotalLevel: field[3] >> 2, KeyScaleLevel: field[3] & 0x03,
			VibratoDepth: field[4] >> 6, TremoloDepth: (field[4] >> 4) & 0x03, Tremolo: field[4]&0x08 != 0,
			Wave: field[4] & 0x07,
		}
		// A percussive handset envelope keeps falling at the release rate
		// once it reaches the sustain level; a sustained one holds there.
		if field[0]&0x04 == 0 {
			op.SustainRate = release
		}
		if index == 0 {
			op.Feedback = (global >> 3) & 0x07
		}
		voice.Operators[index] = op
	}
	return voiceKey{handset: true, bankMSB: message[3], program: message[4]}, voice, true
}

// parseMobileVoice reads the mobile layout after its five-byte header: bank
// MSB, bank LSB, program, drum key, voice type, then the voice body.
func parseMobileVoice(message []byte) (voiceKey, Voice, bool) {
	key := voiceKey{bankMSB: message[5], bankLSB: message[6], program: message[7], drumNote: message[8]}
	if message[9] != 0 {
		// Sampled and other non-FM voices are not synthesized here.
		return voiceKey{}, Voice{}, false
	}
	body := message[10:]
	if message[2] == 0x06 {
		body = unpackSevenBit(body)
	}
	if len(body) < 3 {
		return voiceKey{}, Voice{}, false
	}
	voice := Voice{Algorithm: body[2] & 0x07, LFO: body[2] >> 6, BasicOctave: body[1] & 0x03}
	count := voice.OperatorCount()
	if len(body) < 3+count*7 {
		return voiceKey{}, Voice{}, false
	}
	for index := range count {
		field := body[3+index*7:][:7]
		op := Operator{
			SustainRate: field[0] >> 4, IgnoreKeyOff: field[0]&0x08 != 0, KeyScaleRate: field[0]&0x01 != 0,
			Release: field[1] >> 4, Decay: field[1] & 0x0f, Attack: field[2] >> 4, SustainLevel: field[2] & 0x0f,
			TotalLevel: field[3] >> 2, KeyScaleLevel: field[3] & 0x03,
			TremoloDepth: (field[4] >> 5) & 0x03, Tremolo: field[4]&0x10 != 0,
			VibratoDepth: (field[4] >> 1) & 0x03, Vibrato: field[4]&0x01 != 0,
			Multiple: field[5] >> 4, Detune: field[5] & 0x07, Wave: field[6] >> 3,
		}
		// Only the operators an algorithm feeds back into carry feedback:
		// the first, and the third in the two layouts that name it.
		if index == 0 || index == 2 && (voice.Algorithm == 2 || voice.Algorithm == 5) {
			op.Feedback = field[6] & 0x07
		}
		voice.Operators[index] = op
	}
	return key, voice, true
}

// unpackSevenBit reverses the Yamaha seven-bit packing: each group of up to
// eight bytes starts with one whose low seven bits are the top bits of the
// bytes after it, most significant first.
func unpackSevenBit(packed []byte) []byte {
	data := make([]byte, 0, len(packed))
	for start := 0; start < len(packed); start += 8 {
		group := packed[start:min(start+8, len(packed))]
		high := group[0]
		for index, value := range group[1:] {
			if high&(0x40>>index) != 0 {
				value |= 0x80
			}
			data = append(data, value)
		}
	}
	return data
}

// voiceDefinition is one voice a file defines, in force from its time on.
type voiceDefinition struct {
	time uint32
	key  voiceKey
	slot uint8
}

// voiceTable is every voice a file defines, in the order later definitions
// override earlier ones: setup chunks first in file order, then inline
// messages by time. A definition takes the next slot, so a voice redefined
// mid-song gets a new slot and notes before it keep the old one.
type voiceTable struct {
	definitions []voiceDefinition
	voices      [][]byte // Encoded voice per slot; index zero is unused.
}

func (table *voiceTable) define(time uint32, message []byte) (uint8, []byte, bool) {
	if table == nil || len(table.voices) > maxVoiceSlots {
		return 0, nil, false
	}
	key, voice, ok := parseVoiceExclusive(message)
	if !ok {
		return 0, nil, false
	}
	if len(table.voices) == 0 {
		table.voices = append(table.voices, nil)
	}
	slot := uint8(len(table.voices))
	encoded := voice.Encode()
	table.voices = append(table.voices, encoded)
	table.definitions = append(table.definitions, voiceDefinition{time: time, key: key, slot: slot})
	return slot, encoded, true
}

// lookup answers the slot of the latest definition of key in force at time.
func (table *voiceTable) lookup(key voiceKey, time uint32) uint8 {
	if table == nil {
		return 0
	}
	var found uint8
	var foundTime uint32
	for _, definition := range table.definitions {
		if definition.key == key && definition.time <= time && (found == 0 || definition.time >= foundTime) {
			found, foundTime = definition.slot, definition.time
		}
	}
	return found
}
