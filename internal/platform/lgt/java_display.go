package lgt

import (
	"context"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// What the `Display` reports about itself and about its keys, as a Java title
// asks: how many colours and how deep a pixel, whether it is in colour and
// buffered, which events it has, and the name of a key.
//
// **The screen's own answers are KTF's**, whose LCD is the same 16 bits a
// pixel this one is (rgb565): 65,536 colours at 16 bits a pixel, in colour, and
// double-buffered — a Java title draws its frame off the screen and the frame
// is presented when its paint returns (paintJavaCard). SKT's WIPI Display
// answers 2^24 colours at 24 bits, for a display that is not this one.
//
// **The event answers are this platform's own, by KTF's rule.** KTF's Display
// says of its pointer answer that a title told it has events it is then never
// given is worse off than one told the truth, and it answers true because it
// delivers them. This platform delivers a key as a press and a release and
// nothing more — no repeat and no pointer (lgtEngine in internal/session) — so
// all three event questions answer false here, where KTF answers true. SKT's
// answers false for the two pointer questions and true for the repeat, which
// its keys have.

const (
	javaDisplayColorCount = 1 << 16
	javaDisplayPixelBits  = 16
)

func javaDisplayNumColors(*Client, context.Context, *armcore.Thread, []uint32) (uint32, error) {
	return javaDisplayColorCount, nil
}

func javaDisplayBitsPerPixel(*Client, context.Context, *armcore.Thread, []uint32) (uint32, error) {
	return javaDisplayPixelBits, nil
}

// javaDisplayAnswer is a Display question this platform answers the same way
// every time.
func javaDisplayAnswer(answer bool) func(
	*Client, context.Context, *armcore.Thread, []uint32) (uint32, error) {
	return func(*Client, context.Context, *armcore.Thread, []uint32) (uint32, error) {
		return javaBoolean(answer), nil
	}
}

// javaKeyNames is the name of each key code KTF's Display names. The codes are
// the WIPI ones, which are what the keypad hands this platform too (the
// session gives a title here the codes it gives a KTF one), so a code names
// the same key on both.
var javaKeyNames = map[int32]string{
	-1: "UP", -2: "DOWN", -3: "LEFT", -4: "RIGHT", -5: "FIRE",
	-6: "SOFT1", -7: "SOFT2", -10: "SEND", -11: "END", -13: "VOLUP", -14: "VOLDOWN",
	-16: "CLEAR",
}

// javaKeyName is `Display.getKeyName(key)`: the name of a key code. A digit, the
// star and the hash name themselves; a code no key has answers the empty
// string, which is KTF's answer, where SKT's answers null.
func javaKeyName(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	key := int32(arguments[0])
	name, named := javaKeyNames[key]
	if !named && (key >= '0' && key <= '9' || key == '*' || key == '#') {
		name = string(rune(key))
	}
	return client.newJavaString(name)
}
