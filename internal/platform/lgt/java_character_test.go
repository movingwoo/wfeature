package lgt

import (
	"context"
	"testing"
)

// newTestCharacter is a Character built through the constructor a title links
// against.
func newTestCharacter(t *testing.T, client *Client, unit uint16) uint32 {
	t.Helper()
	object, err := newTestObject(t, client, javaCharacterClass)
	if err != nil {
		t.Fatal(err)
	}
	mustJavaCall(t, client, "java/lang/Character.<init>(C)V", object, uint32(unit))
	return object
}

// A Character answers its code unit, hashes to it, equals another Character
// holding it, and prints as the one character.
func TestJavaCharacterBoxesACodeUnit(t *testing.T) {
	client := fixtureClient(t)
	ctx := context.Background()
	letter, alsoLetter, hangul := newTestCharacter(t, client, 'A'), newTestCharacter(t, client, 'A'), newTestCharacter(t, client, 0xac00)

	if got := mustSlot(t, client, javaCharacterClass, 10, hangul); got != 0xac00 {
		t.Errorf("charValue() = %#x, want 0xac00", got)
	}
	if hash, err := client.javaHashCodeOf(ctx, client.thread, letter); err != nil || hash != 'A' {
		t.Errorf("hashCode() = %d, %v, want 65", hash, err)
	}
	if text, err := client.javaToStringOf(ctx, client.thread, hangul); err != nil || text != "가" {
		t.Errorf("toString() = %q, %v", text, err)
	}
	sixtyFive := newTestInteger(t, client, 'A')
	for _, probe := range []struct {
		name  string
		other uint32
		want  bool
	}{{"another 'A'", alsoLetter, true}, {"a Hangul syllable", hangul, false}, {"an Integer of 65", sixtyFive, false}, {"null", 0, false}} {
		if equal, err := client.javaEqualsOf(ctx, client.thread, letter, probe.other); err != nil || equal != probe.want {
			t.Errorf("'A'.equals(%s) = %v, %v, want %v", probe.name, equal, err, probe.want)
		}
	}
	// The constructor keeps the sixteen bits a char has.
	wide, err := newTestObject(t, client, javaCharacterClass)
	if err != nil {
		t.Fatal(err)
	}
	mustJavaCall(t, client, "java/lang/Character.<init>(C)V", wide, 0x1_0041)
	if got := mustSlot(t, client, javaCharacterClass, 10, wide); got != 'A' {
		t.Errorf("charValue() of a word past sixteen bits = %#x", got)
	}
}

// The static tests are ISO Latin-1's, the same answers the KTF and SKT
// runtimes give, unit for unit.
func TestJavaCharacterTests(t *testing.T) {
	client := fixtureClient(t)
	call := func(name string, arguments ...uint32) uint32 {
		t.Helper()
		return mustJavaCall(t, client, "java/lang/Character."+name, arguments...)
	}
	for _, probe := range []struct {
		unit         uint32
		lower, upper bool
		digit        bool
		toLower      uint32
		toUpper      uint32
	}{
		{'a', true, false, false, 'a', 'A'},
		{'Z', false, true, false, 'z', 'Z'},
		{'7', false, false, true, '7', '7'},
		{0xe9, true, false, false, 0xe9, 0xc9},  // é
		{0xc9, false, true, false, 0xe9, 0xc9},  // É
		{0xdf, false, false, false, 0xdf, 0xdf}, // ß has no capital in Latin-1
		{0xff, false, false, false, 0xff, 0xff}, // nor does ÿ
		{0xf7, false, false, false, 0xf7, 0xf7}, // ÷ is no letter
		{0xd7, false, false, false, 0xd7, 0xd7}, // × either
		{0xac00, false, false, false, 0xac00, 0xac00},
		{0xff11, false, false, false, 0xff11, 0xff11}, // a full-width digit is outside Latin-1
	} {
		if got := call("isLowerCase(C)Z", probe.unit); got != javaBoolean(probe.lower) {
			t.Errorf("isLowerCase(%#x) = %d", probe.unit, got)
		}
		if got := call("isUpperCase(C)Z", probe.unit); got != javaBoolean(probe.upper) {
			t.Errorf("isUpperCase(%#x) = %d", probe.unit, got)
		}
		if got := call("isDigit(C)Z", probe.unit); got != javaBoolean(probe.digit) {
			t.Errorf("isDigit(%#x) = %d", probe.unit, got)
		}
		if got := call("toLowerCase(C)C", probe.unit); got != probe.toLower {
			t.Errorf("toLowerCase(%#x) = %#x, want %#x", probe.unit, got, probe.toLower)
		}
		if got := call("toUpperCase(C)C", probe.unit); got != probe.toUpper {
			t.Errorf("toUpperCase(%#x) = %#x, want %#x", probe.unit, got, probe.toUpper)
		}
	}
	for _, probe := range []struct {
		unit, radix uint32
		want        int32
	}{
		{'7', 10, 7}, {'7', 8, 7}, {'8', 8, -1}, {'f', 16, 15}, {'F', 16, 15}, {'g', 16, -1},
		{'z', 36, 35}, {'Z', 36, 35}, {'1', 1, -1}, {'1', 37, -1}, {'0', uint32(0xffffffff), -1},
		{0xe9, 36, -1}, {0xff11, 10, -1},
	} {
		if got := int32(call("digit(CI)I", probe.unit, probe.radix)); got != probe.want {
			t.Errorf("digit(%#x, %d) = %d, want %d", probe.unit, int32(probe.radix), got, probe.want)
		}
	}
}

// A module that loads the four constants rather than putting them in its own
// code reads them out of the class's storage.
func TestJavaCharacterConstantsAreInTheClassStorage(t *testing.T) {
	client := fixtureClient(t)
	want := []struct {
		name, descriptor string
		value            uint32
	}{{"MAX_VALUE", "C", 0xffff}, {"MIN_RADIX", "I", 2}, {"MAX_RADIX", "I", 36}, {"MIN_VALUE", "C", 0}}
	surface := &javaSurface{Classes: []javaAPIClass{{Name: javaCharacterClass, StaticFields: javaRun{Count: uint32(len(want))}}}}
	for _, field := range want {
		surface.StaticFields = append(surface.StaticFields, javaMemberRef{Name: field.name, Descriptor: field.descriptor})
	}
	layout := newJavaLayout()
	answers, err := layout.layoutPlatformStatics(surface)
	if err != nil {
		t.Fatal(err)
	}
	client.javaLink = &javaLink{surface: surface, layout: layout}
	class, err := client.preparePlatformJavaClass(javaCharacterClass)
	if err != nil {
		t.Fatal(err)
	}
	for index, field := range want {
		address := class.dataBlock + (javaClassDataWords+answers[uint32(index)])*4
		got, err := client.readWord(address)
		if err != nil {
			t.Fatal(err)
		}
		if got != field.value {
			t.Errorf("Character.%s = %#x, want %#x", field.name, got, field.value)
		}
	}
}
