package lgt

import (
	"context"
	"testing"

	"golang.org/x/text/encoding/korean"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/glyph"
)

// The bytes a handset draws are EUC-KR. Rendering them as themselves puts a
// codepoint-marked box on the screen for each half of each syllable, which is
// what one title's whole notice screen looked like, so the width slot has to
// measure the decoded text rather than the bytes.
func TestStringWidthDecodesEUCKR(t *testing.T) {
	client := fixtureClient(t)

	text := "확인"
	encoded, err := korean.EUCKR.NewEncoder().Bytes([]byte(text))
	if err != nil {
		t.Fatalf("encode the fixture text: %v", err)
	}
	pointer, err := client.allocateBytes(append(encoded, 0))
	if err != nil {
		t.Fatalf("allocate the string: %v", err)
	}

	got := callSlot(t, client, slotGetStringWidth, 0, pointer, ^uint32(0))
	if want := uint32(textWidth(text)); got != want {
		t.Fatalf("the width of %q is %d, want %d — the bytes were measured undecoded", text, got, want)
	}
	// Undecoded, the four bytes would each render as their own box, which is a
	// different number: the test is only worth having if the two disagree.
	if raw := uint32(textWidth(string(encoded))); raw == got {
		t.Fatalf("decoded and undecoded both measure %d, so this proves nothing", got)
	}
}

// The count is the third argument, and honouring it is what stops a title that
// wraps text: it grows a run one character at a time until the width no longer
// fits, so an answer that ignores the count is the same every time and the loop
// never ends. One title spent its whole instruction budget in one.
func TestStringWidthMeasuresOnlyTheCountItIsGiven(t *testing.T) {
	client := fixtureClient(t)

	text := "확인"
	encoded, err := korean.EUCKR.NewEncoder().Bytes([]byte(text))
	if err != nil {
		t.Fatalf("encode the fixture text: %v", err)
	}
	pointer, err := client.allocateBytes(append(encoded, 0))
	if err != nil {
		t.Fatalf("allocate the string: %v", err)
	}

	whole := callSlot(t, client, slotGetStringWidth, 0, pointer, ^uint32(0))
	half := callSlot(t, client, slotGetStringWidth, 0, pointer, uint32(len(encoded)/2))
	if half >= whole {
		t.Fatalf("half of %q measures %d and the whole of it %d", text, half, whole)
	}
	if want := uint32(textWidth(string([]rune(text)[:1]))); half != want {
		t.Fatalf("the first syllable of %q measures %d, want %d", text, half, want)
	}
	if none := callSlot(t, client, slotGetStringWidth, 0, pointer, 0); none != 0 {
		t.Fatalf("a count of zero measures %d, want 0", none)
	}
}

// A file name is looked up by its bytes on both sides of the platform, so the
// decoding reader must not be the one names go through.
func TestNamesAreReadAsBytesAndTextAsText(t *testing.T) {
	client := fixtureClient(t)

	encoded, err := korean.EUCKR.NewEncoder().Bytes([]byte("한글"))
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	pointer, err := client.allocateBytes(append(encoded, 0))
	if err != nil {
		t.Fatalf("allocate: %v", err)
	}

	name, err := client.readCString(pointer)
	if err != nil {
		t.Fatalf("read the name: %v", err)
	}
	if name != string(encoded) {
		t.Fatalf("readCString answered %q, want the bytes themselves", name)
	}
	text, err := client.readCText(pointer)
	if err != nil {
		t.Fatalf("read the text: %v", err)
	}
	if text != "한글" {
		t.Fatalf("readCText answered %q, want the decoded text", text)
	}
}

// A game lays its own boxes out of what the font slots answer and then draws
// into them, so the three metrics have to be the face the renderer draws with
// rather than numbers of their own.
func TestFontSlotsAnswerTheFaceThatDraws(t *testing.T) {
	client := fixtureClient(t)

	face := textFace()
	if face != glyph.Handset() {
		t.Fatalf("the text face is not the handset one")
	}
	if height := callSlot(t, client, slotGetFontHeight, 0); height != uint32(face.Height()) {
		t.Fatalf("the font height is %d, want the face's %d", height, face.Height())
	}
	if ascent := callSlot(t, client, slotGetFontAscent, 0); ascent != uint32(face.Ascent) {
		t.Fatalf("the font ascent is %d, want the face's %d", ascent, face.Ascent)
	}
	descent := callSlot(t, client, slotGetFontDescent, 0)
	if want := uint32(face.Height() - face.Ascent); descent != want {
		t.Fatalf("the font descent is %d, want %d", descent, want)
	}
}

// A Korean line has to fit the box a handset layout sized for it. This is the
// line that showed it: the last of a title's usage notice, inside the notice
// box a frame diff measured at 183 pixels wide. At the 16-dot face it is 185
// and its final syllable was cut against the right edge; at the handset face it
// has room to spare. See "The text face is the handset's small one" in
// docs/lgt.md.
func TestAKoreanLineFitsTheBoxItsTitleSized(t *testing.T) {
	const line = "정보이용료가 부과 됩니다."
	const box = 183

	if width := textWidth(line); width > box {
		t.Fatalf("%q measures %d in a %d-pixel box", line, width, box)
	}
	if wide := largeFaceWidth(line); wide <= box {
		t.Fatalf("the 16-dot face measures %d, which fits: this test would pass either way", wide)
	}
}

func largeFaceWidth(value string) int {
	total := 0
	for _, symbol := range value {
		total += glyph.Default().Render(symbol).Advance
	}
	return total
}

// `freeMemory` is declared `()J`, so the high word belongs in r1. Writing only
// r0 leaves whatever the caller had there as the top half of the number, and a
// title comparing the answer against a bound compares against that.
func TestFreeMemoryWritesTheHighWordOfItsLong(t *testing.T) {
	client := fixtureClient(t)

	thread := armcore.NewThread(armcore.NewContext())
	if err := thread.SetRegister(1, 0xdeadbeef); err != nil {
		t.Fatalf("dirty r1: %v", err)
	}
	if _, err := javaFreeMemory(client, context.Background(), thread, []uint32{0}); err != nil {
		t.Fatalf("free memory: %v", err)
	}
	high, err := thread.Register(1)
	if err != nil {
		t.Fatalf("read r1: %v", err)
	}
	if high != 0 {
		t.Fatalf("the high word is %#x, want it written as zero", high)
	}
}

// A Java title makes almost no C calls, so a trace that cannot name the Java
// slots names nothing at all. The interface-table slots are named from the same
// table a failure reports through, rather than from a second one beside it.
func TestJavaInterfaceSlotsAreNamedInTheTrace(t *testing.T) {
	client := fixtureClient(t)

	for slot, want := range map[uint32]string{
		javaSVCAllocate:    javaSVCNames[javaSVCAllocate],
		javaSVCMonitorExit: javaSVCNames[javaSVCMonitorExit],
		javaSVCThrow:       javaSVCNames[javaSVCThrow],
	} {
		if want == "" {
			t.Fatalf("slot %#x has no name to check against", slot)
		}
		if got := client.svcSlotName(svcCategoryJava, slot); got != want {
			t.Fatalf("slot %#x named %q, want %q", slot, got, want)
		}
	}
	// A slot with no name is still a finding rather than a formatting gap: it
	// reads as "unnamed", which marks a slot answered without being understood.
	if got := client.svcSlotName(svcCategoryJava, 0x7fff); got != "" {
		t.Fatalf("an unknown slot named %q, want it left unnamed", got)
	}
}
