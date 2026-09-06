package webhost

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/wsproto"
)

// The server had no bound on how long a request could take to arrive, and the
// reason written down for that was `http.Server.ReadTimeout` killing the
// session socket. It does not: `net/http` clears both connection deadlines
// when a handler hijacks, so nothing the server set before the upgrade reaches
// the socket afterwards.
//
// This is that fact as a test rather than as a belief, because it is the only
// thing holding the timeout up — a Go release that stopped clearing the
// deadline would take every session down after ten minutes of play, and the
// symptom on a phone would be indistinguishable from the network.
func TestTheSessionSocketOutlivesTheServersReadTimeout(t *testing.T) {
	server := newTestServer(t, Options{})
	httpServer := httptest.NewUnstartedServer(server)
	// Short enough that the idle below is several times it, rather than the
	// minutes the real server allows a slow phone to upload an archive in.
	httpServer.Config.ReadHeaderTimeout = 300 * time.Millisecond
	httpServer.Config.ReadTimeout = 300 * time.Millisecond
	httpServer.Start()
	defer httpServer.Close()

	connection, _, err := wsproto.Dial("ws://"+strings.TrimPrefix(httpServer.URL, "http://")+"/api/session", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if _, err := connection.ReadText(); err != nil {
		t.Fatalf("the session did not say it was ready: %v", err)
	}

	// A player reading a menu sends nothing for far longer than this.
	time.Sleep(time.Second)

	if err := connection.WriteText(`{"kind":"stop"}`); err != nil {
		t.Fatalf("a page could not write after idling past the read timeout: %v", err)
	}
	if err := connection.WriteText(`{"kind":"report"}`); err != nil {
		t.Fatalf("a second write failed: %v", err)
	}
	if _, err := connection.ReadText(); err != nil {
		t.Fatalf("a page could not read after idling past the read timeout: %v", err)
	}
}
