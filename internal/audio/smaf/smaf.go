// Package smaf parses SMAF (Synthetic music Mobile Application Format, the
// .mmf files Yamaha's MA sound chips played) and translates it into the timed
// MIDI and PCM events a Host can render.
//
// The format nests: a file is a list of tagged chunks, a score track or PCM
// audio track is itself a list of tagged chunks, and the sequence chunk inside
// those is a byte stream of duration-prefixed events in one of three dialects.
// Parsing answers that structure; player.go turns it into something playable.
package smaf

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// ErrNotSMAF reports a file that does not begin with the SMAF magic.
var ErrNotSMAF = errors.New("not a SMAF file")

// FormatType selects the sequence dialect a score track is written in. The
// three are genuinely different encodings, not variations of one.
type FormatType uint8

const (
	// HandyPhoneStandard is the original Japanese handset encoding: notes are
	// packed into a single status byte and durations use a two-byte variable
	// number of their own.
	HandyPhoneStandard FormatType = 0
	// MobileStandardCompress is the MIDI-shaped encoding, Huffman coded.
	MobileStandardCompress FormatType = 1
	// MobileStandardNoCompress is the same encoding stored plainly.
	MobileStandardNoCompress FormatType = 2
)

// ChannelKind is a score track channel's declared role. Rhythm channels are
// routed to the MIDI drum channel rather than allocated a melodic one.
type ChannelKind uint8

const (
	ChannelNoCare ChannelKind = iota
	ChannelMelody
	ChannelNoMelody
	ChannelRhythm
)

// Channels is a wave's channel count.
type Channels uint8

const (
	Mono Channels = iota
	Stereo
)

// StreamWaveFormat is the encoding of wave data attached to a score track.
type StreamWaveFormat uint8

const (
	TwosComplementPCM StreamWaveFormat = iota
	OffsetBinaryPCM
	YamahaADPCM
)

// PCMWaveFormat is the encoding of a PCM audio track's wave data.
type PCMWaveFormat uint8

const (
	PCMTwosComplement PCMWaveFormat = iota
	PCMAdpcm
	PCMTwinVQ
	PCMMP3
)

// BaseBit is a wave's sample width.
type BaseBit uint8

const (
	Bit4 BaseBit = iota
	Bit8
	Bit12
	Bit16
)

// ChunkKind names the top-level chunks the player cares about. Anything else
// is kept as Unknown so that a file round-trips through the parser without
// losing its shape.
type ChunkKind uint8

const (
	ChunkUnknown ChunkKind = iota
	ChunkContentsInfo
	ChunkOptionalData
	ChunkScoreTrack
	ChunkPCMAudioTrack
	ChunkSoftbankSequence
)

// Chunk is one top-level chunk. Only the field matching Kind is set.
//
// The chunks stay in file order because the player's handset-format channel
// allocation accumulates across tracks: the second score track's channels
// continue where the first one's stopped.
type Chunk struct {
	Kind ChunkKind
	Tag  [4]byte

	ScoreTrack       *ScoreTrack
	PCMAudioTrack    *PCMAudioTrack
	SoftbankSequence []SequenceEvent
	Data             []byte
}

// File is a parsed SMAF file.
type File struct {
	Chunks []Chunk
}

// Parse reads a SMAF file. A chunk that does not parse ends the chunk list
// rather than failing the file: these come out of game archives, the tail of a
// file is often padding, and the tracks already read are still playable.
func Parse(data []byte) (*File, error) {
	if len(data) < 8 || string(data[:4]) != "MMMD" {
		return nil, ErrNotSMAF
	}
	// The length field covers the chunks; the trailing two bytes are a CRC.
	body := data[8:]

	file := &File{}
	for len(body) >= 8 {
		tag, payload, rest, ok := splitChunk(body)
		if !ok {
			break
		}
		chunk, err := parseTopChunk(tag, payload)
		if err != nil {
			break
		}
		file.Chunks = append(file.Chunks, chunk)
		body = rest
	}
	if len(file.Chunks) == 0 {
		return nil, fmt.Errorf("SMAF file has no readable chunks")
	}
	return file, nil
}

// splitChunk peels one tag/length/payload header off data.
func splitChunk(data []byte) (tag [4]byte, payload, rest []byte, ok bool) {
	if len(data) < 8 {
		return tag, nil, nil, false
	}
	copy(tag[:], data[:4])
	length := binary.BigEndian.Uint32(data[4:8])
	if uint64(length) > uint64(len(data)-8) {
		return tag, nil, nil, false
	}
	return tag, data[8 : 8+length], data[8+length:], true
}

func parseTopChunk(tag [4]byte, payload []byte) (Chunk, error) {
	chunk := Chunk{Tag: tag, Data: payload}
	switch {
	case string(tag[:]) == "CNTI":
		chunk.Kind = ChunkContentsInfo
	case string(tag[:]) == "OPDA":
		chunk.Kind = ChunkOptionalData
	case string(tag[:3]) == "MTR":
		track, err := parseScoreTrack(payload)
		if err != nil {
			return chunk, err
		}
		chunk.Kind, chunk.ScoreTrack = ChunkScoreTrack, track
	case string(tag[:3]) == "ATR":
		track, err := parsePCMAudioTrack(payload)
		if err != nil {
			return chunk, err
		}
		chunk.Kind, chunk.PCMAudioTrack = ChunkPCMAudioTrack, track
	case string(tag[:]) == "SEQU":
		events, err := parseSequenceHandyLike(payload, true)
		if err != nil {
			return chunk, err
		}
		chunk.Kind, chunk.SoftbankSequence = ChunkSoftbankSequence, events
	}
	return chunk, nil
}

// parseTimebase maps the encoded timebase to milliseconds per duration unit.
func parseTimebase(raw byte) (uint32, error) {
	switch raw {
	case 0:
		return 1, nil
	case 1:
		return 2, nil
	case 2:
		return 4, nil
	case 3:
		return 5, nil
	case 0x10:
		return 10, nil
	case 0x11:
		return 20, nil
	case 0x12:
		return 40, nil
	case 0x13:
		return 50, nil
	}
	return 0, fmt.Errorf("invalid SMAF timebase %#x", raw)
}

// reader is a cursor over a chunk payload. Every read reports whether it had
// the bytes, so a truncated sequence ends the stream instead of panicking.
type reader struct {
	data   []byte
	offset int
}

func (r *reader) remaining() int { return len(r.data) - r.offset }

func (r *reader) byte() (byte, bool) {
	if r.offset >= len(r.data) {
		return 0, false
	}
	value := r.data[r.offset]
	r.offset++
	return value, true
}

func (r *reader) take(count int) ([]byte, bool) {
	if count < 0 || r.remaining() < count {
		return nil, false
	}
	value := r.data[r.offset : r.offset+count]
	r.offset += count
	return value, true
}

// variableNumber reads the standard MIDI-style variable length quantity used
// by the mobile dialects: seven bits per byte, high bit continues.
func (r *reader) variableNumber() (uint32, bool) {
	first, ok := r.byte()
	if !ok {
		return 0, false
	}
	if first&0x80 == 0 {
		return uint32(first & 0x7f), true
	}
	result := uint32(first & 0x7f)
	for {
		next, ok := r.byte()
		if !ok {
			return 0, false
		}
		result = result<<7 | uint32(next&0x7f)
		if next&0x80 == 0 {
			return result, true
		}
	}
}

// handyVariableNumber reads the handset dialect's two-byte quantity, which is
// not the MIDI one: the second byte contributes all eight of its bits and the
// first is biased by one.
func (r *reader) handyVariableNumber() (uint32, bool) {
	first, ok := r.byte()
	if !ok {
		return 0, false
	}
	if first&0x80 == 0 {
		return uint32(first), true
	}
	second, ok := r.byte()
	if !ok {
		return 0, false
	}
	return (uint32(first&0x7f)+1)<<7 | uint32(second), true
}
