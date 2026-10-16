package webhost

import (
	"fmt"
	"os"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
)

// The socket's report command writes into the same directory the debug-log
// route does, so it keeps that route's two rules: a release writes no reports
// at all, and a debug build writes no more than the rate allows.
func TestTheSessionReportFollowsTheDebugLogRules(t *testing.T) {
	connection, logRoot := sessionFixture(t)
	expectMessage(t, connection, serverReady)
	send(t, connection, clientMessage{Kind: clientStart, Game: "games/skt/canvas.zip"})
	expectMessage(t, connection, serverStarted)

	if !backend.DebugBuild() {
		send(t, connection, clientMessage{Kind: clientReport, ID: 5})
		if reply := expectMessage(t, connection, serverError); reply.ID != 5 {
			t.Fatalf("reply = %+v, want the request refused", reply)
		}
		if _, err := os.Stat(logRoot); !os.IsNotExist(err) {
			t.Fatalf("a release wrote into the log directory anyway (%v)", err)
		}
		return
	}

	for attempt := 0; attempt < debugLogBurst; attempt++ {
		id := uint64(attempt + 1)
		// Reports written in one millisecond share a name unless their
		// labels differ, and the count below is of files.
		send(t, connection, clientMessage{Kind: clientReport, ID: id, Label: fmt.Sprint("report", attempt)})
		if reply := expectMessage(t, connection, serverResult); reply.ID != id {
			t.Fatalf("report %d answered %+v", attempt, reply)
		}
	}
	send(t, connection, clientMessage{Kind: clientReport, ID: 99})
	if reply := expectMessage(t, connection, serverError); reply.ID != 99 {
		t.Fatalf("the report over the rate answered %+v", reply)
	}
	entries, err := os.ReadDir(logRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != debugLogBurst {
		t.Fatalf("the log directory holds %d reports, want %d", len(entries), debugLogBurst)
	}
}
