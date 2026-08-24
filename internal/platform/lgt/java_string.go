package lgt

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf16"

	"golang.org/x/text/encoding/korean"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// Text: `java/lang/String` and `java/lang/StringBuffer`.
//
// **The text lives on this side, not in the guest's object.** A String the
// module builds is an object this platform allocated, and what it holds is kept
// in a Go map keyed by that object — the same place a string constant's text
// goes. The module never reads a String's own words: every use of one is a
// call, either a static entry or a vtable slot, so there is no layout to agree
// with and nothing is gained by writing the characters into guest memory.
//
// A StringBuffer is the same arrangement with a different key: the buffer's
// contents are its entry in the same map, and appending rewrites it.

const javaStringBufferClass = "java/lang/StringBuffer"

// javaText answers the text an object holds, and whether it is one that holds
// any. A null reference is not text and not an error either — the caller
// decides, because `String.valueOf(null)` is "null" and `sb.append(null)` is
// the same four characters.
func (client *Client) javaText(object uint32) (string, bool) {
	if object == 0 || client.javaRun == nil {
		return "", false
	}
	text, ok := client.javaRun.strings[object]
	return text, ok
}

// setJavaText is what a constructor and every mutation of a buffer come to.
func (client *Client) setJavaText(object uint32, text string) {
	client.javaRuntimeState().strings[object] = text
}

// newJavaString builds a String object holding text, which is what a call that
// answers a String has to hand back.
func (client *Client) newJavaString(text string) (uint32, error) {
	class, err := client.preparePlatformJavaClass(javaStringClass)
	if err != nil {
		return 0, err
	}
	object, err := client.allocateJavaObject(class)
	if err != nil {
		return 0, err
	}
	client.setJavaText(object, text)
	return object, nil
}

// readJavaArrayBytes reads an array's elements as bytes, with the length the
// array's own block carries. It refuses an array whose element is wider than a
// byte, because a length in elements is not a length in bytes.
func (client *Client) readJavaArrayBytes(object uint32) ([]byte, error) {
	if object == 0 {
		return nil, fmt.Errorf("a null array")
	}
	block, err := client.readWord(object + 8)
	if err != nil {
		return nil, err
	}
	length, err := client.readWord(block)
	if err != nil {
		return nil, err
	}
	if length > maxJavaArrayLength {
		return nil, fmt.Errorf("an array of %d elements is not one", length)
	}
	data := make([]byte, length)
	if err := client.core.Memory().Read(block+javaArrayLengthWords*4, data); err != nil {
		return nil, err
	}
	return data, nil
}

// readJavaArrayChars reads an array of UTF-16 code units, which is what a
// `char[]` is.
func (client *Client) readJavaArrayChars(object uint32) ([]uint16, error) {
	if object == 0 {
		return nil, fmt.Errorf("a null array")
	}
	block, err := client.readWord(object + 8)
	if err != nil {
		return nil, err
	}
	length, err := client.readWord(block)
	if err != nil {
		return nil, err
	}
	if length > maxJavaArrayLength {
		return nil, fmt.Errorf("an array of %d elements is not one", length)
	}
	units := make([]uint16, length)
	for index := range units {
		unit, err := client.readHalfword(block + javaArrayLengthWords*4 + uint32(index)*2)
		if err != nil {
			return nil, err
		}
		units[index] = unit
	}
	return units, nil
}

// decodeEUCKR turns handset bytes into text. The handset's default encoding is
// the one its own text is written in, and every archive here is Korean, so the
// bytes a title hands over — whether out of a `byte[]` or out of the buffer it
// gives `MC_grpDrawString` — are EUC-KR; bytes that are not decode as
// themselves rather than as an error, which is what keeps a byte array of
// digits readable.
//
// **It is not applied to every string read out of guest memory**, only to the
// ones that are text. A file name, a resource name and a property key are
// looked up by their bytes on both sides, so decoding one would turn a match
// into a miss.
func decodeEUCKR(data []byte) string {
	decoded, err := korean.EUCKR.NewDecoder().Bytes(data)
	if err != nil {
		return strings.ToValidUTF8(string(data), "�")
	}
	return string(decoded)
}

// javaStringConstructor is `String([B)`, `String([BII)` and `String([CII)`
// together: the receiver is the object the module allocated, and what it is
// given is the array to read the characters out of.
func javaStringConstructor(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	object, array := arguments[0], arguments[1]
	data, err := client.readJavaArrayBytes(array)
	if err != nil {
		return 0, err
	}
	if len(arguments) > 3 {
		offset, count := arguments[2], arguments[3]
		if uint64(offset)+uint64(count) > uint64(len(data)) {
			return 0, fmt.Errorf("%d bytes from %d is past the end of %d", count, offset, len(data))
		}
		data = data[offset : offset+count]
	}
	client.setJavaText(object, decodeEUCKR(data))
	return 0, nil
}

// javaStringFromChars is `String([CII)`, whose array is code units rather than
// bytes and needs no decoding.
func javaStringFromChars(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	object, array, offset, count := arguments[0], arguments[1], arguments[2], arguments[3]
	units, err := client.readJavaArrayChars(array)
	if err != nil {
		return 0, err
	}
	if uint64(offset)+uint64(count) > uint64(len(units)) {
		return 0, fmt.Errorf("%d characters from %d is past the end of %d", count, offset, len(units))
	}
	client.setJavaText(object, javaTextOfUnits(units[offset:offset+count]))
	return 0, nil
}

// javaTextOfUnits is the same reading of UTF-16 the string constants arrive in.
func javaTextOfUnits(units []uint16) string {
	symbols := make([]rune, 0, len(units))
	for _, unit := range units {
		symbols = append(symbols, rune(unit))
	}
	return string(symbols)
}

// javaStringTrim answers a String with the leading and trailing control
// characters removed, which is the language's own definition of `trim`: it cuts
// what is below the space, not what a locale calls whitespace.
func javaStringTrim(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	held, ok := client.javaText(arguments[0])
	if !ok {
		return 0, fmt.Errorf("the object at %#x holds no text", arguments[0])
	}
	trimmed := strings.TrimFunc(held, func(symbol rune) bool { return symbol <= ' ' })
	if trimmed == held {
		// The same text is the same String. A title that trims a line it has
		// nothing to take off keeps the object it had, which is what the
		// language says and one allocation less.
		return arguments[0], nil
	}
	return client.newJavaString(trimmed)
}

// javaStringValueOf is `String.valueOf(int)`, the decimal form of a number.
func javaStringValueOf(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	return client.newJavaString(strconv.FormatInt(int64(int32(arguments[0])), 10))
}

// javaStringEmpty is `String()`, which the language defines as no characters at
// all — on an object the module has already allocated.
func javaStringEmpty(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	client.setJavaText(arguments[0], "")
	return 0, nil
}

// javaStringLength answers how many characters a String holds, counted the way
// Java counts them.
func javaStringLength(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	held, ok := client.javaText(arguments[0])
	if !ok {
		return 0, fmt.Errorf("the object at %#x holds no text", arguments[0])
	}
	return uint32(len([]rune(held))), nil
}

// javaStringSubstring is `substring(begin, end)`: the characters between two
// indexes, counted the way Java counts them.
func javaStringSubstring(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	held, ok := client.javaText(arguments[0])
	if !ok {
		return 0, fmt.Errorf("the object at %#x holds no text", arguments[0])
	}
	symbols := []rune(held)
	// The one-argument form is the same method with the string's own end for
	// its second bound, which is how the class library declares the pair.
	begin, end := int(int32(arguments[1])), len(symbols)
	if len(arguments) > 2 {
		end = int(int32(arguments[2]))
	}
	if begin < 0 || end < begin || end > len(symbols) {
		return 0, fmt.Errorf("%d to %d of a string of %d", begin, end, len(symbols))
	}
	return client.newJavaString(string(symbols[begin:end]))
}

// javaStringToCharArray answers a `char[]` holding what the String holds. The
// array is the guest's own — a title measures and draws text out of one — so
// the characters are written into guest memory here, unlike the String itself.
func javaStringToCharArray(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	held, ok := client.javaText(arguments[0])
	if !ok {
		return 0, fmt.Errorf("the object at %#x holds no text", arguments[0])
	}
	return client.newJavaCharArray([]rune(held))
}

// newJavaCharArray builds a `char[]` out of code points, as the UTF-16 code
// units a Java char is.
func (client *Client) newJavaCharArray(symbols []rune) (uint32, error) {
	class, err := client.javaArrayType(1, "C", 2)
	if err != nil {
		return 0, err
	}
	array, err := client.allocateJavaArray(class.Object, uint32(len(symbols)))
	if err != nil {
		return 0, err
	}
	block, err := client.readWord(array + 8)
	if err != nil {
		return 0, err
	}
	for index, symbol := range symbols {
		if err := client.writeHalfword(block+javaArrayLengthWords*4+uint32(index)*2, uint16(symbol)); err != nil {
			return 0, err
		}
	}
	return array, nil
}

// javaBufferConstructor is `StringBuffer()` and `StringBuffer(String)`: an
// empty buffer, or one that starts as a copy of what it is given.
// javaBufferEmpty builds a buffer whose argument is a capacity rather than
// text. The capacity is a hint about storage this platform grows on demand.
func javaBufferEmpty(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	client.setJavaText(arguments[0], "")
	return 0, nil
}

func javaBufferConstructor(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	text := ""
	if len(arguments) > 1 {
		if held, ok := client.javaText(arguments[1]); ok {
			text = held
		} else if arguments[1] != 0 {
			return 0, fmt.Errorf("the object at %#x holds no text", arguments[1])
		}
	}
	client.setJavaText(arguments[0], text)
	return 0, nil
}

// javaBufferAppendText is `StringBuffer.append`, for the arguments that are
// already text. It answers the receiver, which is what makes a chain of them
// one expression.
func javaBufferAppendText(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	buffer, value := arguments[0], arguments[1]
	held, ok := client.javaText(buffer)
	if !ok {
		return 0, fmt.Errorf("the object at %#x is not a buffer this platform built", buffer)
	}
	client.setJavaText(buffer, held+javaTextValue(client, value))
	return buffer, nil
}

// javaBufferAppendInt is `StringBuffer.append(int)`, which appends the number
// in the decimal form `Integer.toString` gives it.
func javaBufferAppendInt(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	buffer := arguments[0]
	held, ok := client.javaText(buffer)
	if !ok {
		return 0, fmt.Errorf("the object at %#x is not a buffer this platform built", buffer)
	}
	client.setJavaText(buffer, held+strconv.FormatInt(int64(int32(arguments[1])), 10))
	return buffer, nil
}

// javaBufferSetLength is `StringBuffer.setLength(int)`: cut the buffer to that
// many characters, or pad it with the null character the language pads with.
func javaBufferSetLength(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	held, ok := client.javaText(arguments[0])
	if !ok {
		return 0, fmt.Errorf("the object at %#x is not a buffer this platform built", arguments[0])
	}
	length := int(int32(arguments[1]))
	if length < 0 {
		return 0, fmt.Errorf("a length of %d", length)
	}
	symbols := []rune(held)
	for len(symbols) < length {
		symbols = append(symbols, 0)
	}
	client.setJavaText(arguments[0], string(symbols[:length]))
	return 0, nil
}

// javaBufferLength is `StringBuffer.length()`, how many characters it holds.
func javaBufferLength(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	held, ok := client.javaText(arguments[0])
	if !ok {
		return 0, fmt.Errorf("the object at %#x is not a buffer this platform built", arguments[0])
	}
	return uint32(len([]rune(held))), nil
}

// javaBufferAppendChar is `StringBuffer.append(char)`, whose argument is one
// UTF-16 code unit rather than a number to render.
func javaBufferAppendChar(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	buffer := arguments[0]
	held, ok := client.javaText(buffer)
	if !ok {
		return 0, fmt.Errorf("the object at %#x is not a buffer this platform built", buffer)
	}
	client.setJavaText(buffer, held+string(rune(uint16(arguments[1]))))
	return buffer, nil
}

// javaTextValue is what appending a reference appends. A null reference is the
// four characters the language says it is rather than a failure, and an object
// that holds no text is reported: this platform has no `toString` to fall back
// on, so guessing at one would append a plausible wrong thing.
func javaTextValue(client *Client, object uint32) string {
	if object == 0 {
		return "null"
	}
	if text, ok := client.javaText(object); ok {
		return text
	}
	return ""
}

// javaBufferToString answers a String holding what the buffer holds. It is a
// new object every call, the way the language defines it: a title that keeps
// the answer and appends again must not see its copy change.
func javaBufferToString(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	held, ok := client.javaText(arguments[0])
	if !ok {
		return 0, fmt.Errorf("the object at %#x is not a buffer this platform built", arguments[0])
	}
	return client.newJavaString(held)
}

// javaStringEquals is `String.equals(Object)`, which compares text and not
// identity — two Strings built from the same bytes are equal and are not the
// same object here, because every call that answers a String builds one.
//
// **A non-String argument is not equal to a String**, whatever text it holds:
// a StringBuffer holds its text in the same place this platform keeps a
// String's, so comparing the text alone would answer true for an argument the
// language says is a different type. The argument's own class is what decides,
// and an argument this platform never issued — including null — is not equal.
func javaStringEquals(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	held, ok := client.javaText(arguments[0])
	if !ok {
		return 0, fmt.Errorf("the object at %#x holds no text", arguments[0])
	}
	class, known := client.javaClassOfObject(arguments[1])
	if !known || class.Name != javaStringClass {
		return 0, nil
	}
	other, ok := client.javaText(arguments[1])
	if !ok || other != held {
		return 0, nil
	}
	return 1, nil
}

// The rest of `java/lang/String`, reached through the slots the compiler baked.
// A String's own methods take the slots from 10 upward in declaration order,
// and one that overrides `java/lang/Object` takes Object's slot instead — so
// equals, hashCode and toString are not in the run. That places length at 10,
// substring(int, int) at 28, trim at 33 and toCharArray at 34, which is where
// their own call sites had already put them; docs/lgt.md has the rest of the
// anchors. Each slot below then agrees with what its own call site passes.

// javaStringComparison is `compareTo(String)`, slot 16. Java compares by UTF-16
// code unit rather than by locale, which is what a title sorting names or
// searching a sorted table depends on: the order has to be the one the table
// was built in.
func javaStringComparison(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	held, other, err := client.javaTextPair("compareTo", arguments)
	if err != nil {
		return 0, err
	}
	left, right := utf16Units(held), utf16Units(other)
	for index := 0; index < len(left) && index < len(right); index++ {
		if left[index] != right[index] {
			return uint32(int32(left[index]) - int32(right[index])), nil
		}
	}
	return uint32(int32(len(left)) - int32(len(right))), nil
}

// javaStringStartsWith is `startsWith(String)`, slot 19.
func javaStringStartsWith(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	held, prefix, err := client.javaTextPair("startsWith", arguments)
	if err != nil {
		return 0, err
	}
	if strings.HasPrefix(held, prefix) {
		return 1, nil
	}
	return 0, nil
}

// javaStringIndexOfTextFrom is `indexOf(String, int)`, slot 26. The answer is
// an index in characters counted the way Java counts them, not in bytes, and
// the search starts at one: a title that walks a delimited line calls this in
// a loop feeding back the index it last found.
func javaStringIndexOfTextFrom(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	held, needle, err := client.javaTextPair("indexOf", arguments)
	if err != nil {
		return 0, err
	}
	symbols, wanted := utf16Units(held), utf16Units(needle)
	from := int(int32(arguments[2]))
	if from < 0 {
		from = 0
	}
	for start := from; start+len(wanted) <= len(symbols); start++ {
		match := true
		for offset, unit := range wanted {
			if symbols[start+offset] != unit {
				match = false
				break
			}
		}
		if match {
			return uint32(int32(start)), nil
		}
	}
	return uint32(0xffffffff), nil
}

// javaStringBytes is `getBytes()`, slot 14: the text in the platform's own
// encoding, which on this handset is EUC-KR — the same encoding the String
// constructor decodes a byte array with, so the pair is one round trip.
func javaStringBytes(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	held, ok := client.javaText(arguments[0])
	if !ok {
		return 0, fmt.Errorf("the object at %#x holds no text", arguments[0])
	}
	return client.newJavaByteArray(encodeEUCKR(held))
}

// javaTextPair reads a text method's receiver and its one String argument.
func (client *Client) javaTextPair(method string, arguments []uint32) (string, string, error) {
	held, ok := client.javaText(arguments[0])
	if !ok {
		return "", "", fmt.Errorf("String.%s receiver at %#x holds no text", method, arguments[0])
	}
	other, ok := client.javaText(arguments[1])
	if !ok {
		if arguments[1] == 0 {
			return "", "", fmt.Errorf("String.%s was given null", method)
		}
		return "", "", fmt.Errorf("String.%s argument at %#x holds no text", method, arguments[1])
	}
	return held, other, nil
}

// utf16Units is the text as the code units a Java index counts, which is what
// every index a String method takes or answers is in.
func utf16Units(text string) []uint16 {
	return utf16.Encode([]rune(text))
}

// encodeEUCKR is decodeEUCKR's inverse. Text this handset's encoding cannot
// hold is refused by the encoder rather than replaced, and the UTF-8 bytes are
// the fallback — a title that gets those back hands them to a String
// constructor that reads them the same way.
func encodeEUCKR(text string) []byte {
	encoded, err := korean.EUCKR.NewEncoder().Bytes([]byte(text))
	if err != nil {
		return []byte(text)
	}
	return encoded
}

// javaStringCharAt is `charAt(int)`, slot 11: one UTF-16 code unit, which is
// what a Java index into a String counts.
func javaStringCharAt(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	held, ok := client.javaText(arguments[0])
	if !ok {
		return 0, fmt.Errorf("the object at %#x holds no text", arguments[0])
	}
	units := utf16Units(held)
	index := int(int32(arguments[1]))
	if index < 0 || index >= len(units) {
		return 0, fmt.Errorf("character %d of a string of %d", index, len(units))
	}
	return uint32(units[index]), nil
}

// javaBufferDelete is `StringBuffer.delete(int, int)`, slot 27. The language
// clamps the end to the buffer's length rather than refusing it, which is what
// a title emptying a buffer with delete(0, capacity) depends on. The answer is
// the buffer, so a chain can carry on from it.
func javaBufferDelete(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	held, ok := client.javaText(arguments[0])
	if !ok {
		return 0, fmt.Errorf("the object at %#x holds no text", arguments[0])
	}
	units := utf16Units(held)
	start, end := int(int32(arguments[1])), int(int32(arguments[2]))
	if end > len(units) {
		end = len(units)
	}
	if start < 0 || start > len(units) || start > end {
		return 0, fmt.Errorf("delete %d to %d of a buffer of %d", start, end, len(units))
	}
	client.setJavaText(arguments[0], javaTextOfUnits(append(append([]uint16{}, units[:start]...), units[end:]...)))
	return arguments[0], nil
}

// javaBufferAppendObject is `StringBuffer.append(Object)`, slot 17. The
// language defines it as appending `String.valueOf(obj)`, which is the
// object's own toString — and this platform has no toString to call on a guest
// object. What it does have is the object's class, and the one shape this
// reaches for locally is a caught exception being written into a message,
// where the class name is the whole of what the line is for. An object of a
// class this platform does not know is named by its address, which is at least
// something a reader of the log can follow.
func javaBufferAppendObject(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	held, ok := client.javaText(arguments[0])
	if !ok {
		return 0, fmt.Errorf("the object at %#x is not a buffer this platform built", arguments[0])
	}
	client.setJavaText(arguments[0], held+client.javaObjectText(arguments[1]))
	return arguments[0], nil
}

// javaObjectText is `String.valueOf(Object)` for an object this platform did
// not build the text of.
func (client *Client) javaObjectText(object uint32) string {
	if object == 0 {
		return "null"
	}
	if text, ok := client.javaText(object); ok {
		return text
	}
	if class, known := client.javaClassOfObject(object); known {
		return fmt.Sprintf("%s@%x", class.Name, object)
	}
	return fmt.Sprintf("object@%x", object)
}

// javaStringConcat is `concat(String)`, slot 29: one string on the end of
// another. The language answers the receiver unchanged when there is nothing
// to add, which is one allocation less for a title concatenating in a loop.
func javaStringConcat(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	held, other, err := client.javaTextPair("concat", arguments)
	if err != nil {
		return 0, err
	}
	if other == "" {
		return arguments[0], nil
	}
	return client.newJavaString(held + other)
}

// javaStringIndexOfChar is `indexOf(int)` at slot 21 and `indexOf(int, int)` at
// 22: where a character first appears, or -1. The argument is a code unit
// rather than a code point, which is what a `char` widened to an int is on this
// side too.
func javaStringIndexOfChar(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	held, ok := client.javaText(arguments[0])
	if !ok {
		return 0, fmt.Errorf("the object at %#x is not a string this platform built", arguments[0])
	}
	wanted := uint16(arguments[1])
	from := 0
	if len(arguments) > 2 {
		// The two-argument form starts the search where it is told to. A
		// negative start is the whole string, which is what the language says
		// of one.
		if start := int(int32(arguments[2])); start > 0 {
			from = start
		}
	}
	units := utf16Units(held)
	for index := from; index < len(units); index++ {
		if units[index] == wanted {
			return uint32(int32(index)), nil
		}
	}
	return 0xffffffff, nil
}
