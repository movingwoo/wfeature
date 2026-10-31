package wipicrt

import (
	"fmt"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/wipic"
)

const maxSprintfOutput = wipic.MaxOutput

// wipicFormat renders a guest format string through the shared WIPI C
// renderer, reading %s arguments out of guest memory.
func (runtime *Runtime) wipicFormat(format []byte, next func(words int) (uint64, error)) ([]byte, error) {
	return wipic.Format(format, next, func(address uint32, limit int) ([]byte, error) {
		if limit >= 0 {
			// A precision reads a slice of a buffer, which is why a game uses
			// one: the bytes it names need not be terminated at all.
			return runtime.ReadBoundedString(address, uint32(limit))
		}
		text, err := runtime.ReadCString(address, maxSprintfOutput)
		if err != nil {
			return nil, err
		}
		return []byte(text), nil
	})
}

// wipicVarargs walks the WIPI C calling convention from the first variadic
// argument slot, pairing words for the long-long modifiers.
func (runtime *Runtime) wipicVarargs(thread *armcore.Thread, first int) func(int) (uint64, error) {
	argumentIndex := first
	return func(count int) (uint64, error) {
		low, argErr := runtime.WIPICArgument(thread, argumentIndex)
		if argErr != nil {
			return 0, argErr
		}
		argumentIndex++
		if count == 1 {
			return uint64(low), nil
		}
		high, argErr := runtime.WIPICArgument(thread, argumentIndex)
		if argErr != nil {
			return 0, argErr
		}
		argumentIndex++
		return uint64(high)<<32 | uint64(low), nil
	}
}

// wipicSprintk implements MC_knlSprintk: format into the destination buffer
// and return the number of bytes written before the terminator.
func (runtime *Runtime) wipicSprintk(thread *armcore.Thread) (uint32, error) {
	destination, err := runtime.WIPICArgument(thread, 0)
	if err != nil {
		return 0, err
	}
	formatAddress, err := runtime.WIPICArgument(thread, 1)
	if err != nil {
		return 0, err
	}
	format, err := runtime.ReadCString(formatAddress, maxSprintfOutput)
	if err != nil {
		return 0, fmt.Errorf("read %s sprintk format: %w", runtime.platformName(), err)
	}
	rendered, err := runtime.wipicFormat([]byte(format), runtime.wipicVarargs(thread, 2))
	if err != nil {
		return 0, fmt.Errorf("format %s sprintk %q: %w", runtime.platformName(), format, err)
	}
	runtime.CountDiagnostic("sprintk " + format)
	if err := runtime.Client.core.Memory().Write(destination, append(rendered, 0)); err != nil {
		return 0, fmt.Errorf("write %s sprintk output at %#x: %w", runtime.platformName(), destination, err)
	}
	return uint32(len(rendered)), nil
}

// wipicPrintk implements MC_knlPrintk, the guest's own debug output. Games log
// their state and their failures through it, so the rendered line is reported
// to the Host rather than being reduced to a count of its format string —
// the format alone hides exactly the values a divergence is visible in.
func (runtime *Runtime) wipicPrintk(thread *armcore.Thread) (uint32, error) {
	formatAddress, err := runtime.WIPICArgument(thread, 0)
	if err != nil {
		return 0, err
	}
	format, err := runtime.ReadCString(formatAddress, maxSprintfOutput)
	if err != nil {
		// A printk that cannot be read is not worth failing the guest over.
		runtime.CountDiagnostic("printk unreadable format")
		return 0, nil
	}
	rendered, err := runtime.wipicFormat([]byte(format), runtime.wipicVarargs(thread, 1))
	if err != nil {
		// Rendering can fail on an argument the subset does not model; the raw
		// format still says which call site produced it.
		runtime.CountDiagnostic("printk " + format)
		runtime.Client.GuestPrint(format)
		return 0, nil
	}
	runtime.CountDiagnostic("printk " + format)
	runtime.Client.GuestPrint(backend.DecodeEUCKR(rendered))
	return 0, nil
}
