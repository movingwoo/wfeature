package ktf

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"

	"github.com/movingwoo/wfeature/internal/platform/compatibility"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
)

// A recognized client enciphers every save under a key it derives from
// PHONENUMBER: strtol of the number after its first character. The first four
// bytes of a save are a salt (the time it was written), the last four a CRC of
// the rest, everything after the salt is shuffled under the salt, and the
// whole is combined with a stream generated from the key. A save that does not
// decipher to a matching CRC is no save: the options record is rewritten with
// defaults and a slot is offered empty.
//
// One capture's saves were all enciphered under key 0, which is what a handset
// answering a number without digits after its first character gives. The
// implicit handset here gives another key, so the capture's options were
// replaced and its three saved games were never offered. ktf.save_subscriber_key
// presents each of those files to the guest enciphered under the session's
// key instead, with the same salt and the same content, so the title's own
// decoder accepts them as if the capture had been taken on this handset. A file
// that does not decipher under key 0 is presented as packaged, the archive is
// not changed, and a file the title writes is its own.
// See docs/ktf.md#save-subscriber-key.

// saveKeyCode is, by the digest of the client image as the JAR packages it,
// the code that does what this file reimplements: its decoder, the encoder's
// key and salt, and the two loaders that call the decoder. A registered client
// whose code differs fails starting.
var saveKeyCode = map[string][]codeRewrite{
	"9be26a6fab3b44ca3e36c19f219bd73f8bcd3a6a224a437dcaf947ae842c0b17": {
		{at: 0x104070, expect: []byte{
			0x70, 0xb5, 0x85, 0xb0, 0x01, 0x30, 0x0d, 0x1c, 0x14, 0x1c, 0x68, 0xf0, 0xb9, 0xfa, 0x02, 0x14,
			0x12, 0x06, 0x12, 0x0e, 0x00, 0x92, 0x02, 0x12, 0x12, 0x06, 0x12, 0x0e, 0x03, 0x06, 0x01, 0x92,
			0x02, 0x0e, 0x1b, 0x0e, 0x02, 0x92, 0x29, 0x1c, 0x22, 0x1c, 0xff, 0xf7, 0x49, 0xff, 0x26, 0x1f,
			0x29, 0x1c, 0x04, 0x22, 0x04, 0xa8, 0x2c, 0x19, 0x67, 0xf0, 0x96, 0xff, 0x04, 0x3c, 0x00, 0x23,
			0x28, 0x1d, 0x31, 0x1c, 0x04, 0x9a, 0xff, 0xf7, 0x05, 0xff, 0x21, 0x1c, 0x04, 0x22, 0x03, 0xa8,
			0x67, 0xf0, 0x8a, 0xff, 0x28, 0x1c, 0x31, 0x1c, 0xff, 0xf7, 0xc8, 0xfe, 0x03, 0x9b, 0x83, 0x42,
			0x02, 0xd0, 0x00, 0x20, 0x05, 0xb0, 0x70, 0xbd, 0x01, 0x20, 0xfb, 0xe7,
		}},
		// The multiplier and increment of the stream, the shuffle constant and
		// the CRC polynomial.
		{at: 0x103fc4, expect: []byte{0x6d, 0x4e, 0xc6, 0x41, 0x87, 0xd6, 0x12, 0x00}},
		{at: 0x103f2c, expect: []byte{0x31, 0xd4, 0x00, 0x00}},
		{at: 0x103e54, expect: []byte{0x20, 0x83, 0xb8, 0xed}},
		{at: 0x103fdc, expect: []byte{
			0x01, 0x30, 0x0d, 0x1c, 0x90, 0x46, 0x68, 0xf0, 0x05, 0xfb, 0x1f, 0x4b, 0x53, 0x44, 0x1b, 0x68,
			0x1b, 0x68, 0x04, 0x1c, 0x18, 0x6f, 0x6d, 0xf0, 0x95, 0xf8, 0x1c, 0x4a, 0x1c, 0x4b, 0x68, 0xf0,
		}},
		// The options loader deciphers 68 bytes; the slot loader deciphers the
		// length its file begins with and requires the slot's number at 5.
		{at: 0x107d22, expect: []byte{
			0x09, 0x49, 0x44, 0x22, 0x70, 0x18, 0x21, 0x1c, 0xfc, 0xf7, 0xa1, 0xf9, 0x00, 0x06, 0x04, 0x0e,
		}},
		{at: 0x107e60, expect: []byte{
			0x11, 0x48, 0x52, 0x46, 0x13, 0x18, 0x18, 0x68, 0x10, 0x4b, 0x29, 0x1c, 0xc0, 0x18, 0x00, 0x9a,
			0xfc, 0xf7, 0xfe, 0xf8, 0x00, 0x06, 0x00, 0x28, 0x02, 0xd0, 0x6b, 0x79, 0x98, 0x45, 0x03, 0xd0,
			0x38, 0x1c, 0x5f, 0xf0, 0xff, 0xfe, 0xc9, 0xe7, 0x00, 0x98,
		}},
	},
}

const (
	saveKeyCapture    = 0
	saveKeyShuffle    = 0xd431
	saveKeyMultiplier = 0x41c64e6d
	saveKeyIncrement  = 0x12d687
	saveKeyOptions    = 68
	saveKeySlot       = 10000
	saveKeySlots      = 3
)

// saveKeyTable is the title's CRC table. It is built with arithmetic shifts of
// a signed word, which is not the usual table, and the title's CRC routine
// shifts the same way.
var saveKeyTable = func() (table [256]uint32) {
	for index := range table {
		value := int32(index)
		for range 8 {
			if value&1 != 0 {
				value = value>>1 ^ int32(-306674912) // 0xedb88320
			} else {
				value >>= 1
			}
		}
		table[index] = uint32(value)
	}
	return table
}()

func saveKeyCRC(data []byte) uint32 {
	crc := int32(-1)
	for _, b := range data {
		crc = int32(saveKeyTable[(uint32(crc)^uint32(b))&0xff]) ^ crc>>8
	}
	return ^uint32(crc)
}

// saveKeyStream combines data with the stream the key generates: four bytes
// of state from the key, their sum per byte, and four steps of the generator
// to the next state.
func saveKeyStream(key uint32, data []byte) {
	state := key
	a, b, c, d := key&0xff, key>>16&0xff, key>>8&0xff, key>>24
	next := func(with uint32) uint32 {
		state = state*saveKeyMultiplier + saveKeyIncrement
		return (with ^ uint32(int32(state)>>16)) & 0xff
	}
	for index := range data {
		data[index] ^= byte(a + b + c + d)
		na := next(b)
		nb := next(c)
		nc := next(d)
		nd := next(a)
		a, b, c, d = na, nb, nc, nd
	}
}

// saveKeyDecipher answers the content of an enciphered save and whether its
// CRC matches.
func saveKeyDecipher(key uint32, enciphered []byte) ([]byte, bool) {
	if len(enciphered) < 8 {
		return nil, false
	}
	plain := bytes.Clone(enciphered)
	saveKeyStream(key, plain)
	salt := binary.LittleEndian.Uint32(plain)
	body := plain[4:]
	count := uint32(len(body))
	for index := range count {
		salt ^= index + saveKeyShuffle
	}
	for index := int(count) - 1; index >= 0; index-- {
		other := salt % count
		body[index], body[other] = body[other], body[index]
		salt ^= uint32(index) + saveKeyShuffle
	}
	return plain, binary.LittleEndian.Uint32(plain[len(plain)-4:]) == saveKeyCRC(plain[:len(plain)-4])
}

// saveKeyEncipher is the title's encoder with the salt the content already
// holds.
func saveKeyEncipher(key uint32, plain []byte) []byte {
	enciphered := bytes.Clone(plain)
	binary.LittleEndian.PutUint32(enciphered[len(enciphered)-4:], saveKeyCRC(enciphered[:len(enciphered)-4]))
	salt := binary.LittleEndian.Uint32(enciphered)
	body := enciphered[4:]
	count := uint32(len(body))
	for index := range count {
		salt ^= index + saveKeyShuffle
		other := salt % count
		body[index], body[other] = body[other], body[index]
	}
	saveKeyStream(key, enciphered)
	return enciphered
}

// saveKeyFromNumber is the title's key for a number: strtol of what follows
// its first character, in base 10, saturated to a signed word.
func saveKeyFromNumber(number string) uint32 {
	if len(number) < 2 {
		return 0
	}
	text := strings.TrimLeft(number[1:], " \t\n\v\f\r")
	negative := false
	if text != "" && (text[0] == '+' || text[0] == '-') {
		negative = text[0] == '-'
		text = text[1:]
	}
	var value int64
	for _, digit := range text {
		if digit < '0' || digit > '9' {
			break
		}
		if value <= 1<<31 {
			value = value*10 + int64(digit-'0')
		}
	}
	if negative {
		value = -value
	}
	value = max(min(value, 1<<31-1), -1<<31)
	return uint32(int32(value))
}

func hasSaveSubscriberKey(image []byte) bool {
	return compatibility.HasFix("ktf", compatibility.NativeModule, clientDigest(image), compatibility.KTFSaveSubscriberKey)
}

// rekeySaves answers files with each of the capture's saves that deciphers
// under the capture's key enciphered under the key number gives instead, after
// checking image, the client the registry selected by digest, for its code.
func rekeySaves(files map[string][]byte, image []byte, digest, number string) (map[string][]byte, error) {
	windows := saveKeyCode[digest]
	if len(windows) == 0 {
		return nil, fmt.Errorf("KTF save subscriber key compatibility: no code is known for client %s", digest)
	}
	for _, window := range windows {
		start := uint64(window.at) - uint64(wipicrt.ImageBase)
		if uint64(window.at) < uint64(wipicrt.ImageBase) || start+uint64(len(window.expect)) > uint64(len(image)) ||
			!bytes.Equal(image[start:start+uint64(len(window.expect))], window.expect) {
			return nil, fmt.Errorf("KTF save subscriber key compatibility: unexpected code at %#x", window.at)
		}
	}
	key := saveKeyFromNumber(number)
	if key == saveKeyCapture {
		return files, nil
	}
	for name, data := range files {
		if strings.EqualFold(name, "save.dat") && len(data) == saveKeyOptions {
			if plain, ok := saveKeyDecipher(saveKeyCapture, data); ok {
				files[name] = saveKeyEncipher(key, plain)
			}
			continue
		}
		slot, ok := saveKeySlotNumber(name)
		if !ok || len(data) != saveKeySlot {
			continue
		}
		length := binary.LittleEndian.Uint32(data)
		if length < 8 || length > saveKeySlot-4 {
			continue
		}
		plain, ok := saveKeyDecipher(saveKeyCapture, data[4:4+length])
		if !ok || plain[5] != byte(slot) {
			continue
		}
		rekeyed := bytes.Clone(data)
		copy(rekeyed[4:], saveKeyEncipher(key, plain))
		files[name] = rekeyed
	}
	return files, nil
}

// saveKeySlotNumber answers the slot a name such as slot1.dat names.
func saveKeySlotNumber(name string) (int, bool) {
	lower := strings.ToLower(name)
	if len(lower) != len("slot0.dat") || !strings.HasPrefix(lower, "slot") || !strings.HasSuffix(lower, ".dat") {
		return 0, false
	}
	slot := int(lower[4] - '0')
	return slot, slot >= 0 && slot < saveKeySlots
}
