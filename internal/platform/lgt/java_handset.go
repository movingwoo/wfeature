package lgt

import (
	"context"
	"fmt"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// `org/kwis/msp/handset/HandsetProperty`, the handset's own values as a Java
// title asks for them.
//
// **The answers are the WIPI C table's**, `wipic.SystemProperties`, because the
// specification defines the identifiers as the ones `MH_sysGetInformation`
// takes and there is one handset behind both sides of the platform. A title
// that asks the C way and a title that asks the Java way get the same string.

const javaHandsetPropertyClass = "org/kwis/msp/handset/HandsetProperty"

// javaSystemProperty is `HandsetProperty.getSystemProperty(String)`.
//
// An identifier the table does not carry **throws**, which is what the
// specification says and what makes the gap findable: an empty string would be
// taken for the handset's answer and stored, and a title that branches on it
// would take the branch for a value it was never given.
func javaSystemProperty(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	name, ok := client.javaText(arguments[0])
	if !ok {
		return 0, fmt.Errorf("the object at %#x holds no property name", arguments[0])
	}
	value, known := client.systemProperty(name)
	if !known {
		if client.logger != nil {
			client.logger.Debug("LGT java handset property is not one this handset has", "name", name)
		}
		return 0, client.throwJavaPlatform(thread, "java/lang/IllegalArgumentException",
			" for the handset property "+name)
	}
	if client.logger != nil {
		client.logger.Debug("LGT java handset property", "name", name, "value", value)
	}
	return client.newJavaString(value)
}

// javaSetSystemProperty is `HandsetProperty.setSystemProperty(name, value)`,
// and it answers false: no property is set. That is KTF's answer from its Java
// class, and its C MC_knlSetSystemProperty refuses every property too
// (M_E_INVALID) — a title does not get to rewrite the handset's own values, so
// what getSystemProperty answers stays what the table says.
func javaSetSystemProperty(*Client, context.Context, *armcore.Thread, []uint32) (uint32, error) {
	return javaFalse, nil
}
