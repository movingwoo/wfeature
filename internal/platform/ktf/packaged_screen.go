package ktf

import "strings"

// The range a Host may name a screen in, mirrored here so that a declaration
// is read by the same bounds a request is: internal/webhost refuses a start
// outside it and the CLI's -screen does the same.
const (
	packagedScreenMinSide = 32
	packagedScreenMaxSide = 1024
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
// 240x320 screen.
// A field that is present and cannot be read — anything but two whole numbers
// either side of `*`, or a side outside what a Host may ask for — answers
// nothing, and the platform's own 240x320 stands.
func PackagedScreen(descriptor Descriptor) (width, height int, ok bool) {
	declared, present := descriptor.Properties["DISPLAYSIZE"]
	if !present {
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

// packagedScreenSide reads one side of a DisplaySize. Only digits are taken,
// so a sign, a space or a unit is a field this platform cannot read rather than
// a number it guessed at, and the length is bounded before the value is built.
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
	if value < packagedScreenMinSide || value > packagedScreenMaxSide {
		return 0, false
	}
	return value, true
}

// sessionScreen is the handset a session starts on: the Host's when it names
// one, and otherwise the one the descriptor declares. Zero for both sides is
// what SetScreen reads as the platform's own.
func sessionScreen(descriptor Descriptor, options SessionOptions) (int, int) {
	if options.Width != 0 || options.Height != 0 {
		return options.Width, options.Height
	}
	width, height, ok := PackagedScreen(descriptor)
	if !ok {
		return 0, 0
	}
	if options.Logger != nil {
		options.Logger.Debug("KTF screen taken from the descriptor", "width", width, "height", height)
	}
	return width, height
}
