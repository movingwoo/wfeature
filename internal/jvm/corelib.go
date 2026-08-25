package jvm

import "fmt"

// The runtime-owned CLDC core library. These classes used to be Java sources
// compiled to class files and embedded, which meant three artifacts per class —
// the source, the compiled bytes, and the Go natives behind them — and a JDK to
// keep them in agreement. They are declared here instead: the metadata a game
// links against and the Go body of every method that has one.
//
// Two rules keep a Go body behaving like the bytecode it replaces.
//
// A field the class declares lives on the object under that field's name, not
// in Go-side state hanging off the object. A game may subclass these classes,
// and one that reads java/io/ByteArrayOutputStream's protected buf has to find
// what the class wrote there.
//
// A call on `this` that the Java source made goes back through the VM rather
// than calling the Go function directly, because that is what invokevirtual
// did: a game that overrides java/io/InputStream.read is calling its own method
// from the inherited read(byte[]).
//
// Coverage is the subset the repository's fixtures and the local archives
// exercise, which is the same boundary the class files drew.
func (vm *VM) registerCoreLibrary() error {
	for _, definition := range coreLibraryDefinitions() {
		if err := vm.defineClass(definition, true); err != nil {
			return fmt.Errorf("core library: %w", err)
		}
	}
	return nil
}

func coreLibraryDefinitions() []ClassDefinition {
	definitions := []ClassDefinition{
		runnableDefinition(),
		classDefinition(),
		stringDefinition(),
		stringBufferDefinition(),
		stringBuilderDefinition(),
		systemDefinition(),
		threadDefinition(),
		ioExceptionDefinition(),
		inputStreamDefinition(),
		outputStreamDefinition(),
		byteArrayInputStreamDefinition(),
		byteArrayOutputStreamDefinition(),
		dataInputStreamDefinition(),
		dataOutputStreamDefinition(),
		printStreamDefinition(),
		readerDefinition(),
		inputStreamReaderDefinition(),
		vectorDefinition(),
		hashtableDefinition(),
		randomDefinition(),
		integerDefinition(),
		longDefinition(),
		byteDefinition(),
		shortDefinition(),
		mathDefinition(),
		runtimeDefinition(),
		calendarDefinition(),
		dateDefinition(),
		enumerationDefinition(),
		timeZoneDefinition(),
		stackDefinition(),
		arrayEnumerationDefinition(),
	}
	// The throwables come last only because they are a generated group rather
	// than a hand-written one; nothing here depends on the order.
	return append(definitions, throwableDefinitions()...)
}

func runnableDefinition() ClassDefinition {
	return ClassDefinition{
		Name:      RunnableClass,
		SuperName: ObjectClass,
		Access:    AccessPublic | AccessInterface | AccessAbstract,
		Methods: []MethodDefinition{
			{Name: "run", Descriptor: "()V", Access: AccessPublic | AccessAbstract},
		},
	}
}

// classDefinition is the reflection surface CLDC has: a name and a resource
// lookup. Instances are made by the runtime rather than by guest code, so it
// publishes no constructor.
func classDefinition() ClassDefinition {
	return ClassDefinition{
		Name:      ClassClass,
		SuperName: ObjectClass,
		Access:    AccessPublic | AccessFinal,
		Methods: []MethodDefinition{
			{Name: "getResourceAsStream", Descriptor: "(Ljava/lang/String;)Ljava/io/InputStream;", Access: AccessPublic | AccessNative},
			{Name: "getName", Descriptor: "()Ljava/lang/String;", Access: AccessPublic | AccessNative},
			{Name: "toString", Descriptor: "()Ljava/lang/String;", Access: AccessPublic | AccessNative},
		},
	}
}

// stringDefinition publishes the string surface. Every one of its methods,
// constructors included, is answered by the natives in builtins.go, where the
// text lives in Go rather than in a guest char array.
func stringDefinition() ClassDefinition {
	native := AccessPublic | AccessNative
	staticNative := AccessPublic | AccessStatic | AccessNative
	return ClassDefinition{
		Name:      StringClass,
		SuperName: ObjectClass,
		Access:    AccessPublic | AccessFinal,
		Methods: []MethodDefinition{
			{Name: "<init>", Descriptor: "()V", Access: AccessPublic},
			{Name: "<init>", Descriptor: "(Ljava/lang/String;)V", Access: AccessPublic},
			{Name: "<init>", Descriptor: "([C)V", Access: AccessPublic},
			{Name: "<init>", Descriptor: "([CII)V", Access: AccessPublic},
			{Name: "<init>", Descriptor: "([B)V", Access: AccessPublic},
			{Name: "<init>", Descriptor: "([BII)V", Access: AccessPublic},
			{Name: "<init>", Descriptor: "([BLjava/lang/String;)V", Access: AccessPublic, Throws: []string{"java/io/IOException"}},
			// The ranged form has had a body since a title needed to decode a
			// record it had read into a longer buffer; it was never declared
			// beside the other six, so interpreted code resolving it found
			// nothing. A body with no declaration is reachable from a native
			// dispatch and from nowhere else.
			{Name: "<init>", Descriptor: "([BIILjava/lang/String;)V", Access: AccessPublic, Throws: []string{"java/io/IOException"}},
			{Name: "length", Descriptor: "()I", Access: native},
			{Name: "charAt", Descriptor: "(I)C", Access: native},
			{Name: "equals", Descriptor: "(Ljava/lang/Object;)Z", Access: native},
			{Name: "hashCode", Descriptor: "()I", Access: native},
			{Name: "concat", Descriptor: "(Ljava/lang/String;)Ljava/lang/String;", Access: native},
			{Name: "getBytes", Descriptor: "()[B", Access: native},
			{Name: "indexOf", Descriptor: "(I)I", Access: native},
			{Name: "indexOf", Descriptor: "(Ljava/lang/String;)I", Access: native},
			{Name: "indexOf", Descriptor: "(II)I", Access: native},
			{Name: "indexOf", Descriptor: "(Ljava/lang/String;I)I", Access: native},
			{Name: "lastIndexOf", Descriptor: "(I)I", Access: native},
			{Name: "lastIndexOf", Descriptor: "(II)I", Access: native},
			{Name: "startsWith", Descriptor: "(Ljava/lang/String;)Z", Access: native},
			{Name: "substring", Descriptor: "(I)Ljava/lang/String;", Access: native},
			{Name: "substring", Descriptor: "(II)Ljava/lang/String;", Access: native},
			{Name: "compareTo", Descriptor: "(Ljava/lang/String;)I", Access: native},
			{Name: "equalsIgnoreCase", Descriptor: "(Ljava/lang/String;)Z", Access: native},
			{Name: "endsWith", Descriptor: "(Ljava/lang/String;)Z", Access: native},
			{Name: "toUpperCase", Descriptor: "()Ljava/lang/String;", Access: native},
			{Name: "toLowerCase", Descriptor: "()Ljava/lang/String;", Access: native},
			{Name: "trim", Descriptor: "()Ljava/lang/String;", Access: native},
			{Name: "toString", Descriptor: "()Ljava/lang/String;", Access: native},
			{Name: "valueOf", Descriptor: "(C)Ljava/lang/String;", Access: staticNative},
			{Name: "valueOf", Descriptor: "([C)Ljava/lang/String;", Access: staticNative},
			{Name: "valueOf", Descriptor: "([CII)Ljava/lang/String;", Access: staticNative},
			{Name: "valueOf", Descriptor: "(I)Ljava/lang/String;", Access: staticNative},
			{Name: "valueOf", Descriptor: "(Ljava/lang/Object;)Ljava/lang/String;", Access: staticNative},
		},
	}
}

func stringBufferDefinition() ClassDefinition {
	native := AccessPublic | AccessNative
	return ClassDefinition{
		Name:      StringBufferClass,
		SuperName: ObjectClass,
		Access:    AccessPublic | AccessFinal,
		Methods: []MethodDefinition{
			{Name: "<init>", Descriptor: "()V", Access: AccessPublic},
			{Name: "<init>", Descriptor: "(Ljava/lang/String;)V", Access: AccessPublic},
			{Name: "append", Descriptor: "(C)Ljava/lang/StringBuffer;", Access: native},
			{Name: "append", Descriptor: "(I)Ljava/lang/StringBuffer;", Access: native},
			{Name: "append", Descriptor: "(Ljava/lang/String;)Ljava/lang/StringBuffer;", Access: native},
			{Name: "delete", Descriptor: "(II)Ljava/lang/StringBuffer;", Access: native},
			{Name: "insert", Descriptor: "(IC)Ljava/lang/StringBuffer;", Access: native},
			{Name: "insert", Descriptor: "(II)Ljava/lang/StringBuffer;", Access: native},
			{Name: "insert", Descriptor: "(ILjava/lang/String;)Ljava/lang/StringBuffer;", Access: native},
			// A title that builds a fixed-width line — a score padded with
			// spaces, a name overwritten in place — edits the buffer by index
			// rather than rebuilding it, so these two are how it reads and
			// writes one character. The bodies were already registered; only
			// the declaration was missing, which made the call a stop.
			{Name: "charAt", Descriptor: "(I)C", Access: native},
			{Name: "setCharAt", Descriptor: "(IC)V", Access: native},
			{Name: "length", Descriptor: "()I", Access: native},
			{Name: "toString", Descriptor: "()Ljava/lang/String;", Access: native},
		},
	}
}

// stringBuilderDefinition is the unsynchronized twin of StringBuffer. CLDC
// never had it, but javac emits it for every string concatenation in a class
// compiled against a modern JDK — including this repository's own fixtures.
func stringBuilderDefinition() ClassDefinition {
	native := AccessPublic | AccessNative
	return ClassDefinition{
		Name:      StringBuilderClass,
		SuperName: ObjectClass,
		Access:    AccessPublic | AccessFinal,
		Methods: []MethodDefinition{
			{Name: "<init>", Descriptor: "()V", Access: AccessPublic},
			{Name: "<init>", Descriptor: "(I)V", Access: AccessPublic},
			{Name: "<init>", Descriptor: "(Ljava/lang/String;)V", Access: AccessPublic},
			{Name: "append", Descriptor: "(C)Ljava/lang/StringBuilder;", Access: native},
			{Name: "append", Descriptor: "(I)Ljava/lang/StringBuilder;", Access: native},
			{Name: "append", Descriptor: "(J)Ljava/lang/StringBuilder;", Access: native},
			{Name: "append", Descriptor: "(Z)Ljava/lang/StringBuilder;", Access: native},
			{Name: "append", Descriptor: "(Ljava/lang/String;)Ljava/lang/StringBuilder;", Access: native},
			{Name: "append", Descriptor: "(Ljava/lang/Object;)Ljava/lang/StringBuilder;", Access: native},
			{Name: "delete", Descriptor: "(II)Ljava/lang/StringBuilder;", Access: native},
			{Name: "insert", Descriptor: "(IC)Ljava/lang/StringBuilder;", Access: native},
			{Name: "insert", Descriptor: "(II)Ljava/lang/StringBuilder;", Access: native},
			{Name: "insert", Descriptor: "(ILjava/lang/String;)Ljava/lang/StringBuilder;", Access: native},
			{Name: "setLength", Descriptor: "(I)V", Access: native},
			{Name: "length", Descriptor: "()I", Access: native},
			{Name: "toString", Descriptor: "()Ljava/lang/String;", Access: native},
		},
	}
}

// systemDefinition holds the system class. Its static methods were already
// answered as natives; what needs the class is out and err, because a field
// read has to resolve the class that declares it. getProperty is declared here
// and implemented by whichever platform is running, since the properties are
// that platform's answer about its handset.
func systemDefinition() ClassDefinition {
	staticNative := AccessPublic | AccessStatic | AccessNative
	return ClassDefinition{
		Name:      SystemClass,
		SuperName: ObjectClass,
		Access:    AccessPublic | AccessFinal,
		Fields: []FieldDefinition{
			{Name: "out", Descriptor: "Ljava/io/PrintStream;", Access: AccessPublic | AccessStatic | AccessFinal},
			{Name: "err", Descriptor: "Ljava/io/PrintStream;", Access: AccessPublic | AccessStatic | AccessFinal},
		},
		Methods: []MethodDefinition{
			{Name: "currentTimeMillis", Descriptor: "()J", Access: staticNative},
			{Name: "identityHashCode", Descriptor: "(Ljava/lang/Object;)I", Access: staticNative},
			{Name: "arraycopy", Descriptor: "(Ljava/lang/Object;ILjava/lang/Object;II)V", Access: staticNative},
			{Name: "gc", Descriptor: "()V", Access: staticNative},
			{Name: "getProperty", Descriptor: "(Ljava/lang/String;)Ljava/lang/String;", Access: staticNative},
			{Name: "exit", Descriptor: "(I)V", Access: staticNative},
			{Name: "<clinit>", Descriptor: "()V", Access: AccessStatic, Body: systemClassInit},
		},
	}
}

// systemClassInit creates the two streams System publishes. They are ordinary
// PrintStream instances rather than Go-side singletons so that a game which
// passes System.out to something expecting an OutputStream gets an object with
// the right class behind it.
func systemClassInit(call *Invocation, _ []Value) (Value, error) {
	for name, stream := range map[string]int32{"out": 0, "err": 1} {
		object, err := call.NewObject(PrintStreamClass, "(I)V", IntValue(stream))
		if err != nil {
			return VoidValue(), err
		}
		if err := call.SetStaticField(SystemClass, name, "Ljava/io/PrintStream;", ReferenceValue(object)); err != nil {
			return VoidValue(), err
		}
	}
	return VoidValue(), nil
}

// threadDefinition is the CLDC thread surface. start, interrupt, isAlive,
// sleep and yield are natives in builtins.go, where the goroutine or the
// platform's own scheduler is; what is left here is the target a thread was
// constructed with and the run that calls it.
func threadDefinition() ClassDefinition {
	return ClassDefinition{
		Name:       ThreadClass,
		SuperName:  ObjectClass,
		Interfaces: []string{RunnableClass},
		Access:     AccessPublic,
		Fields: []FieldDefinition{
			{Name: "target", Descriptor: "Ljava/lang/Runnable;", Access: AccessPrivate},
			{Name: "priority", Descriptor: "I", Access: AccessPrivate},
			{Name: "MIN_PRIORITY", Descriptor: "I", Access: AccessPublic | AccessStatic | AccessFinal, Constant: IntValue(1)},
			{Name: "NORM_PRIORITY", Descriptor: "I", Access: AccessPublic | AccessStatic | AccessFinal, Constant: IntValue(5)},
			{Name: "MAX_PRIORITY", Descriptor: "I", Access: AccessPublic | AccessStatic | AccessFinal, Constant: IntValue(10)},
		},
		Methods: []MethodDefinition{
			{Name: "<init>", Descriptor: "()V", Access: AccessPublic, Body: emptyConstructor},
			{Name: "<init>", Descriptor: "(Ljava/lang/Runnable;)V", Access: AccessPublic, Body: threadInit},
			{Name: "start", Descriptor: "()V", Access: AccessPublic | AccessNative},
			{Name: "interrupt", Descriptor: "()V", Access: AccessPublic | AccessNative},
			{Name: "isAlive", Descriptor: "()Z", Access: AccessPublic | AccessNative},
			{Name: "run", Descriptor: "()V", Access: AccessPublic, Body: threadRun},
			{Name: "sleep", Descriptor: "(J)V", Access: AccessPublic | AccessStatic | AccessNative, Throws: []string{"java/lang/InterruptedException"}},
			{Name: "yield", Descriptor: "()V", Access: AccessPublic | AccessStatic | AccessNative},
			{Name: "currentThread", Descriptor: "()Ljava/lang/Thread;", Access: AccessPublic | AccessStatic | AccessNative},
			{Name: "setPriority", Descriptor: "(I)V", Access: AccessPublic | AccessFinal, Body: threadSetPriority},
			{Name: "getPriority", Descriptor: "()I", Access: AccessPublic | AccessFinal, Body: threadGetPriority},
		},
	}
}

// threadSetPriority keeps what it is told and changes nothing else. There is
// no priority to set: guest threads here are goroutines the platform's own
// scheduler drives in turn, and a title that raises its loader above its frame
// loop is expressing a preference this runtime cannot honour without a
// scheduler that reads it. Keeping the value means getPriority answers what
// was set, which is what a title that saves and restores one is doing.
func threadSetPriority(call *Invocation, arguments []Value) (Value, error) {
	thread, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	priority, err := nativeInt(arguments, 1)
	if err != nil {
		return VoidValue(), err
	}
	if priority < 1 || priority > 10 {
		return VoidValue(), guestException("java/lang/IllegalArgumentException", "thread priority out of range")
	}
	return VoidValue(), setIntField(call.vm, thread, ThreadClass, "priority", priority)
}

func threadGetPriority(call *Invocation, arguments []Value) (Value, error) {
	thread, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	priority, err := intField(call.vm, thread, ThreadClass, "priority")
	if err != nil {
		return VoidValue(), err
	}
	if priority == 0 {
		return IntValue(5), nil
	}
	return IntValue(priority), nil
}

func threadInit(call *Invocation, arguments []Value) (Value, error) {
	thread, err := nativeReference(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	target, err := nativeReference(arguments, 1)
	if err != nil {
		return VoidValue(), err
	}
	return VoidValue(), call.vm.SetField(thread, ThreadClass, "target", "Ljava/lang/Runnable;", ReferenceValue(target))
}

func threadRun(call *Invocation, arguments []Value) (Value, error) {
	thread, err := nativeReference(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	value, err := call.vm.Field(thread, ThreadClass, "target", "Ljava/lang/Runnable;")
	if err != nil {
		return VoidValue(), err
	}
	target, err := value.Reference()
	if err != nil || target == nil {
		return VoidValue(), err
	}
	_, err = call.InvokeVirtual(target, "run", "()V")
	return VoidValue(), err
}

// emptyConstructor is the body of a constructor that only exists so the class
// can be instantiated. Object's own constructor does nothing, so there is
// nothing to pass up to.
func emptyConstructor(_ *Invocation, arguments []Value) (Value, error) {
	if _, err := nativeReference(arguments, 0); err != nil {
		return VoidValue(), err
	}
	return VoidValue(), nil
}

// requireObject reads an argument that must be a reference, and rejects null
// the way a guest field access on it would.
func requireObject(arguments []Value, index int) (*Object, error) {
	object, err := nativeReference(arguments, index)
	if err != nil {
		return nil, err
	}
	if object == nil {
		return nil, guestException("java/lang/NullPointerException", "null argument")
	}
	return object, nil
}

// CoreLibraryDefinitions is the core library as data, for the tools that have
// to see the surface rather than run it — the stub generator that lets a test
// fixture be compiled against this runtime with javac, and anything else that
// reports what a game may link against.
func CoreLibraryDefinitions() []ClassDefinition {
	return coreLibraryDefinitions()
}
