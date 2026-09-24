package webhost

import (
	"bytes"
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/wsproto"
)

// The rule these tests keep is that nothing on the emulator's goroutine waits
// for the socket. It was broken for as long as this file did not exist: the
// emulator sent its own audio and statistics straight down the connection, so
// every one of them queued behind whatever picture the encoder was writing,
// and on a phone link that wait landed on the guest's clock as slow motion. A
// desktop never showed it, because a write to the loopback interface returns
// before the socket has done anything with it.

// stalledRunner is a session whose writer never drains anything, which is what
// a connection to a phone that has stopped reading looks like from here.
func stalledRunner(t *testing.T, depth int) *sessionRunner {
	t.Helper()
	return &sessionRunner{
		server:     newTestServer(t, Options{}),
		outText:    make(chan outboundMessage, depth),
		outFrames:  make(chan outboundMessage, 1),
		writerDone: make(chan struct{}),
	}
}

// finishes runs work and answers whether it returned rather than blocking.
func finishes(work func()) bool {
	done := make(chan struct{})
	go func() { defer close(done); work() }()
	select {
	case <-done:
		return true
	case <-time.After(5 * time.Second):
		return false
	}
}

func TestAudioIsShedRatherThanWaitingForAStalledConnection(t *testing.T) {
	runner := stalledRunner(t, 1)
	runner.audio = &audioCollector{}

	// The first sound fills the queue; every one after it has nowhere to go.
	// What must not happen is the emulator waiting for room.
	for round := 0; round < 4; round++ {
		runner.audio.MIDINoteOn(0, 60, 100)
		if !finishes(runner.flushAudio) {
			t.Fatalf("flushAudio blocked on round %d; the emulator was made to wait for the socket", round)
		}
	}
	if shed := runner.shed.Load(); shed != 3 {
		t.Errorf("shed = %d, want 3", shed)
	}
}

func TestStatisticsAreShedRatherThanWaitingForAStalledConnection(t *testing.T) {
	runner := stalledRunner(t, 0)
	// A report the connection has no room for is worth less than the tick it
	// would have cost: the next one carries the same numbers over a longer
	// window.
	if !finishes(func() { runner.sendDroppable(serverMessage{Kind: serverStats, Stats: &statsMessage{}}) }) {
		t.Fatal("a statistics report blocked on the socket")
	}
	if shed := runner.shed.Load(); shed != 1 {
		t.Errorf("shed = %d, want 1", shed)
	}
}

func TestASendGivesUpOnceTheWriterHasGone(t *testing.T) {
	runner := stalledRunner(t, 0)
	// A message that may not be dropped waits for room, so the session has to
	// have a way to stop waiting when there will never be any. Without this a
	// dead connection holds the goroutine that noticed it was dead.
	close(runner.writerDone)
	if !finishes(func() { runner.send(serverMessage{Kind: serverError, Message: "boom"}) }) {
		t.Fatal("a send waited on a writer that had already gone")
	}
}

// countingTransport is a socket that records each write the writer makes.
type countingTransport struct {
	mutex  sync.Mutex
	wire   bytes.Buffer
	writes int
}

func (c *countingTransport) Write(data []byte) (int, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.writes++
	return c.wire.Write(data)
}

func (c *countingTransport) Read([]byte) (int, error) { return 0, io.EOF }

func (c *countingTransport) snapshot() (int, []byte) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.writes, bytes.Clone(c.wire.Bytes())
}

// writingRunner is a session whose writer writes to a transport the test reads.
func writingRunner(t *testing.T) (*sessionRunner, *countingTransport, func()) {
	t.Helper()
	transport := &countingTransport{}
	runner := &sessionRunner{
		server:       newTestServer(t, Options{}),
		connection:   wsproto.Server(transport),
		outText:      make(chan outboundMessage, 8),
		outFrames:    make(chan outboundMessage, 1),
		frames:       make(chan pendingFrame, 1),
		frameSettled: make(chan struct{}, 1),
		writerDone:   make(chan struct{}),
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	start := func() {
		go func() { defer close(done); runner.writeMessages(ctx, cancel) }()
	}
	t.Cleanup(func() { cancel(); <-done })
	return runner, transport, start
}

// waitForWrites polls until the writer has made at least count writes.
func waitForWrites(t *testing.T, transport *countingTransport, count int) (int, []byte) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if writes, wire := transport.snapshot(); writes >= count {
			return writes, wire
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("the writer made fewer than %d writes", count)
	return 0, nil
}

// readWire splits what the writer wrote into its messages.
func readWire(t *testing.T, wire []byte) []wsproto.Opcode {
	t.Helper()
	client := wsproto.Client(bytes.NewBuffer(wire))
	var opcodes []wsproto.Opcode
	for {
		opcode, _, err := client.ReadMessage()
		if err != nil {
			return opcodes
		}
		opcodes = append(opcodes, opcode)
	}
}

func TestQueuedMessagesLeaveInOneWrite(t *testing.T) {
	runner, transport, start := writingRunner(t)
	runner.outText <- outboundMessage{text: `{"kind":"started"}`}
	runner.outText <- outboundMessage{binary: []byte("WFA2\x06"), audio: true}
	runner.outFrames <- outboundMessage{binary: []byte("picture")}
	start()
	writes, wire := waitForWrites(t, transport, 1)
	time.Sleep(20 * time.Millisecond)
	if writes, _ = transport.snapshot(); writes != 1 {
		t.Fatalf("three queued messages took %d writes", writes)
	}
	// The text queued ahead of the picture is written ahead of it.
	if got := readWire(t, wire); len(got) != 3 || got[0] != wsproto.OpText || got[1] != wsproto.OpBinary || got[2] != wsproto.OpBinary {
		t.Fatalf("messages %v", got)
	}
	if written := runner.written.Load(); written != uint64(len(wire)) {
		t.Fatalf("counted %d bytes written, the wire holds %d", written, len(wire))
	}
	if runner.sent.Load() != 1 || runner.frameBytes.Load() != uint64(len("picture")) {
		t.Fatal("the picture was not counted as the one frame")
	}
}

func TestSoundWaitsBrieflyForThePictureOfItsTick(t *testing.T) {
	runner, transport, start := writingRunner(t)
	runner.encoding.Store(true)
	runner.outText <- outboundMessage{binary: []byte("WFA2\x06"), audio: true}
	start()
	time.Sleep(2 * time.Millisecond)
	runner.outFrames <- outboundMessage{binary: []byte("picture")}
	_, wire := waitForWrites(t, transport, 1)
	time.Sleep(20 * time.Millisecond)
	if writes, _ := transport.snapshot(); writes != 1 {
		t.Fatalf("sound and its picture took %d writes", writes)
	}
	if got := readWire(t, wire); len(got) != 2 {
		t.Fatalf("the write held %d messages, want the sound and the picture", len(got))
	}
}

func TestSoundDoesNotWaitWhenNoPictureIsComing(t *testing.T) {
	runner, transport, start := writingRunner(t)
	runner.outText <- outboundMessage{binary: []byte("WFA2\x06"), audio: true}
	started := time.Now()
	start()
	waitForWrites(t, transport, 1)
	if elapsed := time.Since(started); elapsed >= frameLinger {
		t.Fatalf("sound with no picture on its way waited %v", elapsed)
	}
}

// The page is shown a rate and the session report is shown a run, so emptying
// the window a report is built from used to answer "nothing was shed" for a
// session that had shed plenty.
func TestTheSessionTotalSurvivesAStatisticsWindow(t *testing.T) {
	runner := stalledRunner(t, 0)

	for round := 0; round < 3; round++ {
		runner.sendDroppable(serverMessage{Kind: serverStats, Stats: &statsMessage{}})
	}
	if shed := runner.shed.Load(); shed != 3 {
		t.Fatalf("window shed = %d, want 3", shed)
	}

	// This is what reportStats does to the window every time it sends.
	runner.shed.Swap(0)

	if shed := runner.shed.Load(); shed != 0 {
		t.Errorf("window shed = %d after a report, want it emptied", shed)
	}
	if total := runner.shedTotal.Load(); total != 3 {
		t.Errorf("session total = %d, want the 3 it shed before the report", total)
	}
}
