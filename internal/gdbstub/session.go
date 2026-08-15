package gdbstub

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// Target is what the stub debugs. It is an interface rather than the ARM core
// directly so the protocol can be tested against a stand-in — the only way to
// be sure of a wire format without the client that speaks it.
type Target interface {
	// Registers answers gdb's ARM general register file: r0-r15 then CPSR.
	Registers() ([16]uint32, uint32)
	SetRegister(index int, value uint32) error
	SetCPSR(value uint32)
	ReadMemory(address uint32, length int) ([]byte, error)
	WriteMemory(address uint32, data []byte) error
	SetBreakpoint(address uint32)
	ClearBreakpoint(address uint32)
	// Continue resumes until the next stop and reports the signal that
	// stopped it. Step resumes for one instruction.
	Continue() (Signal, error)
	Step() (Signal, error)
	// Kill ends the session.
	Kill()
}

// Signal is the stop reason reported to the client, in gdb's numbering.
type Signal int

const (
	// SignalTrap is a breakpoint or a completed single step.
	SignalTrap Signal = 5
	// SignalKill is the target ending.
	SignalKill Signal = 9
)

// gdb's ARM layout is r0-r15, eight 96-bit legacy FPA registers, the FPA
// status word, and then CPSR. The FPA registers do not exist on this core, so
// they are reported as zero — a client that reads them gets zeros rather than
// a short packet it would reject.
const (
	fpaRegisters   = 8
	fpaRegisterHex = 24 // 96 bits
)

// Serve runs the protocol on one connection until the client detaches or the
// target is killed.
func Serve(connection io.ReadWriter, target Target) error {
	reader := bufio.NewReader(connection)
	// The target is stopped when a client attaches; gdb expects to be told so
	// as soon as it asks.
	for {
		payload, interrupt, err := readPacket(reader, connection)
		if err != nil {
			return err
		}
		if interrupt {
			if err := writePacket(connection, stopReply(SignalTrap)); err != nil {
				return err
			}
			continue
		}
		reply, done, err := handle(payload, target)
		if err != nil {
			return err
		}
		if err := writePacket(connection, reply); err != nil {
			return err
		}
		if done {
			return nil
		}
	}
}

func stopReply(signal Signal) string {
	return fmt.Sprintf("S%02x", int(signal))
}

// handle turns one request into one reply. An unknown request answers the
// empty packet, which is how the protocol says "not supported" — a client
// then falls back rather than failing.
func handle(payload string, target Target) (reply string, done bool, err error) {
	if payload == "" {
		return "", false, nil
	}
	switch payload[0] {
	case '?':
		return stopReply(SignalTrap), false, nil

	case 'g':
		registers, cpsr := target.Registers()
		builder := &strings.Builder{}
		for _, value := range registers {
			builder.WriteString(hexWord(value))
		}
		for index := 0; index < fpaRegisters; index++ {
			builder.WriteString(strings.Repeat("0", fpaRegisterHex))
		}
		builder.WriteString(hexWord(0)) // FPA status
		builder.WriteString(hexWord(cpsr))
		return builder.String(), false, nil

	case 'G':
		return writeAllRegisters(payload[1:], target)

	case 'p':
		index, parseErr := parseHex(payload[1:])
		if parseErr != nil {
			return "E01", false, nil
		}
		registers, cpsr := target.Registers()
		switch {
		case index < 16:
			return hexWord(registers[index]), false, nil
		case index == 25:
			return hexWord(cpsr), false, nil
		}
		return strings.Repeat("0", fpaRegisterHex), false, nil

	case 'P':
		return writeOneRegister(payload[1:], target)

	case 'm':
		return readMemory(payload[1:], target)

	case 'M':
		return writeMemory(payload[1:], target)

	case 'Z', 'z':
		return breakpoint(payload, target)

	case 'c':
		signal, runErr := target.Continue()
		if runErr != nil {
			return "E03", false, nil
		}
		return stopReply(signal), false, nil

	case 's':
		signal, runErr := target.Step()
		if runErr != nil {
			return "E03", false, nil
		}
		return stopReply(signal), false, nil

	case 'k':
		target.Kill()
		return "", true, nil

	case 'D':
		return "OK", true, nil

	case 'q':
		return query(payload), false, nil

	case 'H':
		// Thread selection: this core presents one thread to the debugger,
		// so any selection is accepted.
		return "OK", false, nil
	}
	return "", false, nil
}

func query(payload string) string {
	switch {
	case strings.HasPrefix(payload, "qSupported"):
		// PacketSize is in hex and bounds one memory transfer.
		return "PacketSize=1000"
	case payload == "qC":
		return "QC1"
	case payload == "qAttached":
		// 1 means "attached to an existing process", so a client's detach
		// leaves the game running rather than killing it.
		return "1"
	case strings.HasPrefix(payload, "qfThreadInfo"):
		return "m1"
	case strings.HasPrefix(payload, "qsThreadInfo"):
		return "l"
	}
	return ""
}

func writeAllRegisters(text string, target Target) (string, bool, error) {
	if len(text) < 16*8 {
		return "E01", false, nil
	}
	for index := 0; index < 16; index++ {
		value, err := parseHexWord(text[index*8 : index*8+8])
		if err != nil {
			return "E01", false, nil
		}
		if err := target.SetRegister(index, value); err != nil {
			return "E01", false, nil
		}
	}
	// CPSR follows the FPA block when the client sent a full register file.
	cpsrOffset := 16*8 + fpaRegisters*fpaRegisterHex + 8
	if len(text) >= cpsrOffset+8 {
		if value, err := parseHexWord(text[cpsrOffset : cpsrOffset+8]); err == nil {
			target.SetCPSR(value)
		}
	}
	return "OK", false, nil
}

func writeOneRegister(text string, target Target) (string, bool, error) {
	name, value, found := strings.Cut(text, "=")
	if !found {
		return "E01", false, nil
	}
	index, err := parseHex(name)
	if err != nil {
		return "E01", false, nil
	}
	word, err := parseHexWord(value)
	if err != nil {
		return "E01", false, nil
	}
	switch {
	case index < 16:
		if err := target.SetRegister(int(index), word); err != nil {
			return "E01", false, nil
		}
	case index == 25:
		target.SetCPSR(word)
	default:
		// A register this core does not have is accepted and dropped rather
		// than refused, because refusing makes gdb abandon the whole write.
	}
	return "OK", false, nil
}

// maxTransfer bounds one memory request, matching the PacketSize advertised
// to the client.
const maxTransfer = 0x800

func readMemory(text string, target Target) (string, bool, error) {
	addressText, lengthText, found := strings.Cut(text, ",")
	if !found {
		return "E01", false, nil
	}
	address, err := parseHex(addressText)
	if err != nil {
		return "E01", false, nil
	}
	length, err := parseHex(lengthText)
	if err != nil || length == 0 {
		return "E01", false, nil
	}
	if length > maxTransfer {
		length = maxTransfer
	}
	data, err := target.ReadMemory(uint32(address), int(length))
	if err != nil {
		// Unreadable memory is an error reply, not an empty one: gdb prints
		// "Cannot access memory" rather than showing zeros as real values.
		return "E05", false, nil
	}
	return hexBytes(data), false, nil
}

func writeMemory(text string, target Target) (string, bool, error) {
	header, payload, found := strings.Cut(text, ":")
	if !found {
		return "E01", false, nil
	}
	addressText, lengthText, found := strings.Cut(header, ",")
	if !found {
		return "E01", false, nil
	}
	address, err := parseHex(addressText)
	if err != nil {
		return "E01", false, nil
	}
	length, err := parseHex(lengthText)
	if err != nil {
		return "E01", false, nil
	}
	data, err := parseHexBytes(payload)
	if err != nil || uint64(len(data)) != length {
		return "E01", false, nil
	}
	if err := target.WriteMemory(uint32(address), data); err != nil {
		return "E05", false, nil
	}
	return "OK", false, nil
}

// breakpoint handles Z0/z0 (software) and Z1/z1 (hardware). This core has one
// mechanism, so both kinds map onto it; watchpoints are not supported and
// answer the empty packet so gdb falls back to single-stepping.
func breakpoint(payload string, target Target) (string, bool, error) {
	if len(payload) < 2 {
		return "E01", false, nil
	}
	kind := payload[1]
	if kind != '0' && kind != '1' {
		return "", false, nil
	}
	fields := strings.Split(payload[2:], ",")
	if len(fields) < 2 || fields[0] != "" && !strings.HasPrefix(payload[2:], ",") {
		// The form is Z0,<addr>,<kind>, so the first field after the type is
		// empty.
		return "E01", false, nil
	}
	if len(fields) < 2 {
		return "E01", false, nil
	}
	address, err := parseHex(fields[1])
	if err != nil {
		return "E01", false, nil
	}
	if payload[0] == 'Z' {
		target.SetBreakpoint(uint32(address))
	} else {
		target.ClearBreakpoint(uint32(address))
	}
	return "OK", false, nil
}
