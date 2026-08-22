package webhost

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/platform/ktf"
	"github.com/movingwoo/wfeature/internal/wsproto"
)

// sessionFixture stands up a server whose game root holds the MIDlet that
// paints, and opens a session on it. The whole point of this file is that the
// path a phone uses can be driven end to end from Go: a real handshake, a real
// game, real frames, and no browser anywhere.
func sessionFixture(t *testing.T) (*wsproto.Conn, string) {
	t.Helper()
	archive, err := os.ReadFile(filepath.Join("..", "platform", "skt", "testdata", "canvas-skt.zip"))
	if err != nil {
		t.Fatalf("read the canvas fixture: %v", err)
	}
	root := t.TempDir()
	gameRoot := filepath.Join(root, "games")
	if err := os.MkdirAll(filepath.Join(gameRoot, "skt"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(gameRoot, "skt", "canvas.zip"), archive, 0o644); err != nil {
		t.Fatalf("write the fixture: %v", err)
	}
	logRoot := filepath.Join(root, "logs")
	server := newTestServer(t, Options{
		GameRoot: gameRoot,
		SaveRoot: filepath.Join(root, "savedata", "ktf"),
		LogRoot:  logRoot,
	})
	httpServer := httptest.NewServer(server)
	t.Cleanup(httpServer.Close)

	connection, _, err := wsproto.Dial("ws://"+strings.TrimPrefix(httpServer.URL, "http://")+"/api/session", nil)
	if err != nil {
		t.Fatalf("dial the session: %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	return connection, logRoot
}

// expectMessage reads until a text message of the wanted kind arrives, letting
// frames, audio and stats pass. A session says several things at once, and a
// test that insisted on their order would be testing the scheduler.
func expectMessage(t *testing.T, connection *wsproto.Conn, kind string) serverMessage {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		opcode, payload, err := connection.ReadMessage()
		if err != nil {
			t.Fatalf("waiting for %q: %v", kind, err)
		}
		if opcode != wsproto.OpText {
			continue
		}
		var message serverMessage
		if err := json.Unmarshal(payload, &message); err != nil {
			t.Fatalf("decode %q: %v", payload, err)
		}
		if message.Kind == serverError && kind != serverError {
			t.Fatalf("session reported an error while waiting for %q: %s", kind, message.Message)
		}
		if message.Kind == kind {
			return message
		}
	}
	t.Fatalf("no %q message arrived", kind)
	return serverMessage{}
}

// expectFrame reads until a picture arrives and decodes it, which is the only
// way to know the page would have had something to draw.
func expectFrame(t *testing.T, connection *wsproto.Conn) (width, height int) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		opcode, payload, err := connection.ReadMessage()
		if err != nil {
			t.Fatalf("waiting for a frame: %v", err)
		}
		if opcode != wsproto.OpBinary {
			// An error on the way to a picture is what the picture was
			// supposed to prove did not happen, so it fails here rather than
			// being skipped over on the way to the next frame.
			var message serverMessage
			if json.Unmarshal(payload, &message) == nil && message.Kind == serverError {
				t.Fatalf("session reported an error while waiting for a frame: %s", message.Message)
			}
			continue
		}
		image, err := png.Decode(bytes.NewReader(payload))
		if err != nil {
			t.Fatalf("the frame is not a PNG: %v", err)
		}
		bounds := image.Bounds()
		return bounds.Dx(), bounds.Dy()
	}
	t.Fatal("no frame arrived")
	return 0, 0
}

func send(t *testing.T, connection *wsproto.Conn, message clientMessage) {
	t.Helper()
	encoded, err := json.Marshal(message)
	if err != nil {
		t.Fatalf("encode %+v: %v", message, err)
	}
	if err := connection.WriteText(string(encoded)); err != nil {
		t.Fatalf("send %s: %v", message.Kind, err)
	}
}

// The page decides from this one field whether to show the run log and the
// report button, so a ready message without it is a release-looking debug run.
func TestSessionReadySaysWhichBuildAnswered(t *testing.T) {
	connection, _ := sessionFixture(t)
	ready := expectMessage(t, connection, serverReady)
	if ready.Profile != backend.BuildProfile() {
		t.Errorf("ready profile = %q, want %q", ready.Profile, backend.BuildProfile())
	}
}

func TestSessionRunsAGameAndSendsPictures(t *testing.T) {
	connection, _ := sessionFixture(t)
	expectMessage(t, connection, serverReady)

	send(t, connection, clientMessage{Kind: clientStart, Game: "games/skt/canvas.zip"})
	started := expectMessage(t, connection, serverStarted)
	if started.Started == nil {
		t.Fatal("the started message carries no description")
	}
	if started.Started.Platform != "skt" {
		t.Errorf("platform = %q, want skt", started.Started.Platform)
	}
	// The page lays out from these before the first picture arrives.
	if started.Started.Width != 240 || started.Started.Height != 320 {
		t.Errorf("screen = %dx%d, want 240x320", started.Started.Width, started.Started.Height)
	}
	// The save owner is what the server roots this game's saves under. Without
	// it a session would silently play with no persistence.
	if started.Started.SaveOwner == "" {
		t.Error("no save owner")
	}

	width, height := expectFrame(t, connection)
	if width != 240 || height != 320 {
		t.Fatalf("frame is %dx%d, want 240x320", width, height)
	}

	// Input reaches the game: the fixture repaints on a key, so another
	// picture arriving is the proof.
	send(t, connection, clientMessage{Kind: clientKey, Action: "press", Code: 148})
	expectFrame(t, connection)

	// The keypad's Menu button is the handset's left soft key, and it is the
	// one key the page sends as a negative number. What is checked here is the
	// protocol rather than the reaction: a MIDlet's soft key runs a command
	// instead of reaching the Canvas, and this fixture has no commands, so a
	// picture is not owed. The session has to take it and carry on, which the
	// next key's picture says and which an error message would now fail.
	send(t, connection, clientMessage{Kind: clientKey, Action: "press", Code: -6})
	send(t, connection, clientMessage{Kind: clientKey, Action: "release", Code: -6})
	send(t, connection, clientMessage{Kind: clientKey, Action: "press", Code: 148})
	expectFrame(t, connection)
}

// The carrier's earlier download package has to reach a browser through the
// same protocol as everything else, and the only way to know it does is to
// drive it: a real handshake, the real archive, real frames, and no browser
// anywhere. It is opt-in because real games are ignored local data.
func TestSessionRunsTheEarlierKTFPackage(t *testing.T) {
	if os.Getenv("WFEATURE_KTF_NATIVE_ACCEPTANCE") != "1" {
		t.Skip("set WFEATURE_KTF_NATIVE_ACCEPTANCE=1 to run the ignored local KTF native package")
	}
	archive, name := localKTFNativePackage(t)
	root := t.TempDir()
	gameRoot := filepath.Join(root, "games")
	if err := os.MkdirAll(filepath.Join(gameRoot, "ktf"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(gameRoot, "ktf", name), archive, 0o644); err != nil {
		t.Fatalf("write the archive: %v", err)
	}
	server := newTestServer(t, Options{
		GameRoot: gameRoot,
		SaveRoot: filepath.Join(root, "savedata", "ktf"),
		LogRoot:  filepath.Join(root, "logs"),
	})
	httpServer := httptest.NewServer(server)
	t.Cleanup(httpServer.Close)
	connection, _, err := wsproto.Dial("ws://"+strings.TrimPrefix(httpServer.URL, "http://")+"/api/session", nil)
	if err != nil {
		t.Fatalf("dial the session: %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })

	expectMessage(t, connection, serverReady)
	send(t, connection, clientMessage{Kind: clientStart, Game: "games/ktf/" + name})
	started := expectMessage(t, connection, serverStarted)
	if started.Started == nil {
		t.Fatal("the started message carries no description")
	}
	if started.Started.Platform != "ktf" {
		t.Errorf("platform = %q, want ktf", started.Started.Platform)
	}
	if started.Started.Width != 240 || started.Started.Height != 320 {
		t.Errorf("screen = %dx%d, want 240x320", started.Started.Width, started.Started.Height)
	}
	// This platform runs on a memory image with instrumented stores, so the
	// panel's write-watch control means something here and is offered.
	if !started.Started.CanWatch {
		t.Error("a WIPI session said it cannot watch writes; the panel would hide a control that works")
	}
	// Without an owner the session would silently play with no persistence,
	// and this package's owner comes out of its module information file.
	if started.Started.SaveOwner == "" {
		t.Error("no save owner")
	}
	width, height := expectFrame(t, connection)
	if width != 240 || height != 320 {
		t.Fatalf("frame is %dx%d, want 240x320", width, height)
	}
	// The key the page sends for fire is what opens this title's own menu, so
	// a further picture after it is input reaching the game.
	send(t, connection, clientMessage{Kind: clientKey, Action: "press", Code: 148})
	send(t, connection, clientMessage{Kind: clientKey, Action: "release", Code: 148})
	expectFrame(t, connection)
}

// localKTFNativePackage finds the earlier KTF package in the ignored local
// game directory, with the name to serve it under.
func localKTFNativePackage(t *testing.T) ([]byte, string) {
	t.Helper()
	directory := filepath.Join("..", "..", "var", "games", "ktf")
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Skipf("read the local KTF game directory: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".zip" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(directory, entry.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		if ktf.IsNativeArchive(data) {
			// The archive's own name is EUC-KR and the served path is a URL,
			// so it is served under a plain one.
			return data, "earlier-package.zip"
		}
	}
	t.Skip("no local KTF native package present")
	return nil, ""
}

// A handful of titles were packaged for a smaller phone and load their artwork
// by the screen they are given, so the page may ask for one. What the started
// message then has to carry is the screen the platform took, because that is
// what the picture will be.
func TestSessionRunsAGameOnTheScreenThePageAsksFor(t *testing.T) {
	connection, _ := sessionFixture(t)
	expectMessage(t, connection, serverReady)

	send(t, connection, clientMessage{Kind: clientStart, Game: "games/skt/canvas.zip", Width: 176, Height: 220})
	started := expectMessage(t, connection, serverStarted)
	if started.Started == nil {
		t.Fatal("the started message carries no description")
	}
	if started.Started.Width != 176 || started.Started.Height != 220 {
		t.Errorf("screen = %dx%d, want 176x220", started.Started.Width, started.Started.Height)
	}
	width, height := expectFrame(t, connection)
	if width != 176 || height != 220 {
		t.Fatalf("frame is %dx%d, want the screen that was asked for", width, height)
	}
}

// A size no handset ever had is refused rather than clamped: it can only come
// from a page this server does not serve, and starting a game on a screen
// nothing was drawn for is worse than not starting it.
func TestSessionRefusesAScreenOutsideTheHandsetRange(t *testing.T) {
	connection, _ := sessionFixture(t)
	expectMessage(t, connection, serverReady)

	send(t, connection, clientMessage{Kind: clientStart, Game: "games/skt/canvas.zip", Width: 20000, Height: 220})
	refusal := expectMessage(t, connection, serverError)
	if !strings.Contains(refusal.Message, "screen") {
		t.Errorf("refusal = %q, want it to name the screen", refusal.Message)
	}
}

func TestSessionMagnifiesWhenTheScaleIsRaised(t *testing.T) {
	// The filter runs on the server, so a phone receives pixels it can draw
	// without resampling — and this is a WIPI-only path, so on a MIDlet the
	// scale must change nothing rather than break the picture.
	connection, _ := sessionFixture(t)
	expectMessage(t, connection, serverReady)
	send(t, connection, clientMessage{Kind: clientStart, Game: "games/skt/canvas.zip"})
	expectMessage(t, connection, serverStarted)
	expectFrame(t, connection)

	send(t, connection, clientMessage{Kind: clientScale, Value: 2})
	width, height := expectFrame(t, connection)
	if width != 240 || height != 320 {
		t.Fatalf("a magnified MIDlet frame is %dx%d, want the surface it drew into", width, height)
	}
}

func TestSessionRefusesAGamePathOutsideTheGameRoot(t *testing.T) {
	connection, _ := sessionFixture(t)
	expectMessage(t, connection, serverReady)
	for _, path := range []string{"games/../../etc/passwd", "games/%2e%2e/escape.jar", "/etc/passwd"} {
		send(t, connection, clientMessage{Kind: clientStart, Game: path})
		message := expectMessage(t, connection, serverError)
		if message.Message == "" {
			t.Errorf("%s was refused without a reason", path)
		}
	}
}

func TestSessionRefusesAMessageItDoesNotUnderstand(t *testing.T) {
	connection, _ := sessionFixture(t)
	expectMessage(t, connection, serverReady)
	// A field that is not part of the protocol is a different page talking, or
	// this one being upgraded underneath the server; either way it is refused
	// rather than half-read.
	if err := connection.WriteText(`{"kind":"key","wiggle":1}`); err != nil {
		t.Fatalf("send: %v", err)
	}
	expectMessage(t, connection, serverError)

	send(t, connection, clientMessage{Kind: "levitate"})
	expectMessage(t, connection, serverError)
}

func TestSessionWritesItsOwnReport(t *testing.T) {
	// The report is written by the side that has the numbers. A page that is
	// only drawing pictures has nothing to say about why a game is slow, so
	// the round trip the browser build makes does not exist here.
	connection, logRoot := sessionFixture(t)
	expectMessage(t, connection, serverReady)
	send(t, connection, clientMessage{Kind: clientStart, Game: "games/skt/canvas.zip"})
	expectMessage(t, connection, serverStarted)

	send(t, connection, clientMessage{Kind: clientReport, ID: 7, Label: "테스트게임"})
	result := expectMessage(t, connection, serverResult)
	if result.ID != 7 {
		t.Errorf("result id = %d, want the request's 7", result.ID)
	}
	report, err := os.ReadFile(result.Message)
	if err != nil {
		t.Fatalf("read the stored report: %v", err)
	}
	if !strings.Contains(string(report), "wfeature session report") {
		t.Fatalf("report = %q", report)
	}
	if !strings.HasPrefix(filepath.Dir(result.Message), logRoot) {
		t.Errorf("the report landed in %s, outside %s", result.Message, logRoot)
	}
}

func TestSessionCheatSearchesAMIDletsObjectGraph(t *testing.T) {
	// The engine sweeps a flat guest address space, which the MIDP runtime does
	// not have — so it builds one over its object graph, and the panel reaches
	// it through the same socket vocabulary the ARM platforms answer.
	connection, _ := sessionFixture(t)
	expectMessage(t, connection, serverReady)
	send(t, connection, clientMessage{Kind: clientStart, Game: "games/skt/canvas.zip"})
	expectMessage(t, connection, serverStarted)

	send(t, connection, clientMessage{Kind: clientCheat, ID: 3, Command: "regions"})
	result := expectMessage(t, connection, serverResult)
	if result.ID != 3 || result.Cheat == nil || !result.Cheat.Searchable {
		t.Fatalf("cheat answer = %+v", result)
	}
	if !strings.Contains(result.Message, "region(s)") {
		t.Fatalf("regions listing = %q, want the map of the object graph", result.Message)
	}

	send(t, connection, clientMessage{Kind: clientCheat, ID: 4, Op: "scan", Type: "u32", Filter: "unknown"})
	scan := expectMessage(t, connection, serverResult)
	if scan.Cheat == nil || scan.Cheat.Count == 0 {
		t.Fatalf("a first scan over a MIDlet's graph found %+v", scan.Cheat)
	}
}

func TestSessionStopEndsTheGameWithoutClosingTheConnection(t *testing.T) {
	connection, _ := sessionFixture(t)
	expectMessage(t, connection, serverReady)
	send(t, connection, clientMessage{Kind: clientStart, Game: "games/skt/canvas.zip"})
	expectMessage(t, connection, serverStarted)
	send(t, connection, clientMessage{Kind: clientStop})

	// The connection still works, which is what lets a page pick another game
	// without reloading.
	send(t, connection, clientMessage{Kind: clientStart, Game: "games/skt/canvas.zip"})
	expectMessage(t, connection, serverStarted)
}

func TestSessionReportsOnAGameThatIsNoLongerRunning(t *testing.T) {
	// A game that quits the moment it starts is the one worth a report, and it
	// is exactly the one that used to answer "no game is running": the
	// diagnostics went with the session when it was closed. The numbers now
	// outlive the game, and the report says how it ended.
	connection, _ := sessionFixture(t)
	expectMessage(t, connection, serverReady)
	send(t, connection, clientMessage{Kind: clientStart, Game: "games/skt/canvas.zip"})
	expectMessage(t, connection, serverStarted)
	send(t, connection, clientMessage{Kind: clientStop})

	send(t, connection, clientMessage{Kind: clientReport, ID: 11})
	result := expectMessage(t, connection, serverResult)
	if result.Message == "no game is running" {
		t.Fatal("the report was refused after the game ended")
	}
	report, err := os.ReadFile(result.Message)
	if err != nil {
		t.Fatalf("read the stored report: %v", err)
	}
	if !strings.Contains(string(report), "ended: the page stopped the game") {
		t.Errorf("the report does not say how the session ended: %q", report)
	}
}

func TestEmptyCheatAnswersStillCarryTheirFields(t *testing.T) {
	// Zero is an answer. A reset narrows to nothing on purpose, and releasing
	// the last freeze leaves an empty list; if those fields are left out of
	// the JSON the panel renders `undefined` instead of `0` and the operation
	// looks broken. A nil slice must not arrive as `null` either.
	encoded, err := encodeMessage(serverMessage{Kind: serverResult, Cheat: (&cheatResult{}).normalize()})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	var decoded struct {
		Cheat map[string]json.RawMessage `json:"cheat"`
	}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for field, want := range map[string]string{
		"count":   "0",
		"items":   "[]",
		"frozen":  "[]",
		"watches": "[]",
		"applied": "0",
	} {
		got, ok := decoded.Cheat[field]
		if !ok {
			t.Errorf("an empty answer left out %q entirely", field)
			continue
		}
		if string(got) != want {
			t.Errorf("%s = %s, want %s", field, got, want)
		}
	}
}

func TestSessionEndpointRefusesAPlainRequest(t *testing.T) {
	server := newTestServer(t, Options{})
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest("GET", "/api/session", nil))
	if recorder.Code == 200 {
		t.Fatal("a request that is not an upgrade was accepted")
	}
}

// A frame is encoded through a shared compressor rather than a fresh one per
// picture. The bytes a phone receives cannot change because of it: the pool
// holds scratch space, and a page that decoded the old stream has to decode
// this one.
func TestPooledPNGEncodingMatchesAFreshEncoder(t *testing.T) {
	picture := image.NewRGBA(image.Rect(0, 0, 240, 320))
	for index := range picture.Pix {
		picture.Pix[index] = byte(index * 31)
	}

	var plain bytes.Buffer
	fresh := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := fresh.Encode(&plain, picture); err != nil {
		t.Fatalf("encode without a pool: %v", err)
	}

	pooled := png.Encoder{CompressionLevel: png.BestSpeed, BufferPool: pngBuffers}
	// Twice: the second encode is the one that reuses what the first returned,
	// which is where a compressor carrying state over would show up.
	for attempt := range 2 {
		var out bytes.Buffer
		if err := pooled.Encode(&out, picture); err != nil {
			t.Fatalf("encode %d through the pool: %v", attempt, err)
		}
		if !bytes.Equal(out.Bytes(), plain.Bytes()) {
			t.Fatalf("encode %d through the pool differs: %d bytes against %d",
				attempt, out.Len(), plain.Len())
		}
	}
}

func TestPNGBufferPoolAnswersNothingWithNil(t *testing.T) {
	// Our half of this is the typed Get: an empty sync.Pool answers a nil any,
	// and the encoder reads a nil buffer as "allocate one". Getting the type
	// assertion wrong would panic on the first frame of every session instead.
	//
	// Whether a Put ever comes back is sync.Pool's business and not something
	// to assert on: under the race detector it deliberately never does.
	var pool pngBufferPool
	var _ png.EncoderBufferPool = &pool

	if buffer := pool.Get(); buffer != nil {
		t.Fatal("an empty pool handed out a buffer")
	}
	pool.Put(&png.EncoderBuffer{})
	pool.Get()
}

// The panel polls for write hits on the same interval it refreshes candidates
// on, so what a platform can do has to reach the page before the panel opens
// rather than as an error every interval. Every platform can watch writes now
// — the MIDlet runtime through the interpreter rather than through a core —
// so what this pins is that the answer is carried at all, and that asking for
// hits is a normal answer rather than a refusal.
func TestStartedSaysWhetherThePlatformCanWatchWrites(t *testing.T) {
	connection, _ := sessionFixture(t)
	expectMessage(t, connection, serverReady)

	send(t, connection, clientMessage{Kind: clientStart, Game: "games/skt/canvas.zip"})
	started := expectMessage(t, connection, serverStarted)
	if started.Started == nil {
		t.Fatal("the started message carries no description")
	}
	if !started.Started.CanWatch {
		t.Error("a MIDlet session said it cannot watch writes; the panel would hide a control that works")
	}

	send(t, connection, clientMessage{Kind: clientCheat, Op: "hits"})
	answer := expectMessage(t, connection, serverResult)
	if answer.Cheat == nil || answer.Cheat.Hits == nil {
		t.Fatalf("asking for write hits answered %+v, want an empty list", answer.Cheat)
	}
	if len(answer.Cheat.Hits.Items) != 0 {
		t.Errorf("a session that has watched nothing reported %d writers", len(answer.Cheat.Hits.Items))
	}
}
