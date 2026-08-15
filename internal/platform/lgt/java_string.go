package lgt

import (
	"context"
	"fmt"
	"strconv"
	"strings"

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
	begin, end := int(int32(arguments[1])), int(int32(arguments[2]))
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
