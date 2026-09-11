package ktf

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// A generated image with an independently chosen layout, token and cipher.
// Instruction fingerprints are installed as code so the guest decoder can
// check the adapter's output, including a non-bijective table's last match.
func certificate52Fixture(shift int) (*Archive, []byte) {
	image := make([]byte, shift+0x1000)
	put := func(offset int, value uint32) { binary.LittleEndian.PutUint32(image[shift+offset:], value) }
	words := func(offset int, code []uint16) {
		for index, word := range code {
			binary.LittleEndian.PutUint16(image[shift+offset+2*index:], word)
		}
	}
	call := func(offset, target int) {
		delta := int32(target - offset - 4)
		words(offset, []uint16{0xf000 | uint16(delta>>12)&0x7ff, 0xf800 | uint16(delta>>1)&0x7ff})
	}
	words(0x100, certificate52Reader)
	words(0x300, certificate52Decoder)
	words(0x400, []uint16{0x4718})
	for _, offset := range []int{0x12a, 0x13e, 0x14a} {
		call(offset, 0x400)
	}
	call(0x162, 0x300)
	put(0x1bc, 0x800-0x112)
	put(0x1c4, 0)
	put(0x340, 0x800-0x310)
	put(0x344, 4)
	put(0x800, ImageBase+uint32(shift+0x900))
	put(0x804, ImageBase+uint32(shift+0xa00))
	copy(image[shift+0x900:], "cert.c2s\x00PHONENUMBER\x00")
	copy(image[shift+0x940:], "fixture-token-01")
	plain := make([]byte, 52)
	copy(plain[10:], "ABCDEF12")
	copy(plain[20:36], image[shift+0x940:shift+0x950])
	copy(plain[36:], "01012345678")
	copy(plain[48:], []byte{0xde, 0xad, 0xbe, 0xef})
	table := image[shift+0xa00 : shift+0xb00]
	for index := range table {
		table[index] = byte(index*73 + 19)
	}
	// The duplicate affects a byte outside all certificate fields. Requiring a
	// permutation would wrongly refuse this format; a lost digit must fail.
	table[251] = table[250]
	for index := 0; index < 48; index++ {
		plain[index] = table[plain[index]]
	}
	return &Archive{Descriptor: Descriptor{AID: "ABCDEF12"}, Files: map[string][]byte{"P/cert.c2s": plain}}, image
}

func TestAuthenticationCertificate52RelocationAndGuestDecode(t *testing.T) {
	for _, shift := range []int{0, 0x28, 0x1200} {
		for _, number := range []string{"0100", "01000000000", "01098765432"} {
			archive, image := certificate52Fixture(shift)
			original := bytes.Clone(archive.Files["P/cert.c2s"])
			adapted, ok := authenticationCertificate52(archive, image, number)
			if !ok {
				t.Fatalf("offset %x: recognition failed", shift)
			}
			if !bytes.Equal(adapted[:36], original[:36]) || !bytes.Equal(adapted[48:], original[48:]) ||
				!bytes.Equal(archive.Files["P/cert.c2s"], original) {
				t.Fatal("adaptation changed another field or the archive")
			}
			core := armcore.NewCore(armcore.CoreOptions{MaxSteps: 200000})
			if err := core.Memory().Map(ImageBase, uint64(len(image)), armcore.PermissionReadWriteExecute); err != nil {
				t.Fatal(err)
			}
			copy(image[shift+0xc00:], adapted)
			if err := core.Memory().Load(ImageBase, image); err != nil {
				t.Fatal(err)
			}
			initial := armcore.NewContext()
			if err := initial.SetPC(ImageBase + uint32(shift+0x301)); err != nil {
				t.Fatal(err)
			}
			initial.Registers[0], initial.Registers[1] = ImageBase+uint32(shift+0xc00), 48
			initial.Registers[armcore.RegisterSP] = ImageBase + uint32(len(image))
			initial.Registers[armcore.RegisterLR] = ImageBase + uint32(shift+0xe01)
			if _, err := core.Run(context.Background(), armcore.NewThread(initial), ImageBase+uint32(shift+0xe00), nil); err != nil {
				t.Fatal(err)
			}
			plain := make([]byte, 52)
			if err := core.Memory().Read(ImageBase+uint32(shift+0xc00), plain); err != nil {
				t.Fatal(err)
			}
			if string(plain[10:18]) != "ABCDEF12" || string(bytes.TrimRight(plain[36:48], "\x00")) != number ||
				!bytes.Equal(plain[48:], original[48:]) {
				t.Fatalf("guest decoded wrong fields: %x", plain)
			}
		}
	}
}

func TestAuthenticationCertificate52RefusesNearMatches(t *testing.T) {
	for index, change := range []func(*Archive, []byte){
		func(a *Archive, b []byte) { a.Descriptor.AID = "OTHER123" },
		func(a *Archive, b []byte) { a.Files["P/cert.c2s"] = a.Files["P/cert.c2s"][:51] },
		func(a *Archive, b []byte) { a.Files["P/cert.c2s"][10] ^= 1 },
		func(a *Archive, b []byte) { a.Files["P/cert.c2s"][36] = b[0xa00+'x'] },
		func(a *Archive, b []byte) { b[0x940] ^= 1 },
		func(a *Archive, b []byte) { b[0x900] ^= 1 },
		func(a *Archive, b []byte) { b[0x909] ^= 1 },
		func(a *Archive, b []byte) { binary.LittleEndian.PutUint32(b[0x804:], 0xfffffff0) },
		func(a *Archive, b []byte) { binary.LittleEndian.PutUint32(b[0x340:], 0x7fffffff) },
		func(a *Archive, b []byte) { b[0xa00+252] = b[0xa00+'9'] },
		func(a *Archive, b []byte) { b[0x12a] ^= 1 },
		func(a *Archive, b []byte) { b[0x163] = 0 },
		func(a *Archive, b []byte) { b[0x400] ^= 1 },
	} {
		a, image := certificate52Fixture(0)
		change(a, image)
		if _, ok := authenticationCertificate52(a, image, "01098765432"); ok {
			t.Fatalf("near match %d was adapted", index)
		}
	}
	for _, pattern := range []struct {
		start int
		words []uint16
	}{{0x100, certificate52Reader}, {0x300, certificate52Decoder}} {
		for index, word := range pattern.words {
			if word == 0 {
				continue
			}
			a, image := certificate52Fixture(0)
			image[pattern.start+index*2] ^= 1
			if _, ok := authenticationCertificate52(a, image, "01000000000"); ok {
				t.Fatalf("changed instruction at %x was accepted", pattern.start+index*2)
			}
		}
	}
	a, image := certificate52Fixture(0)
	for length := 0; length < len(image); length++ {
		// Bounds are exercised at every possible truncated byte, not just words.
		authenticationCertificate52(a, image[:length], "01000000000")
	}
	for _, number := range []string{"", "012345678901", "010abc", "010\x00"} {
		if _, ok := authenticationCertificate52(a, image, number); ok {
			t.Fatalf("invalid number %q accepted", number)
		}
	}
}
