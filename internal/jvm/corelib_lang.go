package jvm

import "sort"

// The java/lang half of the core library that is not text: the throwables, the
// boxed numbers, the arithmetic and the runtime handle.
//
// These classes were natives without a class for a long time, which works for
// as long as a game only ever calls them. It stops working the moment a game
// names one: `new Integer(3)`, `catch (Throwable t)` and `instanceof` all
// resolve the class first, and a native table has no class to resolve. Every
// declaration here exists because a local title asked for the class by name.
//
// The bodies stay in builtins.go where they already were. A declaration whose
// method is marked native and carries no body is answered by the native
// registered under the same key, which is the arrangement String and
// StringBuffer have.

// throwableDefinition is the root of the exception hierarchy. The message a
// catch block reads lives on the object natively, which is where
// guestException puts it when the runtime raises one of these itself.
func throwableDefinition() ClassDefinition {
	return ClassDefinition{
		Name:      ThrowableClass,
		SuperName: ObjectClass,
		Access:    AccessPublic,
		Methods: []MethodDefinition{
			{Name: "<init>", Descriptor: "()V", Access: AccessPublic, Body: throwableInit},
			{Name: "<init>", Descriptor: "(Ljava/lang/String;)V", Access: AccessPublic, Body: throwableInitMessage},
			{Name: "getMessage", Descriptor: "()Ljava/lang/String;", Access: AccessPublic, Body: throwableGetMessage},
			{Name: "toString", Descriptor: "()Ljava/lang/String;", Access: AccessPublic, Body: throwableToString},
			{Name: "printStackTrace", Descriptor: "()V", Access: AccessPublic, Body: throwablePrintStackTrace},
		},
	}
}

func throwableInit(_ *Invocation, arguments []Value) (Value, error) {
	if _, err := requireObject(arguments, 0); err != nil {
		return VoidValue(), err
	}
	return VoidValue(), nil
}

func throwableInitMessage(_ *Invocation, arguments []Value) (Value, error) {
	object, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	messageObject, err := nativeReference(arguments, 1)
	if err != nil {
		return VoidValue(), err
	}
	if messageObject == nil {
		object.Native = nil
		return VoidValue(), nil
	}
	message, ok := nativeStringObject(messageObject)
	if !ok {
		return VoidValue(), guestException("java/lang/IllegalArgumentException", "throwable message is not a string")
	}
	object.Native = message
	return VoidValue(), nil
}

func throwableGetMessage(_ *Invocation, arguments []Value) (Value, error) {
	object, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	message, _ := object.Native.(string)
	if message == "" {
		return ReferenceValue(nil), nil
	}
	return ReferenceValue(nativeStringValue(message)), nil
}

// throwableToString answers what the standard one does — the class name, and
// the message after a colon when there is one — because a title that prints a
// caught exception is putting that text on the screen.
func throwableToString(_ *Invocation, arguments []Value) (Value, error) {
	object, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	text := javaClassName(object.ClassName)
	if message, _ := object.Native.(string); message != "" {
		text += ": " + message
	}
	return ReferenceValue(nativeStringValue(text)), nil
}

func throwablePrintStackTrace(call *Invocation, arguments []Value) (Value, error) {
	object, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	if call.vm.config.Logger != nil {
		call.vm.config.Logger.Error("guest exception stack trace", "class", object.ClassName, "message", object.Native)
	}
	return VoidValue(), nil
}

// javaClassName is the dotted form a game sees, rather than the internal one
// the loader keys on.
func javaClassName(internal string) string {
	name := make([]byte, len(internal))
	for index := 0; index < len(internal); index++ {
		if internal[index] == '/' {
			name[index] = '.'
			continue
		}
		name[index] = internal[index]
	}
	return string(name)
}

// throwableDefinitions declares every exception class the runtime already knew
// the chain of. The chain comes from runtimeClassParents rather than from a
// second table here, so the class a catch block resolves and the class a
// `new` resolves cannot disagree.
//
// A constructor hands its message to its own superclass rather than to
// Throwable directly, which is what a game's own subclass of one of these does
// when it calls super(message).
func throwableDefinitions() []ClassDefinition {
	names := make([]string, 0, len(runtimeClassParents))
	for name := range runtimeClassParents {
		if name == ThrowableClass || name == IOExceptionClass {
			// Throwable is declared above, and IOException with the streams
			// that raise it.
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)

	definitions := make([]ClassDefinition, 0, len(names)+1)
	definitions = append(definitions, throwableDefinition())
	for _, name := range names {
		parent := runtimeClassParents[name]
		definitions = append(definitions, ClassDefinition{
			Name:      name,
			SuperName: parent,
			Access:    AccessPublic,
			Methods: []MethodDefinition{
				{Name: "<init>", Descriptor: "()V", Access: AccessPublic, Body: superInit(parent, "()V")},
				{Name: "<init>", Descriptor: "(Ljava/lang/String;)V", Access: AccessPublic, Body: superInit(parent, "(Ljava/lang/String;)V")},
			},
		})
	}
	return definitions
}

// superInit is the body of a constructor that only hands its arguments up the
// chain, which is every exception constructor here: the message is kept once,
// by Throwable.
func superInit(parent, descriptor string) ContextMethod {
	return func(call *Invocation, arguments []Value) (Value, error) {
		receiver, err := requireObject(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		_, err = call.InvokeSpecial(receiver, parent, "<init>", descriptor, arguments[1:]...)
		return VoidValue(), err
	}
}

// integerDefinition is the boxed int. A title boxes one to put a number in a
// Vector, and parses and formats through the static half.
func integerDefinition() ClassDefinition {
	native := AccessPublic | AccessNative
	staticNative := AccessPublic | AccessStatic | AccessNative
	return ClassDefinition{
		Name:      IntegerClass,
		SuperName: ObjectClass,
		Access:    AccessPublic | AccessFinal,
		Fields: []FieldDefinition{
			{Name: "MIN_VALUE", Descriptor: "I", Access: AccessPublic | AccessStatic | AccessFinal, Constant: IntValue(-2147483648)},
			{Name: "MAX_VALUE", Descriptor: "I", Access: AccessPublic | AccessStatic | AccessFinal, Constant: IntValue(2147483647)},
		},
		Methods: []MethodDefinition{
			{Name: "<init>", Descriptor: "(I)V", Access: native},
			{Name: "intValue", Descriptor: "()I", Access: native},
			{Name: "byteValue", Descriptor: "()B", Access: native},
			{Name: "shortValue", Descriptor: "()S", Access: native},
			{Name: "longValue", Descriptor: "()J", Access: native},
			{Name: "toString", Descriptor: "()Ljava/lang/String;", Access: native},
			{Name: "equals", Descriptor: "(Ljava/lang/Object;)Z", Access: native},
			{Name: "hashCode", Descriptor: "()I", Access: native},
			{Name: "parseInt", Descriptor: "(Ljava/lang/String;)I", Access: staticNative, Throws: []string{"java/lang/NumberFormatException"}},
			{Name: "parseInt", Descriptor: "(Ljava/lang/String;I)I", Access: staticNative, Throws: []string{"java/lang/NumberFormatException"}},
			{Name: "valueOf", Descriptor: "(Ljava/lang/String;)Ljava/lang/Integer;", Access: staticNative, Throws: []string{"java/lang/NumberFormatException"}},
			{Name: "toString", Descriptor: "(I)Ljava/lang/String;", Access: staticNative},
			{Name: "toHexString", Descriptor: "(I)Ljava/lang/String;", Access: staticNative},
			{Name: "toBinaryString", Descriptor: "(I)Ljava/lang/String;", Access: staticNative},
			{Name: "toOctalString", Descriptor: "(I)Ljava/lang/String;", Access: staticNative},
		},
	}
}

// longDefinition and byteDefinition are the two other boxed numbers a local
// title names. Neither is used as an object by anything here — what is asked
// for is the parse — so the surface is the static half plus what boxing needs.
func longDefinition() ClassDefinition {
	native := AccessPublic | AccessNative
	staticNative := AccessPublic | AccessStatic | AccessNative
	return ClassDefinition{
		Name:      LongClass,
		SuperName: ObjectClass,
		Access:    AccessPublic | AccessFinal,
		Fields: []FieldDefinition{
			{Name: "MIN_VALUE", Descriptor: "J", Access: AccessPublic | AccessStatic | AccessFinal, Constant: LongValue(-9223372036854775808)},
			{Name: "MAX_VALUE", Descriptor: "J", Access: AccessPublic | AccessStatic | AccessFinal, Constant: LongValue(9223372036854775807)},
		},
		Methods: []MethodDefinition{
			{Name: "<init>", Descriptor: "(J)V", Access: native},
			{Name: "longValue", Descriptor: "()J", Access: native},
			{Name: "intValue", Descriptor: "()I", Access: native},
			{Name: "toString", Descriptor: "()Ljava/lang/String;", Access: native},
			{Name: "parseLong", Descriptor: "(Ljava/lang/String;)J", Access: staticNative, Throws: []string{"java/lang/NumberFormatException"}},
			{Name: "toString", Descriptor: "(J)Ljava/lang/String;", Access: staticNative},
		},
	}
}

func byteDefinition() ClassDefinition {
	native := AccessPublic | AccessNative
	staticNative := AccessPublic | AccessStatic | AccessNative
	return ClassDefinition{
		Name:      ByteClass,
		SuperName: ObjectClass,
		Access:    AccessPublic | AccessFinal,
		Fields: []FieldDefinition{
			{Name: "MIN_VALUE", Descriptor: "B", Access: AccessPublic | AccessStatic | AccessFinal, Constant: IntValue(-128)},
			{Name: "MAX_VALUE", Descriptor: "B", Access: AccessPublic | AccessStatic | AccessFinal, Constant: IntValue(127)},
		},
		Methods: []MethodDefinition{
			{Name: "<init>", Descriptor: "(B)V", Access: native},
			{Name: "byteValue", Descriptor: "()B", Access: native},
			{Name: "intValue", Descriptor: "()I", Access: native},
			{Name: "toString", Descriptor: "()Ljava/lang/String;", Access: native},
			{Name: "parseByte", Descriptor: "(Ljava/lang/String;)B", Access: staticNative, Throws: []string{"java/lang/NumberFormatException"}},
		},
	}
}

// shortDefinition is the third boxed number, and it is here for the same
// reason the other two are: a title parses one. The AOT title that named it
// reads a UI form's attributes, where a coordinate is a short and a flag is a
// byte, so the class is asked for from the same loader as java/lang/Byte and
// missing it stops the form rather than the number.
func shortDefinition() ClassDefinition {
	native := AccessPublic | AccessNative
	staticNative := AccessPublic | AccessStatic | AccessNative
	return ClassDefinition{
		Name:      ShortClass,
		SuperName: ObjectClass,
		Access:    AccessPublic | AccessFinal,
		Fields: []FieldDefinition{
			{Name: "MIN_VALUE", Descriptor: "S", Access: AccessPublic | AccessStatic | AccessFinal, Constant: IntValue(-32768)},
			{Name: "MAX_VALUE", Descriptor: "S", Access: AccessPublic | AccessStatic | AccessFinal, Constant: IntValue(32767)},
		},
		Methods: []MethodDefinition{
			{Name: "<init>", Descriptor: "(S)V", Access: native},
			{Name: "shortValue", Descriptor: "()S", Access: native},
			{Name: "intValue", Descriptor: "()I", Access: native},
			{Name: "toString", Descriptor: "()Ljava/lang/String;", Access: native},
			{Name: "parseShort", Descriptor: "(Ljava/lang/String;)S", Access: staticNative, Throws: []string{"java/lang/NumberFormatException"}},
		},
	}
}

// mathDefinition publishes the arithmetic. CLDC 1.1 has the floating-point
// half as well, and the natives behind these have carried it for as long as
// the class did not exist to name them through.
func mathDefinition() ClassDefinition {
	staticNative := AccessPublic | AccessStatic | AccessNative
	return ClassDefinition{
		Name:      MathClass,
		SuperName: ObjectClass,
		Access:    AccessPublic | AccessFinal,
		Fields: []FieldDefinition{
			{Name: "E", Descriptor: "D", Access: AccessPublic | AccessStatic | AccessFinal, Constant: DoubleValue(2.718281828459045)},
			{Name: "PI", Descriptor: "D", Access: AccessPublic | AccessStatic | AccessFinal, Constant: DoubleValue(3.141592653589793)},
		},
		Methods: []MethodDefinition{
			{Name: "abs", Descriptor: "(I)I", Access: staticNative},
			{Name: "abs", Descriptor: "(J)J", Access: staticNative},
			{Name: "abs", Descriptor: "(F)F", Access: staticNative},
			{Name: "abs", Descriptor: "(D)D", Access: staticNative},
			{Name: "min", Descriptor: "(II)I", Access: staticNative},
			{Name: "min", Descriptor: "(JJ)J", Access: staticNative},
			{Name: "max", Descriptor: "(II)I", Access: staticNative},
			{Name: "max", Descriptor: "(JJ)J", Access: staticNative},
		},
	}
}

// runtimeDefinition is the memory handle. A title asks it how much room is
// left before it decides how much to cache, so the two sizes matter more than
// the collector does.
func runtimeDefinition() ClassDefinition {
	native := AccessPublic | AccessNative
	return ClassDefinition{
		Name:      RuntimeClass,
		SuperName: ObjectClass,
		Access:    AccessPublic,
		Methods: []MethodDefinition{
			{Name: "getRuntime", Descriptor: "()Ljava/lang/Runtime;", Access: AccessPublic | AccessStatic | AccessNative},
			{Name: "gc", Descriptor: "()V", Access: native},
			{Name: "totalMemory", Descriptor: "()J", Access: native},
			{Name: "freeMemory", Descriptor: "()J", Access: native},
		},
	}
}

// booleanDefinition is the boxed flag. It is the one boxed type this library
// never declared, and a title that keeps a flag in a Vector — or reads
// Boolean.TRUE to put one there — resolves the class before it can box
// anything, so the omission stopped a title rather than a call.
//
// TRUE and FALSE are the two instances the specification publishes. They are
// built in the class initializer rather than answered by a Go singleton so
// that a title comparing a boxed flag against Boolean.TRUE with `==` gets the
// same object every time it reads the field.
func booleanDefinition() ClassDefinition {
	native := AccessPublic | AccessNative
	return ClassDefinition{
		Name:      BooleanClass,
		SuperName: ObjectClass,
		Access:    AccessPublic | AccessFinal,
		Fields: []FieldDefinition{
			{Name: "TRUE", Descriptor: "Ljava/lang/Boolean;", Access: AccessPublic | AccessStatic | AccessFinal},
			{Name: "FALSE", Descriptor: "Ljava/lang/Boolean;", Access: AccessPublic | AccessStatic | AccessFinal},
		},
		Methods: []MethodDefinition{
			{Name: "<init>", Descriptor: "(Z)V", Access: native},
			{Name: "booleanValue", Descriptor: "()Z", Access: native},
			{Name: "equals", Descriptor: "(Ljava/lang/Object;)Z", Access: native},
			{Name: "hashCode", Descriptor: "()I", Access: native},
			{Name: "toString", Descriptor: "()Ljava/lang/String;", Access: native},
			{Name: "<clinit>", Descriptor: "()V", Access: AccessStatic, Body: booleanClassInit},
		},
	}
}

func booleanClassInit(call *Invocation, _ []Value) (Value, error) {
	for name, value := range map[string]int32{"TRUE": 1, "FALSE": 0} {
		object, err := call.NewObject(BooleanClass, "(Z)V", IntValue(value))
		if err != nil {
			return VoidValue(), err
		}
		if err := call.SetStaticField(BooleanClass, name, "Ljava/lang/Boolean;", ReferenceValue(object)); err != nil {
			return VoidValue(), err
		}
	}
	return VoidValue(), nil
}
