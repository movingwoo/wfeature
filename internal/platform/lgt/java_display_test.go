package lgt

import "testing"

// The display describes the 16-bit LCD it is, in KTF's numbers: 65,536 colours
// at 16 bits a pixel, in colour, double-buffered.
func TestJavaDisplayDescribesASixteenBitScreen(t *testing.T) {
	client := fixtureClient(t)
	display := mustJavaCall(t, client, javaDisplayClass+".getDefaultDisplay()Lorg/kwis/msp/lcdui/Display;")
	for name, want := range map[string]uint32{
		"numColors()I": 1 << 16, "getBitsPerPixel()I": 16, "isColor()Z": javaTrue, "isDoubleBuffered()Z": javaTrue,
	} {
		if got := mustJavaCall(t, client, javaDisplayClass+"."+name, display); got != want {
			t.Errorf("%s = %d, want %d", name, got, want)
		}
	}
}

// The display claims no event this platform does not deliver: a key here is a
// press and a release, with no repeat, and there is no pointer. KTF, which
// delivers both, answers true to all three; a title told it has events it is
// never given is worse off than one told the truth, which is KTF's own rule.
func TestJavaDisplayClaimsNoEventItDoesNotDeliver(t *testing.T) {
	client := fixtureClient(t)
	display := mustJavaCall(t, client, javaDisplayClass+".getDefaultDisplay()Lorg/kwis/msp/lcdui/Display;")
	for _, name := range []string{"hasPointerEvents()Z", "hasPointerMotionEvents()Z", "hasRepeatEvents()Z"} {
		if got := mustJavaCall(t, client, javaDisplayClass+"."+name, display); got != javaFalse {
			t.Errorf("%s = %d, want false", name, got)
		}
	}
}

// A key code's name is the one KTF's Display gives it; a digit, the star and
// the hash name themselves; a code no key has is the empty string.
func TestJavaDisplayNamesKeysAsKTFDoes(t *testing.T) {
	client := fixtureClient(t)
	for code, want := range map[int32]string{
		-1: "UP", -2: "DOWN", -3: "LEFT", -4: "RIGHT", -5: "FIRE", -6: "SOFT1", -7: "SOFT2",
		-10: "SEND", -11: "END", -13: "VOLUP", -14: "VOLDOWN", -16: "CLEAR",
		'0': "0", '7': "7", '*': "*", '#': "#",
		-8: "", -15: "", 'a': "", 1000: "",
	} {
		name := mustJavaCall(t, client, javaDisplayClass+".getKeyName(I)Ljava/lang/String;", uint32(code))
		text, ok := client.javaText(name)
		if !ok {
			t.Fatalf("getKeyName(%d) answered %#x, which is no String", code, name)
		}
		if text != want {
			t.Errorf("getKeyName(%d) = %q, want %q", code, text, want)
		}
	}
}

// A display asked for by name is the one display, whatever the name; flushing
// it is accepted.
func TestJavaDisplayByNameIsTheDefaultDisplay(t *testing.T) {
	client := fixtureClient(t)
	display := mustJavaCall(t, client, javaDisplayClass+".getDefaultDisplay()Lorg/kwis/msp/lcdui/Display;")
	for _, name := range []string{"", "main", "sub"} {
		named := mustJavaCall(t, client,
			javaDisplayClass+".getDisplay(Ljava/lang/String;)Lorg/kwis/msp/lcdui/Display;", newTestString(t, client, name))
		if named != display {
			t.Errorf("getDisplay(%q) = %#x, want the default display %#x", name, named, display)
		}
	}
	mustJavaCall(t, client, javaDisplayClass+".flush()V", display)
}
