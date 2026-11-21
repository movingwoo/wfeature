package ktf

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"testing"

	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
)

const saveKeyClient = "9be26a6fab3b44ca3e36c19f219bd73f8bcd3a6a224a437dcaf947ae842c0b17"

// saveKeyImage is a client image holding the recognized code at its addresses
// and nothing else.
func saveKeyImage() []byte {
	image := make([]byte, 0x8000)
	for _, window := range saveKeyCode[saveKeyClient] {
		copy(image[window.at-wipicrt.ImageBase:], window.expect)
	}
	return image
}

func saveKeyContent(length int, salt uint32, slot byte) []byte {
	plain := make([]byte, length)
	for index := range plain {
		plain[index] = byte(index*7 + 3)
	}
	binary.LittleEndian.PutUint32(plain, salt)
	plain[5] = slot
	return plain
}

// Enciphering and deciphering are inverses under one key, the CRC detects
// another key, and the salt the content holds is kept.
func TestSaveKeyCipherRoundTrips(t *testing.T) {
	plain := saveKeyContent(68, 0x5800c0de, 0)
	enciphered := saveKeyEncipher(1000000000, plain)
	if bytes.Equal(enciphered, plain) {
		t.Fatal("nothing was enciphered")
	}
	deciphered, ok := saveKeyDecipher(1000000000, enciphered)
	if !ok || !bytes.Equal(deciphered[:64], plain[:64]) {
		t.Fatalf("round trip: ok=%v", ok)
	}
	if _, ok := saveKeyDecipher(0, enciphered); ok {
		t.Fatal("another key deciphered the save")
	}
	if _, ok := saveKeyDecipher(1000000000, enciphered[:7]); ok {
		t.Fatal("a save shorter than its salt and CRC deciphered")
	}
}

// saveKeyKnownAnswer is an authored 68-byte record (byte i is 7i+3, then the
// salt 0x5800c0de written over the first four) enciphered under the implicit
// handset's key. The title's own decoder, run on the client image, accepts it
// and answers the record; it pins the cipher to the title rather than to its
// inverse.
const saveKeyKnownAnswer = "4145650ea0cd529b11c2ca051874002b957016f0e655a1c486e5bd3aca1528e730006cbd8775f1affcd267dee583f68ff6d67718033b8bb864c68df213f349349fee65b7"

func TestSaveKeyCipherMatchesTheTitle(t *testing.T) {
	plain := make([]byte, 68)
	for index := range plain {
		plain[index] = byte(index*7 + 3)
	}
	binary.LittleEndian.PutUint32(plain, 0x5800c0de)
	known, err := hex.DecodeString(saveKeyKnownAnswer)
	if err != nil {
		t.Fatal(err)
	}
	key := saveKeyFromNumber("01000000000")
	if got := saveKeyEncipher(key, plain); !bytes.Equal(got, known) {
		t.Fatalf("enciphered %x", got)
	}
	deciphered, ok := saveKeyDecipher(key, known)
	if !ok || !bytes.Equal(deciphered[:64], plain[:64]) {
		t.Fatalf("the known answer deciphered to %x (ok=%v)", deciphered, ok)
	}
}

// The title's key is strtol of the number after its first character.
func TestSaveKeyFromNumber(t *testing.T) {
	for number, want := range map[string]uint32{
		"01000000000":  1000000000,
		"01012345678":  1012345678,
		"0":            0,
		"":             0,
		"0x123":        0,
		"00000000000":  0,
		"099999999999": 1<<31 - 1,
		"0-5":          ^uint32(4),
	} {
		if got := saveKeyFromNumber(number); got != want {
			t.Errorf("%q: key %d, want %d", number, got, want)
		}
	}
}

// The capture's options record and slots, enciphered under the capture's key,
// are presented under the session's; anything that does not decipher under
// the capture's key, or names another slot, is presented as packaged.
func TestSaveKeyPresentsTheCaptureUnderTheSessionKey(t *testing.T) {
	options := saveKeyEncipher(saveKeyCapture, saveKeyContent(saveKeyOptions, 1476400000, 0))
	slot := make([]byte, saveKeySlot)
	payload := saveKeyEncipher(saveKeyCapture, saveKeyContent(2000, 1476400100, 1))
	binary.LittleEndian.PutUint32(slot, uint32(len(payload)))
	copy(slot[4:], payload)
	wrongSlot := bytes.Clone(slot) // slot 1's content under the name of slot 2
	foreign := saveKeyEncipher(77, saveKeyContent(saveKeyOptions, 1476400000, 0))
	files := map[string][]byte{
		"save.dat": bytes.Clone(options), "slot1.dat": bytes.Clone(slot), "slot2.dat": wrongSlot,
		"save.bak": bytes.Clone(options), "other.dat": bytes.Clone(foreign),
	}
	const number = "01000000000"
	key := saveKeyFromNumber(number)
	got, err := rekeySaves(files, saveKeyImage(), saveKeyClient, number)
	if err != nil {
		t.Fatal(err)
	}
	if plain, ok := saveKeyDecipher(key, got["save.dat"]); !ok || !bytes.Equal(plain[:64], saveKeyContent(saveKeyOptions, 1476400000, 0)[:64]) {
		t.Fatal("the options record was not presented under the session's key")
	}
	if length := binary.LittleEndian.Uint32(got["slot1.dat"]); length != uint32(len(payload)) {
		t.Fatalf("slot length = %d", length)
	}
	if plain, ok := saveKeyDecipher(key, got["slot1.dat"][4:4+len(payload)]); !ok || plain[5] != 1 {
		t.Fatal("the slot was not presented under the session's key")
	}
	if !bytes.Equal(got["slot1.dat"][4+len(payload):], slot[4+len(payload):]) {
		t.Fatal("the bytes after the slot's content changed")
	}
	for name, want := range map[string][]byte{"slot2.dat": slot, "save.bak": options, "other.dat": foreign} {
		if !bytes.Equal(got[name], want) {
			t.Fatalf("%s was changed", name)
		}
	}
}

func TestSaveKeyRejectsUnexpectedCode(t *testing.T) {
	image := saveKeyImage()
	window := saveKeyCode[saveKeyClient][0]
	image[window.at-wipicrt.ImageBase] ^= 0xff
	if _, err := rekeySaves(map[string][]byte{}, image, saveKeyClient, "01000000000"); err == nil {
		t.Fatal("changed code accepted")
	}
	if _, err := rekeySaves(map[string][]byte{}, image[:0x100], saveKeyClient, "01000000000"); err == nil {
		t.Fatal("a short image was accepted")
	}
	if _, err := rekeySaves(map[string][]byte{}, saveKeyImage(), "unknown", "01000000000"); err == nil {
		t.Fatal("a client without recorded code was accepted")
	}
	if hasSaveSubscriberKey([]byte("unrecognized client")) {
		t.Fatal("unknown revision matched")
	}
}
