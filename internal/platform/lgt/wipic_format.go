package lgt

import (
	"fmt"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/wipic"
)

// wipicArgument reads one WIPI C argument. The first four are in registers and
// the rest are on the stack, which only the variadic slots reach.
func (client *Client) wipicArgument(thread *armcore.Thread, index int) (uint32, error) {
	if index < 4 {
		return thread.Register(index)
	}
	stack, err := thread.Register(armcore.RegisterSP)
	if err != nil {
		return 0, err
	}
	return client.readWord(stack + uint32(index-4)*4)
}

// wipicVarargs walks the arguments from the first variadic slot, pairing words
// for the long-long modifiers.
func (client *Client) wipicVarargs(thread *armcore.Thread, first int) func(int) (uint64, error) {
	index := first
	return func(words int) (uint64, error) {
		low, err := client.wipicArgument(thread, index)
		if err != nil {
			return 0, err
		}
		index++
		if words == 1 {
			return uint64(low), nil
		}
		high, err := client.wipicArgument(thread, index)
		if err != nil {
			return 0, err
		}
		index++
		return uint64(high)<<32 | uint64(low), nil
	}
}

// wipicFormat renders a guest format string, reading %s arguments out of guest
// memory. The renderer is shared with the other WIPI platform: a format string
// means the same thing wherever the call arrived from.
func (client *Client) wipicFormat(thread *armcore.Thread, format []byte, first int) ([]byte, error) {
	return wipic.Format(format, client.wipicVarargs(thread, first), func(address uint32, limit int) ([]byte, error) {
		if limit >= 0 {
			// A precision reads a slice of a buffer, which is why a game uses
			// one: the bytes it names need not be terminated at all.
			return client.readBoundedString(address, uint32(limit)), nil
		}
		text, err := client.readCString(address)
		if err != nil {
			return nil, err
		}
		return []byte(text), nil
	})
}

// wipicSprintk formats into the destination buffer and answers the number of
// bytes written before the terminator.
func (client *Client) wipicSprintk(thread *armcore.Thread) (uint32, error) {
	destination, err := client.wipicArgument(thread, 0)
	if err != nil {
		return 0, err
	}
	formatAddress, err := client.wipicArgument(thread, 1)
	if err != nil {
		return 0, err
	}
	format, err := client.readCString(formatAddress)
	if err != nil {
		return 0, fmt.Errorf("read LGT sprintk format: %w", err)
	}
	rendered, err := client.wipicFormat(thread, []byte(format), 2)
	if err != nil {
		return 0, fmt.Errorf("format LGT sprintk %q: %w", format, err)
	}
	if err := client.core.Memory().Write(destination, append(rendered, 0)); err != nil {
		return 0, fmt.Errorf("write LGT sprintk output at %#x: %w", destination, err)
	}
	return uint32(len(rendered)), nil
}
