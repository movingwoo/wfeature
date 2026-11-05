package lgt

import "testing"

// A font reports the face, style and size the last getFont asked for, as KTF's
// one font object does, and the plain system face of medium size before any
// request. The object is the one font every call answers, so the default font
// and a Graphics' font report the same.
func TestJavaFontReportsTheFaceStyleAndSizeAskedFor(t *testing.T) {
	client := fixtureClient(t)
	font := mustJavaCall(t, client, javaFontClass+".getDefaultFont()Lorg/kwis/msp/lcdui/Font;")
	report := func() map[string]uint32 {
		t.Helper()
		answers := map[string]uint32{}
		for _, name := range []string{"getFace()I", "getStyle()I", "getSize()I",
			"isPlain()Z", "isBold()Z", "isItalic()Z", "isUnderlined()Z"} {
			answers[name] = mustJavaCall(t, client, javaFontClass+"."+name, font)
		}
		return answers
	}
	want := map[string]uint32{"getFace()I": 0, "getStyle()I": 0, "getSize()I": 0,
		"isPlain()Z": javaTrue, "isBold()Z": javaFalse, "isItalic()Z": javaFalse, "isUnderlined()Z": javaFalse}
	for name, answer := range report() {
		if answer != want[name] {
			t.Errorf("before any request %s = %d, want %d", name, answer, want[name])
		}
	}

	// FACE_PROPORTIONAL, STYLE_BOLD | STYLE_UNDERLINED, SIZE_LARGE.
	asked := mustJavaCall(t, client, javaFontClass+".getFont(III)Lorg/kwis/msp/lcdui/Font;", 64, 5, 16)
	if asked != font {
		t.Fatalf("getFont answered %#x, want the one font %#x", asked, font)
	}
	want = map[string]uint32{"getFace()I": 64, "getStyle()I": 5, "getSize()I": 16,
		"isPlain()Z": javaFalse, "isBold()Z": javaTrue, "isItalic()Z": javaFalse, "isUnderlined()Z": javaTrue}
	for name, answer := range report() {
		if answer != want[name] {
			t.Errorf("after getFont(64, 5, 16) %s = %d, want %d", name, answer, want[name])
		}
	}
	// KTF takes any three numbers, without the specification's
	// IllegalArgumentException, and so does this.
	mustJavaCall(t, client, javaFontClass+".getFont(III)Lorg/kwis/msp/lcdui/Font;", 0xffffffff, 2, 3)
	if got := mustJavaCall(t, client, javaFontClass+".getFace()I", font); got != 0xffffffff {
		t.Errorf("getFace() after getFont(-1, 2, 3) = %#x, want -1", got)
	}
	if got := mustJavaCall(t, client, javaFontClass+".isItalic()Z", font); got != javaTrue {
		t.Error("a font asked for in italics answers that it is not italic")
	}
}

// The baseline is the face's ascent: where drawString puts it and what a
// BASELINE anchor measures.
func TestJavaFontBaselineIsTheAscent(t *testing.T) {
	client := fixtureClient(t)
	font := mustJavaCall(t, client, javaFontClass+".getDefaultFont()Lorg/kwis/msp/lcdui/Font;")
	got := mustJavaCall(t, client, javaFontClass+".getBaselinePosition()I", font)
	if want := uint32(textFace().Ascent); got != want || got == 0 {
		t.Errorf("getBaselinePosition() = %d, want the ascent %d", got, want)
	}
	if height := mustJavaCall(t, client, javaFontClass+".getHeight()I", font); got > height {
		t.Errorf("the baseline %d is below the height %d", got, height)
	}
}
