package lgt

import (
	"context"
	"fmt"
	"strings"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// The launcher call names a class, and the class is in the image.
//
// `0x83` asks for the platform's own `org/kwis/msp/lcdui/Main` and hands it the
// module's `argv`, whose first word is the name of the application's Jlet — the
// class the platform is meant to construct and start. Nothing runs it yet. What
// is here finds the class the name refers to and reports it, because the whole
// difficulty of this call was that the name looked like it referred to nothing:
// the class list the module hands over does not contain it, and reading that
// list as the whole of the application is what hid it. See docs/lgt.md, "What
// "Game" names, and why it is not in the class list".

// javaLauncherChainLimit stops a walk through a superclass chain that a
// malformed image could make circular.
const javaLauncherChainLimit = 16

// runJavaLauncher is the platform's Jlet launcher: find the class argv names,
// lay it out, build one, construct it and start it. Every step is a call into
// the module's own compiled code, and every step is made on the thread that is
// running — the launcher call itself is a platform call, and its caller's frame
// is live above it.
//
// What this does not do is a Jlet's lifecycle. `startApp` is entered; nothing
// yet pauses, resumes or destroys the application, and nothing drives the
// event queue it is expected to run on.
func (client *Client) runJavaLauncher(
	ctx context.Context, thread *armcore.Thread, count, argv uint32,
) error {
	names := client.readJavaLauncherArguments(count, argv)
	if len(names) == 0 || names[0] == "" {
		return fmt.Errorf("the launcher was given %d arguments at %#x, none of them a name", count, argv)
	}
	handle, found := client.findJavaClassRecord(names[0])
	if !found {
		return fmt.Errorf("%q is not a class in this module", names[0])
	}
	class, err := client.prepareJavaClass(ctx, thread, handle)
	if err != nil {
		return err
	}
	object, err := client.allocateJavaObject(class)
	if err != nil {
		return err
	}
	constructor, owner, ok := client.findJavaMethod(class.Record, "<init>")
	if !ok {
		return fmt.Errorf("%s declares no constructor", class.Name)
	}
	if _, err := client.callOn(ctx, thread, constructor.Body, []uint32{object}); err != nil {
		return fmt.Errorf("construct %s through %s.<init>%s at %#x: %w",
			class.Name, owner, constructor.Descriptor, constructor.Body, err)
	}
	start, owner, ok := client.findJavaMethod(class.Record, "startApp")
	if !ok {
		return fmt.Errorf("%s declares no startApp", class.Name)
	}
	// The arguments the application is started with are the ones past the class
	// name, and they arrive as a `String[]`. Nothing here builds one yet, so
	// what is passed is null: a title that reads it says so through the null
	// throw, which is a better place to find out than a guessed array.
	if _, err := client.callOn(ctx, thread, start.Body, []uint32{object, 0}); err != nil {
		return fmt.Errorf("start %s through %s.startApp%s at %#x: %w",
			class.Name, owner, start.Descriptor, start.Body, err)
	}
	return nil
}

// describeJavaLauncher reports what the launcher was asked to start: the class
// argv names, where its record is, and the method the platform would enter.
// The failure it decorates is still a failure — this says what the next step
// is rather than claiming one was taken.
func (client *Client) describeJavaLauncher(count, argv uint32) string {
	names := client.readJavaLauncherArguments(count, argv)
	if len(names) == 0 {
		return fmt.Sprintf("%d arguments at %#x, none of them a name", count, argv)
	}
	handle, found := client.findJavaClassRecord(names[0])
	if !found {
		return fmt.Sprintf("%q is not a class in this module", names[0])
	}
	record, err := client.readJavaClass(handle, nil)
	if err != nil {
		return fmt.Sprintf("%q is a class record at %#x that does not read: %v", names[0], handle, err)
	}
	parts := []string{fmt.Sprintf("%q is a class record at %#x", record.Name, handle)}
	if chain := client.javaSuperChain(record); len(chain) > 0 {
		parts = append(parts, "extending "+strings.Join(chain, " -> "))
	}
	if start, owner, ok := client.findJavaMethod(record, "startApp"); ok {
		parts = append(parts, fmt.Sprintf("with %s.startApp%s at %#x", owner, start.Descriptor, start.Body))
	}
	parts = append(parts, "argv "+quoteJavaLauncherArguments(names))
	return strings.Join(parts, ", ")
}

func quoteJavaLauncherArguments(names []string) string {
	quoted := make([]string, len(names))
	for index, name := range names {
		quoted[index] = fmt.Sprintf("%q", name)
	}
	return strings.Join(quoted, " ")
}

// readJavaLauncherArguments reads the argv the module built for the launcher.
func (client *Client) readJavaLauncherArguments(count, argv uint32) []string {
	if argv == 0 || count == 0 || count > 16 {
		return nil
	}
	names := make([]string, 0, count)
	for index := uint32(0); index < count; index++ {
		pointer, err := client.readWord(argv + index*4)
		if err != nil {
			break
		}
		text, ok := client.readPrintableString(pointer)
		if !ok {
			text = ""
		}
		names = append(names, text)
	}
	return names
}

// javaSuperChain walks a record's superclasses, naming each, up to the platform
// class the chain ends at.
func (client *Client) javaSuperChain(record javaClass) []string {
	chain := make([]string, 0, 4)
	for step := 0; step < javaLauncherChainLimit; step++ {
		if record.SuperHandle == 0 {
			if record.Super != "" {
				chain = append(chain, record.Super)
			}
			return chain
		}
		next, err := client.readJavaClass(record.SuperHandle, nil)
		if err != nil {
			return chain
		}
		chain = append(chain, next.Name)
		record = next
	}
	return chain
}

// findJavaMethod answers a method by name, from a record or from the classes it
// extends, and names the class it was found on. A method record carries its own
// entry point, which is what makes starting the application a call to an
// address the image already holds — and the lifecycle methods are as often the
// superclass's as the launcher class's own.
func (client *Client) findJavaMethod(record javaClass, name string) (javaMember, string, bool) {
	for step := 0; step < javaLauncherChainLimit; step++ {
		for _, method := range record.Methods {
			if method.Name == name {
				return method, record.Name, true
			}
		}
		if record.SuperHandle == 0 {
			break
		}
		next, err := client.readJavaClass(record.SuperHandle, nil)
		if err != nil {
			break
		}
		record = next
	}
	return javaMember{}, "", false
}
