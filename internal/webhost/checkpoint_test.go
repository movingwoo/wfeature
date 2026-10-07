package webhost

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/session"
	"github.com/movingwoo/wfeature/internal/testfixture"
	"github.com/movingwoo/wfeature/internal/wsproto"
)

func checkpointSKTBrowserArchive(t *testing.T, kind string) []byte {
	t.Helper()
	if kind == "java" {
		archive, err := os.ReadFile(filepath.Join("..", "platform", "skt", "testdata", "checkpoint-skt.zip"))
		if err != nil {
			t.Fatal(err)
		}
		return archive
	}
	// Authored SGS: load a counter and show white; each key saves its next
	// value and shows black. Shutdown has no save side effect.
	data := make([]byte, 52)
	data[0] = 1
	copy(data[10:26], "Host checkpoint")
	for index, code := range [][]byte{
		{5, 16, 5, 1, 0x98, 0x55, 0x78, 0xff}, {0xff}, {0xff},
		{0x3a, 16, 1, 5, 16, 5, 1, 0x99, 0x56, 0x78, 0xff},
	} {
		binary.LittleEndian.PutUint16(data[28+index*2:], uint16(len(data)))
		data = append(data, code...)
	}
	variables := len(data)
	for range 17 {
		data = append(data, 1, 1, 0, 0)
	}
	for i, offset := range []int{variables, len(data), len(data), len(data)} {
		binary.LittleEndian.PutUint16(data[44+i*2:], uint16(offset))
	}
	var descriptor []byte
	for _, value := range []string{"application/x-gnex-sgs", "SGS"} {
		descriptor = binary.LittleEndian.AppendUint32(descriptor, uint32(len(value)))
		descriptor = append(descriptor, value...)
	}
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	for _, entry := range []struct {
		name string
		data []byte
	}{{"checkpoint.mod", descriptor}, {"checkpoint.sgs", data}} {
		file, err := writer.Create(entry.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write(entry.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return archive.Bytes()
}

func checkpointServer(t *testing.T, root string) *Server {
	t.Helper()
	return newTestServer(t, Options{GameRoot: filepath.Join(root, "games"), SaveRoot: filepath.Join(root, "saves", "ktf"), LogRoot: filepath.Join(root, "logs")})
}

func checkpointServerFiles(t *testing.T) (string, []byte) {
	t.Helper()
	archive, err := testfixture.KTFCheckpointArchive()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	writeCheckpointGame(t, root, "ktf", archive)
	return root, archive
}

func writeCheckpointGame(t *testing.T, root, platform string, archive []byte) {
	t.Helper()
	path := filepath.Join(root, "games", platform, "checkpoint.zip")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, archive, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestCheckpointBrowserHostResumesAPausedSlot(t *testing.T) {
	for _, variant := range []string{"ktf", "lgt", "skt_java", "skt_sgs"} {
		platform := variant
		if strings.HasPrefix(variant, "skt_") {
			platform = "skt"
		}
		for _, startup := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s_startup_%t", variant, startup), func(t *testing.T) {
				root, _ := checkpointServerFiles(t)
				game := "games/" + platform + "/checkpoint.zip"
				if platform == "lgt" {
					archive, err := testfixture.LGTCheckpointArchive()
					if err != nil {
						t.Fatal(err)
					}
					writeCheckpointGame(t, root, platform, archive)
				}
				if platform == "skt" {
					writeCheckpointGame(t, root, platform, checkpointSKTBrowserArchive(t, strings.TrimPrefix(variant, "skt_")))
				}
				r := &sessionRunner{server: checkpointServer(t, root), frames: make(chan pendingFrame, 1), outText: make(chan outboundMessage, 64)}
				r.startGame(t.Context(), clientMessage{Kind: clientStart, Game: game, ID: 1})
				if r.game == nil || !r.started.CanCheckpoint {
					t.Fatalf("fixture did not start: %+v", readCheckpointReplies(t, r))
				}
				t.Cleanup(r.stopGame)
				if err := r.game.Pause(t.Context()); err != nil {
					t.Fatal(err)
				}
				r.quickSave(t.Context(), clientMessage{Kind: clientQuickSave, ID: 2})
				if !r.started.HasCheckpoint {
					t.Fatalf("paused slot was not saved: %+v", readCheckpointReplies(t, r))
				}
				readCheckpointReplies(t, r)
				if startup {
					r.stopGame()
					r.startGame(t.Context(), clientMessage{Kind: clientStart, Game: game, QuickLoad: true, ID: 3})
				} else {
					if err := r.game.Resume(t.Context()); err != nil {
						t.Fatal(err)
					}
					r.quickLoad(t.Context(), clientMessage{Kind: clientQuickLoad, ID: 3})
				}
				if r.game == nil || !r.game.Running() || r.game.Paused() {
					t.Fatalf("visible browser adopted a paused game: %+v", readCheckpointReplies(t, r))
				}
				if _, err := r.game.Tick(t.Context(), 0); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestCheckpointSKTBrowserHandlersPreserveFailedLoadAndResetEpochs(t *testing.T) {
	for _, kind := range []string{"java", "sgs"} {
		t.Run(kind, func(t *testing.T) {
			archive := checkpointSKTBrowserArchive(t, kind)
			root := t.TempDir()
			writeCheckpointGame(t, root, "skt", archive)
			r := &sessionRunner{server: checkpointServer(t, root), frames: make(chan pendingFrame, 8), outText: make(chan outboundMessage, 64)}
			r.startGame(t.Context(), clientMessage{Kind: clientStart, Game: "games/skt/checkpoint.zip", ID: 1})
			if r.game == nil || !r.started.CanCheckpoint || r.started.HasCheckpoint {
				t.Fatalf("SKT checkpoint capability missing: %+v", readCheckpointReplies(t, r))
			}
			t.Cleanup(r.stopGame)
			readCheckpointReplies(t, r)
			r.handle(t.Context(), clientMessage{Kind: clientKey, Action: session.KeyPress, Code: '1'})
			r.handle(t.Context(), clientMessage{Kind: clientQuickSave, ID: 2})
			if replies := readCheckpointReplies(t, r); len(replies) != 1 || replies[0].Kind != serverResult || !r.started.HasCheckpoint {
				t.Fatalf("SKT save result=%+v", replies)
			}
			savedFrame, _, _, _ := r.game.Frame()
			r.handle(t.Context(), clientMessage{Kind: clientKey, Action: session.KeyRelease, Code: '1'})
			r.handle(t.Context(), clientMessage{Kind: clientKey, Action: session.KeyPress, Code: '2'})
			before, _, _, _ := r.game.Frame()
			store := backend.NewDirectorySaveStore(r.saveDirectory)
			if err := store.StoreSave("progress", []byte("later progress")); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "games", "skt", "checkpoint.zip")
			if err := os.WriteFile(path, append(bytes.Clone(archive), 0), 0600); err != nil {
				t.Fatal(err)
			}
			r.handle(t.Context(), clientMessage{Kind: clientQuickLoad, ID: 3})
			if replies := readCheckpointReplies(t, r); len(replies) != 1 || replies[0].Kind != serverError || r.outputEpoch.Load() != 0 || !r.game.Running() || r.game.Failed() != nil {
				t.Fatalf("failed SKT load displaced its source: %+v", replies)
			}
			after, _, _, _ := r.game.Frame()
			if !bytes.Equal(before, after) || !reflect.DeepEqual(r.game.HeldKeys(), []int32{'2'}) {
				t.Fatal("failed SKT load changed the live frame or held input")
			}
			if _, err := r.game.Tick(t.Context(), 0); err != nil {
				t.Fatalf("source cannot continue after refused load: %v", err)
			}
			if err := os.WriteFile(path, archive, 0600); err != nil {
				t.Fatal(err)
			}
			r.handle(t.Context(), clientMessage{Kind: clientQuickLoad, ID: 4})
			replies := readCheckpointReplies(t, r)
			if len(replies) == 0 || replies[0].Kind != serverRestored || replies[0].Epoch != 1 || !r.started.Restored || len(r.game.HeldKeys()) != 0 || len(r.heldKeys) != 0 {
				t.Fatalf("SKT load did not reset output/input epochs: %+v", replies)
			}
			after, _, _, _ = r.game.Frame()
			if !bytes.Equal(savedFrame, after) {
				t.Fatal("SKT load did not restore its presented frame")
			}
			var final pendingFrame
			for len(r.frames) > 0 {
				final = <-r.frames
			}
			if final.Epoch != 1 || !final.Force {
				t.Fatal("restored SKT output did not request a complete frame")
			}
			r.handle(t.Context(), clientMessage{Kind: clientKey, ID: 5, Action: session.KeyPress, Code: '3'})
			if replies := readCheckpointReplies(t, r); len(replies) != 1 || replies[0].Kind != serverError || len(r.game.HeldKeys()) != 0 {
				t.Fatalf("stale SKT input reached the restored guest: %+v", replies)
			}
			r.handle(t.Context(), clientMessage{Kind: clientKey, Action: session.KeyPress, Code: '4', Epoch: 1})
			if !reflect.DeepEqual(r.game.HeldKeys(), []int32{'4'}) {
				t.Fatal("current SKT input epoch was refused")
			}
			if data, found := store.LoadSave("progress"); !found || string(data) != "later progress" {
				t.Fatal("SKT load rewound the later ordinary save")
			}
		})
	}
}

func TestCheckpointSKTBrowserProtocol(t *testing.T) {
	for _, kind := range []string{"java", "sgs"} {
		t.Run(kind, func(t *testing.T) {
			archive := checkpointSKTBrowserArchive(t, kind)
			root := t.TempDir()
			writeCheckpointGame(t, root, "skt", archive)
			server := checkpointServer(t, root)
			connection, _ := dialCheckpointServer(t, server)
			send(t, connection, clientMessage{Kind: clientStart, Game: "games/skt/checkpoint.zip", ID: 1})
			started := expectMessage(t, connection, serverStarted)
			if started.Started == nil || !started.Started.CanCheckpoint || started.Started.HasCheckpoint {
				t.Fatalf("SKT wire capability=%+v", started)
			}
			send(t, connection, clientMessage{Kind: clientKey, Action: session.KeyPress, Code: '1'})
			send(t, connection, clientMessage{Kind: clientQuickSave, ID: 2})
			if reply := expectMessage(t, connection, serverResult); reply.ID != 2 {
				t.Fatalf("save acknowledgement=%+v", reply)
			}
			send(t, connection, clientMessage{Kind: clientKey, Action: session.KeyRelease, Code: '1'})
			send(t, connection, clientMessage{Kind: clientQuickLoad, ID: 3})
			restored := expectMessage(t, connection, serverRestored)
			if restored.Epoch != 1 || restored.Started == nil || !restored.Started.HasCheckpoint || !restored.Started.Restored {
				t.Fatalf("SKT wire restore=%+v", restored)
			}
			for {
				opcode, payload, err := connection.ReadMessage()
				if err != nil {
					t.Fatal(err)
				}
				if opcode != wsproto.OpBinary || !bytes.HasPrefix(payload, pictureMagic) {
					continue
				}
				if len(payload) < pictureHeaderSize || payload[4] != pictureComplete {
					t.Fatal("new SKT timeline began without a complete picture")
				}
				picture, _ := applyStreamMessage(t, nil, payload)
				if picture.Bounds().Empty() {
					t.Fatal("restored SKT picture is empty")
				}
				break
			}
			send(t, connection, clientMessage{Kind: clientKey, ID: 4, Action: session.KeyPress, Code: '2'})
			if reply := expectMessage(t, connection, serverError); reply.ID != 4 || reply.Epoch != 1 || !strings.Contains(reply.Message, "previous timeline") {
				t.Fatalf("stale SKT wire input=%+v", reply)
			}
			send(t, connection, clientMessage{Kind: clientKey, Action: session.KeyPress, Code: '3', Epoch: 1})
			send(t, connection, clientMessage{Kind: clientQuickSave, ID: 5, Epoch: 1})
			if reply := expectMessage(t, connection, serverResult); reply.ID != 5 {
				t.Fatalf("restored SKT session stopped responding: %+v", reply)
			}
			send(t, connection, clientMessage{Kind: clientStop, ID: 6, Epoch: 1})
			expectMessage(t, connection, serverResult)
			store := backend.NewDirectorySaveStore(server.saveDirectory("skt", started.Started.SaveOwner))
			data, found, err := store.LoadCheckpoint(backend.SaveIdentity(archive))
			if err != nil || !found {
				t.Fatalf("SKT protocol did not persist its slot: %v", err)
			}
			game, err := session.RestoreCheckpoint(t.Context(), archive, data, session.Options{SaveStore: store})
			if err != nil {
				t.Fatal(err)
			}
			defer game.Close()
			if !reflect.DeepEqual(game.HeldKeys(), []int32{'3'}) {
				t.Fatalf("wire load retained old input or dropped current input: %v", game.HeldKeys())
			}
		})
	}
}

func readCheckpointReplies(t *testing.T, r *sessionRunner) []serverMessage {
	t.Helper()
	var replies []serverMessage
	for len(r.outText) > 0 {
		out := <-r.outText
		if out.text == "" {
			continue
		}
		var reply serverMessage
		if err := json.Unmarshal([]byte(out.text), &reply); err != nil {
			t.Fatal(err)
		}
		replies = append(replies, reply)
	}
	return replies
}

func TestCheckpointCommandsPreserveFailedLoadAndRejectStaleInput(t *testing.T) {
	root, archive := checkpointServerFiles(t)
	r := &sessionRunner{server: checkpointServer(t, root), frames: make(chan pendingFrame, 1), outText: make(chan outboundMessage, 64)}
	r.startGame(t.Context(), clientMessage{Kind: clientStart, Game: "games/ktf/checkpoint.zip", ID: 1})
	if r.game == nil || !r.started.CanCheckpoint || r.started.HasCheckpoint {
		t.Fatalf("fixture did not start: %+v", readCheckpointReplies(t, r))
	}
	t.Cleanup(r.stopGame)
	readCheckpointReplies(t, r)
	if ok, _ := r.server.holdSaveDirectory(r.saveDirectory, "external write", false); ok {
		t.Fatal("checkpoint session did not retain its save claim")
	}
	store := backend.NewDirectorySaveStore(r.saveDirectory)
	if err := store.StoreSave("progress", []byte("saved")); err != nil {
		t.Fatal(err)
	}
	r.handle(t.Context(), clientMessage{Kind: clientKey, Action: session.KeyPress, Code: 49})
	r.handle(t.Context(), clientMessage{Kind: clientQuickSave, ID: 2})
	if replies := readCheckpointReplies(t, r); len(replies) != 1 || replies[0].Kind != serverResult || !r.started.HasCheckpoint {
		t.Fatalf("save reply = %+v", replies)
	}
	checkpoint, found, err := store.LoadCheckpoint(backend.SaveIdentity(archive))
	if err != nil || !found {
		t.Fatalf("saved slot = %v, %v", found, err)
	}
	old := r.game.KTF().Client
	if err := old.Core().Memory().Write(testfixture.KTFCheckpointStartupCounter, []byte{9, 0, 0, 0}); err != nil {
		t.Fatal(err)
	}
	if err := store.StoreSave("progress", []byte("later")); err != nil {
		t.Fatal(err)
	}
	// A changed archive is refused before replacement and before any output reset.
	path := filepath.Join(root, "games", "ktf", "checkpoint.zip")
	if err := os.WriteFile(path, append(bytes.Clone(archive), 0), 0600); err != nil {
		t.Fatal(err)
	}
	r.handle(t.Context(), clientMessage{Kind: clientQuickLoad, ID: 3})
	if replies := readCheckpointReplies(t, r); len(replies) != 1 || replies[0].Kind != serverError || r.game.KTF().Client != old || r.outputEpoch.Load() != 0 {
		t.Fatalf("failed load changed the current session: %+v", replies)
	}
	if data, found := store.LoadSave("progress"); !found || string(data) != "later" {
		t.Fatal("failed load changed durable saves")
	}
	if err := os.WriteFile(path, archive, 0600); err != nil {
		t.Fatal(err)
	}
	r.handle(t.Context(), clientMessage{Kind: clientQuickLoad, ID: 4})
	replies := readCheckpointReplies(t, r)
	if len(replies) < 1 || replies[0].Kind != serverRestored || replies[0].Epoch != 1 || r.game.KTF().Client == old || len(r.game.HeldKeys()) != 0 || len(r.heldKeys) != 0 {
		t.Fatalf("load did not adopt/reset input: %+v", replies)
	}
	var word [4]byte
	if err := r.game.KTF().Client.Core().Memory().Read(testfixture.KTFCheckpointStartupCounter, word[:]); err != nil || binary.LittleEndian.Uint32(word[:]) != 1 {
		t.Fatal("load replayed startup or lost guest memory")
	}
	// The load brought the guest back and left the save written after the
	// quick save as it was.
	if data, found := store.LoadSave("progress"); !found || string(data) != "later" {
		t.Fatalf("load changed the save written after the quick save: %q, found %v", data, found)
	}
	r.handle(t.Context(), clientMessage{Kind: clientKey, Action: session.KeyPress, Code: 50})
	if len(r.game.HeldKeys()) != 0 {
		t.Fatal("queued old key reached the new timeline")
	}
	r.handle(t.Context(), clientMessage{Kind: clientKey, Action: session.KeyPress, Code: 51, Epoch: 1})
	if keys := r.game.HeldKeys(); len(keys) != 1 || keys[0] != 51 {
		t.Fatal("new timeline input was dropped")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	r.handle(ctx, clientMessage{Kind: clientQuickSave, ID: 5, Epoch: 1})
	if after, _, err := store.LoadCheckpoint(backend.SaveIdentity(archive)); err != nil || !bytes.Equal(after, checkpoint) {
		t.Fatal("canceled capture replaced the existing slot")
	}
}

func TestCheckpointEncoderRestartsWithCompleteFrame(t *testing.T) {
	r := &sessionRunner{server: newTestServer(t, Options{}), protocol: protocolStream,
		frames: make(chan pendingFrame, 4), outFrames: make(chan outboundMessage, 4)}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); r.writeFrames(ctx) }()
	defer func() { close(r.frames); <-done }()
	frame := scrollingFrames(1, 0)[0]
	r.frames <- frame
	first := <-r.outFrames
	if first.binary[4] != pictureComplete {
		t.Fatal("initial frame was a patch")
	}
	r.outputEpoch.Store(1)
	r.frames <- frame // An old intermediate frame was already queued.
	frame.Epoch = 1
	r.frames <- frame // Same pixels still require a new complete base.
	select {
	case next := <-r.outFrames:
		if next.epoch != 1 || next.binary[4] != pictureComplete {
			t.Fatal("old frame escaped or new timeline began with a patch")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("unchanged restored frame was discarded")
	}
}

func TestCheckpointWriterDropsOldFramesAndSoundBeforeReset(t *testing.T) {
	r, transport, start := writingRunner(t)
	r.outText <- outboundMessage{binary: []byte("old sound"), audio: true, timeline: true}
	r.outFrames <- outboundMessage{binary: []byte("old picture"), timeline: true}
	r.outputEpoch.Store(1)
	r.send(serverMessage{Kind: serverRestored, ID: 7})
	r.sendAudio([]audioEvent{{Kind: audioNoteOn, Note: 60, Velocity: 80}}, false)
	start()
	_, wire := waitForWrites(t, transport, 1)
	if bytes.Contains(wire, []byte("old sound")) || bytes.Contains(wire, []byte("old picture")) {
		t.Fatal("old output reached the replacement timeline")
	}
	client := wsproto.Client(bytes.NewBuffer(wire))
	_, data, err := client.ReadMessage()
	if err != nil || !bytes.Contains(data, []byte(`"kind":"restored"`)) {
		t.Fatalf("reset did not precede reconstructed audio: %s, %v", data, err)
	}
}

func dialCheckpointServer(t *testing.T, server *Server) (*wsproto.Conn, func()) {
	t.Helper()
	httpServer := httptest.NewServer(server)
	connection, _, err := wsproto.Dial("ws://"+strings.TrimPrefix(httpServer.URL, "http://")+"/api/session?protocol=2", nil)
	if err != nil {
		httpServer.Close()
		t.Fatal(err)
	}
	if err := connection.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
		t.Fatal(err)
	}
	closeAll := func() { connection.Close(); httpServer.Close() }
	t.Cleanup(closeAll)
	expectMessage(t, connection, serverReady)
	return connection, closeAll
}

func checkpointConsole(t *testing.T, connection *wsproto.Conn, epoch uint64, command string) string {
	t.Helper()
	send(t, connection, clientMessage{Kind: clientCheat, ID: 100, Epoch: epoch, Command: command})
	reply := expectMessage(t, connection, serverResult)
	if reply.ID != 100 || reply.Cheat == nil {
		t.Fatalf("console answer = %+v", reply)
	}
	return reply.Message
}

func TestCheckpointServerSubprocess(t *testing.T) {
	testCheckpointServerSubprocess(t, "TestCheckpointServerSubprocess", "ktf", testfixture.KTFCheckpointStartupCounter, testfixture.KTFCheckpointArchive)
}

func TestCheckpointNativeServerSubprocess(t *testing.T) {
	testCheckpointServerSubprocess(t, "TestCheckpointNativeServerSubprocess", "ktf", testfixture.KTFNativeCheckpointStartupCounter, testfixture.KTFNativeCheckpointArchive)
}

// The LGT Clet takes the same commands over the same socket: a live load, a
// refused one, and a load in a server process that never ran the title.
func TestCheckpointLGTServerSubprocess(t *testing.T) {
	testCheckpointServerSubprocess(t, "TestCheckpointLGTServerSubprocess", "lgt", testfixture.LGTCheckpointStartupCounter, testfixture.LGTCheckpointArchive)
}

func testCheckpointServerSubprocess(t *testing.T, testName, platform string, counter uint32, build func() ([]byte, error)) {
	game := "games/" + platform + "/checkpoint.zip"
	if root := os.Getenv("WFEATURE_WEB_CHECKPOINT_FIXTURE"); root != "" {
		server := checkpointServer(t, root)
		connection, _ := dialCheckpointServer(t, server)
		send(t, connection, clientMessage{Kind: clientStart, Game: game, QuickLoad: true, ID: 1})
		restored := expectMessage(t, connection, serverRestored)
		if restored.Epoch != 1 || restored.Started == nil || !restored.Started.Restored || !restored.Started.HasCheckpoint {
			t.Fatalf("fresh server did not restore: %+v", restored)
		}
		read := checkpointConsole(t, connection, 1, fmt.Sprintf("read 0x%x u32", counter))
		if !strings.Contains(read, "= 5") {
			t.Fatalf("new server reran startup or lost the checkpoint: %s", read)
		}
		store := backend.NewDirectorySaveStore(server.saveDirectory(platform, restored.Started.SaveOwner))
		if data, found := store.LoadSave("progress"); !found || string(data) != "written after source stopped" {
			t.Fatalf("the new server's load changed the save written after the quick save: %q, found %v", data, found)
		}
		send(t, connection, clientMessage{Kind: clientStop, ID: 2, Epoch: 1})
		expectMessage(t, connection, serverResult)
		return
	}
	archive, err := build()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	writeCheckpointGame(t, root, platform, archive)
	server := checkpointServer(t, root)
	summary, err := session.Inspect(archive)
	if err != nil {
		t.Fatal(err)
	}
	store := backend.NewDirectorySaveStore(server.saveDirectory(platform, summary.SaveOwner))
	if err := store.StoreSave("progress", []byte("saved by source server")); err != nil {
		t.Fatal(err)
	}
	connection, closeSource := dialCheckpointServer(t, server)
	send(t, connection, clientMessage{Kind: clientStart, Game: game, ID: 1})
	expectMessage(t, connection, serverStarted)
	checkpointConsole(t, connection, 0, fmt.Sprintf("set 0x%x 5 u32", counter))
	send(t, connection, clientMessage{Kind: clientQuickSave, ID: 2})
	expectMessage(t, connection, serverResult)
	checkpointConsole(t, connection, 0, fmt.Sprintf("set 0x%x 9 u32", counter))
	send(t, connection, clientMessage{Kind: clientQuickLoad, ID: 3})
	expectMessage(t, connection, serverRestored)
	read := checkpointConsole(t, connection, 1, fmt.Sprintf("read 0x%x u32", counter))
	if !strings.Contains(read, "= 5") {
		t.Fatalf("live load lost guest state: %s", read)
	}
	send(t, connection, clientMessage{Kind: clientCheat, ID: 4, Command: fmt.Sprintf("set 0x%x 88 u32", counter)})
	if reply := expectMessage(t, connection, serverError); reply.ID != 4 || !strings.Contains(reply.Message, "previous timeline") {
		t.Fatalf("stale mutation was not refused: %+v", reply)
	}
	// Corrupt only the slot; refusal must preserve the live game and its epoch.
	identity := backend.SaveIdentity(archive)
	data, found, err := store.LoadCheckpoint(identity)
	if err != nil || !found {
		t.Fatalf("checkpoint slot missing: %v", err)
	}
	slots, err := filepath.Glob(filepath.Join(root, "saves", platform, ".wfeature-quicksave", "owners", "*", fmt.Sprintf("%x.v2.wfq", identity)))
	if err != nil || len(slots) != 1 {
		t.Fatalf("slot paths = %v, %v", slots, err)
	}
	broken := bytes.Clone(data)
	broken[len(broken)-1] ^= 1
	if err := os.WriteFile(slots[0], broken, 0600); err != nil {
		t.Fatal(err)
	}
	send(t, connection, clientMessage{Kind: clientQuickLoad, ID: 5, Epoch: 1})
	expectMessage(t, connection, serverError)
	if read := checkpointConsole(t, connection, 1, fmt.Sprintf("read 0x%x u32", counter)); !strings.Contains(read, "= 5") {
		t.Fatalf("failed load changed live execution: %s", read)
	}
	if err := store.StoreCheckpoint(identity, data); err != nil {
		t.Fatal(err)
	}
	send(t, connection, clientMessage{Kind: clientStop, ID: 6, Epoch: 1})
	expectMessage(t, connection, serverResult)
	closeSource()
	if err := store.StoreSave("progress", []byte("written after source stopped")); err != nil {
		t.Fatal(err)
	}
	binaryPath := os.Getenv("WFEATURE_WEB_CHECKPOINT_RESTORE_BINARY")
	if binaryPath == "" {
		binaryPath, err = os.Executable()
		if err != nil {
			t.Fatal(err)
		}
	}
	command := exec.CommandContext(t.Context(), binaryPath, "-test.run=^"+testName+"$", "-test.v")
	command.Env = append(os.Environ(), "WFEATURE_WEB_CHECKPOINT_FIXTURE="+root)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("new server process failed: %v\n%s", err, output)
	}
}
