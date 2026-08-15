package gdbstub

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
)

// Listen accepts one gdb connection at a time on address and serves it
// against target. It returns when the listener is closed.
//
// One at a time is deliberate: two clients driving the same stopped guest
// would each see the other's resumes.
func Listen(listener net.Listener, target Target, logger *slog.Logger) error {
	for {
		connection, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return fmt.Errorf("accept gdb connection: %w", err)
		}
		if logger != nil {
			logger.Debug("gdb client attached", "remote", connection.RemoteAddr())
		}
		err = Serve(connection, target)
		connection.Close()
		if logger != nil {
			logger.Debug("gdb client detached", "error", err)
		}
		if err != nil && !errors.Is(err, ErrDetached) && !errors.Is(err, io.EOF) {
			return err
		}
	}
}

// ListenAndServe opens a listener and serves it. The caller closes the
// returned listener to stop.
func ListenAndServe(address string, target Target, logger *slog.Logger) (net.Listener, error) {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("listen for gdb on %s: %w", address, err)
	}
	go func() {
		if err := Listen(listener, target, logger); err != nil && logger != nil {
			logger.Error("gdb stub stopped", "error", err)
		}
	}()
	return listener, nil
}
