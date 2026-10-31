package ktf

import (
	"strings"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
)

// The handset a descriptor without DisplaySize was packaged for; see
// PackagedScreen.
const (
	earlierDescriptorWidth  = 176
	earlierDescriptorHeight = 220
)

// PackagedScreen answers the handset a descriptor says its title was packaged
// for, which is the screen a session starts on when its Host names none.
//
// **The descriptor says it in DisplaySize**: `176*220` or `240*320`, the two
// handset generations this carrier sold titles for, and one local archive
// declares `240*400`. A title was published once per generation, and the
// local library holds the same title as a 176x220 package beside a 240x320
// one. A 176x220 package started on a 240x320 screen keeps its artwork at
// 176x220: some leave the rest of the screen empty, and others lay their
// background and their text out across the size they were told while their
// pictures stay in a corner. A frame cannot tell the second kind from a title
// that uses the room, because both draw across the whole screen; the
// declaration can. A package that does lay itself out for the screen it is
// given lays itself out on its own handset too, as it did on that phone. See
// docs/history/ktf.md, "The handset a title was packaged for is in its
// descriptor".
//
// **A descriptor without the field is an earlier revision of the format**,
// one that carries no ResizeType either, and every local archive written in it
// is a 176x220 package; two of them keep their pictures in a corner of a
// 240x320 screen. Both fields have to be missing for that reading: every local
// descriptor that declares a DisplaySize carries a ResizeType beside it, so
// one with a ResizeType and no DisplaySize is the current format with the
// field left out — a repacked or hand-made archive — and says nothing about
// its handset. So does a descriptor whose fields ran past what ParseDescriptor
// keeps, because there the field may have been dropped rather than never
// written.
// A field that is present and cannot be read — anything but two whole numbers
// either side of `*`, or a side outside what a Host may ask for — answers
// nothing, and the platform's own 240x320 stands.
func PackagedScreen(descriptor Descriptor) (width, height int, ok bool) {
	declared, present := descriptor.Properties["DISPLAYSIZE"]
	if !present {
		if _, current := descriptor.Properties["RESIZETYPE"]; current || descriptor.Truncated {
			return 0, 0, false
		}
		return earlierDescriptorWidth, earlierDescriptorHeight, true
	}
	widthText, heightText, found := strings.Cut(declared, "*")
	if !found {
		return 0, 0, false
	}
	width, widthOK := packagedScreenSide(widthText)
	height, heightOK := packagedScreenSide(heightText)
	if !widthOK || !heightOK {
		return 0, 0, false
	}
	return width, height, true
}

// AutomaticScreen is the handset a session for this descriptor starts on when
// its Host names none: the one PackagedScreen reads, and otherwise the
// platform's own 240x320. A Host shows it beside a setting whose automatic
// choice sends no size, because only this side can read the archive.
func AutomaticScreen(descriptor Descriptor) (width, height int) {
	width, height, ok := PackagedScreen(descriptor)
	if !ok {
		return wipicrt.RuntimeDisplayPixelWidth, wipicrt.RuntimeDisplayPixelHeight
	}
	return width, height
}

// packagedScreenSide reads one side of a DisplaySize. Only digits are taken,
// so a sign, a space or a unit is a field this platform cannot read rather than
// a number it guessed at, and the length is bounded before the value is built.
// The range is the one a Host's request is held to, so a declaration a Host
// could not have asked for is not adopted either.
func packagedScreenSide(text string) (int, bool) {
	if text == "" || len(text) > 4 {
		return 0, false
	}
	value := 0
	for index := 0; index < len(text); index++ {
		digit := text[index]
		if digit < '0' || digit > '9' {
			return 0, false
		}
		value = value*10 + int(digit-'0')
	}
	if !backend.ValidScreenSide(value) {
		return 0, false
	}
	return value, true
}

// sessionScreen is the handset a session starts on, and whether the
// descriptor chose it: the Host's when it names one, and otherwise the one the
// descriptor declares. Zero for both sides is what SetScreen reads as the
// platform's own.
//
// **A Host names a screen only with two positive sides.** Anything else — one
// side, a negative value — is a request that names no handset, and passing it
// through ran the title on the platform's own while the client kept the
// unusable pair, which every quick save then refused as an invalid screen.
// webhost and the CLI never send one, so this is the descriptor deciding
// rather than a guess at half a handset.
func sessionScreen(descriptor Descriptor, options SessionOptions) (width, height int, declared bool) {
	if options.Width > 0 && options.Height > 0 {
		return options.Width, options.Height, false
	}
	width, height, ok := PackagedScreen(descriptor)
	if !ok {
		return 0, 0, false
	}
	return width, height, true
}
