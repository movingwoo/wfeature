package lgt

import (
	"context"
	"fmt"
	"strings"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// `Class.forName`, `newInstance` and `getName`: a class asked for by name, an
// instance built from it, and the name back.
//
// A class object is the object the module resolves a class to, and it is
// dispatched on as a `java/lang/Class` (javaGetClass), so Class's own slots —
// newInstance at 10, getName at 15, getResourceAsStream at 16, in declaration
// order — are reached through it.

const (
	javaClassNotFoundClass = "java/lang/ClassNotFoundException"
	javaInstantiationClass = "java/lang/InstantiationException"
	javaNullaryConstructor = "()V"
	javaConstructorName    = "<init>"
)

// javaClassObjectFor gives a class object Class's dispatch table and answers
// it, which is what makes a call on the answer reach Class's slots.
func (client *Client) javaClassObjectFor(object uint32) (uint32, error) {
	meta, err := client.preparePlatformJavaClass(javaClassClass)
	if err != nil {
		return 0, err
	}
	if err := client.writeWord(object, meta.VTable); err != nil {
		return 0, err
	}
	return object, nil
}

// javaClassForName is `Class.forName(String)`: the class with that name,
// initialised. A class the module declares is reached through its own record's
// "give me it, initialised" thunk — the call every `new` makes first — so its
// static initialiser runs as the language says it does here. A class of the
// platform's is laid out the way any platform class is. Any other name is the
// ClassNotFoundException a title probing for an optional class expects.
func javaClassForName(
	client *Client, ctx context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	if arguments[0] == 0 {
		return 0, client.throwJavaPlatform(thread, javaThrowNullClass, ": Class.forName(null)")
	}
	name, err := client.javaHeldText(arguments[0])
	if err != nil {
		return 0, err
	}
	internal := strings.ReplaceAll(name, ".", "/")
	if handle, found := client.findJavaClassRecord(internal); found {
		record, err := client.readJavaClass(handle, nil)
		if err != nil {
			return 0, fmt.Errorf("the class record of %s: %w", name, err)
		}
		if record.Body[1] == 0 {
			return 0, fmt.Errorf("%s has no initialising thunk", name)
		}
		object, err := client.callOn(ctx, thread, record.Body[1], nil)
		if err != nil {
			return 0, client.rethrowFromCallback(thread, fmt.Errorf("initialise %s: %w", name, err))
		}
		return client.javaClassObjectFor(object)
	}
	if client.javaPlatformClassKnown(internal) {
		class, err := client.preparePlatformJavaClass(internal)
		if err != nil {
			return 0, err
		}
		return client.javaClassObjectFor(class.Object)
	}
	return 0, client.throwJavaPlatform(thread, javaClassNotFoundClass, ": "+name)
}

// javaPlatformClassKnown reports whether a name is one of the platform's
// classes: one this platform has laid out or serves a member of, or one the
// module's own class list names.
func (client *Client) javaPlatformClassKnown(name string) bool {
	if _, ok := client.javaRuntimeState().byName[name]; ok {
		return true
	}
	if _, ok := javaBakedVirtualSlots[name]; ok {
		return true
	}
	if _, ok := javaPlatformSupers[name]; ok {
		return true
	}
	prefix := name + "."
	for key := range javaPlatformMethods {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	if link := client.javaLink; link != nil && link.surface != nil {
		for _, class := range link.surface.Classes {
			if class.Name == name {
				return true
			}
		}
	}
	return false
}

// javaClassNewInstance is `Class.newInstance()`, slot 10: a new object of the
// class, built by its no-argument constructor. A class the module declares is
// initialised first and constructed through its own `<init>()V`; a class of
// the platform's through the constructor this platform implements for it. An
// array type, or a class whose record declares no such constructor, is the
// InstantiationException the language names.
func javaClassNewInstance(
	client *Client, ctx context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	class, ok := client.javaClassOfIdentity(arguments[0])
	if !ok {
		return 0, fmt.Errorf("the class object at %#x was not issued here", arguments[0])
	}
	if class.ElementBytes != 0 {
		return 0, client.throwJavaPlatform(thread, javaInstantiationClass, ": "+class.Name)
	}
	if class.Handle == 0 {
		method, ok := javaPlatformMethods[class.Name+"."+javaConstructorName+javaNullaryConstructor]
		if !ok {
			return 0, fmt.Errorf("%s has no no-argument constructor this platform implements", class.Name)
		}
		object, err := client.allocateJavaObject(class)
		if err != nil {
			return 0, err
		}
		if _, err := method.Implementat(client, ctx, thread, []uint32{object}); err != nil {
			return 0, err
		}
		return object, nil
	}
	var constructor javaMember
	found := false
	for _, method := range class.Record.Methods {
		if method.Name == javaConstructorName && method.Descriptor == javaNullaryConstructor && method.Body != 0 {
			constructor, found = method, true
			break
		}
	}
	if !found {
		if !class.Record.Inline {
			// A record laid out by the compiler names no members at all, so
			// there is no constructor here to find, rather than none to call.
			return 0, fmt.Errorf("%s carries no member list to find its constructor in", class.Name)
		}
		return 0, client.throwJavaPlatform(thread, javaInstantiationClass, ": "+class.Name+" has no "+
			javaConstructorName+javaNullaryConstructor)
	}
	if !class.initialized && class.Record.Body[1] != 0 {
		if _, err := client.callOn(ctx, thread, class.Record.Body[1], nil); err != nil {
			return 0, client.rethrowFromCallback(thread, fmt.Errorf("initialise %s: %w", class.Name, err))
		}
	}
	object, err := client.allocateJavaObject(class)
	if err != nil {
		return 0, err
	}
	if _, err := client.callOn(ctx, thread, constructor.Body, []uint32{object}); err != nil {
		return 0, client.rethrowFromCallback(thread,
			fmt.Errorf("construct %s through %s at %#x: %w", class.Name, javaConstructorName, constructor.Body, err))
	}
	return object, nil
}

// javaClassGetName is `Class.getName()`, slot 15: the name with dots, as the
// source wrote it.
func javaClassGetName(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	class, ok := client.javaClassOfIdentity(arguments[0])
	if !ok {
		return 0, fmt.Errorf("the class object at %#x was not issued here", arguments[0])
	}
	return client.newJavaString(javaDottedName(class.Name))
}
