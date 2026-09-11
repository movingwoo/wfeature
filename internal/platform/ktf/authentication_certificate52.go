package ktf

import (
	"bytes"
	"encoding/binary"

	"github.com/movingwoo/wfeature/internal/wipic"
)

// These instruction fingerprints describe a Thumb certificate reader, not an
// archive identity. Calls and data addresses are resolved from this execution's
// relocated image. In particular, the substitution table belongs to the archive;
// it is neither bundled here nor assumed to be a permutation.
//
// The reader reads 52 bytes, decodes 48, then compares AID[8], MIN[12] and
// application token[16]. Its complete return paths ignore the final four bytes.
// Keep that trailer: the corresponding writer is not a standard CRC32 encoder.
var certificate52Reader = []uint16{
	0xb5f0, 0x4657, 0x4646, 0xb4c0, 0x4b2c, 0x469a, 0x4b2c, 0x44fa,
	0x4453, 0x681e, 0xb09b, 0x4b2b, 0x9200, 0x6832, 0x4453, 0x9101,
	0x4680, 0x2101, 0x6818, 0x6813, 0x2201, 0, 0, // open
	0x1c05, 0x2800, 0xdb40, 0x6833, 0xaf02, 0x685b, 0x1c39, 0x2234,
	0, 0, 0x6833, 0x1c04, 0x68db, 0x1c28, 0, 0, // read, close
	0x2c34, 0xd005, 0x481d, 0xb01b, 0xbc18, 0x4698, 0x46a2, 0xbdf0,
	0x1c38, 0x2130, 0, 0, // decode(buffer, 48)
	0x2400, 0x19e3, 0x4640, 0x5d02, 0x7a9b, 0x429a, 0xd11e,
	0x3401, 0x2c07, 0xddf6, // AID at 10, length 8
	0x1c39, 0x2400, 0x3124, 0x9800, 0x780a, 0x5d03, 0x3101, 0x4293,
	0xd110, 0x3401, 0x2c0b, 0xddf6, // MIN at 36, length 12
	0x2400, 0x9901, 0x19e3, 0x5d0a, 0x7d1b, 0x429a, 0xd104,
	0x3401, 0x2c0f, 0xddf6, // token at 20, length 16
	0x2001, 0xe7d4, 0x4808, 0xe7d2, 0x4808, 0xe7d0,
	0x4808, 0xe7ce, 0x4808, 0xe7cc,
}

var certificate52Decoder = []uint16{
	0xb570, 0x4656, 0xb440, 0x4b0e, 0x2500, 0x469a, 0x44fa,
	0x1c04, 0x1c0e, 0x428d, 0xda0e, 0x4b0b, 0x4453, 0x6818,
	0x7821, 0x2200, 0x5c83, 0x428b, 0xd009, 0x3201, 0x2aff,
	0xddf9, 0x3501, 0x3401, 0x42b5, 0xdbf3, 0xbc08, 0x469a,
	0xbd70, 0x7022, 0xe7f3, // search all 256 entries; last match wins
}

func matchCertificateInstructions(image []byte, offset int, words []uint16) bool {
	if offset < 0 || offset&1 != 0 || offset > len(image)-2*len(words) {
		return false
	}
	for index, word := range words {
		if word != 0 && binary.LittleEndian.Uint16(image[offset+index*2:]) != word {
			return false
		}
	}
	return true
}

func certificateThumbCall(image []byte, offset int) (int, bool) {
	if offset < 0 || offset > len(image)-4 {
		return 0, false
	}
	hi, lo := binary.LittleEndian.Uint16(image[offset:]), binary.LittleEndian.Uint16(image[offset+2:])
	if hi&0xf800 != 0xf000 || lo&0xf800 != 0xf800 {
		return 0, false
	}
	delta := int32(hi&0x7ff)<<12 | int32(lo&0x7ff)<<1
	delta = delta << 9 >> 9
	target := int64(offset) + 4 + int64(delta)
	return int(target), target >= 0 && target < int64(len(image))
}

func certificateImageWord(image []byte, offset int64) (uint32, bool) {
	if offset < 0 || offset > int64(len(image))-4 || offset&3 != 0 {
		return 0, false
	}
	return binary.LittleEndian.Uint32(image[offset:]), true
}

// Resolve the PC-relative static base and its pointer-table entry, allowing
// signed relative offsets but never wrapping an address back into the image.
func certificateImagePointer(image []byte, pc, baseLiteral, entryLiteral int) (int, bool) {
	base, ok := certificateImageWord(image, int64(baseLiteral))
	if !ok {
		return 0, false
	}
	entry, ok := certificateImageWord(image, int64(entryLiteral))
	if !ok {
		return 0, false
	}
	pointer, ok := certificateImageWord(image, int64(pc)+int64(int32(base))+int64(int32(entry)))
	offset := int64(pointer) - int64(ImageBase)
	return int(offset), ok && offset >= 0 && offset < int64(len(image))
}

func authenticationCertificate52(archive *Archive, image []byte, number string) ([]byte, bool) {
	if archive == nil || len(archive.Descriptor.AID) != 8 || wipic.ValidateSubscriberNumber(number) != nil {
		return nil, false
	}
	original := archive.GuestFiles()[certificateName]
	if len(original) != 52 || !bytes.Contains(image, []byte("PHONENUMBER\x00")) {
		return nil, false
	}
	var result []byte
	candidates := 0
	for offset := 0; offset <= len(image)-0xdc; offset += 2 {
		if !matchCertificateInstructions(image, offset, certificate52Reader) {
			continue
		}
		candidates++
		if candidates > 64 {
			return nil, false
		}
		open, ok := certificateThumbCall(image, offset+0x2a)
		read, readOK := certificateThumbCall(image, offset+0x3e)
		close, closeOK := certificateThumbCall(image, offset+0x4a)
		decoder, decodeOK := certificateThumbCall(image, offset+0x62)
		if !ok || !readOK || !closeOK || open != read || open != close || !decodeOK ||
			open > len(image)-2 || binary.LittleEndian.Uint16(image[open:]) != 0x4718 || // bx r3
			!matchCertificateInstructions(image, decoder, certificate52Decoder) {
			continue
		}
		name, ok := certificateImagePointer(image, offset+0x12, offset+0xbc, offset+0xc4)
		if !ok || name > len(image)-9 || !bytes.Equal(image[name:name+9], []byte("cert.c2s\x00")) {
			continue
		}
		tableOffset, ok := certificateImagePointer(image, decoder+0x10, decoder+0x40, decoder+0x44)
		if !ok || tableOffset > len(image)-256 {
			continue
		}
		table := image[tableOffset : tableOffset+256]
		var inverse [256]byte
		for index := range inverse {
			inverse[index] = byte(index)
		}
		for index, value := range table {
			inverse[value] = byte(index)
		}
		var plain [48]byte
		for index := range plain {
			plain[index] = inverse[original[index]]
		}
		if !bytes.Equal(plain[:10], make([]byte, 10)) || string(plain[10:18]) != archive.Descriptor.AID ||
			plain[18] != 0 || plain[19] != 0 || !bytes.Contains(image, plain[20:36]) {
			continue
		}
		oldNumber := bytes.TrimRight(plain[36:], "\x00")
		if len(oldNumber) > 11 || wipic.ValidateSubscriberNumber(string(oldNumber)) != nil {
			continue
		}
		var identity [12]byte
		copy(identity[:], number)
		adapted := bytes.Clone(original)
		for index, value := range identity {
			if inverse[table[value]] != value {
				return nil, false
			}
			adapted[36+index] = table[value]
		}
		if result != nil && !bytes.Equal(result, adapted) {
			return nil, false
		}
		result = adapted
	}
	return result, result != nil
}
