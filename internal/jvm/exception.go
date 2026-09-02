package jvm

import (
	"errors"
	"fmt"
)

const RuntimeExceptionClass = "java/lang/RuntimeException"

type GuestException struct {
	Object  *Object
	Message string
}

func (e *GuestException) Error() string {
	className := "java/lang/Throwable"
	if e.Object != nil && e.Object.ClassName != "" {
		className = e.Object.ClassName
	}
	if e.Message == "" {
		return className
	}
	return fmt.Sprintf("%s: %s", className, e.Message)
}

func guestException(className, message string) error {
	return &GuestException{
		Object:  &Object{ClassName: className, Native: message},
		Message: message,
	}
}

// Throw builds the error a Go method body returns to raise a guest exception
// the game can catch. The message is what the exception's getMessage answers,
// so it is written for whoever reads the game's own error path, not only for a
// log line.
func Throw(className, message string) error {
	return guestException(className, message)
}

// IsGuestException reports whether err contains a guest exception assignable to
// className. It recognizes both runtime-owned core exception classes and
// application classes loaded from the configured class source.
func (vm *VM) IsGuestException(err error, className string) bool {
	if className == "" {
		return false
	}
	var guest *GuestException
	if !errors.As(err, &guest) || guest.Object == nil || guest.Object.ClassName == "" {
		return false
	}
	return vm.catches(guest.Object.ClassName, className)
}

func (vm *VM) catches(exceptionClass, catchClass string) bool {
	if catchClass == "" || exceptionClass == catchClass {
		return true
	}

	for current := exceptionClass; current != ""; {
		if current == catchClass {
			return true
		}
		if parent := runtimeClassParent(current); parent != "" {
			current = parent
			continue
		}
		if class, err := vm.loader.Load(current); err == nil {
			if class.SuperName == current {
				break
			}
			current = class.SuperName
			continue
		}
		break
	}
	return false
}

// runtimeClassParents is the superclass chain of the exception types the
// runtime knows without a class file. It is built once: the lookup runs on
// every native method resolution, so a map literal here would allocate the
// whole table on each call.
var runtimeClassParents = map[string]string{
	"java/lang/ArithmeticException":             "java/lang/RuntimeException",
	"java/lang/ClassCastException":              "java/lang/RuntimeException",
	"java/lang/IllegalArgumentException":        "java/lang/RuntimeException",
	"java/lang/IllegalStateException":           "java/lang/RuntimeException",
	"java/lang/NullPointerException":            "java/lang/RuntimeException",
	"java/lang/NegativeArraySizeException":      "java/lang/RuntimeException",
	"java/lang/IndexOutOfBoundsException":       "java/lang/RuntimeException",
	"java/lang/ArrayIndexOutOfBoundsException":  "java/lang/IndexOutOfBoundsException",
	"java/lang/StringIndexOutOfBoundsException": "java/lang/IndexOutOfBoundsException",
	"java/lang/IllegalMonitorStateException":    "java/lang/RuntimeException",
	"java/lang/ArrayStoreException":             "java/lang/RuntimeException",
	"java/lang/IllegalThreadStateException":     "java/lang/IllegalArgumentException",
	"java/lang/NumberFormatException":           "java/lang/IllegalArgumentException",
	RuntimeExceptionClass:                       "java/lang/Exception",
	"java/lang/InterruptedException":            "java/lang/Exception",
	"java/io/IOException":                       "java/lang/Exception",
	"java/io/EOFException":                      "java/io/IOException",
	"java/io/UTFDataFormatException":            "java/io/IOException",
	"java/io/UnsupportedEncodingException":      "java/io/IOException",
	"java/lang/ClassNotFoundException":          "java/lang/Exception",
	"java/lang/Exception":                       "java/lang/Throwable",
	"java/lang/Error":                           "java/lang/Throwable",
	// A title that guards a large allocation catches this one by name. The
	// runtime never raises it — an allocation past a bound here is refused
	// with the limit that refused it — but the class has to exist or the catch
	// resolves to nothing and the session ends where the handset caught and
	// carried on.
	"java/lang/VirtualMachineError":    "java/lang/Error",
	"java/lang/OutOfMemoryError":       "java/lang/VirtualMachineError",
	"java/util/NoSuchElementException": "java/lang/RuntimeException",
	"java/util/EmptyStackException":    "java/lang/RuntimeException",
	"java/lang/Throwable":              "java/lang/Object",
}

func runtimeClassParent(className string) string {
	return runtimeClassParents[className]
}

// ThrowableParents copies the superclass chain of the exception types this
// runtime knows without a class file. A platform that publishes its own class
// records to guest code builds them from this rather than from a list of its
// own, for the reason the declarations here are built from it: the class a
// `catch` resolves and the class a `new` resolves cannot be allowed to
// disagree.
func ThrowableParents() map[string]string {
	parents := make(map[string]string, len(runtimeClassParents))
	for name, parent := range runtimeClassParents {
		parents[name] = parent
	}
	return parents
}
