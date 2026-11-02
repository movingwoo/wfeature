package gdbstub

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
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

const (
	guestBase uint32 = 0x1000
	guestEnd  uint32 = 0x7fff0000
)

// guestCore maps code at guestBase and a stack page, and loads program as
// ARM words or, when thumb is set, as Thumb halfwords.
func guestCore(t *testing.T, thumb bool, program ...uint32) *armcore.Core {
	t.Helper()
	core := armcore.NewCore(armcore.CoreOptions{MaxSteps: 1 << 30})
	if err := core.Memory().Map(guestBase, 0x1000, armcore.PermissionReadWriteExecute); err != nil {
		t.Fatal(err)
	}
	if err := core.Memory().Map(0x2000, 0x1000, armcore.PermissionReadWrite); err != nil {
		t.Fatal(err)
	}
	var code []byte
	for _, word := range program {
		if thumb {
			code = binary.LittleEndian.AppendUint16(code, uint16(word))
		} else {
			code = binary.LittleEndian.AppendUint32(code, word)
		}
	}
	if err := core.Memory().Load(guestBase, code); err != nil {
		t.Fatal(err)
	}
	return core
}

// runGuest runs the program from guestBase on its own goroutine, the way a
// session runs its guest, and answers a channel that reports how it ended.
func runGuest(ctx context.Context, core *armcore.Core, target *CoreTarget, thumb bool) <-chan error {
	done := make(chan error, 1)
	go func() {
		initial := armcore.NewContext()
		initial.Registers[armcore.RegisterSP] = 0x3000
		entry := guestBase
		if thumb {
			entry |= 1
		}
		_, err := core.Call(ctx, armcore.NewThread(initial), entry, guestEnd, nil, nil)
		target.Finished()
		done <- err
	}()
	return done
}

// client is the gdb side of a connection, one packet at a time.
type client struct {
	t      *testing.T
	conn   net.Conn
	reader *bufio.Reader
}

func newClient(t *testing.T, conn net.Conn) *client {
	return &client{t: t, conn: conn, reader: bufio.NewReader(conn)}
}

func (c *client) send(payload string) {
	c.t.Helper()
	// A pipe write waits for its reader; a stub that stopped reading fails
	// the test rather than holding it.
	if err := c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		c.t.Fatal(err)
	}
	if _, err := io.WriteString(c.conn, encodePacket(payload)); err != nil {
		c.t.Fatalf("send %q: %v", payload, err)
	}
}

// acknowledged waits for the stub's acknowledgement of the last packet, which
// it cannot send until someone reads it.
func (c *client) acknowledged() {
	c.t.Helper()
	if err := c.conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		c.t.Fatal(err)
	}
	if symbol, err := c.reader.ReadByte(); err != nil || symbol != '+' {
		c.t.Fatalf("acknowledgement = %q, %v", symbol, err)
	}
	if err := c.conn.SetReadDeadline(time.Time{}); err != nil {
		c.t.Fatal(err)
	}
}

// reply waits for the next packet, skipping acknowledgements.
func (c *client) reply() string {
	c.t.Helper()
	type result struct {
		payload string
		err     error
	}
	got := make(chan result, 1)
	go func() {
		for {
			symbol, err := c.reader.ReadByte()
			if err != nil {
				got <- result{err: err}
				return
			}
			if symbol != '$' {
				continue
			}
			body, err := c.reader.ReadString('#')
			if err != nil {
				got <- result{err: err}
				return
			}
			var digits [2]byte
			if _, err := io.ReadFull(c.reader, digits[:]); err != nil {
				got <- result{err: err}
				return
			}
			got <- result{payload: strings.TrimSuffix(body, "#")}
			return
		}
	}()
	select {
	case outcome := <-got:
		if outcome.err != nil {
			c.t.Fatalf("reading a reply: %v", outcome.err)
		}
		return outcome.payload
	case <-time.After(5 * time.Second):
		c.t.Fatal("no reply within five seconds")
		return ""
	}
}

func (c *client) exchange(payload string) string {
	c.t.Helper()
	c.send(payload)
	return c.reply()
}

// serve runs Serve on one end of a pipe and answers the other end and a
// channel that reports when Serve returned.
func serve(target Target) (net.Conn, <-chan error) {
	server, peer := net.Pipe()
	done := make(chan error, 1)
	go func() {
		err := Serve(server, target)
		server.Close()
		done <- err
	}()
	return peer, done
}

func waitFor(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// A counted loop: r0 counts to 200 and the function returns.
var countedLoop = []uint32{
	0xe3a00000, // mov r0, #0
	0xe2800001, // loop: add r0, r0, #1
	0xe35000c8, // cmp r0, #200
	0x1afffffc, // bne loop
	0xe12fff1e, // bx lr
}

// A client that stops the guest and then leaves — by detaching, or by
// dropping the connection — has to leave it running, with the breakpoint it
// set gone, as qAttached promises.
func TestALeavingClientReleasesTheGuest(t *testing.T) {
	for _, leave := range []string{"detach", "disconnect"} {
		t.Run(leave, func(t *testing.T) {
			core := guestCore(t, false, countedLoop...)
			target := NewCoreTarget(core)
			defer target.Detach()
			peer, served := serve(target)
			c := newClient(t, peer)
			if got := c.exchange("Z0,1004,4"); got != "OK" {
				t.Fatalf("Z0 reply = %q", got)
			}
			done := runGuest(context.Background(), core, target, false)
			if got := c.exchange("c"); got != "S05" {
				t.Fatalf("c reply = %q, want a breakpoint stop", got)
			}
			if leave == "detach" {
				if got := c.exchange("D"); got != "OK" {
					t.Fatalf("D reply = %q", got)
				}
			}
			peer.Close()
			select {
			case err := <-done:
				if err != nil {
					t.Fatalf("guest run error = %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("the guest was still parked after its client left")
			}
			if left := core.Breakpoints(); len(left) != 0 {
				t.Fatalf("breakpoints left armed after the client left: %#x", left)
			}
			select {
			case <-served:
			case <-time.After(5 * time.Second):
				t.Fatal("Serve did not return after the client left")
			}
		})
	}
}

// An endless loop, which is what a game's main loop is.
var endlessLoop = []uint32{0xeafffffe} // b .

// Ctrl-C while the target runs is the client asking it to stop, and a
// connection dropped while it runs has to end the session rather than leave
// Serve waiting for a stop that may never come.
func TestARunningTargetHearsItsClient(t *testing.T) {
	t.Run("interrupt", func(t *testing.T) {
		core := guestCore(t, false, endlessLoop...)
		target := NewCoreTarget(core)
		defer target.Detach()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := runGuest(ctx, core, target, false)
		peer, served := serve(target)
		c := newClient(t, peer)
		c.send("c")
		time.Sleep(20 * time.Millisecond)
		// A pipe write waits for its reader, and a stub that is not reading
		// while the target runs would hold this test rather than fail it.
		go peer.Write([]byte{0x03})
		if got := c.reply(); got != "S05" {
			t.Fatalf("reply to the interrupt = %q, want a stop", got)
		}
		registers, _ := target.Registers()
		if registers[armcore.RegisterPC] != guestBase {
			t.Fatalf("stopped at %#x, want inside the loop at %#x", registers[armcore.RegisterPC], guestBase)
		}
		if got := c.exchange("D"); got != "OK" {
			t.Fatalf("D reply = %q", got)
		}
		peer.Close()
		<-served
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("the guest did not run on after the client detached")
		}
	})
	t.Run("disconnect", func(t *testing.T) {
		core := guestCore(t, false, endlessLoop...)
		target := NewCoreTarget(core)
		defer target.Detach()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := runGuest(ctx, core, target, false)
		peer, served := serve(target)
		c := newClient(t, peer)
		c.send("c")
		c.acknowledged()
		time.Sleep(20 * time.Millisecond)
		peer.Close()
		select {
		case <-served:
		case <-time.After(5 * time.Second):
			t.Fatal("Serve was still waiting for a stop after its client hung up")
		}
		cancel()
		<-done
	})
}

// A client that connects to a running guest asks '?' and takes the answer as
// a stopped target, so the guest stops: its registers are where it stopped,
// not an empty file. The same holds for the next client after one detached.
func TestAConnectingClientStopsARunningGuest(t *testing.T) {
	core := guestCore(t, false, endlessLoop...)
	target := NewCoreTarget(core)
	defer target.Detach()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := runGuest(ctx, core, target, false)
	for _, client := range []string{"first", "after a detach"} {
		peer, served := serve(target)
		c := newClient(t, peer)
		if got := c.exchange("?"); got != "S05" {
			t.Fatalf("%s: ? reply = %q, want a stop", client, got)
		}
		if !target.parked() {
			t.Fatalf("%s: the guest was still running after '?' answered a stop", client)
		}
		if got := c.exchange("p0f"); got != hexWord(guestBase) {
			t.Fatalf("%s: pc = %s, want inside the loop at %s", client, got, hexWord(guestBase))
		}
		if got := c.exchange("?"); got != "S05" {
			t.Fatalf("%s: a second ? reply = %q", client, got)
		}
		if got := c.exchange("D"); got != "OK" {
			t.Fatalf("%s: D reply = %q", client, got)
		}
		peer.Close()
		<-served
		waitFor(t, "the guest to run on after the detach", func() bool { return !target.parked() })
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the guest did not run on after its last client detached")
	}
}

// fakeListener hands out the connections it is given, then reports closed.
type fakeListener struct {
	connections chan net.Conn
	closed      chan struct{}
	once        sync.Once
}

func (listener *fakeListener) Accept() (net.Conn, error) {
	select {
	case connection := <-listener.connections:
		return connection, nil
	case <-listener.closed:
		return nil, net.ErrClosed
	}
}

func (listener *fakeListener) Close() error {
	listener.once.Do(func() { close(listener.closed) })
	return nil
}

func (listener *fakeListener) Addr() net.Addr { return &net.TCPAddr{} }

// resetConn is a connection whose first read fails the way a reset one does.
type resetConn struct{ net.Conn }

func (resetConn) Read([]byte) (int, error) { return 0, errors.New("connection reset by peer") }

// One client's broken connection ends that client, not the stub: the next one
// is still accepted.
func TestAResetConnectionDoesNotStopTheStub(t *testing.T) {
	listener := &fakeListener{connections: make(chan net.Conn, 2), closed: make(chan struct{})}
	first, firstPeer := net.Pipe()
	defer firstPeer.Close()
	second, secondPeer := net.Pipe()
	listener.connections <- resetConn{first}
	listener.connections <- second
	stopped := make(chan error, 1)
	go func() { stopped <- Listen(listener, newStubTarget(), nil) }()

	c := newClient(t, secondPeer)
	if got := c.exchange("?"); got != "S05" {
		t.Fatalf("second client's ? reply = %q", got)
	}
	secondPeer.Close()
	listener.Close()
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatalf("Listen() error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Listen did not return after its listener closed")
	}
}

// A stop the guest reached while nobody was waiting — a breakpoint hit between
// the client setting it and asking to continue — is the stop the continue
// reports, and the guest stays there. Reporting it while releasing the guest
// leaves the client one stop behind for the rest of the session.
func TestAContinueReportsAStopThatHappenedBeforeIt(t *testing.T) {
	core := guestCore(t, false, countedLoop...)
	target := NewCoreTarget(core)
	defer target.Detach()
	target.SetBreakpoint(guestBase + 4)
	done := runGuest(context.Background(), core, target, false)
	waitFor(t, "the guest to park", target.parked)

	if signal, err := target.Continue(); err != nil || signal != SignalTrap {
		t.Fatalf("Continue() = %v, %v, want a trap", signal, err)
	}
	time.Sleep(50 * time.Millisecond)
	registers, _ := target.Registers()
	if registers[0] != 0 {
		t.Fatalf("r0 = %d after the continue, want 0 from the stop it reported", registers[0])
	}
	// The thread is the one stopped there, so a register write reaches it.
	if err := target.SetRegister(0, 198); err != nil {
		t.Fatalf("SetRegister at the reported stop: %v", err)
	}
	if signal, err := target.Continue(); err != nil || signal != SignalTrap {
		t.Fatalf("second Continue() = %v, %v", signal, err)
	}
	registers, _ = target.Registers()
	if registers[0] != 199 {
		t.Fatalf("r0 = %d at the next stop, want 199", registers[0])
	}
	target.ClearBreakpoint(guestBase + 4)
	if signal, err := target.Continue(); err != nil || signal != SignalKill {
		t.Fatalf("final Continue() = %v, %v, want the run's end", signal, err)
	}
	if err := <-done; err != nil {
		t.Fatalf("guest run error = %v", err)
	}
}

// endless serves bytes of one value up to a limit, counting what was read.
type endless struct {
	value byte
	limit int
	read  int
}

func (source *endless) Read(buffer []byte) (int, error) {
	if source.read >= source.limit {
		return 0, io.EOF
	}
	count := min(len(buffer), source.limit-source.read)
	for index := range buffer[:count] {
		buffer[index] = source.value
	}
	source.read += count
	return count, nil
}

// A peer that opens a packet and never closes it must not make the stub
// buffer without limit.
func TestAPacketBodyIsBounded(t *testing.T) {
	source := &endless{value: 'a', limit: 16 << 20}
	reader := bufio.NewReader(io.MultiReader(strings.NewReader("$"), source))
	_, _, err := readPacket(reader, io.Discard)
	if err == nil {
		t.Fatal("an unterminated packet body was accepted")
	}
	if source.read > 1<<20 {
		t.Fatalf("read %d bytes of one packet before giving up, want the bound to stop it", source.read)
	}

	// The largest request a client sends — a full register file, or a
	// memory write the advertised packet size allows — still arrives.
	payload := "M2000,800:" + strings.Repeat("ab", 0x800)
	body, _, err := readPacket(bufio.NewReader(strings.NewReader(encodePacket(payload))), io.Discard)
	if err != nil || body != payload {
		t.Fatalf("a %d-byte packet: err=%v", len(payload), err)
	}
}

// replyWithin waits up to wait for the next packet, skipping acknowledgements,
// and reports whether one came.
func (c *client) replyWithin(wait time.Duration) (string, bool) {
	c.t.Helper()
	if err := c.conn.SetReadDeadline(time.Now().Add(wait)); err != nil {
		c.t.Fatal(err)
	}
	defer c.conn.SetReadDeadline(time.Time{})
	for {
		symbol, err := c.reader.ReadByte()
		if err != nil {
			return "", false
		}
		if symbol != '$' {
			continue
		}
		body, err := c.reader.ReadString('#')
		if err != nil {
			return "", false
		}
		var digits [2]byte
		if _, err := io.ReadFull(c.reader, digits[:]); err != nil {
			return "", false
		}
		return strings.TrimSuffix(body, "#"), true
	}
}

// waitForReply sends a request and requires its answer within a second, well
// inside what a client allows before it gives up on the target.
func (c *client) waitForReply(request, want string) {
	c.t.Helper()
	c.send(request)
	if got, ok := c.replyWithin(time.Second); !ok || got != want {
		c.t.Fatalf("%s reply = %q (answered: %v), want %q within a second", request, got, ok, want)
	}
}

// hangUp serves a client that sends requests and closes its connection at
// once, so the end of the connection is already queued behind the last
// request, as it is when a client writes a packet and closes the socket.
func hangUp(target Target, writer io.Writer, requests ...string) {
	var input strings.Builder
	for _, request := range requests {
		input.WriteString(encodePacket(request))
	}
	_ = Serve(struct {
		io.Reader
		io.Writer
	}{strings.NewReader(input.String()), writer}, target)
}

// resetWriter takes the acknowledgements and fails every packet, the way a
// connection fails once the client has reset it.
type resetWriter struct{}

func (resetWriter) Write(data []byte) (int, error) {
	if len(data) == 1 && (data[0] == '+' || data[0] == '-') {
		return 1, nil
	}
	return 0, errors.New("connection reset by peer")
}

// A client that asks for a stop or a step and hangs up straight away has
// nobody left to let the guest go, so nothing it asked for may stop the guest
// once it has gone: a stopped guest holds the run, and the terminal's Ctrl-C
// with it, until another client attaches.
func TestAClientThatHangsUpLeavesTheGuestRunning(t *testing.T) {
	const rounds = 50
	for _, leave := range []struct {
		name     string
		writer   io.Writer
		requests []string
	}{
		{"? then hang up", io.Discard, []string{"?"}},
		{"s then hang up", io.Discard, []string{"s"}},
		{"? s then hang up", io.Discard, []string{"?", "s"}},
		{"? then reset", resetWriter{}, []string{"?"}},
	} {
		t.Run(leave.name, func(t *testing.T) {
			stopped := 0
			for round := 0; round < rounds; round++ {
				core := guestCore(t, false, endlessLoop...)
				target := NewCoreTarget(core)
				ctx, cancel := context.WithCancel(context.Background())
				done := runGuest(ctx, core, target, false)
				hangUp(target, leave.writer, leave.requests...)
				// Whatever the client set going has had the time it needs to
				// reach the guest.
				time.Sleep(10 * time.Millisecond)
				// With nobody attached, a cancel, which is the terminal's
				// Ctrl-C, ends the run.
				cancel()
				select {
				case <-done:
				case <-time.After(time.Second):
					stopped++
					target.Detach()
					<-done
				}
				target.Detach()
			}
			if stopped != 0 {
				t.Fatalf("the guest stayed stopped after %d of %d clients left", stopped, rounds)
			}
		})
	}
}

// A continue that its client's hang-up overtook must not stay behind waiting
// for a stop: the next client's stop would go to it, and that client would
// never hear its own.
func TestAClientThatHangsUpLeavesNothingWaiting(t *testing.T) {
	const rounds = 50
	unanswered := 0
	for round := 0; round < rounds; round++ {
		core := guestCore(t, false, endlessLoop...)
		target := NewCoreTarget(core)
		ctx, cancel := context.WithCancel(context.Background())
		done := runGuest(ctx, core, target, false)
		hangUp(target, io.Discard, "c")
		time.Sleep(10 * time.Millisecond)
		peer, served := serve(target)
		c := newClient(t, peer)
		c.send("?")
		if got, ok := c.replyWithin(time.Second); !ok || got != "S05" {
			unanswered++
		}
		peer.Close()
		<-served
		cancel()
		target.Detach()
		<-done
	}
	if unanswered != 0 {
		t.Fatalf("the next client's ? went unanswered after %d of %d clients hung up", unanswered, rounds)
	}
}

// A client that attaches while no guest code runs (a run waiting on its
// driver, a title with nothing due, a guest inside a call the host is still
// answering) is told the target stopped without waiting for an instruction
// that may be a long way off, and the stop is armed, so the guest stops at the
// first instruction it runs. The client then reads the place it stopped, and
// no stop arrives later as the answer to another request.
func TestAStopIsAnsweredWhileNoGuestCodeRuns(t *testing.T) {
	t.Run("before the guest runs", func(t *testing.T) {
		core := guestCore(t, false, countedLoop...)
		target := NewCoreTarget(core)
		defer target.Detach()
		peer, served := serve(target)
		c := newClient(t, peer)
		c.waitForReply("?", "S05")
		done := runGuest(context.Background(), core, target, false)
		waitFor(t, "the guest to stop at its first instruction", target.parked)
		if got := c.exchange("p0f"); got != hexWord(guestBase) {
			t.Fatalf("pc = %s, want the entry at %s", got, hexWord(guestBase))
		}
		// From there a continue runs the guest to its end.
		c.waitForReply("c", "W00")
		if err := <-done; err != nil {
			t.Fatalf("guest run error = %v", err)
		}
		peer.Close()
		<-served
	})
	t.Run("inside a supervisor call", func(t *testing.T) {
		core := guestCore(t, false,
			0xef000000, // svc #0
			0xeafffffe, // b .
		)
		target := NewCoreTarget(core)
		defer target.Detach()
		entered := make(chan struct{})
		answer := make(chan struct{})
		handler := func(context.Context, *armcore.Thread, armcore.SupervisorCall) error {
			close(entered)
			<-answer
			return nil
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan error, 1)
		go func() {
			initial := armcore.NewContext()
			initial.Registers[armcore.RegisterSP] = 0x3000
			_, err := core.Call(ctx, armcore.NewThread(initial), guestBase, guestEnd, nil, handler)
			target.Finished()
			done <- err
		}()
		<-entered
		peer, served := serve(target)
		c := newClient(t, peer)
		c.waitForReply("?", "S05")
		close(answer)
		waitFor(t, "the guest to stop after the call returned", target.parked)
		if got := c.exchange("p0f"); got != hexWord(guestBase+4) {
			t.Fatalf("pc = %s, want the instruction after the call at %s", got, hexWord(guestBase+4))
		}
		if got := c.exchange("D"); got != "OK" {
			t.Fatalf("D reply = %q", got)
		}
		peer.Close()
		<-served
		cancel()
		<-done
	})
	t.Run("an interrupt", func(t *testing.T) {
		core := guestCore(t, false, countedLoop...)
		target := NewCoreTarget(core)
		defer target.Detach()
		peer, served := serve(target)
		c := newClient(t, peer)
		c.send("c")
		c.acknowledged()
		go peer.Write([]byte{0x03})
		if got, ok := c.replyWithin(time.Second); !ok || got != "S05" {
			t.Fatalf("reply to the interrupt = %q (answered: %v), want a stop within a second", got, ok)
		}
		done := runGuest(context.Background(), core, target, false)
		waitFor(t, "the guest to stop at its first instruction", target.parked)
		if got := c.exchange("qC"); got != "QC1" {
			t.Fatalf("qC reply = %q, want QC1 with no late stop before it", got)
		}
		if got := c.exchange("D"); got != "OK" {
			t.Fatalf("D reply = %q", got)
		}
		peer.Close()
		<-served
		if err := <-done; err != nil {
			t.Fatalf("guest run error = %v", err)
		}
	})
}

// A guest whose run is over is reported as exited, or as killed when its
// client killed it, and at once: a stop reply would tell the client there is
// still a target to continue.
func TestAnEndedGuestIsReportedAsGone(t *testing.T) {
	t.Run("exited", func(t *testing.T) {
		core := guestCore(t, false, countedLoop...)
		target := NewCoreTarget(core)
		defer target.Detach()
		if err := <-runGuest(context.Background(), core, target, false); err != nil {
			t.Fatalf("guest run error = %v", err)
		}
		peer, served := serve(target)
		c := newClient(t, peer)
		for _, request := range []string{"?", "c", "s"} {
			c.waitForReply(request, "W00")
		}
		peer.Close()
		<-served
	})
	t.Run("killed", func(t *testing.T) {
		core := guestCore(t, false, endlessLoop...)
		target := NewCoreTarget(core)
		defer target.Detach()
		done := runGuest(context.Background(), core, target, false)
		peer, served := serve(target)
		c := newClient(t, peer)
		if got := c.exchange("?"); got != "S05" {
			t.Fatalf("? reply = %q", got)
		}
		c.send("k")
		// A kill has no reply, and the stub ends the connection once it has
		// taken it, so the acknowledgement is the last thing to read.
		if err := peer.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
			t.Fatal(err)
		}
		if symbol, err := c.reader.ReadByte(); err != nil || symbol != '+' {
			t.Fatalf("acknowledgement of k = %q, %v", symbol, err)
		}
		<-served
		peer.Close()
		if err := <-done; !errors.Is(err, ErrKilled) {
			t.Fatalf("run error after k = %v, want ErrKilled", err)
		}
		peer, served = serve(target)
		c = newClient(t, peer)
		c.waitForReply("?", "X09")
		peer.Close()
		<-served
	})
}

// The stop a client was told of while no guest code ran is the one the guest
// then makes at its first instruction, so a continue from it runs on to the
// breakpoint rather than reporting that first instruction as a stop of its
// own, whether the guest reached it before the continue or not.
func TestAContinueFromAnArmedStopRunsToTheBreakpoint(t *testing.T) {
	for _, reached := range []bool{false, true} {
		core := guestCore(t, false, countedLoop...)
		target := NewCoreTarget(core)
		peer, served := serve(target)
		c := newClient(t, peer)
		c.waitForReply("?", "S05")
		if got := c.exchange("Z0,1004,4"); got != "OK" {
			t.Fatalf("Z0 reply = %q", got)
		}
		done := runGuest(context.Background(), core, target, false)
		if reached {
			waitFor(t, "the guest to stop at its first instruction", target.parked)
		}
		if got := c.exchange("c"); got != "S05" {
			t.Fatalf("reached=%v: c reply = %q", reached, got)
		}
		if got := c.exchange("p0f"); got != hexWord(guestBase+4) {
			t.Fatalf("reached=%v: pc = %s, want the breakpoint at %s", reached, got, hexWord(guestBase+4))
		}
		if got := c.exchange("z0,1004,4"); got != "OK" {
			t.Fatalf("z0 reply = %q", got)
		}
		c.waitForReply("c", "W00")
		if err := <-done; err != nil {
			t.Fatalf("guest run error = %v", err)
		}
		peer.Close()
		<-served
		target.Detach()
	}
}
