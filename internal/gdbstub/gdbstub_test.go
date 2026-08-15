package gdbstub

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

// stubTarget stands in for the ARM core so the wire format can be tested
// without a running game — and without gdb, which this machine does not have.
type stubTarget struct {
	registers  [16]uint32
	cpsr       uint32
	memory     map[uint32]byte
	breakpoint map[uint32]bool
	continued  int
	stepped    int
	killed     bool
	readFails  bool
}

func newStubTarget() *stubTarget {
	return &stubTarget{memory: map[uint32]byte{}, breakpoint: map[uint32]bool{}}
}

func (target *stubTarget) Registers() ([16]uint32, uint32) { return target.registers, target.cpsr }

func (target *stubTarget) SetRegister(index int, value uint32) error {
	if index < 0 || index >= 16 {
		return errors.New("bad register")
	}
	target.registers[index] = value
	return nil
}

func (target *stubTarget) SetCPSR(value uint32) { target.cpsr = value }

func (target *stubTarget) ReadMemory(address uint32, length int) ([]byte, error) {
	if target.readFails {
		return nil, errors.New("unmapped")
	}
	data := make([]byte, length)
	for index := range data {
		data[index] = target.memory[address+uint32(index)]
	}
	return data, nil
}

func (target *stubTarget) WriteMemory(address uint32, data []byte) error {
	for index, symbol := range data {
		target.memory[address+uint32(index)] = symbol
	}
	return nil
}

func (target *stubTarget) SetBreakpoint(address uint32)   { target.breakpoint[address] = true }
func (target *stubTarget) ClearBreakpoint(address uint32) { delete(target.breakpoint, address) }

func (target *stubTarget) Continue() (Signal, error) {
	target.continued++
	return SignalTrap, nil
}

func (target *stubTarget) Step() (Signal, error) {
	target.stepped++
	return SignalTrap, nil
}

func (target *stubTarget) Kill() { target.killed = true }

// conversation drives a whole session over a pipe of packets and returns the
// replies, which is what a client would see.
func conversation(t *testing.T, target Target, requests ...string) []string {
	t.Helper()
	input := &bytes.Buffer{}
	for _, request := range requests {
		input.WriteString(encodePacket(request))
	}
	output := &bytes.Buffer{}
	err := Serve(struct {
		io.Reader
		io.Writer
	}{input, output}, target)
	if err != nil && !errors.Is(err, ErrDetached) {
		t.Fatalf("Serve() error = %v", err)
	}
	return replies(t, output.String())
}

// replies strips the acknowledgements and unwraps each packet, checking the
// checksum the stub produced — a wrong one is the failure a client would see
// as a hang.
func replies(t *testing.T, stream string) []string {
	t.Helper()
	var out []string
	for len(stream) > 0 {
		switch stream[0] {
		case '+', '-':
			stream = stream[1:]
			continue
		case '$':
		default:
			t.Fatalf("unexpected byte %q in the reply stream", stream[0])
		}
		end := strings.IndexByte(stream, '#')
		if end < 0 || end+3 > len(stream) {
			t.Fatalf("truncated reply packet in %q", stream)
		}
		payload := stream[1:end]
		want, err := parseHexByte(stream[end+1 : end+3])
		if err != nil {
			t.Fatal(err)
		}
		if got := checksum(payload); got != want {
			t.Fatalf("reply %q checksum = %#x, want %#x", payload, got, want)
		}
		out = append(out, payload)
		stream = stream[end+3:]
	}
	return out
}

func TestRegisterFileRoundTripsThroughTheWire(t *testing.T) {
	target := newStubTarget()
	target.registers[0] = 0x11223344
	target.registers[15] = 0x00001000
	target.cpsr = 0x60000010

	got := conversation(t, target, "g")
	if len(got) != 1 {
		t.Fatalf("replies = %q", got)
	}
	// Little-endian per register, r0 first.
	if !strings.HasPrefix(got[0], "44332211") {
		t.Fatalf("r0 = %q, want little-endian 0x11223344", got[0][:8])
	}
	// The reply is r0-r15, eight 96-bit FPA registers, the FPA status word,
	// then CPSR — a client that reads a short packet rejects it.
	wantLength := 16*8 + 8*24 + 8 + 8
	if len(got[0]) != wantLength {
		t.Fatalf("register packet is %d hex digits, want %d", len(got[0]), wantLength)
	}
	if !strings.HasSuffix(got[0], hexWord(0x60000010)) {
		t.Fatalf("CPSR is not last in %q", got[0][len(got[0])-8:])
	}

	// Writing one register back lands where it was read from.
	if got := conversation(t, target, "P0=efbeadde"); got[0] != "OK" {
		t.Fatalf("P reply = %q", got[0])
	}
	if target.registers[0] != 0xdeadbeef {
		t.Fatalf("r0 after P = %#x", target.registers[0])
	}
	if got := conversation(t, target, "p19"); got[0] != hexWord(0x60000010) {
		t.Fatalf("p25 (CPSR) = %q", got[0])
	}
}

func TestMemoryReadAndWrite(t *testing.T) {
	target := newStubTarget()
	target.memory[0x1000] = 0xde
	target.memory[0x1001] = 0xad

	got := conversation(t, target, "m1000,2")
	if got[0] != "dead" {
		t.Fatalf("m reply = %q", got[0])
	}

	got = conversation(t, target, "M2000,3:010203")
	if got[0] != "OK" {
		t.Fatalf("M reply = %q", got[0])
	}
	if target.memory[0x2000] != 1 || target.memory[0x2002] != 3 {
		t.Fatalf("memory after M = %v", target.memory)
	}

	// A length that disagrees with the payload is refused rather than
	// half-applied.
	if got := conversation(t, target, "M3000,4:0102"); got[0] != "E01" {
		t.Fatalf("mismatched M reply = %q", got[0])
	}

	// Unreadable memory is an error, not zeros: gdb must say "cannot access"
	// rather than show a plausible value.
	target.readFails = true
	if got := conversation(t, target, "m1000,4"); got[0] != "E05" {
		t.Fatalf("unmapped m reply = %q", got[0])
	}
}

func TestBreakpointsAndExecutionControl(t *testing.T) {
	target := newStubTarget()
	got := conversation(t, target, "Z0,1234,4", "c", "s", "z0,1234,4")
	if len(got) != 4 {
		t.Fatalf("replies = %q", got)
	}
	if got[0] != "OK" || got[3] != "OK" {
		t.Fatalf("breakpoint replies = %q", got)
	}
	if got[1] != "S05" || got[2] != "S05" {
		t.Fatalf("resume replies = %q, want a trap stop each", got[1:3])
	}
	if target.continued != 1 || target.stepped != 1 {
		t.Fatalf("continued=%d stepped=%d", target.continued, target.stepped)
	}
	if len(target.breakpoint) != 0 {
		t.Fatalf("breakpoint survived z0: %v", target.breakpoint)
	}

	// A watchpoint type this core cannot serve answers the empty packet, so
	// gdb falls back to single-stepping instead of giving up.
	if got := conversation(t, target, "Z2,1234,4"); got[0] != "" {
		t.Fatalf("watchpoint reply = %q, want an empty packet", got[0])
	}
}

func TestHandshakeAndDetach(t *testing.T) {
	target := newStubTarget()
	got := conversation(t, target, "qSupported:multiprocess+", "?", "qAttached", "D")
	if len(got) != 4 {
		t.Fatalf("replies = %q", got)
	}
	if !strings.Contains(got[0], "PacketSize=") {
		t.Fatalf("qSupported reply = %q", got[0])
	}
	if got[1] != "S05" {
		t.Fatalf("? reply = %q, want a stopped target", got[1])
	}
	// qAttached=1 means a detach leaves the game running rather than killing
	// it, which is what a user attaching mid-session expects.
	if got[2] != "1" {
		t.Fatalf("qAttached reply = %q", got[2])
	}
	if got[3] != "OK" {
		t.Fatalf("D reply = %q", got[3])
	}
	if target.killed {
		t.Fatal("detaching killed the target")
	}
}

func TestKillEndsTheSession(t *testing.T) {
	target := newStubTarget()
	conversation(t, target, "k")
	if !target.killed {
		t.Fatal("k did not kill the target")
	}
}

func TestCorruptPacketIsNegativelyAcknowledged(t *testing.T) {
	target := newStubTarget()
	// A packet whose checksum is wrong, followed by a correct one: the stub
	// must ask for a resend rather than acting on the corrupt request.
	input := strings.NewReader("$m1000,2#00" + encodePacket("?"))
	output := &bytes.Buffer{}
	err := Serve(struct {
		io.Reader
		io.Writer
	}{input, output}, target)
	if err != nil && !errors.Is(err, ErrDetached) {
		t.Fatalf("Serve() error = %v", err)
	}
	if !strings.HasPrefix(output.String(), "-") {
		t.Fatalf("stream = %q, want a negative acknowledgement first", output.String())
	}
	if got := replies(t, output.String()); len(got) != 1 || got[0] != "S05" {
		t.Fatalf("replies = %q, want only the valid request answered", got)
	}
}

func TestInterruptReportsAStop(t *testing.T) {
	target := newStubTarget()
	input := strings.NewReader("\x03")
	output := &bytes.Buffer{}
	err := Serve(struct {
		io.Reader
		io.Writer
	}{input, output}, target)
	if err != nil && !errors.Is(err, ErrDetached) {
		t.Fatalf("Serve() error = %v", err)
	}
	if got := replies(t, output.String()); len(got) != 1 || got[0] != "S05" {
		t.Fatalf("interrupt replies = %q", got)
	}
}

func TestUnknownRequestAnswersTheEmptyPacket(t *testing.T) {
	// The protocol's "not supported" is an empty packet; anything else makes
	// a client treat the stub as broken rather than as limited.
	got := conversation(t, newStubTarget(), "vCont?", "qWhatever")
	for _, reply := range got {
		if reply != "" {
			t.Fatalf("unknown request answered %q, want the empty packet", reply)
		}
	}
}
