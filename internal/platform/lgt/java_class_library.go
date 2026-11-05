package lgt

import (
	"context"
	"fmt"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// The class library's core — Object, text, numbers, collections, readers,
// threads, Class and Throwable — placed by the specification rather than one
// watched call at a time.
//
// **Every slot below is the declaration-order rule's** (javaBakedVirtualSlots):
// a class's own methods from slot 10 in the order the specification's CLDC
// class page declares them, an override at the slot of the class above it,
// and Object's own at 1 to 9. The anchors call sites settled — `String` at 3,
// 10, 11, 14, 16, 19, 21, 22, 26 to 29, 33 and 34, `Vector` from 10 to 31,
// `Thread` at 10, 13 and 14, `Class` at 16 — all sit on those pages' order, so
// a slot between or after them is arithmetic rather than a guess. What a
// method answers is the page's own account of it. Floating point is left out,
// with the rest of what titles of this era do not use.
//
// **Some classes here have no anchor at all**: Boolean, Character, Writer,
// OutputStreamWriter, and Date past getTime. Their slots are the rule's alone —
// the class page's order, counted from where the class above it ends — and
// TestJavaBakedSlotsFollowTheDeclarationOrder holds them to it like the rest.
// Where the KTF and SKT runtimes have a member (internal/jvm), this one answers
// what they answer for the same arguments; where they have none, it answers
// what CLDC 1.1 does.
//
// They are registered from java_api.go's init rather than written into its
// tables, because most of them call back into the title — a key's hashCode, an
// element's toString, a constructor — and a guest call reaches these tables
// back.
func registerJavaClassLibrary() {
	for key, method := range map[string]javaPlatformMethod{
		"java/lang/String.valueOf(Ljava/lang/Object;)Ljava/lang/String;": {
			Words: 1, Implementat: javaStringValueOfObject},
		"java/lang/String.valueOf(Z)Ljava/lang/String;":    {Words: 1, Implementat: javaStringValueOfBoolean},
		"java/lang/String.valueOf(C)Ljava/lang/String;":    {Words: 1, Implementat: javaStringValueOfChar},
		"java/lang/String.valueOf([C)Ljava/lang/String;":   {Words: 1, Implementat: javaStringValueOfChars},
		"java/lang/String.valueOf([CII)Ljava/lang/String;": {Words: 3, Implementat: javaStringValueOfChars},
		"java/lang/String.valueOf(J)Ljava/lang/String;":    {Words: 2, Implementat: javaStringValueOfLong},
		"java/lang/String.<init>(Ljava/lang/String;)V":     {Words: 2, Implementat: javaStringCopy},
		"java/lang/String.<init>(Ljava/lang/StringBuffer;)V": {
			Words: 2, Implementat: javaStringCopy},
		"java/lang/String.<init>([BLjava/lang/String;)V": {Words: 3, Implementat: javaStringFromBytesIn},
		"java/lang/String.<init>([BIILjava/lang/String;)V": {
			Words: 5, Implementat: javaStringFromBytesIn},

		"java/lang/System.identityHashCode(Ljava/lang/Object;)I": {Words: 1, Implementat: javaSystemIdentityHash},
		"java/lang/System.exit(I)V":                              {Words: 1, Implementat: javaSystemExit},
		"java/lang/System.getProperty(Ljava/lang/String;)Ljava/lang/String;": {
			Words: 1, Implementat: javaSystemGetProperty},
		"java/lang/Class.forName(Ljava/lang/String;)Ljava/lang/Class;": {
			Words: 1, Implementat: javaClassForName},

		"java/util/Hashtable.<init>()V":  {Words: 1, Implementat: javaHashtableConstructor},
		"java/util/Hashtable.<init>(I)V": {Words: 2, Implementat: javaHashtableConstructor},

		"java/lang/Integer.<init>(I)V": {Words: 2, Implementat: javaNumberConstructor(32)},
		"java/lang/Short.<init>(S)V":   {Words: 2, Implementat: javaNumberConstructor(16)},
		"java/lang/Byte.<init>(B)V":    {Words: 2, Implementat: javaNumberConstructor(8)},
		"java/lang/Long.<init>(J)V":    {Words: 3, Implementat: javaLongConstructor},
		"java/lang/Integer.toString(II)Ljava/lang/String;": {
			Words: 2, Implementat: javaIntegerToStringRadix},
		"java/lang/Integer.toHexString(I)Ljava/lang/String;": {
			Words: 1, Implementat: javaIntegerUnsigned(16)},
		"java/lang/Integer.toOctalString(I)Ljava/lang/String;": {
			Words: 1, Implementat: javaIntegerUnsigned(8)},
		"java/lang/Integer.toBinaryString(I)Ljava/lang/String;": {
			Words: 1, Implementat: javaIntegerUnsigned(2)},
		"java/lang/Integer.valueOf(Ljava/lang/String;)Ljava/lang/Integer;": {
			Words: 1, Implementat: javaIntegerValueOf},
		"java/lang/Integer.valueOf(Ljava/lang/String;I)Ljava/lang/Integer;": {
			Words: 2, Implementat: javaIntegerValueOf},
		"java/lang/Long.parseLong(Ljava/lang/String;)J":    {Words: 1, Implementat: javaParseLong},
		"java/lang/Long.parseLong(Ljava/lang/String;I)J":   {Words: 2, Implementat: javaParseLong},
		"java/lang/Long.toString(JI)Ljava/lang/String;":    {Words: 3, Implementat: javaLongToStringRadix},
		"java/lang/Short.parseShort(Ljava/lang/String;I)S": {Words: 2, Implementat: javaParseRadix(16)},
		"java/lang/Byte.parseByte(Ljava/lang/String;I)B":   {Words: 2, Implementat: javaParseRadix(8)},
		"java/lang/Math.abs(J)J":                           {Words: 2, Implementat: javaMathAbsLong},
		"java/lang/Thread.activeCount()I":                  {Implementat: javaThreadActiveCount},

		"java/lang/Boolean.<init>(Z)V":        {Words: 2, Implementat: javaBooleanConstructor},
		"java/lang/Character.<init>(C)V":      {Words: 2, Implementat: javaCharacterConstructor},
		"java/lang/Character.isLowerCase(C)Z": {Words: 1, Implementat: javaCharacterIsLowerCase},
		"java/lang/Character.isUpperCase(C)Z": {Words: 1, Implementat: javaCharacterIsUpperCase},
		"java/lang/Character.isDigit(C)Z":     {Words: 1, Implementat: javaCharacterIsDigit},
		"java/lang/Character.toLowerCase(C)C": {Words: 1, Implementat: javaCharacterToLowerCase},
		"java/lang/Character.toUpperCase(C)C": {Words: 1, Implementat: javaCharacterToUpperCase},
		"java/lang/Character.digit(CI)I":      {Words: 2, Implementat: javaCharacterDigit},

		"java/io/ByteArrayInputStream.<init>([BII)V": {Words: 4, Implementat: javaByteStreamRangeConstructor},
		"java/io/InputStreamReader.<init>(Ljava/io/InputStream;Ljava/lang/String;)V": {
			Words: 3, Implementat: javaReaderConstructorIn},
		"java/io/DataInputStream.readUTF(Ljava/io/DataInput;)Ljava/lang/String;": {
			Words: 1, Implementat: javaDataInputReadUTF},
		"java/io/Reader.<init>()V":                   {Words: 1, Implementat: javaReaderBaseConstructor},
		"java/io/Reader.<init>(Ljava/lang/Object;)V": {Words: 2, Implementat: javaReaderBaseConstructor},
		"java/io/Writer.<init>()V":                   {Words: 1, Implementat: javaWriterBaseConstructor},
		"java/io/Writer.<init>(Ljava/lang/Object;)V": {Words: 2, Implementat: javaWriterBaseConstructor},
		"java/io/OutputStreamWriter.<init>(Ljava/io/OutputStream;)V": {
			Words: 2, Implementat: javaStreamWriterConstructor},
		"java/io/OutputStreamWriter.<init>(Ljava/io/OutputStream;Ljava/lang/String;)V": {
			Words: 3, Implementat: javaStreamWriterConstructor},
	} {
		if _, duplicate := javaPlatformMethods[key]; duplicate {
			panic("LGT java platform method declared twice: " + key)
		}
		javaPlatformMethods[key] = method
	}

	slot := func(called string, words int, implementation func(*Client, context.Context, *armcore.Thread, []uint32) (uint32, error)) javaBakedSlot {
		return javaBakedSlot{Called: called, Method: javaPlatformMethod{Words: words, Implementat: implementation}}
	}
	numberSlots := func(value string, bits int) map[uint32]javaBakedSlot {
		return map[uint32]javaBakedSlot{
			javaHashCodeSlot: slot("hashCode()I", 1, javaNumberHashCode),
			javaEqualsSlot:   slot("equals(Ljava/lang/Object;)Z", 2, javaNumberEquals),
			javaToStringSlot: slot("toString()Ljava/lang/String;", 1, javaNumberToString),
			10:               slot(value, 1, javaNumberValue(bits)),
		}
	}
	integerSlots := numberSlots("byteValue()B", 8)
	integerSlots[11] = slot("shortValue()S", 1, javaNumberValue(16))
	integerSlots[12] = slot("intValue()I", 1, javaNumberValue(32))
	integerSlots[13] = slot("longValue()J", 1, javaNumberValue(64))

	for class, slots := range map[string]map[uint32]javaBakedSlot{
		javaObjectClass: {
			javaHashCodeSlot: slot("hashCode()I", 1, javaObjectHashCode),
			javaEqualsSlot:   slot("equals(Ljava/lang/Object;)Z", 2, javaObjectEquals),
			javaToStringSlot: slot("toString()Ljava/lang/String;", 1, javaObjectToString),
			// notifyAll is notify's answer for the same reason: there is no
			// queue of waiting threads to move anything out of (java_api.go).
			6: slot("notifyAll()V", 1, javaNoResult),
			8: {Called: "wait(JI)V", Method: javaPlatformMethod{
				Words: 4, Implementat: javaObjectWaitNanos, Parks: javaRemainderWait}},
		},
		javaStringClass: {
			javaHashCodeSlot: slot("hashCode()I", 1, javaStringHashCode),
			javaToStringSlot: slot("toString()Ljava/lang/String;", 1, javaStringToString),
			12:               slot("getChars(II[CI)V", 5, javaTextGetChars),
			13:               slot("getBytes(Ljava/lang/String;)[B", 2, javaStringBytesIn),
			15:               slot("equalsIgnoreCase(Ljava/lang/String;)Z", 2, javaStringEqualsIgnoreCase),
			17:               slot("regionMatches(ZILjava/lang/String;II)Z", 6, javaStringRegionMatches),
			18:               slot("startsWith(Ljava/lang/String;I)Z", 3, javaStringStartsWithAt),
			20:               slot("endsWith(Ljava/lang/String;)Z", 2, javaStringEndsWith),
			23:               slot("lastIndexOf(I)I", 2, javaStringLastIndexOfChar),
			24:               slot("lastIndexOf(II)I", 3, javaStringLastIndexOfChar),
			25:               slot("indexOf(Ljava/lang/String;)I", 2, javaStringIndexOfText),
			30:               slot("replace(CC)Ljava/lang/String;", 3, javaStringReplace),
			31:               slot("toLowerCase()Ljava/lang/String;", 1, javaStringCase(false)),
			32:               slot("toUpperCase()Ljava/lang/String;", 1, javaStringCase(true)),
			// The last method the page declares, one past toCharArray's anchor
			// at 34; see java_string_intern.go.
			35: slot("intern()Ljava/lang/String;", 1, javaStringIntern),
		},
		javaStringBufferClass: {
			11: slot("capacity()I", 1, javaBufferCapacityOf),
			12: slot("ensureCapacity(I)V", 2, javaBufferEnsureCapacity),
			14: slot("charAt(I)C", 2, javaBufferCharAt),
			15: slot("getChars(II[CI)V", 5, javaTextGetChars),
			16: slot("setCharAt(IC)V", 3, javaBufferSetCharAt),
			// Slot 17 sits one before the String append and takes one reference
			// that is not a String: the call site hands it the exception it just
			// caught, on its way to a line it prints. `append(Object)` is what the
			// specification declares there.
			17: slot("append(Ljava/lang/Object;)Ljava/lang/StringBuffer;", 2, javaBufferAppendObject),
			19: slot("append([C)Ljava/lang/StringBuffer;", 2, javaBufferAppendChars),
			20: slot("append([CII)Ljava/lang/StringBuffer;", 4, javaBufferAppendChars),
			24: slot("append(J)Ljava/lang/StringBuffer;", 3, javaBufferAppendLong),
			28: slot("deleteCharAt(I)Ljava/lang/StringBuffer;", 2, javaBufferDeleteCharAt),
			29: slot("insert(ILjava/lang/Object;)Ljava/lang/StringBuffer;", 3, javaBufferInsertObject),
			31: slot("insert(I[C)Ljava/lang/StringBuffer;", 3, javaBufferInsertChars),
			32: slot("insert(IZ)Ljava/lang/StringBuffer;", 3, javaBufferInsertBoolean),
			33: slot("insert(IC)Ljava/lang/StringBuffer;", 3, javaBufferInsertChar),
			35: slot("insert(IJ)Ljava/lang/StringBuffer;", 4, javaBufferInsertLong),
			38: slot("reverse()Ljava/lang/StringBuffer;", 1, javaBufferReverse),
		},
		javaVectorClass: {
			javaToStringSlot: slot("toString()Ljava/lang/String;", 1, javaVectorToString),
			11:               slot("trimToSize()V", 1, javaVectorTrimToSize),
			12:               slot("ensureCapacity(I)V", 2, javaVectorEnsureCapacity),
			13:               slot("setSize(I)V", 2, javaVectorSetSize),
			14:               slot("capacity()I", 1, javaVectorCapacity),
			17:               slot("elements()Ljava/util/Enumeration;", 1, javaVectorElements),
			18:               slot("contains(Ljava/lang/Object;)Z", 2, javaVectorContains),
			// Slot 19 takes the receiver and one reference and its answer is kept.
			// **One call site cannot choose between `indexOf`, `contains` and
			// `removeElement`**, which all take one reference and answer something,
			// and this is where the numbering above earns its keep: `size` at 15
			// and `addElement` at 29 are fourteen apart, which is exactly what this
			// class's declaration order puts between them, and the same order puts
			// `contains` at 18, `indexOf(Object)` at 19 and `removeElement` at 30.
			//
			// The site agrees with the number rather than merely allowing it. It
			// dispatches this slot on a vector of strings with a string that is in
			// it, and hands the answer straight to `new char[n][]` and
			// `new int[n][3]` — a count of rows, which `indexOf` answers and a
			// boolean does not.
			19: slot("indexOf(Ljava/lang/Object;)I", 2, javaVectorIndexOf),
			20: slot("indexOf(Ljava/lang/Object;I)I", 3, javaVectorIndexOf),
			21: slot("lastIndexOf(Ljava/lang/Object;)I", 2, javaVectorLastIndexOf),
			22: slot("lastIndexOf(Ljava/lang/Object;I)I", 3, javaVectorLastIndexOf),
			25: slot("lastElement()Ljava/lang/Object;", 1, javaVectorLast),
			30: slot("removeElement(Ljava/lang/Object;)Z", 2, javaVectorRemoveElement),
		},
		javaStackClass: {
			34: slot("peek()Ljava/lang/Object;", 1, javaStackPeek),
			36: slot("search(Ljava/lang/Object;)I", 2, javaStackSearch),
		},
		javaHashtableClass: {
			javaToStringSlot: slot("toString()Ljava/lang/String;", 1, javaHashtableToString),
			10:               slot("size()I", 1, javaHashtableSize),
			11:               slot("isEmpty()Z", 1, javaHashtableEmpty),
			12:               slot("keys()Ljava/util/Enumeration;", 1, javaHashtableEnumerate(true)),
			13:               slot("elements()Ljava/util/Enumeration;", 1, javaHashtableEnumerate(false)),
			14:               slot("contains(Ljava/lang/Object;)Z", 2, javaHashtableContains),
			15:               slot("containsKey(Ljava/lang/Object;)Z", 2, javaHashtableContainsKey),
			16:               slot("get(Ljava/lang/Object;)Ljava/lang/Object;", 2, javaHashtableGet),
			17:               slot("rehash()V", 1, javaHashtableRehash),
			18:               slot("put(Ljava/lang/Object;Ljava/lang/Object;)Ljava/lang/Object;", 3, javaHashtablePut),
			19:               slot("remove(Ljava/lang/Object;)Ljava/lang/Object;", 2, javaHashtableRemove),
			20:               slot("clear()V", 1, javaHashtableClear),
		},
		javaVectorEnumeratorClass:    javaEnumerationSlots,
		javaHashtableEnumeratorClass: javaEnumerationSlots,
		javaIntegerClass:             integerSlots,
		javaLongClass:                numberSlots("longValue()J", 64),
		javaShortClass:               numberSlots("shortValue()S", 16),
		javaByteClass:                numberSlots("byteValue()B", 8),
		javaInputStreamReaderClass: {
			10: slot("read()I", 1, javaReaderReadChar),
			12: slot("read([CII)I", 4, javaReaderRead),
			13: slot("skip(J)J", 3, javaReaderSkip),
			14: slot("ready()Z", 1, javaReaderReady),
			15: slot("markSupported()Z", 1, javaStreamMarkSupported),
			16: slot("mark(I)V", 2, javaReaderMark),
			17: slot("reset()V", 1, javaStreamReset),
		},
		javaDataInputStreamClass: {
			26: slot("readUnsignedShort()I", 1, javaStreamReadUnsignedShort),
		},
		// Thread's run between start at 10 and interrupt; getPriority one past
		// setPriority's anchor at 14. See java_thread.go.
		javaThreadClass: {
			11: slot("run()V", 1, javaThreadRun),
			15: slot("getPriority()I", 1, javaThreadGetPriority),
			17: {Called: "join()V", Method: javaPlatformMethod{
				Words: 1, Implementat: javaThreadJoin, Parks: javaRemainderJoin}},
		},
		// Date declares getTime and setTime, then equals, hashCode and
		// toString over Object's; getTime at 10 is the anchor. See
		// java_calendar.go.
		javaDateClass: {
			javaHashCodeSlot: slot("hashCode()I", 1, javaDateHashCode),
			javaEqualsSlot:   slot("equals(Ljava/lang/Object;)Z", 2, javaDateEquals),
			11:               slot("setTime(J)V", 3, javaDateSetTime),
		},
		// setSeed, next, nextInt, nextInt(int): one past the nextInt anchor at
		// 12. See java_random.go.
		"java/util/Random": {
			13: slot("nextInt(I)I", 2, javaRandomNextBounded),
		},
		// The sink's text, over Object's toString; and the data stream's
		// writeChars, between writeDouble and the writeUTF at 24. See
		// java_stream.go.
		javaByteSinkClass: {
			javaToStringSlot: slot("toString()Ljava/lang/String;", 1, javaByteSinkToString),
		},
		javaDataOutputStreamClass: {
			23: slot("writeChars(Ljava/lang/String;)V", 2, javaSinkWriteChars),
		},
		// The bodies the abstract stream bases give a title's own subclass:
		// Reader's at InputStreamReader's served positions, OutputStream's at
		// the ones ByteArrayOutputStream and DataOutputStream serve. The two
		// methods each declares abstract — Reader's read([CII) and close,
		// OutputStream's write(int) — have no body to serve. See java_reader.go
		// and java_output_stream.go.
		javaReaderClass: {
			10: slot("read()I", 1, javaReaderBaseRead),
			11: slot("read([C)I", 2, javaReaderBaseReadArray),
			13: slot("skip(J)J", 3, javaReaderBaseSkip),
			14: slot("ready()Z", 1, javaReaderBaseReady),
			15: slot("markSupported()Z", 1, javaReaderBaseMarkSupported),
			16: slot("mark(I)V", 2, javaReaderBaseMark),
			17: slot("reset()V", 1, javaReaderBaseReset),
		},
		javaOutputStreamClass: {
			11: slot("write([B)V", 2, javaOutputStreamBaseWriteAll),
			12: slot("write([BII)V", 4, javaOutputStreamBaseWriteRange),
			13: slot("flush()V", 1, javaOutputStreamBaseFlush),
			14: slot("close()V", 1, javaOutputStreamBaseClose),
		},
		// The character sink and the bridge onto a byte stream; see
		// java_writer.go.
		javaWriterClass: {
			10: slot("write(I)V", 2, javaWriterBaseWriteChar),
			11: slot("write([C)V", 2, javaWriterBaseWriteChars),
			13: slot("write(Ljava/lang/String;)V", 2, javaWriterBaseWriteText),
			14: slot("write(Ljava/lang/String;II)V", 4, javaWriterBaseWriteTextRange),
		},
		javaStreamWriterClass: {
			10: slot("write(I)V", 2, javaStreamWriterWriteChar),
			12: slot("write([CII)V", 4, javaStreamWriterWriteChars),
			14: slot("write(Ljava/lang/String;II)V", 4, javaStreamWriterWriteText),
			15: slot("flush()V", 1, javaStreamWriterFlush),
			16: slot("close()V", 1, javaStreamWriterClose),
		},
		// The two boxes beside the numbers; see java_boolean.go and
		// java_character.go.
		javaBooleanClass: {
			javaHashCodeSlot: slot("hashCode()I", 1, javaBooleanHashCode),
			javaEqualsSlot:   slot("equals(Ljava/lang/Object;)Z", 2, javaBooleanEquals),
			javaToStringSlot: slot("toString()Ljava/lang/String;", 1, javaBooleanToString),
			10:               slot("booleanValue()Z", 1, javaBooleanValue),
		},
		javaCharacterClass: {
			javaHashCodeSlot: slot("hashCode()I", 1, javaCharacterHashCode),
			javaEqualsSlot:   slot("equals(Ljava/lang/Object;)Z", 2, javaCharacterEquals),
			javaToStringSlot: slot("toString()Ljava/lang/String;", 1, javaCharacterToString),
			10:               slot("charValue()C", 1, javaCharacterValue),
		},
		javaClassClass: {
			10: slot("newInstance()Ljava/lang/Object;", 1, javaClassNewInstance),
			15: slot("getName()Ljava/lang/String;", 1, javaClassGetName),
		},
		"java/lang/Throwable": {
			javaToStringSlot: slot("toString()Ljava/lang/String;", 1, javaThrowableToString),
			10:               slot("getMessage()Ljava/lang/String;", 1, javaThrowableGetMessage),
			11:               slot("printStackTrace()V", 1, javaThrowablePrintStackTrace),
		},
		"java/lang/Runtime": {
			10: slot("exit(I)V", 2, javaRuntimeExit),
		},
	} {
		served := javaBakedVirtualSlots[class]
		if served == nil {
			served = map[uint32]javaBakedSlot{}
			javaBakedVirtualSlots[class] = served
		}
		for index, entry := range slots {
			if _, duplicate := served[index]; duplicate {
				panic(fmt.Sprintf("LGT java %s vtable slot %d declared twice", class, index))
			}
			served[index] = entry
		}
	}

	javaPlatformStaticFillers[javaBooleanClass] = (*Client).initializeJavaBooleanStatics
	javaPlatformStaticFillers[javaCharacterClass] = (*Client).initializeJavaCharacterStatics
}

// javaPlatformStaticFillers are the platform classes whose static fields hold
// values of their own rather than one of javaPlatformStatics' singletons —
// Boolean's two instances, Character's four constants — and what fills them
// when the class is laid out (initializePlatformStatics).
var javaPlatformStaticFillers = map[string]func(*Client, *javaRuntimeClass) error{}

// javaPlatformStaticWord answers where in a platform class's storage the
// module reads one of its static fields, when the module asked the load call
// to number that field. A field it never asked for is one nothing reads, and is
// left alone, the way initializePlatformStatics leaves one.
func (client *Client) javaPlatformStaticWord(class *javaRuntimeClass, name, descriptor string) (uint32, bool) {
	if client.javaLink == nil || client.javaLink.layout == nil || class.dataBlock == 0 {
		return 0, false
	}
	laid, ok := client.javaLink.layout.classes[class.Name]
	if !ok {
		return 0, false
	}
	slot, declared := laid.Statics[name+descriptor]
	if !declared || slot >= class.StaticWords {
		return 0, false
	}
	return class.dataBlock + (javaClassDataWords+slot)*4, true
}

// javaClassReaches reports whether a class is the named one or below it, which
// is the `instanceof` test (javaTypeCheck) asked of a class rather than of a
// word a call site handed over.
func (client *Client) javaClassReaches(class *javaRuntimeClass, name string) bool {
	for step := class; step != nil; step = client.javaSuperOf(step) {
		if step.Name == name {
			return true
		}
	}
	return false
}

// javaObjectIs is javaClassReaches for an object: false for null and for a
// word this platform never issued as an object.
func (client *Client) javaObjectIs(object uint32, name string) bool {
	class, known := client.javaClassOfObject(object)
	return known && client.javaClassReaches(class, name)
}
