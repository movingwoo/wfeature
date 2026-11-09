package ktf

import (
	"bytes"
	"log/slog"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// coreLibraryInternals are the core library's public members that are not
// CLDC's and so are not published to KTF titles: helpers the library keeps for
// its own console and enumerations. Every other public or protected member of a
// class this table publishes is declared, because a title reaches a member
// through the table and not through the JVM — see "Publishing the rest of the
// core library" in docs/history/ktf.md.
var coreLibraryInternals = map[string]string{
	"java/io/PrintStream.<init>(I)V":                             "the library's own console stream; CLDC's takes an OutputStream",
	"java/io/PrintStream.print([B)V":                             "a console helper; CLDC's PrintStream prints no byte array",
	"java/io/PrintStream.println([B)V":                           "a console helper; CLDC's PrintStream prints no byte array",
	"java/lang/Byte.toString(B)Ljava/lang/String;":               "a helper; CLDC's Byte has only the instance toString",
	"net/wfeature/ArrayEnumeration.<init>([Ljava/lang/Object;)V": "the library's own enumeration class",
}

// A class the table publishes publishes every CLDC member the core library has
// a body for. They used to be added one at a time, each when a title died on
// the lookup, and the one a sweep found last was found only after the one
// before it was fixed; a class with a member missing is a class a title
// nobody has run can still stop on.
func TestEveryPublishedCoreClassDeclaresItsCLDCMembers(t *testing.T) {
	for _, definition := range jvm.CoreLibraryDefinitions() {
		table, published := runtimeJavaClasses[definition.Name]
		if !published {
			continue
		}
		declared := map[string]bool{}
		for _, method := range table.methods {
			if method.class == definition.Name {
				declared[method.name+method.descriptor] = true
			}
		}
		for _, method := range definition.Methods {
			if method.Access&(jvm.AccessPublic|jvm.AccessProtected) == 0 {
				continue
			}
			member := definition.Name + "." + method.Name + method.Descriptor
			_, internal := coreLibraryInternals[member]
			switch {
			case declared[method.Name+method.Descriptor] && internal:
				t.Errorf("%s is published and also listed as internal", member)
			case !declared[method.Name+method.Descriptor] && !internal:
				t.Errorf("%s has a body the KTF table does not publish", member)
			}
		}
	}
	for member := range coreLibraryInternals {
		class, _, _ := strings.Cut(member, ".")
		if _, published := runtimeJavaClasses[class]; !published {
			t.Errorf("internal member %s belongs to a class the table does not publish", member)
		}
	}
}

// The members published together, each looked up the way guest code does —
// through the class record — and the ones with a value to give made to give
// it.
func TestTheRestOfTheCoreLibraryResolvesAndAnswers(t *testing.T) {
	client, runtime := newTestRuntime(t)
	members := []struct{ class, name, descriptor string }{
		{"java/io/DataInputStream", "readFloat", "()F"},
		{"java/io/DataInputStream", "readDouble", "()D"},
		{"java/io/DataOutputStream", "writeFloat", "(F)V"},
		{"java/io/DataOutputStream", "writeDouble", "(D)V"},
		{"java/io/InputStream", "<init>", "()V"},
		{"java/io/PrintStream", "print", "([C)V"},
		{"java/io/PrintStream", "println", "([C)V"},
		{"java/io/PrintStream", "close", "()V"},
		{"java/lang/Byte", "parseByte", "(Ljava/lang/String;I)B"},
		{"java/lang/Byte", "equals", "(Ljava/lang/Object;)Z"},
		{"java/lang/Byte", "hashCode", "()I"},
		{"java/lang/Class", "toString", "()Ljava/lang/String;"},
		{"java/lang/Integer", "toString", "(II)Ljava/lang/String;"},
		{"java/lang/Integer", "valueOf", "(Ljava/lang/String;I)Ljava/lang/Integer;"},
		{"java/lang/Integer", "equals", "(Ljava/lang/Object;)Z"},
		{"java/lang/Integer", "hashCode", "()I"},
		{"java/lang/Integer", "floatValue", "()F"},
		{"java/lang/Integer", "doubleValue", "()D"},
		{"java/lang/Long", "parseLong", "(Ljava/lang/String;I)J"},
		{"java/lang/Long", "toString", "(JI)Ljava/lang/String;"},
		{"java/lang/Long", "equals", "(Ljava/lang/Object;)Z"},
		{"java/lang/Long", "hashCode", "()I"},
		{"java/lang/Long", "floatValue", "()F"},
		{"java/lang/Long", "doubleValue", "()D"},
		{"java/lang/Math", "ceil", "(D)D"},
		{"java/lang/Math", "cos", "(D)D"},
		{"java/lang/Math", "floor", "(D)D"},
		{"java/lang/Math", "max", "(DD)D"},
		{"java/lang/Math", "max", "(FF)F"},
		{"java/lang/Math", "min", "(DD)D"},
		{"java/lang/Math", "min", "(FF)F"},
		{"java/lang/Math", "sin", "(D)D"},
		{"java/lang/Math", "sqrt", "(D)D"},
		{"java/lang/Math", "tan", "(D)D"},
		{"java/lang/Math", "toDegrees", "(D)D"},
		{"java/lang/Math", "toRadians", "(D)D"},
		{"java/lang/Short", "parseShort", "(Ljava/lang/String;I)S"},
		{"java/lang/Short", "equals", "(Ljava/lang/Object;)Z"},
		{"java/lang/Short", "hashCode", "()I"},
		{"java/lang/String", "intern", "()Ljava/lang/String;"},
		{"java/lang/Thread", "activeCount", "()I"},
		{"java/lang/Thread", "getPriority", "()I"},
		{"java/lang/Thread", "join", "()V"},
		{"java/util/Calendar", "setTimeZone", "(Ljava/util/TimeZone;)V"},
		{"java/util/Random", "next", "(I)I"},
	}
	if len(members) != 45 {
		t.Fatalf("listed %d members, want the 45 the table lacked", len(members))
	}
	for _, member := range members {
		classAddress, err := runtime.ensureJavaClass(member.class)
		if err != nil {
			t.Fatal(err)
		}
		method, found, err := client.JVM().FindAOTMethod(classAddress, member.name, member.descriptor)
		if err != nil || !found || method.Body == 0 {
			t.Errorf("%s.%s%s does not resolve from the guest's class record: found=%v err=%v", member.class, member.name, member.descriptor, found, err)
		}
	}

	vm := client.vm
	text := func(value jvm.Value, err error) string {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		object, _ := value.Reference()
		got, _ := jvm.StringText(object)
		return got
	}
	str := func(value string) jvm.Value { return jvm.ReferenceValue(vm.NewString(value)) }
	if got := text(vm.InvokeStatic("java/lang/Integer", "toString", "(II)Ljava/lang/String;", jvm.IntValue(-255), jvm.IntValue(16))); got != "-ff" {
		t.Errorf("Integer.toString(-255, 16) = %q", got)
	}
	if got := text(vm.InvokeStatic("java/lang/Long", "toString", "(JI)Ljava/lang/String;", jvm.LongValue(1295), jvm.IntValue(36))); got != "zz" {
		t.Errorf("Long.toString(1295, 36) = %q", got)
	}
	integers := []struct {
		class, name, descriptor, digits string
		radix                           int32
		want                            int64
	}{
		{"java/lang/Byte", "parseByte", "(Ljava/lang/String;I)B", "-80", 16, -128},
		{"java/lang/Short", "parseShort", "(Ljava/lang/String;I)S", "7fff", 16, 32767},
		{"java/lang/Long", "parseLong", "(Ljava/lang/String;I)J", "-zz", 36, -1295},
	}
	for _, parse := range integers {
		value, err := vm.InvokeStatic(parse.class, parse.name, parse.descriptor, str(parse.digits), jvm.IntValue(parse.radix))
		if err != nil {
			t.Fatalf("%s.%s(%q, %d): %v", parse.class, parse.name, parse.digits, parse.radix, err)
		}
		got, err := value.Int64()
		if err != nil {
			narrow, _ := value.Int32()
			got = int64(narrow)
		}
		if got != parse.want {
			t.Errorf("%s.%s(%q, %d) = %d, want %d", parse.class, parse.name, parse.digits, parse.radix, got, parse.want)
		}
	}
	boxed, err := vm.InvokeStatic("java/lang/Integer", "valueOf", "(Ljava/lang/String;I)Ljava/lang/Integer;", str("7f"), jvm.IntValue(16))
	if err != nil {
		t.Fatal(err)
	}
	seven, _ := boxed.Reference()
	other, err := vm.NewObject("java/lang/Integer", "(I)V", jvm.IntValue(127))
	if err != nil {
		t.Fatal(err)
	}
	if same, err := vm.InvokeVirtual(seven, "equals", "(Ljava/lang/Object;)Z", jvm.ReferenceValue(other)); err != nil || same != jvm.IntValue(1) {
		t.Errorf("Integer.valueOf(\"7f\", 16).equals(new Integer(127)) = %v/%v", same, err)
	}
	if hash, err := vm.InvokeVirtual(seven, "hashCode", "()I"); err != nil || hash != jvm.IntValue(127) {
		t.Errorf("Integer(127).hashCode() = %v/%v", hash, err)
	}
	if widened, err := vm.InvokeVirtual(seven, "doubleValue", "()D"); err != nil || widened != jvm.DoubleValue(127) {
		t.Errorf("Integer(127).doubleValue() = %v/%v", widened, err)
	}
	long, err := vm.NewObject("java/lang/Long", "(J)V", jvm.LongValue(1<<32|5))
	if err != nil {
		t.Fatal(err)
	}
	if hash, err := vm.InvokeVirtual(long, "hashCode", "()I"); err != nil || hash != jvm.IntValue(4) {
		t.Errorf("Long(1<<32|5).hashCode() = %v/%v, want 4", hash, err)
	}
	for _, check := range []struct {
		name string
		in   float64
		want float64
	}{
		{"sqrt", 2.25, 1.5},
		{"floor", -1.5, -2},
		{"ceil", -1.5, -1},
		{"toDegrees", math.Pi, 180},
	} {
		value, err := vm.InvokeStatic("java/lang/Math", check.name, "(D)D", jvm.DoubleValue(check.in))
		if err != nil || value != jvm.DoubleValue(check.want) {
			t.Errorf("Math.%s(%v) = %v/%v, want %v", check.name, check.in, value, err, check.want)
		}
	}
	first, err := vm.InvokeVirtual(vm.NewString("stage"), "intern", "()Ljava/lang/String;")
	if err != nil {
		t.Fatal(err)
	}
	second, err := vm.InvokeVirtual(vm.NewString("stage"), "intern", "()Ljava/lang/String;")
	if err != nil {
		t.Fatal(err)
	}
	if a, _ := first.Reference(); a == nil || a != mustReference(second) {
		t.Error("two equal strings interned to different objects")
	}

	var printed bytes.Buffer
	client.Logger = slog.New(slog.NewTextHandler(&printed, nil))
	stream, err := vm.NewObject("java/io/PrintStream", "(I)V", jvm.IntValue(1))
	if err != nil {
		t.Fatal(err)
	}
	chars, err := vm.NewArray(jvm.Type{Kind: jvm.TypeChar}, 3)
	if err != nil {
		t.Fatal(err)
	}
	for index, unit := range []rune("점수:") {
		if err := jvm.SetArrayElement(chars, index, jvm.IntValue(unit)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := vm.InvokeVirtual(stream, "println", "([C)V", jvm.ReferenceValue(chars)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(printed.String(), "text=점수:") {
		t.Errorf("println(char[]) logged %q", printed.String())
	}
}

func mustReference(value jvm.Value) *jvm.Object {
	object, _ := value.Reference()
	return object
}

// The CLDC members the class library shares with SKT, each found the way
// guest code finds it: through the class record, with a body to call. The ones
// that override a member above them take that member's slot rather than a new
// one, so a call compiled against the slot reaches the override; and the two
// writer classes are records of their own, the abstract one above the other.
func TestTheSharedCLDCMembersResolveFromTheGuestsClassRecords(t *testing.T) {
	client, runtime := newTestRuntime(t)
	members := []struct{ class, name, descriptor string }{
		{"java/lang/String", "regionMatches", "(ZILjava/lang/String;II)Z"},
		{"java/lang/String", "startsWith", "(Ljava/lang/String;I)Z"},
		{"java/lang/String", "valueOf", "(Z)Ljava/lang/String;"},
		{"java/lang/StringBuffer", "capacity", "()I"},
		{"java/lang/StringBuffer", "ensureCapacity", "(I)V"},
		{"java/lang/StringBuffer", "insert", "(IZ)Ljava/lang/StringBuffer;"},
		{"java/lang/StringBuffer", "insert", "(I[C)Ljava/lang/StringBuffer;"},
		{"java/lang/StringBuffer", "insert", "(IJ)Ljava/lang/StringBuffer;"},
		{"java/lang/StringBuffer", "insert", "(ILjava/lang/Object;)Ljava/lang/StringBuffer;"},
		{"java/lang/StringBuffer", "reverse", "()Ljava/lang/StringBuffer;"},
		{"java/util/Vector", "indexOf", "(Ljava/lang/Object;I)I"},
		{"java/util/Vector", "lastIndexOf", "(Ljava/lang/Object;I)I"},
		{"java/util/Vector", "toString", "()Ljava/lang/String;"},
		{"java/util/Stack", "toString", "()Ljava/lang/String;"},
		{"java/util/Hashtable", "contains", "(Ljava/lang/Object;)Z"},
		{"java/util/Hashtable", "rehash", "()V"},
		{"java/util/Hashtable", "toString", "()Ljava/lang/String;"},
		{"java/io/Reader", "<init>", "(Ljava/lang/Object;)V"},
		{"java/io/Reader", "mark", "(I)V"},
		{"java/io/Reader", "reset", "()V"},
		{"java/io/InputStreamReader", "markSupported", "()Z"},
		{"java/io/InputStreamReader", "mark", "(I)V"},
		{"java/io/InputStreamReader", "reset", "()V"},
		{"java/lang/Runtime", "exit", "(I)V"},
		{"java/lang/Class", "newInstance", "()Ljava/lang/Object;"},
		{"java/io/ByteArrayOutputStream", "toString", "()Ljava/lang/String;"},
		{"java/io/DataInputStream", "readUTF", "(Ljava/io/DataInput;)Ljava/lang/String;"},
		{"java/io/Writer", "<init>", "()V"},
		{"java/io/Writer", "<init>", "(Ljava/lang/Object;)V"},
		{"java/io/Writer", "write", "(I)V"},
		{"java/io/Writer", "write", "([C)V"},
		{"java/io/Writer", "write", "([CII)V"},
		{"java/io/Writer", "write", "(Ljava/lang/String;)V"},
		{"java/io/Writer", "write", "(Ljava/lang/String;II)V"},
		{"java/io/Writer", "flush", "()V"},
		{"java/io/Writer", "close", "()V"},
		{"java/io/OutputStreamWriter", "<init>", "(Ljava/io/OutputStream;)V"},
		{"java/io/OutputStreamWriter", "<init>", "(Ljava/io/OutputStream;Ljava/lang/String;)V"},
		{"java/io/OutputStreamWriter", "write", "(I)V"},
		{"java/io/OutputStreamWriter", "write", "([CII)V"},
		{"java/io/OutputStreamWriter", "write", "(Ljava/lang/String;II)V"},
		{"java/io/OutputStreamWriter", "write", "(Ljava/lang/String;)V"},
		{"java/io/OutputStreamWriter", "flush", "()V"},
		{"java/io/OutputStreamWriter", "close", "()V"},
		{runtimeTimerClass, "schedule", "(Ljava/util/TimerTask;Ljava/util/Date;)V"},
		{runtimeTimerClass, "schedule", "(Ljava/util/TimerTask;Ljava/util/Date;J)V"},
		{runtimeTimerClass, "scheduleAtFixedRate", "(Ljava/util/TimerTask;Ljava/util/Date;J)V"},
	}
	vm := client.JVM()
	resolve := func(class, name, descriptor string) jvm.AOTMethodMetadata {
		t.Helper()
		address, err := runtime.ensureJavaClass(class)
		if err != nil {
			t.Fatal(err)
		}
		method, found, err := vm.FindAOTMethod(address, name, descriptor)
		if err != nil || !found || method.Body == 0 {
			t.Errorf("%s.%s%s does not resolve from the guest's class record: found=%v err=%v", class, name, descriptor, found, err)
		}
		return method
	}
	for _, member := range members {
		resolve(member.class, member.name, member.descriptor)
	}
	for _, override := range []struct{ class, above, name, descriptor string }{
		{"java/util/Vector", jvm.ObjectClass, "toString", "()Ljava/lang/String;"},
		{"java/util/Hashtable", jvm.ObjectClass, "toString", "()Ljava/lang/String;"},
		{"java/io/ByteArrayOutputStream", jvm.ObjectClass, "toString", "()Ljava/lang/String;"},
		{"java/io/InputStreamReader", "java/io/Reader", "markSupported", "()Z"},
		{"java/io/InputStreamReader", "java/io/Reader", "mark", "(I)V"},
		{"java/io/OutputStreamWriter", "java/io/Writer", "write", "(Ljava/lang/String;II)V"},
	} {
		own, inherited := resolve(override.class, override.name, override.descriptor), resolve(override.above, override.name, override.descriptor)
		if own.Address == inherited.Address || own.VTableIndex != inherited.VTableIndex {
			t.Errorf("%s.%s%s is at slot %d (method %#x), want %s's slot %d in a method of its own",
				override.class, override.name, override.descriptor, own.VTableIndex, own.Address, override.above, inherited.VTableIndex)
		}
	}
	writer, err := runtime.ensureJavaClass("java/io/Writer")
	if err != nil {
		t.Fatal(err)
	}
	streamWriter, err := runtime.ensureJavaClass("java/io/OutputStreamWriter")
	if err != nil {
		t.Fatal(err)
	}
	if record, ok := vm.AOTClassAt(writer); !ok || record.AccessFlags&0x0400 == 0 || record.SuperName != jvm.ObjectClass {
		t.Errorf("Writer's record = %+v, want an abstract class above Object", record)
	}
	if record, ok := vm.AOTClassAt(streamWriter); !ok || record.AccessFlags&0x0400 != 0 || record.SuperName != "java/io/Writer" {
		t.Errorf("OutputStreamWriter's record = %+v, want a concrete class above Writer", record)
	}

	// What the records' stubs call — the member at the record's own class —
	// answers.
	vector, err := vm.NewObject("java/util/Vector", "()V")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := vm.InvokeVirtual(vector, "addElement", "(Ljava/lang/Object;)V", jvm.ReferenceValue(vm.NewString("a"))); err != nil {
		t.Fatal(err)
	}
	text, err := vm.InvokeSpecial(vector, "java/util/Vector", "toString", "()Ljava/lang/String;")
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := jvm.StringText(mustReference(text)); got != "[a]" {
		t.Errorf("Vector.toString through its record = %q", got)
	}
	buffer, err := vm.NewObject("java/lang/StringBuffer", "(Ljava/lang/String;)V", jvm.ReferenceValue(vm.NewString("abc")))
	if err != nil {
		t.Fatal(err)
	}
	if capacity, err := vm.InvokeSpecial(buffer, "java/lang/StringBuffer", "capacity", "()I"); err != nil || capacity != jvm.IntValue(19) {
		t.Errorf("StringBuffer.capacity through its record = %v, %v, want 19", capacity, err)
	}
	// Runtime.exit ends the session as System.exit does here: this platform
	// installs no exit of its own, so both are the same refusal.
	runtimeObject, err := vm.InvokeStatic("java/lang/Runtime", "getRuntime", "()Ljava/lang/Runtime;")
	if err != nil {
		t.Fatal(err)
	}
	_, runtimeErr := vm.InvokeSpecial(mustReference(runtimeObject), "java/lang/Runtime", "exit", "(I)V", jvm.IntValue(0))
	_, systemErr := vm.InvokeStatic("java/lang/System", "exit", "(I)V", jvm.IntValue(0))
	if runtimeErr == nil || systemErr == nil || !strings.Contains(runtimeErr.Error(), "no platform shutdown installed") ||
		!strings.Contains(systemErr.Error(), "no platform shutdown installed") {
		t.Errorf("Runtime.exit = %v and System.exit = %v, want the same ending", runtimeErr, systemErr)
	}
}

// Class.newInstance of a class this platform holds as guest records is made
// the way the Host makes a title's main class: the instance allocated with its
// guest layout, then the class's own <init>()V run through the JVM-to-AOT
// bridge on the guest thread. The class here is a runtime record whose
// constructor is a native; a title's is its compiled code, reached the same
// way. A record that cannot be instantiated is refused before anything is
// allocated.
func TestClassNewInstanceMakesAGuestClassAsTheMainClassIsMade(t *testing.T) {
	client, runtime := newTestRuntime(t)
	vm := client.JVM()
	const name = "test/Stage"
	made := 0
	if err := vm.RegisterNative(name, "<init>", "()V", func(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		receiver := mustReference(arguments[0])
		if _, bound := vm.AOTAddress(receiver); !bound {
			t.Error("the constructor ran on an instance with no guest layout")
		}
		made++
		return jvm.VoidValue(), nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.createRuntimeJavaClass(runtimeJavaClass{name: name, superName: jvm.ObjectClass, accessFlags: 0x0021,
		methods: []runtimeJavaMethod{{class: name, name: "<init>", descriptor: "()V", accessFlags: 0x0001}}}); err != nil {
		t.Fatal(err)
	}
	runtime.CurrentThread, runtime.CurrentContext = client.Thread, t.Context()
	defer func() { runtime.CurrentThread, runtime.CurrentContext = nil, nil }()
	newInstance := func(dotted string) (*jvm.Object, error) {
		class, err := vm.InvokeStatic(jvm.ClassClass, "forName", "(Ljava/lang/String;)Ljava/lang/Class;", jvm.ReferenceValue(vm.NewString(dotted)))
		if err != nil {
			t.Fatal(err)
		}
		result, err := vm.InvokeVirtual(mustReference(class), "newInstance", "()Ljava/lang/Object;")
		if err != nil {
			return nil, err
		}
		return mustReference(result), nil
	}
	tracked := len(runtime.objects)
	object, err := newInstance("test.Stage")
	if err != nil {
		t.Fatal(err)
	}
	if object == nil || object.ClassName != name || made != 1 {
		t.Fatalf("newInstance = %v, constructor runs %d, want one %s", object, made, name)
	}
	if _, bound := vm.AOTAddress(object); !bound || len(runtime.objects) != tracked+1 {
		t.Fatal("the instance newInstance answered is not one guest allocation")
	}
	if _, err := runtime.ensureJavaClass(runtimeCardClass); err != nil {
		t.Fatal(err)
	}
	before := len(runtime.objects)
	if _, err := newInstance("org.kwis.msp.lcdui.Card"); !vm.IsGuestException(err, "java/lang/InstantiationException") {
		t.Fatalf("newInstance of the abstract Card = %v, want InstantiationException", err)
	}
	if len(runtime.objects) != before {
		t.Fatal("a refused class was allocated")
	}
}

// setPriority used to keep nothing, so a getPriority published beside it would
// have answered the default whatever a title set. It keeps what is in range
// now, in the field getPriority reads, and still ignores what is not.
func TestThreadPriorityIsKeptForGetPriority(t *testing.T) {
	client, _ := newTestRuntime(t)
	vm := client.vm
	thread, err := vm.NewObject("java/lang/Thread", "()V")
	if err != nil {
		t.Fatal(err)
	}
	priority := func() jvm.Value {
		t.Helper()
		value, err := vm.InvokeVirtual(thread, "getPriority", "()I")
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	if got := priority(); got != jvm.IntValue(5) {
		t.Fatalf("a new thread's priority = %v, want NORM_PRIORITY 5", got)
	}
	for _, set := range []struct{ value, want int32 }{{7, 7}, {11, 7}, {0, 7}, {1, 1}, {10, 10}} {
		if _, err := vm.InvokeVirtual(thread, "setPriority", "(I)V", jvm.IntValue(set.value)); err != nil {
			t.Fatalf("setPriority(%d): %v", set.value, err)
		}
		if got := priority(); got != jvm.IntValue(set.want) {
			t.Fatalf("after setPriority(%d) getPriority = %v, want %d", set.value, got, set.want)
		}
	}
}

// Thread.join on the cooperative scheduler parks the worker that calls it a
// slice at a time and returns only once the thread it waits for has ended.
func TestThreadJoinParksTheWorkerUntilTheThreadEnds(t *testing.T) {
	client, runtime := newTestRuntime(t)
	t.Cleanup(client.StopThreads)
	vm := client.vm
	target, err := vm.NewObject("java/lang/Thread", "()V")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := vm.InvokeVirtual(target, "start", "()V"); err != nil {
		t.Fatal(err)
	}
	if !vm.GuestThreadAlive(target) {
		t.Fatal("a started thread is not alive")
	}
	joiner := &guestWorker{
		javaThread: &jvm.Object{ClassName: "java/lang/Thread"},
		armThread:  armcore.NewThread(armcore.NewContext()),
		grant:      make(chan struct{}),
		events:     make(chan workerEvent, 1),
		finished:   make(chan struct{}),
	}
	client.activeWorker = joiner
	returned := make(chan error, 1)
	go func() {
		_, err := runtimeThreadJoin(runtime, vm, []jvm.Value{jvm.ReferenceValue(target)})
		returned <- err
	}()
	for slice := 0; slice < 3; slice++ {
		select {
		case <-joiner.events:
		case err := <-returned:
			t.Fatalf("join returned (%v) while the thread was alive", err)
		case <-time.After(5 * time.Second):
			t.Fatal("join neither parked nor returned")
		}
		if slice == 2 {
			vm.EndGuestThread(target)
		}
		joiner.grant <- struct{}{}
	}
	select {
	case err := <-returned:
		if err != nil {
			t.Fatalf("join = %v", err)
		}
	case <-joiner.events:
		t.Fatal("join parked again after the thread ended")
	case <-time.After(5 * time.Second):
		t.Fatal("join did not return after the thread ended")
	}
}

// A join on the client thread cannot park — the thread it waits for cannot run
// until the call returns — so it returns, and a thread that never started or
// already ended is joined at once from anywhere.
func TestThreadJoinOutsideAWorkerReturns(t *testing.T) {
	client, runtime := newTestRuntime(t)
	t.Cleanup(client.StopThreads)
	vm := client.vm
	idle, err := vm.NewObject("java/lang/Thread", "()V")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeThreadJoin(runtime, vm, []jvm.Value{jvm.ReferenceValue(idle)}); err != nil {
		t.Fatalf("join of a thread that never started = %v", err)
	}
	running, err := vm.NewObject("java/lang/Thread", "()V")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := vm.InvokeVirtual(running, "start", "()V"); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := runtimeThreadJoin(runtime, vm, []jvm.Value{jvm.ReferenceValue(running)})
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("join on the client thread = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("join on the client thread blocked")
	}
}
