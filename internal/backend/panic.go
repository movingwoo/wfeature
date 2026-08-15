package backend

import (
	"fmt"
	"log/slog"
	"runtime/debug"
)

// guestPanicStack bounds the stack a report carries. A guest call chain is
// deep — an ARM thread inside a JVM invocation inside a platform native is
// routine — and the frames near the panic are the ones worth having.
const guestPanicStack = 16 << 10

// GuestPanic turns a recovered panic into the error the caller was already
// prepared to receive.
//
// Guest code runs on goroutines of its own: a platform's guest threads, and a
// session start that has to keep the Host's event loop alive while it runs. A
// panic on one of those is not the Go runtime's usual "this program has a bug
// worth stopping for" — it is one archive doing something this emulator does
// not implement yet, on a machine where the only person who will ever read the
// crash is the one who was playing. Left alone it takes the process with it,
// and with the process goes everything that would have said what happened: the
// session's own post-mortem report, the ordered boundary trace, the page's log
// and the sibling session that was fine. **The panic itself is the least
// informative failure this emulator has, which is the argument for converting
// it rather than for surviving it.**
//
// So the stack goes to the log, where a debug build's report and the server's
// own log file both keep it, and the caller gets a one-line error to put where
// it already puts "the game failed". Nothing here retries or continues: the
// thread that panicked is over, and its session ends the way any failed
// session does.
func GuestPanic(logger *slog.Logger, where string, recovered any) error {
	stack := debug.Stack()
	if len(stack) > guestPanicStack {
		stack = stack[:guestPanicStack]
	}
	if logger != nil {
		logger.Error("emulator panicked", "where", where, "panic", fmt.Sprint(recovered), "stack", string(stack))
	}
	return fmt.Errorf("%s panicked: %v", where, recovered)
}
