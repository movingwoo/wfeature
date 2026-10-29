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
		javaThreadClass: {
			17: {Called: "join()V", Method: javaPlatformMethod{
				Words: 1, Implementat: javaThreadJoin, Parks: javaRemainderJoin}},
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
}
