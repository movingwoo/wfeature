package lgt

import (
	"context"
	"fmt"
	"math"
	"strconv"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// `java/io/PrintStream`, which is the whole of what `System.out` is here.
//
// A title of this era logs its own state with `System.out.println`, and it logs
// the things that matter: the resource it failed to load, the state it is
// entering, the number it read out of its own data. That output is the game's
// own account of itself, so it goes where the guest's other account of itself
// goes — the `printk` line — rather than to a stream nothing can read.
//
// **Nothing is written anywhere else.** There is no console for a browser page
// to print into and no file for a print to reach, so the line goes through the
// same logging boundary `printk` does — which a debug build records and a
// release build drops.
//
// The slots come from the class library's declaration order the same way every
// other platform class's do, and this one is checked by its own call site:
// `OutputStream` declares `write(int)`, `write(byte[])`, `write(byte[],int,int)`,
// `flush` and `close`, taking 10 to 14; `PrintStream` overrides four of those
// and adds `checkError`, `setError`, the nine `print` forms and the ten
// `println` forms, in that order. That puts `println(String)` at 34, which is
// the slot a local title dispatches on with a String in its second register.
const javaPrintStreamClass = "java/io/PrintStream"

// javaPrintStreamSlots is the class's own run, from `checkError` at 15.
const (
	javaPrintCheckError = 15
	javaPrintSetError   = 16
	javaPrintBoolean    = 17
	javaPrintChar       = 18
	javaPrintInt        = 19
	javaPrintLong       = 20
	javaPrintFloat      = 21
	javaPrintDouble     = 22
	javaPrintChars      = 23
	javaPrintString     = 24
	javaPrintObject     = 25
	javaPrintlnEmpty    = 26
	javaPrintlnBoolean  = 27
	javaPrintlnChar     = 28
	javaPrintlnInt      = 29
	javaPrintlnLong     = 30
	javaPrintlnFloat    = 31
	javaPrintlnDouble   = 32
	javaPrintlnChars    = 33
	javaPrintlnString   = 34
	javaPrintlnObject   = 35
)

// javaPrintText renders one argument the way `String.valueOf` would, so that a
// print of anything reads as the value rather than as an address.
func (client *Client) javaPrintText(kind uint32, arguments []uint32) string {
	switch kind {
	case javaPrintlnEmpty:
		return ""
	case javaPrintBoolean, javaPrintlnBoolean:
		if arguments[1] != 0 {
			return "true"
		}
		return "false"
	case javaPrintChar, javaPrintlnChar:
		return string(rune(arguments[1] & 0xffff))
	case javaPrintInt, javaPrintlnInt:
		return strconv.FormatInt(int64(int32(arguments[1])), 10)
	case javaPrintLong, javaPrintlnLong:
		return strconv.FormatInt(int64(uint64(arguments[2])<<32|uint64(arguments[1])), 10)
	case javaPrintFloat, javaPrintlnFloat:
		return strconv.FormatFloat(float64(math.Float32frombits(arguments[1])), 'g', -1, 32)
	case javaPrintDouble, javaPrintlnDouble:
		// The pair goes in the two registers past the receiver, low word first,
		// which is where this module's compiler puts a `long` too — it does not
		// align a 64-bit argument onto an even register. See writeLong.
		return strconv.FormatFloat(
			math.Float64frombits(uint64(arguments[2])<<32|uint64(arguments[1])), 'g', -1, 64)
	case javaPrintChars, javaPrintlnChars:
		units, err := client.readJavaArrayChars(arguments[1])
		if err != nil {
			return ""
		}
		return javaTextOfUnits(units)
	}
	// A String and an Object both come to the text this platform holds for the
	// object, and a reference it holds none for prints the way a class without
	// a `toString` does — as something rather than as nothing, because a log
	// line that loses its subject is worse than one that names it oddly.
	if arguments[1] == 0 {
		return "null"
	}
	if text, ok := client.javaText(arguments[1]); ok {
		return text
	}
	if class, ok := client.javaClassOfObject(arguments[1]); ok {
		return fmt.Sprintf("%s@%#x", class.Name, arguments[1])
	}
	return fmt.Sprintf("%#x", arguments[1])
}

// javaPrint is every `print` and `println` slot: render the argument and log
// the line. The answer is void, and a `println` is one line whether or not the
// title builds it out of several calls — a `print` that is not followed by a
// newline is its own line here, which loses the join and keeps the content.
func javaPrint(kind uint32) func(
	*Client, context.Context, *armcore.Thread, []uint32) (uint32, error) {
	return func(
		client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
	) (uint32, error) {
		text := client.javaPrintText(kind, arguments)
		if client.logger != nil {
			client.logger.Debug("LGT java print", "text", text)
		}
		return 0, nil
	}
}

// javaPrintStreamSlots is what a dispatch through `System.out` comes to. The
// four `OutputStream` slots a PrintStream overrides are not here: nothing local
// reaches them, and a `write` of raw bytes is not a line.
var javaPrintStreamSlots = map[uint32]javaBakedSlot{
	javaPrintCheckError: {Called: "checkError()Z",
		Method: javaPlatformMethod{Words: 1, Implementat: javaZeroResult}},
	javaPrintSetError: {Called: "setError()V",
		Method: javaPlatformMethod{Words: 1, Implementat: javaNoResult}},
}

func init() {
	// The print run is one shape with twenty entries, so it is built rather
	// than written out: a `long` and a `double` take two argument words and
	// everything else takes one, and `println()` takes only the receiver.
	for _, entry := range []struct {
		slot   uint32
		called string
		words  int
	}{
		{javaPrintBoolean, "print(Z)V", 2},
		{javaPrintChar, "print(C)V", 2},
		{javaPrintInt, "print(I)V", 2},
		{javaPrintLong, "print(J)V", 3},
		{javaPrintFloat, "print(F)V", 2},
		{javaPrintDouble, "print(D)V", 3},
		{javaPrintChars, "print([C)V", 2},
		{javaPrintString, "print(Ljava/lang/String;)V", 2},
		{javaPrintObject, "print(Ljava/lang/Object;)V", 2},
		{javaPrintlnEmpty, "println()V", 1},
		{javaPrintlnBoolean, "println(Z)V", 2},
		{javaPrintlnChar, "println(C)V", 2},
		{javaPrintlnInt, "println(I)V", 2},
		{javaPrintlnLong, "println(J)V", 3},
		{javaPrintlnFloat, "println(F)V", 2},
		{javaPrintlnDouble, "println(D)V", 3},
		{javaPrintlnChars, "println([C)V", 2},
		{javaPrintlnString, "println(Ljava/lang/String;)V", 2},
		{javaPrintlnObject, "println(Ljava/lang/Object;)V", 2},
	} {
		javaPrintStreamSlots[entry.slot] = javaBakedSlot{
			Called: entry.called,
			Method: javaPlatformMethod{
				Words: entry.words, Implementat: javaPrint(entry.slot),
			},
		}
	}
	javaBakedVirtualSlots[javaPrintStreamClass] = javaPrintStreamSlots
}
