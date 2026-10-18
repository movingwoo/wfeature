package gdbstub

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"sync"
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

// liveTarget is a Target running a real guest, which its client can reach
// while it runs: resume takes the client's Ctrl-C through interrupt, and
// endClient undoes what the client set up and lets the guest go when the
// client leaves, by detaching or by dropping the connection. CoreTarget is
// one. A target that is not — the protocol tests' stand-in, which never runs —
// is resumed in line, because nothing the client sends could reach it while it
// runs.
type liveTarget interface {
	Target
	attachClient() uint64
	resume(client uint64, mode resumeMode, interrupt <-chan struct{}) (Signal, error)
	gone() (string, bool)
	endClient()
}

// request is one thing read from the client: a packet, the interrupt byte,
// or the error that ended the connection.
type request struct {
	payload   string
	interrupt bool
	err       error
}

// Serve runs the protocol on one connection until the client detaches, the
// connection ends or the target is killed. However it ends, a live target is
// told its client has gone, so a guest the client stopped is let go.
//
// The connection is read on a goroutine of its own, because a client has
// something to say while the target runs — a Ctrl-C, or a hang-up — and the
// wait for the next stop has to hear it.
func Serve(connection io.ReadWriter, target Target) error {
	live, isLive := target.(liveTarget)
	var client uint64
	if isLive {
		client = live.attachClient()
		defer live.endClient()
	}
	writer := &serialWriter{writer: connection}
	defer writer.close()
	requests := make(chan request)
	quit := make(chan struct{})
	defer close(quit)
	go readRequests(bufio.NewReader(connection), writer, requests, quit)

	// The target is stopped when a client attaches; gdb expects to be told so
	// as soon as it asks.
	var backlog []request
	for {
		var next request
		if len(backlog) > 0 {
			next, backlog = backlog[0], backlog[1:]
		} else {
			next = <-requests
		}
		if next.err != nil {
			return next.err
		}
		if next.interrupt {
			if err := writePacket(writer, stopReply(SignalTrap)); err != nil {
				return err
			}
			continue
		}
		if next.payload == "k" {
			// A kill has no reply, which the protocol says: the target it
			// would come from is gone. A reply nobody reads would also hold
			// a connection whose client has stopped reading.
			target.Kill()
			return nil
		}
		var reply string
		var done bool
		var err error
		if isLive && (resumes(next.payload) || next.payload == "?") {
			reply, backlog, err = run(next.payload, live, client, requests, backlog)
		} else {
			reply, done, err = handle(next.payload, target)
		}
		if err != nil {
			return err
		}
		if err := writePacket(writer, reply); err != nil {
			return err
		}
		if done {
			return nil
		}
	}
}

// readRequests reads the connection until it ends, handing Serve each request
// and finally the error that ended it. It stops early once Serve has returned.
func readRequests(reader *bufio.Reader, writer io.Writer, requests chan<- request, quit <-chan struct{}) {
	for {
		payload, interrupt, err := readPacket(reader, writer)
		select {
		case requests <- request{payload: payload, interrupt: interrupt, err: err}:
		case <-quit:
			return
		}
		if err != nil {
			return
		}
	}
}

// resumes reports whether a request runs the target.
func resumes(payload string) bool {
	return payload != "" && (payload[0] == 'c' || payload[0] == 's')
}

// run resumes a live target and waits for its stop while still listening to
// the client. A Ctrl-C interrupts the target, and a request that arrives
// meanwhile waits its turn in the backlog. A connection that ends abandons
// the wait: the client is told its target has gone, which lets the guest go
// and returns the resume that was waiting.
//
// '?' comes here too. A client asks it first and takes the answer as
// "stopped": its register and memory reads follow from there. So a guest
// that is running — on a client's first connection, or after the last one
// left — is stopped at its next instruction, the way an interrupt stops it,
// and the client reads the place it stopped rather than an empty register
// file; one that is already stopped is answered at once.
func run(payload string, target liveTarget, client uint64, requests <-chan request, backlog []request) (string, []request, error) {
	mode := resumeHalt
	switch payload[0] {
	case 'c':
		mode = resumeContinue
	case 's':
		mode = resumeStep
	}
	interrupt := make(chan struct{}, 1)
	result := make(chan string, 1)
	go func() {
		signal, err := target.resume(client, mode, interrupt)
		if err != nil {
			result <- "E03"
			return
		}
		if reply, ended := target.gone(); ended && signal == SignalKill {
			result <- reply
			return
		}
		result <- stopReply(signal)
	}()
	for {
		select {
		case reply := <-result:
			return reply, backlog, nil
		case next := <-requests:
			switch {
			case next.err != nil:
				target.endClient()
				return "", backlog, next.err
			case next.interrupt:
				select {
				case interrupt <- struct{}{}:
				default:
				}
			default:
				backlog = append(backlog, next)
			}
		}
	}
}

// serialWriter lets the reading goroutine's acknowledgements and Serve's
// replies share the connection: one write at a time, and none once Serve has
// returned and the connection is no longer its to write.
type serialWriter struct {
	mu     sync.Mutex
	writer io.Writer
	closed bool
}

func (writer *serialWriter) Write(data []byte) (int, error) {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if writer.closed {
		return 0, io.ErrClosedPipe
	}
	return writer.writer.Write(data)
}

func (writer *serialWriter) close() {
	writer.mu.Lock()
	writer.closed = true
	writer.mu.Unlock()
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
	// Parsed whole before any of it is written, so a malformed packet is
	// refused rather than half-applied.
	var values [16]uint32
	for index := range values {
		value, err := parseHexWord(text[index*8 : index*8+8])
		if err != nil {
			return "E01", false, nil
		}
		values[index] = value
	}
	for index, value := range values {
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
