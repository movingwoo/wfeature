package ktf

import (
	"encoding/binary"
	"testing"
)

func subscriberFixture(shift int) (*Archive, []byte) {
	image := make([]byte, shift+0xc00)
	put := func(offset int, value uint32) { binary.LittleEndian.PutUint32(image[shift+offset:], value) }
	for index, word := range authenticationSubscriberAccessor {
		binary.LittleEndian.PutUint16(image[shift+0x100+index*2:], word)
	}
	for _, offset := range []int{0x110, 0x13a, 0x140, 0x154, 0x168} {
		delta := int32(0x400 - offset - 4)
		binary.LittleEndian.PutUint16(image[shift+offset:], 0xf000|uint16(delta>>12)&0x7ff)
		binary.LittleEndian.PutUint16(image[shift+offset+2:], 0xf800|uint16(delta>>1)&0x7ff)
	}
	binary.LittleEndian.PutUint16(image[shift+0x400:], 0x4718)
	put(0x170, 0x800-0x10e)
	put(0x17c, 0)
	put(0x184, 4)
	put(0x800, ImageBase+uint32(shift+0x900))
	put(0x804, ImageBase+uint32(shift+0xa00))
	copy(image[shift+0x900:], "PHONENUMBER\x00")
	copy(image[shift+0xa00:], "01024681357\x00")
	return &Archive{Files: map[string][]byte{"P/prefs": make([]byte, 64)}}, image
}

func TestAuthenticationSubscriberUsesTheArchiveFallbackAfterRelocation(t *testing.T) {
	for _, shift := range []int{0, 0x18, 0x1200} {
		archive, image := subscriberFixture(shift)
		if number, ok := authenticationSubscriber(archive, image); !ok || number != "01024681357" {
			t.Fatalf("offset %x: identity = %q, %v", shift, number, ok)
		}
		// The number is data owned by this image, not a bundled handset value.
		copy(image[shift+0xa00:], "01976543210")
		if number, ok := authenticationSubscriber(archive, image); !ok || number != "01976543210" {
			t.Fatalf("archive identity was not read: %q, %v", number, ok)
		}
	}
}

func TestAuthenticationSubscriberRejectsNearMatches(t *testing.T) {
	for index, change := range []func(*Archive, []byte){
		func(a *Archive, b []byte) { a.Files["P/prefs"] = make([]byte, 63) },
		func(a *Archive, b []byte) { a.Files["P/cert.c2s"] = nil },
		func(a *Archive, b []byte) { b[0x900] ^= 1 },
		func(a *Archive, b []byte) { b[0xa00] = 'x' },
		func(a *Archive, b []byte) { b[0xa0b] = '1' },
		func(a *Archive, b []byte) { binary.LittleEndian.PutUint32(b[0x804:], 0xffffffff) },
		func(a *Archive, b []byte) { binary.LittleEndian.PutUint32(b[0x170:], 0x7fffffff) },
		func(a *Archive, b []byte) { b[0x13a] ^= 1 },
		func(a *Archive, b []byte) { b[0x400] ^= 1 },
	} {
		a, image := subscriberFixture(0)
		change(a, image)
		if _, ok := authenticationSubscriber(a, image); ok {
			t.Fatalf("near match %d accepted", index)
		}
	}
	for index, word := range authenticationSubscriberAccessor {
		if word == 0 {
			continue
		}
		a, image := subscriberFixture(0)
		image[0x100+index*2] ^= 1
		if _, ok := authenticationSubscriber(a, image); ok {
			t.Fatalf("changed instruction at %x accepted", 0x100+index*2)
		}
	}
	a, image := subscriberFixture(0)
	for length := range image {
		authenticationSubscriber(a, image[:length])
	}
}
