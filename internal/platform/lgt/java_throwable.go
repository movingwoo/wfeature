package lgt

import (
	"context"
	"strings"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// `java/lang/Throwable`'s getMessage, toString and printStackTrace: the three
// a title's catch block calls on what it caught.
//
// Throwable's own methods take its slots from 10 — getMessage, then
// printStackTrace — and toString takes Object's 4. Every exception class is
// below Throwable in javaPlatformSupers, so one entry answers for all of them.
//
// **The message is the one the platform threw with.** An exception this
// platform raises is built here (throwJavaPlatform), and the text that names
// what went wrong is kept against the object as its message, the way the KTF
// and SKT runtimes give a host-raised exception the same text. An exception
// with none answers null, as one built without a message does.

// javaThrowMessage is the message a platform throw's description becomes: the
// description without the separator the uncaught report puts in front of it.
func javaThrowMessage(what string) string {
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(what), ":"))
}

// javaThrowableGetMessage is `getMessage()`, slot 10.
func javaThrowableGetMessage(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	message, ok := client.javaRuntimeState().messages[arguments[0]]
	if !ok {
		return 0, nil
	}
	return client.newJavaString(message)
}

// javaThrowableText is `Throwable.toString()`: the class's name, and ": " and
// the message when there is one — the receiver's own getMessage, as the class
// library asks it.
func (client *Client) javaThrowableText(
	ctx context.Context, thread *armcore.Thread, object uint32,
) (string, error) {
	name := "java.lang.Throwable"
	if class, known := client.javaClassOfObject(object); known {
		name = javaDottedName(class.Name)
	}
	message, err := client.javaCallVirtual(ctx, thread, object, 10)
	if err != nil {
		return "", err
	}
	if message == 0 {
		return name, nil
	}
	text, ok := client.javaText(message)
	if !ok {
		return name, nil
	}
	return name + ": " + text, nil
}

// javaThrowableToString is `toString()`, slot 4.
func javaThrowableToString(
	client *Client, ctx context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	text, err := client.javaThrowableText(ctx, thread, arguments[0])
	if err != nil {
		return 0, err
	}
	return client.newJavaString(text)
}

// javaThrowablePrintStackTrace is `printStackTrace()`, slot 11: the
// throwable's toString on the error stream, which is the same line a
// `System.err.println` makes here (java_print.go), with the guest stack it was
// printed from beside it.
func javaThrowablePrintStackTrace(
	client *Client, ctx context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	text, err := client.javaToStringOf(ctx, thread, arguments[0])
	if err != nil {
		return 0, err
	}
	if client.logger != nil {
		client.logger.Debug("LGT java print", "text", text, "stack", client.javaBacktraceLine(thread))
	}
	return 0, nil
}
