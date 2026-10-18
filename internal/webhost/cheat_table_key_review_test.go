package webhost

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/wsproto"
)

// A table the panel saves carries what it always did: the panel's operations
// do not make the text console, whose making binds the image's key to the
// session, and a served page never asks for the console.
func TestAPanelTableIsSavedWithoutTheConsole(t *testing.T) {
	root, _ := checkpointServerFiles(t)
	server := checkpointServer(t, root)
	httpServer := httptest.NewServer(server)
	t.Cleanup(httpServer.Close)
	connection, _, err := wsproto.Dial("ws://"+strings.TrimPrefix(httpServer.URL, "http://")+"/api/session?protocol=2", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { connection.Close() })
	if err := connection.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
		t.Fatal(err)
	}
	expectMessage(t, connection, serverReady)
	send(t, connection, clientMessage{Kind: clientStart, Game: "games/ktf/checkpoint.zip", ID: 1})
	started := expectMessage(t, connection, serverStarted)
	send(t, connection, clientMessage{Kind: clientCheat, ID: 2, Epoch: started.Epoch, Op: "saveTable"})
	result := expectMessage(t, connection, serverResult)
	if result.Cheat == nil || result.Cheat.Table == "" {
		t.Fatalf("saveTable answered %+v", result)
	}
	if strings.Contains(result.Cheat.Table, `"image"`) {
		t.Fatalf("the panel's table carries the console's key: %s", result.Cheat.Table)
	}
}
