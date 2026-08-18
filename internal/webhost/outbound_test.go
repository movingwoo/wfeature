package webhost

import (
	"testing"
	"time"
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
