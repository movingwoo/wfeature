package ktf

import (
	"fmt"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/wipic"
)

const maxSprintfOutput = wipic.MaxOutput

// wipicFormat renders a guest format string through the shared WIPI C
// renderer, reading %s arguments out of guest memory.
func (runtime *initializationRuntime) wipicFormat(format []byte, next func(words int) (uint64, error)) ([]byte, error) {
	return wipic.Format(format, next, func(address uint32, limit int) ([]byte, error) {
		if limit >= 0 {
			// A precision reads a slice of a buffer, which is why a game uses
			// one: the bytes it names need not be terminated at all.
			return runtime.readBoundedString(address, uint32(limit))
		}
		text, err := runtime.readCString(address, maxSprintfOutput)
		if err != nil {
			return nil, err
		}
		return []byte(text), nil
	})
}

// wipicVarargs walks the WIPI C calling convention from the first variadic
// argument slot, pairing words for the long-long modifiers.
func (runtime *initializationRuntime) wipicVarargs(thread *armcore.Thread, first int) func(int) (uint64, error) {
	argumentIndex := first
	return func(count int) (uint64, error) {
		low, argErr := runtime.wipicArgument(thread, argumentIndex)
		if argErr != nil {
			return 0, argErr
		}
		argumentIndex++
		if count == 1 {
			return uint64(low), nil
		}
		high, argErr := runtime.wipicArgument(thread, argumentIndex)
		if argErr != nil {
			return 0, argErr
		}
		argumentIndex++
		return uint64(high)<<32 | uint64(low), nil
	}
}

// wipicSprintk implements MC_knlSprintk: format into the destination buffer
// and return the number of bytes written before the terminator.
func (runtime *initializationRuntime) wipicSprintk(thread *armcore.Thread) (uint32, error) {
	destination, err := runtime.wipicArgument(thread, 0)
	if err != nil {
		return 0, err
	}
	formatAddress, err := runtime.wipicArgument(thread, 1)
	if err != nil {
		return 0, err
	}
	format, err := runtime.readCString(formatAddress, maxSprintfOutput)
	if err != nil {
		return 0, fmt.Errorf("read KTF sprintk format: %w", err)
	}
	rendered, err := runtime.wipicFormat([]byte(format), runtime.wipicVarargs(thread, 2))
	if err != nil {
		return 0, fmt.Errorf("format KTF sprintk %q: %w", format, err)
	}
	runtime.countDiagnostic("sprintk " + format)
	if err := runtime.client.core.Memory().Write(destination, append(rendered, 0)); err != nil {
		return 0, fmt.Errorf("write KTF sprintk output at %#x: %w", destination, err)
	}
	return uint32(len(rendered)), nil
}

// wipicPrintk implements MC_knlPrintk, the guest's own debug output. Games log
// their state and their failures through it, so the rendered line is reported
// to the Host rather than being reduced to a count of its format string —
// the format alone hides exactly the values a divergence is visible in.
func (runtime *initializationRuntime) wipicPrintk(thread *armcore.Thread) (uint32, error) {
	formatAddress, err := runtime.wipicArgument(thread, 0)
	if err != nil {
		return 0, err
	}
	format, err := runtime.readCString(formatAddress, maxSprintfOutput)
	if err != nil {
		// A printk that cannot be read is not worth failing the guest over.
		runtime.countDiagnostic("printk unreadable format")
		return 0, nil
	}
	rendered, err := runtime.wipicFormat([]byte(format), runtime.wipicVarargs(thread, 1))
	if err != nil {
		// Rendering can fail on an argument the subset does not model; the raw
		// format still says which call site produced it.
		runtime.countDiagnostic("printk " + format)
		runtime.client.guestPrint(format)
		return 0, nil
	}
	runtime.countDiagnostic("printk " + format)
	runtime.client.guestPrint(decodeEUCKR(rendered))
	return 0, nil
}
