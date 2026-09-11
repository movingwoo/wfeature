package ktf

import (
	"bytes"
	"encoding/binary"

	"github.com/movingwoo/wfeature/internal/wipic"
)

// This accessor reads PHONENUMBER and substitutes its own twelve-byte string
// when the result has at most four characters. The receipt uses that substituted
// identity. Expose the archive's full fallback consistently to every session
// identity API instead of giving unrelated guest callers a truncated number.
var authenticationSubscriberAccessor = []uint16{
	0xb570, 0x4656, 0xb440, 0x4b1a, 0x469a, 0x44fa, 0x2114, 0x1c04,
	0, 0, // reserve string storage
	0x4b17, 0x4453, 0x681b, 0x681b, 0x6822, 0x681b, 0x189b, 0x1c1d,
	0x4b14, 0x4453, 0x681e, 0x4b14, 0x6832, 0x4453, 0x3508,
	0x6818, 0x1c29, 0x6f53, 0x2214, 0, 0, // get property, capacity 20
	0x6820, 0, 0, 0x2804, 0xdd0a, // length <= 4 selects the fallback
	0x4b0d, 0x6832, 0x4453, 0x6818, 0x1c29, 0x6813, 0, 0, // print
	0xbc08, 0x469a, 0xbd70,
	0x4b09, 0x4453, 0x6819, 0x1c28, 0x220c, 0, 0, 0xe7ec, // copy 12
}

func authenticationSubscriber(archive *Archive, image []byte) (string, bool) {
	if archive == nil || len(archive.GuestFiles()["prefs"]) != 64 {
		return "", false
	}
	if _, present := archive.GuestFiles()[certificateName]; present {
		return "", false
	}
	var result string
	candidates := 0
	for offset := 0; offset <= len(image)-0x88; offset += 2 {
		if !matchCertificateInstructions(image, offset, authenticationSubscriberAccessor) {
			continue
		}
		candidates++
		if candidates > 64 {
			return "", false
		}
		valid := true
		for _, call := range []int{0x10, 0x3a, 0x40, 0x54, 0x68} {
			if _, ok := certificateThumbCall(image, offset+call); !ok {
				valid = false
			}
		}
		property, propertyOK := certificateThumbCall(image, offset+0x3a)
		print, printOK := certificateThumbCall(image, offset+0x54)
		if !valid || !propertyOK || !printOK || property != print || property > len(image)-2 ||
			binary.LittleEndian.Uint16(image[property:]) != 0x4718 {
			continue
		}
		name, ok := certificateImagePointer(image, offset+0xe, offset+0x70, offset+0x7c)
		if !ok || name > len(image)-12 || !bytes.Equal(image[name:name+12], []byte("PHONENUMBER\x00")) {
			continue
		}
		fallback, ok := certificateImagePointer(image, offset+0xe, offset+0x70, offset+0x84)
		if !ok || fallback > len(image)-12 || image[fallback+11] != 0 {
			continue
		}
		number := string(image[fallback : fallback+11])
		if wipic.ValidateSubscriberNumber(number) != nil {
			continue
		}
		if result != "" && result != number {
			return "", false
		}
		result = number
	}
	return result, result != ""
}
