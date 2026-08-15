package wsproto

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAcceptKeyMatchesTheSpecificationExample(t *testing.T) {
	// The key and answer from RFC 6455 section 1.3. A browser refuses the
	// connection outright when this value is wrong, so pinning it to the
	// published pair is what proves the hash and the GUID are right.
	if got := AcceptKey("dGhlIHNhbXBsZSBub25jZQ=="); got != "s3pPLMBiTxaQ9kYGzzhZRbK+xOo=" {
		t.Fatalf("AcceptKey = %q", got)
	}
}

func TestIsUpgradeReadsTokenLists(t *testing.T) {
	// Real browsers send `Connection: keep-alive, Upgrade`, so an equality
	// test on the header value would reject them.
	for _, test := range []struct {
		name       string
		connection string
		upgrade    string
		want       bool
	}{
		{"plain", "Upgrade", "websocket", true},
		{"token list", "keep-alive, Upgrade", "websocket", true},
		{"lower case", "upgrade", "WebSocket", true},
		{"not an upgrade", "keep-alive", "websocket", false},
		{"another protocol", "Upgrade", "h2c", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/session", nil)
			request.Header.Set("Connection", test.connection)
			request.Header.Set("Upgrade", test.upgrade)
			if got := IsUpgrade(request); got != test.want {
				t.Fatalf("IsUpgrade = %v, want %v", got, test.want)
			}
		})
	}
}

// handshakeServer starts a real HTTP server that upgrades /api/session and
// echoes one message back, which is the only way to exercise the hijack path.
func handshakeServer(t *testing.T, upgrader Upgrader) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		connection, err := upgrader.Accept(writer, request)
		if err != nil {
			// Accept has already answered the request; the handler only stops.
			return
		}
		defer connection.Close()
		for {
			opcode, payload, err := connection.ReadMessage()
			if err != nil {
				return
			}
			if err := connection.WriteMessage(opcode, payload); err != nil {
				return
			}
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func websocketURL(t *testing.T, server *httptest.Server) string {
	t.Helper()
	return "ws://" + strings.TrimPrefix(server.URL, "http://") + "/api/session"
}

func TestUpgradedConnectionCarriesMessagesBothWays(t *testing.T) {
	// The end-to-end path: a real handshake over a real socket, then a text
	// and a binary message through the hijacked connection. This is what the
	// session server will stand on, and none of it needs a browser.
	server := handshakeServer(t, Upgrader{})
	connection, response, err := Dial(websocketURL(t, server), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer connection.Close()
	if response.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status = %s", response.Status)
	}

	if err := connection.WriteText(`{"kind":"key","name":"FIRE"}`); err != nil {
		t.Fatalf("write text: %v", err)
	}
	text, err := connection.ReadText()
	if err != nil {
		t.Fatalf("read text: %v", err)
	}
	if text != `{"kind":"key","name":"FIRE"}` {
		t.Fatalf("text = %q", text)
	}

	frame := make([]byte, 32*1024)
	for index := range frame {
		frame[index] = byte(index)
	}
	if err := connection.WriteBinary(frame); err != nil {
		t.Fatalf("write binary: %v", err)
	}
	opcode, payload, err := connection.ReadMessage()
	if err != nil {
		t.Fatalf("read binary: %v", err)
	}
	if opcode != OpBinary || len(payload) != len(frame) {
		t.Fatalf("got %s of %d bytes, want binary of %d", opcode, len(payload), len(frame))
	}
}

func TestClosingTheConnectionEndsTheServerSession(t *testing.T) {
	server := handshakeServer(t, Upgrader{})
	connection, _, err := Dial(websocketURL(t, server), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if err := connection.WriteClose(CloseNormal, "done"); err != nil {
		t.Fatalf("close: %v", err)
	}
	_, _, err = connection.ReadMessage()
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("read after close: %v, want ErrClosed", err)
	}
	_ = connection.Close()
}

func TestHandshakeRefusals(t *testing.T) {
	// Each refusal has to come back as an HTTP status, because at this point
	// the connection is still an ordinary request and the client has no other
	// way to learn what was wrong.
	const validKey = "dGhlIHNhbXBsZSBub25jZQ=="
	for _, test := range []struct {
		name    string
		method  string
		headers map[string]string
		status  int
	}{
		{
			name:   "not an upgrade",
			method: http.MethodGet,
			headers: map[string]string{
				"Sec-WebSocket-Version": "13", "Sec-WebSocket-Key": validKey,
			},
			status: http.StatusBadRequest,
		},
		{
			name:   "post",
			method: http.MethodPost,
			headers: map[string]string{
				"Connection": "Upgrade", "Upgrade": "websocket",
				"Sec-WebSocket-Version": "13", "Sec-WebSocket-Key": validKey,
			},
			status: http.StatusMethodNotAllowed,
		},
		{
			name:   "older draft version",
			method: http.MethodGet,
			headers: map[string]string{
				"Connection": "Upgrade", "Upgrade": "websocket",
				"Sec-WebSocket-Version": "8", "Sec-WebSocket-Key": validKey,
			},
			status: http.StatusUpgradeRequired,
		},
		{
			name:   "missing key",
			method: http.MethodGet,
			headers: map[string]string{
				"Connection": "Upgrade", "Upgrade": "websocket",
				"Sec-WebSocket-Version": "13",
			},
			status: http.StatusBadRequest,
		},
		{
			// A key that is not sixteen bytes of base64 means the request was
			// never a WebSocket handshake, whatever its headers claim.
			name:   "malformed key",
			method: http.MethodGet,
			headers: map[string]string{
				"Connection": "Upgrade", "Upgrade": "websocket",
				"Sec-WebSocket-Version": "13", "Sec-WebSocket-Key": "short",
			},
			status: http.StatusBadRequest,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, "/api/session", nil)
			for name, value := range test.headers {
				request.Header.Set(name, value)
			}
			recorder := httptest.NewRecorder()
			upgrader := Upgrader{}
			if _, err := upgrader.Accept(recorder, request); err == nil {
				t.Fatal("the handshake was accepted")
			}
			if recorder.Code != test.status {
				t.Fatalf("status = %d, want %d", recorder.Code, test.status)
			}
		})
	}
}

func TestVersionRefusalAdvertisesThirteen(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/session", nil)
	request.Header.Set("Connection", "Upgrade")
	request.Header.Set("Upgrade", "websocket")
	request.Header.Set("Sec-WebSocket-Version", "8")
	request.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	recorder := httptest.NewRecorder()
	upgrader := Upgrader{}
	if _, err := upgrader.Accept(recorder, request); err == nil {
		t.Fatal("an old draft version was accepted")
	}
	if got := recorder.Header().Get("Sec-WebSocket-Version"); got != "13" {
		t.Fatalf("Sec-WebSocket-Version = %q, want 13", got)
	}
}

func TestOriginIsCheckedBecauseWebSocketsIgnoreCORS(t *testing.T) {
	// A WebSocket handshake is not subject to CORS, so without this any page
	// the user visits could drive a session on their own machine.
	server := handshakeServer(t, Upgrader{})
	if _, _, err := Dial(websocketURL(t, server), http.Header{"Origin": {"http://elsewhere.example"}}); err == nil {
		t.Fatal("a foreign origin was accepted")
	}
	// The page's own origin still works, and so does a client that sends none.
	origin := strings.TrimPrefix(server.URL, "http://")
	connection, _, err := Dial(websocketURL(t, server), http.Header{"Origin": {"http://" + origin}})
	if err != nil {
		t.Fatalf("dial from the server's own origin: %v", err)
	}
	_ = connection.Close()
}

func TestCheckOriginOverrideIsHonoured(t *testing.T) {
	// A phone reaching the server by LAN IP while the page was loaded from a
	// hostname is a real configuration, so the policy has to be replaceable.
	server := handshakeServer(t, Upgrader{CheckOrigin: func(*http.Request) bool { return true }})
	connection, _, err := Dial(websocketURL(t, server), http.Header{"Origin": {"http://elsewhere.example"}})
	if err != nil {
		t.Fatalf("dial with an allow-all origin check: %v", err)
	}
	_ = connection.Close()
}

func TestUpgraderMessageLimitReachesTheConnection(t *testing.T) {
	server := handshakeServer(t, Upgrader{MaxMessageSize: 64})
	connection, _, err := Dial(websocketURL(t, server), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer connection.Close()
	if err := connection.WriteBinary(make([]byte, 128)); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, _, err = connection.ReadMessage()
	var closeError *CloseError
	if !errors.As(err, &closeError) {
		t.Fatalf("read: %v, want a close error", err)
	}
	if closeError.Code != CloseMessageTooBig {
		t.Fatalf("close code = %d, want %d", closeError.Code, CloseMessageTooBig)
	}
}
