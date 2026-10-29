package lgt

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
)

// javaDeclarationOrder is each class's instance methods in the order the
// specification's CLDC class page declares them, with its superclass. Object's
// own take slots 1 to 9; every other class's new methods take slots from where
// its superclass's run ends, and a method it redeclares keeps the slot above.
var javaDeclarationOrder = map[string]struct {
	super   string
	methods []string
}{
	"java/lang/Object": {"", []string{"getClass()Ljava/lang/Class;", "hashCode()I", "equals(Ljava/lang/Object;)Z",
		"toString()Ljava/lang/String;", "notify()V", "notifyAll()V", "wait(J)V", "wait(JI)V", "wait()V"}},
	"java/lang/String": {"java/lang/Object", []string{"length()I", "charAt(I)C", "getChars(II[CI)V",
		"getBytes(Ljava/lang/String;)[B", "getBytes()[B", "equals(Ljava/lang/Object;)Z",
		"equalsIgnoreCase(Ljava/lang/String;)Z", "compareTo(Ljava/lang/String;)I",
		"regionMatches(ZILjava/lang/String;II)Z", "startsWith(Ljava/lang/String;I)Z", "startsWith(Ljava/lang/String;)Z",
		"endsWith(Ljava/lang/String;)Z", "hashCode()I", "indexOf(I)I", "indexOf(II)I", "lastIndexOf(I)I",
		"lastIndexOf(II)I", "indexOf(Ljava/lang/String;)I", "indexOf(Ljava/lang/String;I)I",
		"substring(I)Ljava/lang/String;", "substring(II)Ljava/lang/String;", "concat(Ljava/lang/String;)Ljava/lang/String;",
		"replace(CC)Ljava/lang/String;", "toLowerCase()Ljava/lang/String;", "toUpperCase()Ljava/lang/String;",
		"trim()Ljava/lang/String;", "toString()Ljava/lang/String;", "toCharArray()[C", "intern()Ljava/lang/String;"}},
	"java/lang/StringBuffer": {"java/lang/Object", []string{"length()I", "capacity()I", "ensureCapacity(I)V",
		"setLength(I)V", "charAt(I)C", "getChars(II[CI)V", "setCharAt(IC)V",
		"append(Ljava/lang/Object;)Ljava/lang/StringBuffer;", "append(Ljava/lang/String;)Ljava/lang/StringBuffer;",
		"append([C)Ljava/lang/StringBuffer;", "append([CII)Ljava/lang/StringBuffer;", "append(Z)Ljava/lang/StringBuffer;",
		"append(C)Ljava/lang/StringBuffer;", "append(I)Ljava/lang/StringBuffer;", "append(J)Ljava/lang/StringBuffer;",
		"append(F)Ljava/lang/StringBuffer;", "append(D)Ljava/lang/StringBuffer;", "delete(II)Ljava/lang/StringBuffer;",
		"deleteCharAt(I)Ljava/lang/StringBuffer;", "insert(ILjava/lang/Object;)Ljava/lang/StringBuffer;",
		"insert(ILjava/lang/String;)Ljava/lang/StringBuffer;", "insert(I[C)Ljava/lang/StringBuffer;",
		"insert(IZ)Ljava/lang/StringBuffer;", "insert(IC)Ljava/lang/StringBuffer;", "insert(II)Ljava/lang/StringBuffer;",
		"insert(IJ)Ljava/lang/StringBuffer;", "insert(IF)Ljava/lang/StringBuffer;", "insert(ID)Ljava/lang/StringBuffer;",
		"reverse()Ljava/lang/StringBuffer;", "toString()Ljava/lang/String;"}},
	"java/util/Vector": {"java/lang/Object", []string{"copyInto([Ljava/lang/Object;)V", "trimToSize()V",
		"ensureCapacity(I)V", "setSize(I)V", "capacity()I", "size()I", "isEmpty()Z", "elements()Ljava/util/Enumeration;",
		"contains(Ljava/lang/Object;)Z", "indexOf(Ljava/lang/Object;)I", "indexOf(Ljava/lang/Object;I)I",
		"lastIndexOf(Ljava/lang/Object;)I", "lastIndexOf(Ljava/lang/Object;I)I", "elementAt(I)Ljava/lang/Object;",
		"firstElement()Ljava/lang/Object;", "lastElement()Ljava/lang/Object;", "setElementAt(Ljava/lang/Object;I)V",
		"removeElementAt(I)V", "insertElementAt(Ljava/lang/Object;I)V", "addElement(Ljava/lang/Object;)V",
		"removeElement(Ljava/lang/Object;)Z", "removeAllElements()V", "toString()Ljava/lang/String;"}},
	"java/util/Stack": {"java/util/Vector", []string{"push(Ljava/lang/Object;)Ljava/lang/Object;",
		"pop()Ljava/lang/Object;", "peek()Ljava/lang/Object;", "empty()Z", "search(Ljava/lang/Object;)I"}},
	"java/util/Hashtable": {"java/lang/Object", []string{"size()I", "isEmpty()Z", "keys()Ljava/util/Enumeration;",
		"elements()Ljava/util/Enumeration;", "contains(Ljava/lang/Object;)Z", "containsKey(Ljava/lang/Object;)Z",
		"get(Ljava/lang/Object;)Ljava/lang/Object;", "rehash()V",
		"put(Ljava/lang/Object;Ljava/lang/Object;)Ljava/lang/Object;", "remove(Ljava/lang/Object;)Ljava/lang/Object;",
		"clear()V", "toString()Ljava/lang/String;"}},
	"java/util/VectorEnumerator":    {"java/lang/Object", []string{"hasMoreElements()Z", "nextElement()Ljava/lang/Object;"}},
	"java/util/HashtableEnumerator": {"java/lang/Object", []string{"hasMoreElements()Z", "nextElement()Ljava/lang/Object;"}},
	"java/io/InputStream": {"java/lang/Object", []string{"read()I", "read([B)I", "read([BII)I", "skip(J)J",
		"available()I", "close()V", "mark(I)V", "reset()V", "markSupported()Z"}},
	"java/io/DataInputStream": {"java/io/InputStream", []string{"read([B)I", "read([BII)I", "readFully([B)V",
		"readFully([BII)V", "skipBytes(I)I", "readBoolean()Z", "readByte()B", "readUnsignedByte()I", "readShort()S",
		"readUnsignedShort()I", "readChar()C", "readInt()I", "readLong()J", "readFloat()F", "readDouble()D",
		"readUTF()Ljava/lang/String;"}},
	"java/io/Reader": {"java/lang/Object", []string{"read()I", "read([C)I", "read([CII)I", "skip(J)J", "ready()Z",
		"markSupported()Z", "mark(I)V", "reset()V", "close()V"}},
	"java/io/InputStreamReader": {"java/io/Reader", []string{"read()I", "read([CII)I", "skip(J)J", "ready()Z",
		"markSupported()Z", "mark(I)V", "reset()V", "close()V"}},
	"java/lang/Thread": {"java/lang/Object", []string{"start()V", "run()V", "interrupt()V", "isAlive()Z",
		"setPriority(I)V", "getPriority()I", "getName()Ljava/lang/String;", "join()V", "toString()Ljava/lang/String;"}},
	"java/lang/Class": {"java/lang/Object", []string{"newInstance()Ljava/lang/Object;",
		"isInstance(Ljava/lang/Object;)Z", "isAssignableFrom(Ljava/lang/Class;)Z", "isInterface()Z", "isArray()Z",
		"getName()Ljava/lang/String;", "getResourceAsStream(Ljava/lang/String;)Ljava/io/InputStream;",
		"toString()Ljava/lang/String;"}},
	"java/lang/Throwable": {"java/lang/Object", []string{"getMessage()Ljava/lang/String;",
		"toString()Ljava/lang/String;", "printStackTrace()V"}},
	"java/lang/Integer": {"java/lang/Object", []string{"byteValue()B", "shortValue()S", "intValue()I", "longValue()J",
		"floatValue()F", "doubleValue()D", "toString()Ljava/lang/String;", "hashCode()I", "equals(Ljava/lang/Object;)Z"}},
	"java/lang/Long": {"java/lang/Object", []string{"longValue()J", "floatValue()F", "doubleValue()D",
		"toString()Ljava/lang/String;", "hashCode()I", "equals(Ljava/lang/Object;)Z"}},
	"java/lang/Short": {"java/lang/Object", []string{"shortValue()S", "toString()Ljava/lang/String;", "hashCode()I",
		"equals(Ljava/lang/Object;)Z"}},
	"java/lang/Byte": {"java/lang/Object", []string{"byteValue()B", "toString()Ljava/lang/String;", "hashCode()I",
		"equals(Ljava/lang/Object;)Z"}},
	"java/lang/Runtime": {"java/lang/Object", []string{"exit(I)V", "freeMemory()J", "totalMemory()J", "gc()V"}},
	"java/util/Random": {"java/lang/Object", []string{"setSeed(J)V", "next(I)I", "nextInt()I", "nextInt(I)I",
		"nextLong()J", "nextFloat()F", "nextDouble()D"}},
}

// javaDeclaredSlots numbers a class's methods by the rule.
func javaDeclaredSlots(t *testing.T, class string) (map[string]uint32, uint32) {
	t.Helper()
	declared, ok := javaDeclarationOrder[class]
	if !ok {
		t.Fatalf("no declaration order for %s", class)
	}
	slots, next := map[string]uint32{}, uint32(1)
	if declared.super != "" {
		var inherited map[string]uint32
		inherited, next = javaDeclaredSlots(t, declared.super)
		for key, slot := range inherited {
			slots[key] = slot
		}
		if next < 10 {
			next = 10
		}
	}
	for _, method := range declared.methods {
		if _, override := slots[method]; override {
			continue
		}
		slots[method] = next
		next++
	}
	return slots, next
}

// Every slot this platform serves on these classes is the one the
// specification's declaration order gives the method it is named for. The
// anchors call sites settled were the evidence for the rule; this is what
// keeps every slot added by the rule on it.
func TestJavaBakedSlotsFollowTheDeclarationOrder(t *testing.T) {
	checked := 0
	for class := range javaDeclarationOrder {
		slots, _ := javaDeclaredSlots(t, class)
		for index, baked := range javaBakedVirtualSlots[class] {
			if !strings.Contains(baked.Called, "(") {
				// A slot named by what its call site does rather than by a
				// signature; DataInputStream 25 is the one.
				continue
			}
			want, declared := slots[baked.Called]
			if !declared {
				t.Errorf("%s slot %d serves %s, which the class does not declare", class, index, baked.Called)
				continue
			}
			if want != index {
				t.Errorf("%s serves %s at slot %d; the declaration order puts it at %d", class, baked.Called, index, want)
			}
			checked++
		}
	}
	if checked < 150 {
		t.Fatalf("checked %d slots; the table is smaller than it should be", checked)
	}
}

// slotCall runs one served slot of a class as a call through it would.
func slotCall(t *testing.T, client *Client, class string, slot uint32, arguments ...uint32) (uint32, error) {
	t.Helper()
	baked, ok := javaBakedVirtualSlots[class][slot]
	if !ok {
		t.Fatalf("%s slot %d is not served", class, slot)
	}
	if baked.Method.Words != len(arguments) {
		t.Fatalf("%s.%s takes %d words and the test passed %d", class, baked.Called, baked.Method.Words, len(arguments))
	}
	return baked.Method.Implementat(client, context.Background(), client.thread, arguments)
}

func mustSlot(t *testing.T, client *Client, class string, slot uint32, arguments ...uint32) uint32 {
	t.Helper()
	answer, err := slotCall(t, client, class, slot, arguments...)
	if err != nil {
		t.Fatalf("%s slot %d: %v", class, slot, err)
	}
	return answer
}

func mustText(t *testing.T, client *Client, object uint32) string {
	t.Helper()
	text, ok := client.javaText(object)
	if !ok {
		t.Fatalf("the answer at %#x holds no text", object)
	}
	return text
}

// installGuestClass builds a class the title declares, rooted at Object, with
// some of its slots filled by code of its own, and one instance of it.
func installGuestClass(t *testing.T, client *Client, name string, bodies map[uint32]uint32) uint32 {
	t.Helper()
	object, err := client.preparePlatformJavaClass(javaObjectClass)
	if err != nil {
		t.Fatal(err)
	}
	class := &javaRuntimeClass{Name: name, Handle: 1, Slots: object.Slots, Instance: 4, Super: object,
		Record: javaClass{Name: name}}
	classObject, err := client.allocateJavaClassObject(class)
	if err != nil {
		t.Fatal(err)
	}
	class.Object = classObject
	class.dataBlock, _ = client.readWord(classObject + 8)
	runtime := client.javaRuntimeState()
	runtime.byObject[classObject] = class
	runtime.byName[name] = class
	if err := client.buildJavaVTable(class); err != nil {
		t.Fatal(err)
	}
	for slot, body := range bodies {
		if err := client.writeWord(class.VTable+4+slot*4, body); err != nil {
			t.Fatal(err)
		}
	}
	instance, err := client.allocateJavaObject(class)
	if err != nil {
		t.Fatal(err)
	}
	return instance
}

// Object's own forms answer for a class that overrides nothing: its identity,
// itself and nothing else, and its name with the identity's hash. A library
// class that declares its own form this platform lacks is refused by name.
func TestJavaObjectDefaultsAnswerOnlyForAClassThatOverridesNothing(t *testing.T) {
	client := fixtureClient(t)
	ctx := context.Background()
	guest := installGuestClass(t, client, "game/Thing", nil)
	other := installGuestClass(t, client, "game/Other", nil)

	hash, err := client.javaCallVirtual(ctx, client.thread, guest, javaHashCodeSlot)
	if err != nil || hash != guest {
		t.Fatalf("hashCode() = %#x, %v, want the identity %#x", hash, err, guest)
	}
	if hash, _ := javaSystemIdentityHash(client, ctx, client.thread, []uint32{guest}); hash != guest {
		t.Errorf("System.identityHashCode = %#x, want %#x", hash, guest)
	}
	for _, probe := range []struct {
		other uint32
		want  uint32
	}{{guest, javaTrue}, {other, javaFalse}, {0, javaFalse}} {
		if equal, err := client.javaCallVirtual(ctx, client.thread, guest, javaEqualsSlot, probe.other); err != nil || equal != probe.want {
			t.Errorf("equals(%#x) = %d, %v, want %d", probe.other, equal, err, probe.want)
		}
	}
	text, err := client.javaToStringOf(ctx, client.thread, guest)
	if err != nil {
		t.Fatal(err)
	}
	if want := fmt.Sprintf("game.Thing@%x", guest); text != want {
		t.Errorf("toString() = %q, want %q", text, want)
	}

	thread, err := newTestObject(t, client, javaThreadClass)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.javaCallVirtual(ctx, client.thread, thread, javaToStringSlot); err == nil ||
		!strings.Contains(err.Error(), "java/lang/Thread declares its own toString") {
		t.Errorf("a Thread's toString = %v, want it refused by name", err)
	}
	// The label an append builds keeps the name it had before toString was
	// asked at all, rather than stopping the title.
	if text, err := client.javaToStringOf(ctx, client.thread, thread); err != nil ||
		text != fmt.Sprintf("%s@%x", javaThreadClass, thread) {
		t.Errorf("String.valueOf(a Thread) = %q, %v, want its earlier label", text, err)
	}
	// So does a title's own Thread subclass, the shape a game loop has.
	threadClass, err := client.preparePlatformJavaClass(javaThreadClass)
	if err != nil {
		t.Fatal(err)
	}
	loop := &javaRuntimeClass{Name: "game/Loop", Handle: 1, Slots: threadClass.Slots, Instance: 4,
		Super: threadClass, Record: javaClass{Name: "game/Loop"}}
	if loop.Object, err = client.allocateJavaClassObject(loop); err != nil {
		t.Fatal(err)
	}
	loop.dataBlock, _ = client.readWord(loop.Object + 8)
	client.javaRuntimeState().byObject[loop.Object] = loop
	client.javaRuntimeState().byName[loop.Name] = loop
	if err := client.buildJavaVTable(loop); err != nil {
		t.Fatal(err)
	}
	instance, err := client.allocateJavaObject(loop)
	if err != nil {
		t.Fatal(err)
	}
	buffer := newTestBuffer(t, client, "at ")
	if _, err := javaBufferAppendObject(client, ctx, client.thread, []uint32{buffer, instance}); err != nil {
		t.Fatalf("append(a Thread subclass) = %v, want its label", err)
	}
	if text := mustText(t, client, buffer); text != fmt.Sprintf("at game/Loop@%x", instance) {
		t.Errorf("append(a Thread subclass) built %q", text)
	}
	date, err := newTestObject(t, client, javaDateClass)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.javaHashCodeOf(ctx, client.thread, date); err == nil {
		t.Error("a Date's hashCode was answered with Object's")
	}
}

// A slot the title filled is its own code, and the platform asks it the way a
// call site would: a key whose class overrides hashCode and equals is filed
// and found the way the title says.
func TestJavaCallVirtualRunsTheTitlesOverride(t *testing.T) {
	client := fixtureClient(t)
	ctx := context.Background()
	// hashCode: movs r0, #7; bx lr. equals: movs r0, #1; bx lr.
	hash := installThumbAt(t, client, 0, 0x2007, 0x4770)
	equal := installThumbAt(t, client, 8, 0x2001, 0x4770)
	first := installGuestClass(t, client, "game/Key", map[uint32]uint32{javaHashCodeSlot: hash, javaEqualsSlot: equal})
	second, err := client.allocateJavaObject(client.javaRuntimeState().byName["game/Key"])
	if err != nil {
		t.Fatal(err)
	}
	if answer, err := client.javaHashCodeOf(ctx, client.thread, first); err != nil || answer != 7 {
		t.Fatalf("the title's hashCode answered %d, %v, want 7", answer, err)
	}
	table := newTestHashtable(t, client)
	value := newTestString(t, client, "kept")
	mustSlot(t, client, javaHashtableClass, 18, table, first, value)
	// A different object the title calls equal is the same key.
	if got := mustSlot(t, client, javaHashtableClass, 16, table, second); got != value {
		t.Errorf("get by an equal key = %#x, want %#x", got, value)
	}
}

// System.getProperty answers the CLDC table, then the handset's, and null for
// a name neither has; exit ends the session as notifyDestroyed does.
func TestJavaSystemPropertyAndExit(t *testing.T) {
	client := fixtureClient(t)
	ctx := context.Background()
	for _, probe := range []struct {
		name, want string
		known      bool
	}{
		{"microedition.configuration", "CLDC-1.0", true},
		{"microedition.encoding", "EUC-KR", true},
		{"MEDIADEVICES", "Yamaha_MA3", true},
		{"no.such.property", "", false},
	} {
		answer, err := javaSystemGetProperty(client, ctx, client.thread, []uint32{newTestString(t, client, probe.name)})
		if err != nil {
			t.Fatalf("getProperty(%q): %v", probe.name, err)
		}
		if !probe.known {
			if answer != 0 {
				t.Errorf("getProperty(%q) = %q, want null", probe.name, mustText(t, client, answer))
			}
			continue
		}
		if got := mustText(t, client, answer); got != probe.want {
			t.Errorf("getProperty(%q) = %q, want %q", probe.name, got, probe.want)
		}
	}
	if _, err := javaSystemGetProperty(client, ctx, client.thread, []uint32{0}); thrownClass(err) != javaThrowNullClass {
		t.Errorf("getProperty(null) = %v, want NullPointerException", err)
	}
	_, err := javaSystemExit(client, ctx, client.thread, []uint32{3})
	if !errors.Is(err, ErrGuestExited) || !client.exited {
		t.Fatalf("System.exit = %v, want the guest's own ending", err)
	}
}

// Class.forName answers a platform class by its dotted name and refuses one
// nothing declares; getName gives the dotted name back; newInstance builds a
// platform class through its own constructor and a title's through its record.
func TestJavaClassForNameNewInstanceAndGetName(t *testing.T) {
	client := fixtureClient(t)
	ctx := context.Background()
	classObject, err := javaClassForName(client, ctx, client.thread, []uint32{newTestString(t, client, "java.util.Vector")})
	if err != nil {
		t.Fatal(err)
	}
	if got := mustText(t, client, mustSlot(t, client, javaClassClass, 15, classObject)); got != "java.util.Vector" {
		t.Errorf("getName() = %q", got)
	}
	vector := mustSlot(t, client, javaClassClass, 10, classObject)
	if _, held := client.javaRuntimeState().vectors[vector]; !held {
		t.Error("newInstance of Vector did not construct one")
	}
	// The answer dispatches as a Class, which is what makes the two calls
	// above reach Class's slots from the title.
	if dispatched, err := client.javaCallVirtual(ctx, client.thread, classObject, 15); err != nil ||
		mustText(t, client, dispatched) != "java.util.Vector" {
		t.Errorf("getName through the class object's own table = %v", err)
	}
	if _, err := javaClassForName(client, ctx, client.thread, []uint32{newTestString(t, client, "com.example.Missing")}); thrownClass(err) != javaClassNotFoundClass {
		t.Errorf("forName of an unknown class = %v, want ClassNotFoundException", err)
	}

	// A title's class whose record names a no-argument constructor:
	// movs r0, #0; bx lr.
	constructor := installThumbAt(t, client, 16, 0x2000, 0x4770)
	guest := installGuestClass(t, client, "game/Made", nil)
	class := client.javaRuntimeState().byName["game/Made"]
	class.Record.Inline = true
	class.Record.Methods = []javaMember{{Name: "<init>", Descriptor: "()V", Body: constructor}}
	class.initialized = true
	made := mustSlot(t, client, javaClassClass, 10, class.Object)
	if found, ok := client.javaClassOfObject(made); !ok || found != class || made == guest {
		t.Errorf("newInstance answered %#x, not a new %s", made, class.Name)
	}
	class.Record.Methods = nil
	if _, err := slotCall(t, client, javaClassClass, 10, class.Object); thrownClass(err) != javaInstantiationClass {
		t.Errorf("newInstance of a class with no <init>()V = %v, want InstantiationException", err)
	}
}

// An exception the platform throws carries what it says went wrong as its
// message; toString is the dotted class name and the message.
func TestJavaThrowableMessageAndToString(t *testing.T) {
	client := fixtureClient(t)
	ctx := context.Background()
	err := client.throwJavaPlatform(client.thread, javaIOExceptionClass, ": the file is gone")
	var uncaught *javaUncaughtThrow
	if !errors.As(err, &uncaught) {
		t.Fatalf("the throw = %v", err)
	}
	if got := mustText(t, client, mustSlot(t, client, "java/lang/Throwable", 10, uncaught.Object)); got != "the file is gone" {
		t.Errorf("getMessage() = %q", got)
	}
	text, err := client.javaToStringOf(ctx, client.thread, uncaught.Object)
	if err != nil || text != "java.io.IOException: the file is gone" {
		t.Errorf("toString() = %q, %v", text, err)
	}
	if _, err := slotCall(t, client, "java/lang/Throwable", 11, uncaught.Object); err != nil {
		t.Errorf("printStackTrace() = %v", err)
	}
	bare, err := newTestObject(t, client, "java/lang/RuntimeException")
	if err != nil {
		t.Fatal(err)
	}
	if message := mustSlot(t, client, "java/lang/Throwable", 10, bare); message != 0 {
		t.Errorf("an exception with no message answered %q", mustText(t, client, message))
	}
	if text, _ := client.javaToStringOf(ctx, client.thread, bare); text != "java.lang.RuntimeException" {
		t.Errorf("toString() with no message = %q", text)
	}
}

// The numbers: a value of the class's width, equal only to the same class
// holding the same value, hashing the way the class library hashes it.
func TestJavaNumbers(t *testing.T) {
	client := fixtureClient(t)
	ctx := context.Background()
	newNumber := func(class, constructor string, words ...uint32) uint32 {
		object, err := newTestObject(t, client, class)
		if err != nil {
			t.Fatal(err)
		}
		mustJavaCall(t, client, class+".<init>("+constructor+")V", append([]uint32{object}, words...)...)
		return object
	}
	five := newNumber(javaIntegerClass, "I", 5)
	alsoFive := newNumber(javaIntegerClass, "I", 5)
	longFive := newNumber(javaLongClass, "J", 5, 0)
	negative := newNumber(javaIntegerClass, "I", uint32(0xffffff80))
	big := newNumber(javaLongClass, "J", 1, 2)

	if got := mustSlot(t, client, javaIntegerClass, 12, five); got != 5 {
		t.Errorf("intValue() = %d", got)
	}
	if got := mustSlot(t, client, javaIntegerClass, 10, negative); int32(got) != -128 {
		t.Errorf("byteValue() of -128 = %d", int32(got))
	}
	if got := mustSlot(t, client, javaLongClass, 10, big); got != 1 {
		t.Errorf("longValue() low word = %d", got)
	}
	if high, _ := client.thread.Register(1); high != 2 {
		t.Errorf("longValue() high word = %d, want 2", high)
	}
	if got := mustSlot(t, client, javaLongClass, javaHashCodeSlot, big); got != 3 {
		t.Errorf("Long.hashCode of (2<<32)|1 = %d, want 3", got)
	}
	if equal, _ := client.javaEqualsOf(ctx, client.thread, five, alsoFive); !equal {
		t.Error("two Integers of 5 are not equal")
	}
	if equal, _ := client.javaEqualsOf(ctx, client.thread, five, longFive); equal {
		t.Error("an Integer equals a Long")
	}
	if text, _ := client.javaToStringOf(ctx, client.thread, negative); text != "-128" {
		t.Errorf("toString() = %q", text)
	}
	if got := mustText(t, client, mustJavaCall(t, client, "java/lang/Integer.toHexString(I)Ljava/lang/String;", 0xffffffff)); got != "ffffffff" {
		t.Errorf("toHexString(-1) = %q", got)
	}
	if got := mustText(t, client, mustJavaCall(t, client, "java/lang/Integer.toString(II)Ljava/lang/String;", uint32(0xfffffff1), 16)); got != "-f" {
		t.Errorf("toString(-15, 16) = %q", got)
	}
	boxed := mustJavaCall(t, client, "java/lang/Integer.valueOf(Ljava/lang/String;)Ljava/lang/Integer;", newTestString(t, client, "42"))
	if equal, _ := client.javaEqualsOf(ctx, client.thread, boxed, newNumber(javaIntegerClass, "I", 42)); !equal {
		t.Error("Integer.valueOf(\"42\") is not an Integer of 42")
	}
	low := mustJavaCall(t, client, "java/lang/Long.parseLong(Ljava/lang/String;)J", newTestString(t, client, "-4294967296"))
	if high, _ := client.thread.Register(1); low != 0 || high != 0xffffffff {
		t.Errorf("parseLong(-2^32) = %#x:%#x", high, low)
	}
	if _, err := javaCall(t, client, "java/lang/Long.parseLong(Ljava/lang/String;)J", newTestString(t, client, "x")); thrownClass(err) != javaNumberFormatClass {
		t.Errorf("parseLong(\"x\") = %v, want NumberFormatException", err)
	}
}

// What the new tables hold survives a checkpoint: the tables come back as
// they were, bucket for bucket and element for element.
func TestJavaClassLibraryStateSurvivesACheckpoint(t *testing.T) {
	client := fixtureClient(t)
	table := newTestHashtable(t, client)
	for _, key := range []string{"a", "b", "c"} {
		mustSlot(t, client, javaHashtableClass, 18, table, newTestString(t, client, key), newTestString(t, client, key+key))
	}
	keys := mustSlot(t, client, javaHashtableClass, 12, table)
	mustSlot(t, client, javaHashtableEnumeratorClass, 11, keys)
	vector := newFixtureVector(t, client)
	mustSlot(t, client, javaVectorClass, 12, vector, 40)
	walk := mustSlot(t, client, javaVectorClass, 17, vector)
	number, err := newTestObject(t, client, javaLongClass)
	if err != nil {
		t.Fatal(err)
	}
	mustJavaCall(t, client, "java/lang/Long.<init>(J)V", number, 7, 9)
	buffer := newTestBuffer(t, client, "abc")
	_ = client.throwJavaPlatform(client.thread, javaIOExceptionClass, ": kept")

	runtime := client.javaRuntimeState()
	before := []any{runtime.hashtables, runtime.enumerations, runtime.numbers, runtime.vectorShapes,
		runtime.bufferCapacities, runtime.messages}
	saved, err := client.captureJavaState(&stateBudget{})
	if err != nil {
		t.Fatal(err)
	}
	library, err := client.captureJavaLibraryState()
	if err != nil || library == nil {
		t.Fatalf("captureJavaLibraryState() = %v, %v", library, err)
	}
	record, err := encodeLGTSessionRecord(sessionCheckpointState{Version: sessionCheckpointVersion, Client: clientState{Java: saved}}, nil, library)
	if err != nil {
		t.Fatal(err)
	}
	session, media, decodedLibrary, err := decodeLGTSessionRecord(record)
	if err != nil || media != nil || decodedLibrary == nil {
		t.Fatalf("decodeLGTSessionRecord() = %v, %v, %v", media, decodedLibrary, err)
	}
	if err := client.restoreJavaState(session.Client.Java); err != nil {
		t.Fatal(err)
	}
	if err := client.restoreJavaLibraryState(decodedLibrary); err != nil {
		t.Fatal(err)
	}
	restored := client.javaRuntimeState()
	after := []any{restored.hashtables, restored.enumerations, restored.numbers, restored.vectorShapes,
		restored.bufferCapacities, restored.messages}
	for index := range before {
		if !reflect.DeepEqual(before[index], after[index]) {
			t.Errorf("table %d came back as %#v, want %#v", index, after[index], before[index])
		}
	}
	if restored == runtime || restored.enumerations[walk] == runtime.enumerations[walk] {
		t.Fatal("the restore did not build a runtime of its own")
	}
	if got := mustSlot(t, client, javaStringBufferClass, 11, buffer); got != 19 {
		t.Errorf("the restored buffer's capacity = %d, want 19", got)
	}
}

// The class library's tables travel in a record of their own, so every shape
// an earlier build wrote still decodes — with no tables, which is what such a
// build left — and a session with none is written in the earliest shape.
func TestJavaClassLibraryRecordLeavesEarlierRecordsReadable(t *testing.T) {
	session := sessionCheckpointState{Version: sessionCheckpointVersion}
	for _, shape := range []struct {
		name    string
		media   *javaMediaState
		library *javaLibraryState
	}{
		{"session alone", nil, nil},
		{"media wrapper", &javaMediaState{}, nil},
		{"class library", nil, &javaLibraryState{Numbers: []javaNumberState{{Object: 0x1000, Value: 7}}}},
		{"both", &javaMediaState{}, &javaLibraryState{Messages: []javaTextState{{Object: 0x1000, Text: []byte("m")}}}},
	} {
		record, err := encodeLGTSessionRecord(session, shape.media, shape.library)
		if err != nil {
			t.Fatalf("%s: %v", shape.name, err)
		}
		_, media, library, err := decodeLGTSessionRecord(record)
		if err != nil {
			t.Fatalf("%s: %v", shape.name, err)
		}
		if (media != nil) != (shape.media != nil) || !reflect.DeepEqual(library, shape.library) {
			t.Errorf("%s came back as media %v, library %+v", shape.name, media, library)
		}
	}
	// The earlier shapes are the earlier builds' own: an encoder with nothing
	// new to write produces exactly what they read.
	legacy, err := backend.EncodeCheckpointRecord(session)
	if err != nil {
		t.Fatal(err)
	}
	if record, _ := encodeLGTSessionRecord(session, nil, nil); string(record) != string(legacy) {
		t.Error("a session with no class library tables is not written in the version-2 shape")
	}
}
