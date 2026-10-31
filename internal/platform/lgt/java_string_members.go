package lgt

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
)

// The rest of `java/lang/String` and `java/lang/StringBuffer` that is not about
// floating-point numbers. Every slot is the declaration-order rule's
// (javaBakedVirtualSlots): String's own methods from 10, StringBuffer's from 10,
// and the three of Object's both override — hashCode, equals, toString — at
// Object's 2, 3 and 4. The specification's class pages give the order; the
// anchors the call sites settled sit on it without exception.
//
// Case is ISO Latin-1's alone, which is all CLDC's Character maps: a Korean
// title's own text has no case, and ASCII and Latin-1 letters change the way
// the class library changes them.

// javaStringIndexClass and javaIndexClass are the two index failures the class
// library throws from text: String's own, and the general one StringBuffer's
// charAt and setCharAt name.
const (
	javaStringIndexClass        = "java/lang/StringIndexOutOfBoundsException"
	javaIndexClass              = "java/lang/IndexOutOfBoundsException"
	javaUnsupportedEncodingName = "java/io/UnsupportedEncodingException"
	javaNegativeArraySizeClass  = "java/lang/NegativeArraySizeException"
)

// javaCharset is one of the encodings a title may name: UTF-8, which the
// standard requires everywhere, and the handset's own. The names are the ones
// the KTF and SKT runtimes accept (internal/jvm), so a title names the same
// encodings on all three; anything else is UnsupportedEncodingException.
type javaCharset int

const (
	javaCharsetUnknown javaCharset = iota
	javaCharsetUTF8
	javaCharsetPlatform
)

func javaCharsetOf(name string) javaCharset {
	normalized := strings.ToUpper(name)
	for _, cut := range []string{"-", "_", " "} {
		normalized = strings.ReplaceAll(normalized, cut, "")
	}
	switch normalized {
	case "UTF8":
		return javaCharsetUTF8
	case "EUCKR", "KSC5601", "KSC56011987", "KSC56011989", "MS949", "CP949", "XWINDOWS949", "WINDOWS949":
		return javaCharsetPlatform
	}
	return javaCharsetUnknown
}

func (charset javaCharset) decode(data []byte) string {
	if charset == javaCharsetUTF8 {
		return strings.ToValidUTF8(string(data), "�")
	}
	return backend.DecodeEUCKR(data)
}

func (charset javaCharset) encode(text string) []byte {
	if charset == javaCharsetUTF8 {
		// An isolated surrogate half is kept as its own three bytes on this
		// side; UTF-8 has no form for one and the class library writes '?'.
		return []byte(strings.ToValidUTF8(text, "?"))
	}
	return backend.EncodeEUCKR(text)
}

// javaCharsetNamed reads the encoding argument a String method takes, throwing
// what the language throws for null and for a name it does not know.
func (client *Client) javaCharsetNamed(thread *armcore.Thread, object uint32) (javaCharset, error) {
	if object == 0 {
		return 0, client.throwJavaPlatform(thread, javaThrowNullClass, ": a null encoding name")
	}
	name, ok := client.javaText(object)
	if !ok {
		return 0, fmt.Errorf("the encoding name at %#x holds no text", object)
	}
	charset := javaCharsetOf(name)
	if charset == javaCharsetUnknown {
		return 0, client.throwJavaPlatform(thread, javaUnsupportedEncodingName, ": "+name)
	}
	return charset, nil
}

// javaHeldText is the text of a String or StringBuffer receiver.
func (client *Client) javaHeldText(object uint32) (string, error) {
	held, ok := client.javaText(object)
	if !ok {
		return "", fmt.Errorf("the object at %#x holds no text", object)
	}
	return held, nil
}

// javaTextArgument is a String argument a method dereferences: null is the
// NullPointerException the call throws on it.
func (client *Client) javaTextArgument(thread *armcore.Thread, object uint32, what string) (string, error) {
	if object == 0 {
		return "", client.throwJavaPlatform(thread, javaThrowNullClass, ": "+what+" of null")
	}
	return client.javaHeldText(object)
}

// javaUpperUnit and javaLowerUnit are Character.toUpperCase and toLowerCase as
// CLDC defines them: ISO Latin-1 letters only, and every other unit itself.
// ß and ÿ have no capital inside Latin-1 and stay as they are.
func javaUpperUnit(unit uint16) uint16 {
	switch {
	case unit >= 'a' && unit <= 'z':
		return unit - 0x20
	case unit >= 0xe0 && unit <= 0xfe && unit != 0xf7:
		return unit - 0x20
	}
	return unit
}

func javaLowerUnit(unit uint16) uint16 {
	switch {
	case unit >= 'A' && unit <= 'Z':
		return unit + 0x20
	case unit >= 0xc0 && unit <= 0xde && unit != 0xd7:
		return unit + 0x20
	}
	return unit
}

// javaUnitCount is how many UTF-16 code units a text is, counted without
// building them.
func javaUnitCount(text string) int {
	count := 0
	for len(text) > 0 {
		if len(text) >= 3 && text[0] == 0xed && text[1] >= 0xa0 && text[1] <= 0xbf && text[2]&0xc0 == 0x80 {
			count++
			text = text[3:]
			continue
		}
		symbol, size := utf8.DecodeRuneInString(text)
		text = text[size:]
		count++
		if symbol > 0xffff {
			count++
		}
	}
	return count
}

// javaStringHashCode is `String.hashCode()`, slot 2: `s[0]*31^(n-1) + ... +
// s[n-1]` over the UTF-16 code units, in 32-bit arithmetic. It is the number
// the class library defines, so a title that files Strings in a Hashtable
// enumerates them in the order a handset does.
func javaStringHashCode(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	held, err := client.javaHeldText(arguments[0])
	if err != nil {
		return 0, err
	}
	var hash int32
	for _, unit := range utf16Units(held) {
		hash = 31*hash + int32(unit)
	}
	return uint32(hash), nil
}

// javaStringToString is `String.toString()`, slot 4: the String itself.
func javaStringToString(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	if _, err := client.javaHeldText(arguments[0]); err != nil {
		return 0, err
	}
	return arguments[0], nil
}

// javaTextGetChars is `getChars(int srcBegin, int srcEnd, char[] dst, int
// dstBegin)`, String's slot 12 and StringBuffer's 15: the units between two
// indexes, copied into an array the title holds. The source bounds are the
// text's own failure and the destination's the array copy's, as the class
// library throws them.
func javaTextGetChars(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	held, err := client.javaHeldText(arguments[0])
	if err != nil {
		return 0, err
	}
	units := utf16Units(held)
	begin, end, at := int64(int32(arguments[1])), int64(int32(arguments[2])), int64(int32(arguments[4]))
	if begin < 0 || end > int64(len(units)) || begin > end {
		return 0, client.throwJavaPlatform(thread, javaStringIndexClass,
			fmt.Sprintf(": getChars(%d, %d) of %d", begin, end, len(units)))
	}
	if arguments[3] == 0 {
		return 0, client.throwJavaPlatform(thread, javaThrowNullClass, ": getChars into null")
	}
	if err := client.writeJavaChars(thread, arguments[3], at, units[begin:end]); err != nil {
		return 0, err
	}
	return 0, nil
}

// writeJavaChars writes code units into a `char[]` from an index, refusing a
// range past either end the way an array copy does.
func (client *Client) writeJavaChars(thread *armcore.Thread, array uint32, at int64, units []uint16) error {
	view, err := client.javaArrayElements(array)
	if err != nil {
		return err
	}
	if view.Width != 2 || view.Reference {
		return fmt.Errorf("characters copied into an array of %d-byte elements", view.Width)
	}
	if at < 0 || at+int64(len(units)) > int64(view.Length) {
		return client.throwJavaPlatform(thread, javaThrowArrayClass,
			fmt.Sprintf(": %d characters at %d of an array of %d", len(units), at, view.Length))
	}
	if len(units) == 0 {
		return nil
	}
	data := make([]byte, 2*len(units))
	for index, unit := range units {
		data[2*index], data[2*index+1] = byte(unit), byte(unit>>8)
	}
	return client.core.Memory().Write(view.Block+javaArrayLengthWords*4+uint32(at)*2, data)
}

// readJavaCharRange reads `count` units of a `char[]` from `offset`, the
// argument pair of every method that takes part of an array. A null array is
// NullPointerException and a range outside it the array index failure.
func (client *Client) readJavaCharRange(
	thread *armcore.Thread, array, offset, count uint32,
) ([]uint16, error) {
	if array == 0 {
		return nil, client.throwJavaPlatform(thread, javaThrowNullClass, ": characters from null")
	}
	units, err := client.readJavaArrayChars(array)
	if err != nil {
		return nil, err
	}
	from, length := int64(int32(offset)), int64(int32(count))
	if from < 0 || length < 0 || from+length > int64(len(units)) {
		return nil, client.throwJavaPlatform(thread, javaThrowArrayClass,
			fmt.Sprintf(": %d characters from %d of an array of %d", length, from, len(units)))
	}
	return units[from : from+length], nil
}

// javaStringBytesIn is `getBytes(String enc)`, slot 13: the text in a named
// encoding.
func javaStringBytesIn(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	held, err := client.javaHeldText(arguments[0])
	if err != nil {
		return 0, err
	}
	charset, err := client.javaCharsetNamed(thread, arguments[1])
	if err != nil {
		return 0, err
	}
	return client.newJavaByteArray(charset.encode(held))
}

// javaStringEqualsIgnoreCase is `equalsIgnoreCase(String)`, slot 15. Null is
// not equal; otherwise the two are the same length and every pair of units is
// equal once both are upper case, or once both are lower case again — the
// class library's own test, which makes the comparison symmetric.
func javaStringEqualsIgnoreCase(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	held, err := client.javaHeldText(arguments[0])
	if err != nil {
		return 0, err
	}
	if arguments[1] == 0 {
		return javaFalse, nil
	}
	other, err := client.javaHeldText(arguments[1])
	if err != nil {
		return 0, err
	}
	left, right := utf16Units(held), utf16Units(other)
	return javaBoolean(len(left) == len(right) && javaUnitsMatch(left, right, true)), nil
}

// javaUnitsMatch compares two runs of units of the same length, with or
// without case.
func javaUnitsMatch(left, right []uint16, ignoreCase bool) bool {
	for index := range left {
		first, second := left[index], right[index]
		if first == second {
			continue
		}
		if !ignoreCase {
			return false
		}
		upperFirst, upperSecond := javaUpperUnit(first), javaUpperUnit(second)
		if upperFirst == upperSecond || javaLowerUnit(upperFirst) == javaLowerUnit(upperSecond) {
			continue
		}
		return false
	}
	return true
}

// javaStringRegionMatches is `regionMatches(boolean ignoreCase, int toffset,
// String other, int ooffset, int len)`, slot 17: whether two runs match. A run
// that does not fit inside its string is false rather than a failure, and a
// length of zero or less matches.
func javaStringRegionMatches(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	held, err := client.javaHeldText(arguments[0])
	if err != nil {
		return 0, err
	}
	other, err := client.javaTextArgument(thread, arguments[3], "regionMatches")
	if err != nil {
		return 0, err
	}
	left, right := utf16Units(held), utf16Units(other)
	from, otherFrom, length := int64(int32(arguments[2])), int64(int32(arguments[4])), int64(int32(arguments[5]))
	if from < 0 || otherFrom < 0 || from > int64(len(left))-length || otherFrom > int64(len(right))-length {
		return javaFalse, nil
	}
	if length <= 0 {
		return javaTrue, nil
	}
	return javaBoolean(javaUnitsMatch(left[from:from+length], right[otherFrom:otherFrom+length], arguments[1] != 0)), nil
}

// javaStringStartsWithAt is `startsWith(String prefix, int toffset)`, slot 18:
// the prefix at an index rather than at the start. An index outside the string
// is false.
func javaStringStartsWithAt(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	held, err := client.javaHeldText(arguments[0])
	if err != nil {
		return 0, err
	}
	prefix, err := client.javaTextArgument(thread, arguments[1], "startsWith")
	if err != nil {
		return 0, err
	}
	units, wanted := utf16Units(held), utf16Units(prefix)
	at := int64(int32(arguments[2]))
	if at < 0 || at > int64(len(units))-int64(len(wanted)) {
		return javaFalse, nil
	}
	return javaBoolean(javaUnitsMatch(units[at:at+int64(len(wanted))], wanted, false)), nil
}

// javaStringEndsWith is `endsWith(String)`, slot 20.
func javaStringEndsWith(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	held, err := client.javaHeldText(arguments[0])
	if err != nil {
		return 0, err
	}
	suffix, err := client.javaTextArgument(thread, arguments[1], "endsWith")
	if err != nil {
		return 0, err
	}
	return javaBoolean(strings.HasSuffix(held, suffix)), nil
}

// javaStringLastIndexOfChar is `lastIndexOf(int)` at slot 23 and
// `lastIndexOf(int, int)` at 24: the last place a unit appears at or before an
// index, or -1. A start past the end searches the whole string, and a negative
// one finds nothing.
func javaStringLastIndexOfChar(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	held, err := client.javaHeldText(arguments[0])
	if err != nil {
		return 0, err
	}
	units := utf16Units(held)
	from := int64(len(units)) - 1
	if len(arguments) > 2 {
		if start := int64(int32(arguments[2])); start < from {
			from = start
		}
	}
	wanted := uint16(arguments[1])
	for index := from; index >= 0; index-- {
		if units[index] == wanted {
			return uint32(int32(index)), nil
		}
	}
	return 0xffffffff, nil
}

// javaStringIndexOfText is `indexOf(String)`, slot 25: the two-argument form
// from the start.
func javaStringIndexOfText(
	client *Client, ctx context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	if arguments[1] == 0 {
		return 0, client.throwJavaPlatform(thread, javaThrowNullClass, ": indexOf(null)")
	}
	return javaStringIndexOfTextFrom(client, ctx, thread, []uint32{arguments[0], arguments[1], 0})
}

// javaStringReplace is `replace(char, char)`, slot 30. A string with nothing
// to replace is answered as itself, as the class library answers it.
func javaStringReplace(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	held, err := client.javaHeldText(arguments[0])
	if err != nil {
		return 0, err
	}
	old, replacement := uint16(arguments[1]), uint16(arguments[2])
	units := utf16Units(held)
	changed := false
	for index, unit := range units {
		if unit == old && old != replacement {
			units[index], changed = replacement, true
		}
	}
	if !changed {
		return arguments[0], nil
	}
	return client.newJavaString(javaTextOfUnits(units))
}

// javaStringCase is `toLowerCase()` at slot 31 and `toUpperCase()` at 32. A
// string that is already in that case is answered as itself.
func javaStringCase(upper bool) func(*Client, context.Context, *armcore.Thread, []uint32) (uint32, error) {
	return func(client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32) (uint32, error) {
		held, err := client.javaHeldText(arguments[0])
		if err != nil {
			return 0, err
		}
		units := utf16Units(held)
		changed := false
		for index, unit := range units {
			mapped := javaLowerUnit(unit)
			if upper {
				mapped = javaUpperUnit(unit)
			}
			if mapped != unit {
				units[index], changed = mapped, true
			}
		}
		if !changed {
			return arguments[0], nil
		}
		return client.newJavaString(javaTextOfUnits(units))
	}
}

// javaStringCopy is `String(String)` and `String(StringBuffer)`: a String
// holding the same text, as a new object.
func javaStringCopy(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	text, err := client.javaTextArgument(thread, arguments[1], "String")
	if err != nil {
		return 0, err
	}
	client.setJavaText(arguments[0], text)
	return 0, nil
}

// javaStringFromBytesIn is `String(byte[], String enc)` and `String(byte[],
// int offset, int length, String enc)`: bytes decoded in a named encoding.
func javaStringFromBytesIn(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	if arguments[1] == 0 {
		return 0, client.throwJavaPlatform(thread, javaThrowNullClass, ": String of a null array")
	}
	charset, err := client.javaCharsetNamed(thread, arguments[len(arguments)-1])
	if err != nil {
		return 0, err
	}
	data, err := client.readJavaArrayBytes(arguments[1])
	if err != nil {
		return 0, err
	}
	if len(arguments) == 5 {
		offset, length := int64(int32(arguments[2])), int64(int32(arguments[3]))
		if offset < 0 || length < 0 || offset+length > int64(len(data)) {
			return 0, client.throwJavaPlatform(thread, javaStringIndexClass,
				fmt.Sprintf(": %d bytes from %d of %d", length, offset, len(data)))
		}
		data = data[offset : offset+length]
	}
	client.setJavaText(arguments[0], charset.decode(data))
	return 0, nil
}

// javaStringValueOfBoolean, javaStringValueOfChar and javaStringValueOfLong
// are the static valueOf forms of those three.
func javaStringValueOfBoolean(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	return client.newJavaString(strconv.FormatBool(arguments[0] != 0))
}

func javaStringValueOfChar(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	return client.newJavaString(javaTextOfUnits([]uint16{uint16(arguments[0])}))
}

func javaStringValueOfLong(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	return client.newJavaString(strconv.FormatInt(int64(uint64(arguments[0])|uint64(arguments[1])<<32), 10))
}

// javaStringValueOfChars is `valueOf(char[])` and `valueOf(char[], int, int)`.
func javaStringValueOfChars(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	if arguments[0] == 0 {
		return 0, client.throwJavaPlatform(thread, javaThrowNullClass, ": valueOf a null array")
	}
	units, err := client.readJavaArrayChars(arguments[0])
	if err != nil {
		return 0, err
	}
	if len(arguments) == 3 {
		offset, count := int64(int32(arguments[1])), int64(int32(arguments[2]))
		if offset < 0 || count < 0 || offset+count > int64(len(units)) {
			return 0, client.throwJavaPlatform(thread, javaStringIndexClass,
				fmt.Sprintf(": %d characters from %d of %d", count, offset, len(units)))
		}
		units = units[offset : offset+count]
	}
	return client.newJavaString(javaTextOfUnits(units))
}

// A StringBuffer's capacity is the length of the array the class library
// would hold its characters in. Nothing here holds one, but the number is what
// `capacity()` answers, and it follows the library's own rules: 16 for an empty
// buffer, a buffer's text plus 16 for one built from a String, the count it is
// given, and on growing past it `(capacity + 1) * 2`, or the length needed if
// that is more. Every change to a buffer's text goes through setJavaText,
// which grows the number the way the change would have grown the array.
const javaBufferDefaultCapacity = 16

// javaBufferGrown is the capacity a buffer holding `length` units has, from
// the capacity it had.
func javaBufferGrown(capacity int32, length int) int32 {
	if int64(length) <= int64(capacity) {
		return capacity
	}
	grown := (int64(capacity) + 1) * 2
	if int64(length) > grown {
		grown = int64(length)
	}
	if grown > math.MaxInt32 {
		grown = math.MaxInt32
	}
	return int32(grown)
}

// javaBufferCapacity answers a buffer's capacity. A buffer a checkpoint from
// an earlier build restored has none on record, and is given the one its text
// would have grown an empty buffer to.
func (client *Client) javaBufferCapacity(object uint32) (int32, error) {
	held, err := client.javaHeldText(object)
	if err != nil {
		return 0, err
	}
	runtime := client.javaRuntimeState()
	capacity, ok := runtime.bufferCapacities[object]
	if !ok {
		capacity = javaBufferGrown(javaBufferDefaultCapacity, javaUnitCount(held))
		runtime.bufferCapacities[object] = capacity
	}
	return capacity, nil
}

// javaBufferCapacityOf is `StringBuffer.capacity()`, slot 11.
func javaBufferCapacityOf(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	capacity, err := client.javaBufferCapacity(arguments[0])
	return uint32(capacity), err
}

// javaBufferEnsureCapacity is `StringBuffer.ensureCapacity(int)`, slot 12.
func javaBufferEnsureCapacity(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	capacity, err := client.javaBufferCapacity(arguments[0])
	if err != nil {
		return 0, err
	}
	if minimum := int32(arguments[1]); minimum > capacity {
		client.javaRuntimeState().bufferCapacities[arguments[0]] = javaBufferGrown(capacity, int(minimum))
	}
	return 0, nil
}

// javaBufferCharAt is `StringBuffer.charAt(int)`, slot 14.
func javaBufferCharAt(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	held, err := client.javaHeldText(arguments[0])
	if err != nil {
		return 0, err
	}
	units := utf16Units(held)
	index := int64(int32(arguments[1]))
	if index < 0 || index >= int64(len(units)) {
		return 0, client.throwJavaPlatform(thread, javaIndexClass,
			fmt.Sprintf(": character %d of a buffer of %d", index, len(units)))
	}
	return uint32(units[index]), nil
}

// javaBufferSetCharAt is `StringBuffer.setCharAt(int, char)`, slot 16.
func javaBufferSetCharAt(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	held, err := client.javaHeldText(arguments[0])
	if err != nil {
		return 0, err
	}
	units := utf16Units(held)
	index := int64(int32(arguments[1]))
	if index < 0 || index >= int64(len(units)) {
		return 0, client.throwJavaPlatform(thread, javaIndexClass,
			fmt.Sprintf(": character %d of a buffer of %d", index, len(units)))
	}
	units[index] = uint16(arguments[2])
	client.setJavaText(arguments[0], javaTextOfUnits(units))
	return 0, nil
}

// javaBufferAppendUnits puts code units on the end of a buffer and answers it.
func (client *Client) javaBufferAppendUnits(object uint32, units []uint16) (uint32, error) {
	held, err := client.javaHeldText(object)
	if err != nil {
		return 0, err
	}
	existing := utf16Units(held)
	if uint64(len(existing))+uint64(len(units)) > uint64(maxJavaArrayLength) {
		return 0, fmt.Errorf("append exceeds the string length limit")
	}
	client.setJavaText(object, javaTextOfUnits(append(existing, units...)))
	return object, nil
}

// javaBufferAppendChars is `append(char[])` at slot 19 and `append(char[],
// int, int)` at 20.
func javaBufferAppendChars(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	if _, err := client.javaHeldText(arguments[0]); err != nil {
		return 0, err
	}
	if arguments[1] == 0 {
		return 0, client.throwJavaPlatform(thread, javaThrowNullClass, ": append of a null array")
	}
	var units []uint16
	var err error
	if len(arguments) == 4 {
		units, err = client.readJavaCharRange(thread, arguments[1], arguments[2], arguments[3])
	} else {
		units, err = client.readJavaArrayChars(arguments[1])
	}
	if err != nil {
		return 0, err
	}
	return client.javaBufferAppendUnits(arguments[0], units)
}

// javaBufferAppendLong is `append(long)`, slot 24.
func javaBufferAppendLong(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	value := int64(uint64(arguments[1]) | uint64(arguments[2])<<32)
	return client.javaBufferAppendUnits(arguments[0], utf16Units(strconv.FormatInt(value, 10)))
}

// javaBufferDeleteCharAt is `deleteCharAt(int)`, slot 28.
func javaBufferDeleteCharAt(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	held, err := client.javaHeldText(arguments[0])
	if err != nil {
		return 0, err
	}
	units := utf16Units(held)
	index := int64(int32(arguments[1]))
	if index < 0 || index >= int64(len(units)) {
		return 0, client.throwJavaPlatform(thread, javaStringIndexClass,
			fmt.Sprintf(": deleteCharAt(%d) of a buffer of %d", index, len(units)))
	}
	client.setJavaText(arguments[0], javaTextOfUnits(append(units[:index:index], units[index+1:]...)))
	return arguments[0], nil
}

// javaBufferInsertObject is `insert(int, Object)`, slot 29: what
// String.valueOf makes of the object, which is its own toString.
func javaBufferInsertObject(
	client *Client, ctx context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	text, err := client.javaToStringOf(ctx, thread, arguments[2])
	if err != nil {
		return 0, err
	}
	return client.javaBufferInsert(thread, arguments[0], int32(arguments[1]), text)
}

// javaBufferInsertChars is `insert(int, char[])`, slot 31.
func javaBufferInsertChars(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	if arguments[2] == 0 {
		return 0, client.throwJavaPlatform(thread, javaThrowNullClass, ": insert of a null array")
	}
	units, err := client.readJavaArrayChars(arguments[2])
	if err != nil {
		return 0, err
	}
	return client.javaBufferInsert(thread, arguments[0], int32(arguments[1]), javaTextOfUnits(units))
}

// javaBufferInsertBoolean, javaBufferInsertChar and javaBufferInsertLong are
// `insert(int, boolean)`, `insert(int, char)` and `insert(int, long)`, slots
// 32, 33 and 35: the same text the matching append adds.
func javaBufferInsertBoolean(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	return client.javaBufferInsert(thread, arguments[0], int32(arguments[1]), strconv.FormatBool(arguments[2] != 0))
}

func javaBufferInsertChar(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	return client.javaBufferInsert(thread, arguments[0], int32(arguments[1]),
		javaTextOfUnits([]uint16{uint16(arguments[2])}))
}

func javaBufferInsertLong(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	value := int64(uint64(arguments[2]) | uint64(arguments[3])<<32)
	return client.javaBufferInsert(thread, arguments[0], int32(arguments[1]), strconv.FormatInt(value, 10))
}

// javaBufferReverse is `StringBuffer.reverse()`, slot 38: the units in the
// other order. CLDC's reverse predates surrogate pairs and turns every unit
// round, so a pair comes out as its two halves the other way about.
func javaBufferReverse(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	held, err := client.javaHeldText(arguments[0])
	if err != nil {
		return 0, err
	}
	units := utf16Units(held)
	for left, right := 0, len(units)-1; left < right; left, right = left+1, right-1 {
		units[left], units[right] = units[right], units[left]
	}
	client.setJavaText(arguments[0], javaTextOfUnits(units))
	return arguments[0], nil
}
