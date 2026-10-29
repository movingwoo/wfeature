package lgt

import (
	"context"
	"fmt"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// `java/lang/System` and `java/lang/Runtime`: the property table, the ending,
// and the identity hash.

// javaSystemPropertyTable is what CLDC guarantees System.getProperty answers,
// stated as what this runtime is rather than as a handset it is not. It is the
// table the KTF runtime answers from (internal/platform/ktf), because the
// question is the same on both platforms; everything else comes from the
// handset table HandsetProperty and the WIPI C call answer from.
var javaSystemPropertyTable = map[string]string{
	"microedition.platform":      "wfeature",
	"microedition.configuration": "CLDC-1.0",
	"microedition.profiles":      "",
	"microedition.encoding":      "EUC-KR",
	"microedition.locale":        "ko-KR",
}

// javaSystemGetProperty is `System.getProperty(String)`. **An unknown name is
// null**, as the specification has it, rather than the IllegalArgumentException
// HandsetProperty throws for one: a title that probes for an optional property
// tests the answer against null, and an empty string would read as "the handset
// has it and calls it nothing".
func javaSystemGetProperty(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	if arguments[0] == 0 {
		return 0, client.throwJavaPlatform(thread, javaThrowNullClass, ": System.getProperty(null)")
	}
	name, ok := client.javaText(arguments[0])
	if !ok {
		return 0, fmt.Errorf("the object at %#x holds no property name", arguments[0])
	}
	value, known := javaSystemPropertyTable[name]
	if !known {
		value, known = client.systemProperty(name)
	}
	if client.logger != nil {
		client.logger.Debug("LGT java system property", "name", name, "known", known, "value", value)
	}
	if !known {
		return 0, nil
	}
	return client.newJavaString(value)
}

// javaSystemIdentityHash is `System.identityHashCode(Object)`: the hash
// Object's own hashCode answers, whatever the class overrides, and 0 for null.
func javaSystemIdentityHash(
	_ *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	return javaIdentityHash(arguments[0]), nil
}

// javaSystemExit is `System.exit(int)`, and javaRuntimeExit the same call on
// the runtime, slot 10 of `java/lang/Runtime`.
func javaSystemExit(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	return 0, client.javaExit(thread, int32(arguments[0]))
}

func javaRuntimeExit(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	return 0, client.javaExit(thread, int32(arguments[1]))
}

// javaExit is the title ending itself, which is the ending `notifyDestroyed`
// already gives a session (ErrGuestExited). The status is a number for a
// process the handset never shows, so it is only reported.
func (client *Client) javaExit(thread *armcore.Thread, status int32) error {
	client.exited = true
	client.exitedFrom, _ = thread.Register(14)
	client.flushOpenFiles()
	return fmt.Errorf("exit(%d): %w", status, client.exitError())
}
