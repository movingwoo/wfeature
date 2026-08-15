package skt

import (
	"github.com/movingwoo/wfeature/internal/api/midp"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// registerConnectorNatives connects the Generic Connection Framework factory
// to this runtime. Every entry lands on the same refusal, so the table is the
// whole surface: a name that reached one of these methods never gets a
// connection back.
func (runtime *Runtime) registerConnectorNatives() error {
	const (
		text       = "Ljava/lang/String;"
		connection = "Ljavax/microedition/io/Connection;"
	)

	registrations := []nativeRegistration{
		{midp.ConnectorClass, "open", "(" + text + ")" + connection, runtime.refuseConnection},
		{midp.ConnectorClass, "open", "(" + text + "I)" + connection, runtime.refuseConnection},
		{midp.ConnectorClass, "open", "(" + text + "IZ)" + connection, runtime.refuseConnection},
		{midp.ConnectorClass, "openInputStream", "(" + text + ")Ljava/io/InputStream;", runtime.refuseConnection},
		{midp.ConnectorClass, "openDataInputStream", "(" + text + ")Ljava/io/DataInputStream;", runtime.refuseConnection},
		{midp.ConnectorClass, "openOutputStream", "(" + text + ")Ljava/io/OutputStream;", runtime.refuseConnection},
		{midp.ConnectorClass, "openDataOutputStream", "(" + text + ")Ljava/io/DataOutputStream;", runtime.refuseConnection},
	}

	for _, registration := range registrations {
		if err := runtime.registerNative(registration.class, registration.name, registration.descriptor, registration.method); err != nil {
			return err
		}
	}
	return nil
}

// refuseConnection answers every Connector call with the exception MIDP names
// for a target that cannot be found or a protocol that is not supported. Both
// are true here: nothing in this runtime reaches a network.
//
// Refusing beats the two alternatives. Handing back a connection that never
// delivers bytes strands a game on a screen it only leaves when a read
// completes, and that is a state its author never saw, because a handset with
// no coverage refused the connection outright. Leaving the class absent is
// worse still: the loader answers "class not found", which stops the session
// instead of reaching the game's own catch block.
//
// The refusal covers the stream helpers as well as open(). A game that asks
// for the stream directly is asking for the same connection in one step, and
// answering that one differently would only move the failure to the first
// read.
func (runtime *Runtime) refuseConnection(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	name, err := stringArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if runtime.logger != nil {
		runtime.logger.Debug("MIDP connection refused", "name", name)
	}
	return jvm.VoidValue(), newGuestException(midp.ConnectionNotFoundExceptionClass, name)
}
