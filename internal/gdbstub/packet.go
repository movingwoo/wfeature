// Package gdbstub speaks the GDB remote serial protocol on behalf of the ARM
// core, so a real debugger can attach to a running game.
//
// The protocol layer here is deliberately separate from the transport and
// from the core: it turns bytes into requests and answers into bytes, which
// is the part that can be tested without a gdb client or a socket. That
// separation is the whole reason this package exists rather than a handful of
// methods on the core — the machine this was written on has no gdb, so the
// only honest way to be confident in it is to test the protocol directly.
package gdbstub

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
)

// ErrDetached reports that the client closed the connection.
var ErrDetached = errors.New("gdb client detached")

// checksum is the protocol's modulo-256 sum of the payload.
func checksum(payload string) byte {
	sum := byte(0)
	for index := 0; index < len(payload); index++ {
		sum += payload[index]
	}
	return sum
}

// encodePacket wraps a payload as $<payload>#<checksum>.
func encodePacket(payload string) string {
	return fmt.Sprintf("$%s#%02x", payload, checksum(payload))
}

// readPacket reads one packet, acknowledging it. It skips the "+"/"-"
// acknowledgements and the interrupt byte the client sends to stop a running
// target, reporting the latter as an empty payload with interrupt set.
func readPacket(reader *bufio.Reader, writer io.Writer) (payload string, interrupt bool, err error) {
	for {
		symbol, err := reader.ReadByte()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return "", false, ErrDetached
			}
			return "", false, err
		}
		switch symbol {
		case '+', '-':
			continue
		case 0x03:
			// Ctrl-C: the client asking a running target to stop.
			return "", true, nil
		case '$':
		default:
			continue
		}

		body, err := reader.ReadString('#')
		if err != nil {
			if errors.Is(err, io.EOF) {
				return "", false, ErrDetached
			}
			return "", false, err
		}
		body = strings.TrimSuffix(body, "#")
		var digits [2]byte
		if _, err := io.ReadFull(reader, digits[:]); err != nil {
			if errors.Is(err, io.EOF) {
				return "", false, ErrDetached
			}
			return "", false, err
		}
		expected, parseErr := parseHexByte(string(digits[:]))
		if parseErr != nil || expected != checksum(body) {
			// A corrupt packet is negatively acknowledged and the client
			// resends it.
			if _, err := writer.Write([]byte{'-'}); err != nil {
				return "", false, err
			}
			continue
		}
		if _, err := writer.Write([]byte{'+'}); err != nil {
			return "", false, err
		}
		return body, false, nil
	}
}

// writePacket sends one reply.
func writePacket(writer io.Writer, payload string) error {
	_, err := io.WriteString(writer, encodePacket(payload))
	return err
}

func parseHexByte(text string) (byte, error) {
	value, err := parseHex(text)
	if err != nil {
		return 0, err
	}
	return byte(value), nil
}

// parseHex reads an unsigned hex number, which is how every address, length
// and register value arrives.
func parseHex(text string) (uint64, error) {
	if text == "" {
		return 0, fmt.Errorf("empty hex value")
	}
	value := uint64(0)
	for index := 0; index < len(text); index++ {
		digit := text[index]
		var nibble uint64
		switch {
		case digit >= '0' && digit <= '9':
			nibble = uint64(digit - '0')
		case digit >= 'a' && digit <= 'f':
			nibble = uint64(digit-'a') + 10
		case digit >= 'A' && digit <= 'F':
			nibble = uint64(digit-'A') + 10
		default:
			return 0, fmt.Errorf("invalid hex digit %q", digit)
		}
		if value > (1 << 60) {
			return 0, fmt.Errorf("hex value %q is too large", text)
		}
		value = value<<4 | nibble
	}
	return value, nil
}

// hexBytes renders a byte slice as the protocol's lower-case hex.
func hexBytes(data []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, len(data)*2)
	for index, symbol := range data {
		out[index*2] = digits[symbol>>4]
		out[index*2+1] = digits[symbol&0xf]
	}
	return string(out)
}

// parseHexBytes is the inverse.
func parseHexBytes(text string) ([]byte, error) {
	if len(text)%2 != 0 {
		return nil, fmt.Errorf("hex byte string has an odd length")
	}
	data := make([]byte, len(text)/2)
	for index := range data {
		value, err := parseHexByte(text[index*2 : index*2+2])
		if err != nil {
			return nil, err
		}
		data[index] = value
	}
	return data, nil
}

// hexWord renders a 32-bit register in the target's byte order, which for ARM
// here is little-endian.
func hexWord(value uint32) string {
	return hexBytes([]byte{byte(value), byte(value >> 8), byte(value >> 16), byte(value >> 24)})
}

// parseHexWord reads one little-endian register value.
func parseHexWord(text string) (uint32, error) {
	data, err := parseHexBytes(text)
	if err != nil {
		return 0, err
	}
	if len(data) != 4 {
		return 0, fmt.Errorf("register value is %d bytes, want 4", len(data))
	}
	return uint32(data[0]) | uint32(data[1])<<8 | uint32(data[2])<<16 | uint32(data[3])<<24, nil
}
