package webhost

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
	"github.com/movingwoo/wfeature/internal/platform/ktf"
	"github.com/movingwoo/wfeature/internal/session"
)

func TestTextInputCommitOwnershipAndRetry(t *testing.T) {
	game := &session.Session{}
	runner := &sessionRunner{game: game, gameCtx: context.Background(), outText: make(chan outboundMessage, 1)}
	calls := 0
	runner.textInputGame, runner.textInputID = game, 8
	runner.textInput = &backend.TextInput{Commit: func(_ context.Context, text string) error {
		calls++
		if text != "한글" {
			return backend.ErrInvalidTextInput
		}
		return nil
	}}
	send := func(edit uint64, text string, want string) {
		t.Helper()
		runner.handleTextInput(clientMessage{Kind: clientText, Action: "commit", ID: 3, Edit: edit, Text: text})
		var reply serverMessage
		if err := json.Unmarshal([]byte((<-runner.outText).text), &reply); err != nil {
			t.Fatal(err)
		}
		if reply.Kind != want || reply.ID != 3 {
			t.Fatalf("unexpected reply: %+v", reply)
		}
	}
	send(7, "한글", serverError)
	send(8, strings.Repeat("a", backend.MaxTextInputBytes+1), serverError)
	if calls != 0 {
		t.Fatal("invalid transport request reached guest")
	}
	send(8, "invalid", serverError)
	send(8, "한글", serverResult)
	send(8, "한글", serverError)
	if calls != 2 {
		t.Fatalf("commit calls = %d", calls)
	}
	runner.textInput = &backend.TextInput{Commit: func(context.Context, string) error { t.Fatal("old game commit reached"); return nil }}
	runner.game = &session.Session{}
	send(8, "한글", serverError)
}

func TestTextInputStaleCommitCannotBecomeCurrentAgain(t *testing.T) {
	game := &session.Session{}
	runner := &sessionRunner{game: game, gameCtx: context.Background(), outText: make(chan outboundMessage, 2)}
	runner.textInputGame, runner.textInputID = game, 4
	calls := 0
	runner.textInput = &backend.TextInput{Commit: func(context.Context, string) error {
		calls++
		if calls == 1 {
			return backend.ErrTextInputChanged
		}
		return nil
	}}
	commit := clientMessage{Kind: clientText, Action: "commit", ID: 5, Edit: 4, Text: "complete text"}
	runner.handleTextInput(commit)
	if reply := textInputReply(t, runner); reply.Kind != serverError || reply.Message != backend.ErrTextInputChanged.Error() {
		t.Fatalf("first stale reply = %+v", reply)
	}
	if runner.textInput != nil || runner.textInputGame != nil {
		t.Fatal("stale edit remains installed")
	}
	runner.handleTextInput(commit)
	if reply := textInputReply(t, runner); reply.Kind != serverError || reply.Message != backend.ErrTextInputChanged.Error() {
		t.Fatalf("repeated stale reply = %+v", reply)
	}
	if calls != 1 {
		t.Fatalf("stale closure calls = %d, want 1", calls)
	}
}

func TestTextInputGuestExitSettlesRequestAndEndsGame(t *testing.T) {
	game := &session.Session{}
	runner := &sessionRunner{
		server:     newTestServer(t, Options{LogRoot: t.TempDir()}),
		game:       game,
		gameCtx:    context.Background(),
		outText:    make(chan outboundMessage, 3),
		statsSince: time.Now(),
	}
	runner.textInputGame, runner.textInputID = game, 6
	runner.textInput = &backend.TextInput{Commit: func(context.Context, string) error { return session.ErrExited }}
	runner.handleTextInput(clientMessage{Kind: clientText, Action: "commit", ID: 7, Edit: 6, Text: "complete text"})

	if reply := textInputReply(t, runner); reply.Kind != serverExited {
		t.Fatalf("exit event = %+v", reply)
	}
	answer := textInputReply(t, runner)
	if answer.Kind != serverError || answer.ID != 7 || !answer.Exited {
		t.Fatalf("commit answer = %+v", answer)
	}
	if runner.game != nil || runner.textInput != nil || runner.textInputGame != nil {
		t.Fatal("runner retained game or text edit after guest exit")
	}
}

func TestTextInputOpenEndsGameWhenHeldInputReleaseExits(t *testing.T) {
	game, err := session.Start(t.Context(), textInputExitArchive(t), session.Options{DisableAuthentication: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(game.Close)
	client := game.KTF().Client
	if err := client.JVM().RegisterNative("fixture/ExitCard", "showNotify", "(Z)V", func(*jvm.VM, []jvm.Value) (jvm.Value, error) {
		return jvm.VoidValue(), nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := client.JVM().RegisterNative("fixture/ExitCard", "keyNotify", "(II)Z", func(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		eventType, err := arguments[1].Int32()
		if err != nil {
			return jvm.VoidValue(), err
		}
		if eventType == ktf.KeyReleased {
			return jvm.VoidValue(), fmt.Errorf("fixture release: %w", ktf.ErrGuestExited)
		}
		return jvm.IntValue(0), nil
	}); err != nil {
		t.Fatal(err)
	}
	display := &jvm.Object{ClassName: "org/kwis/msp/lcdui/Display", Fields: make(map[string]jvm.Value)}
	card := &jvm.Object{ClassName: "fixture/ExitCard", Fields: make(map[string]jvm.Value)}
	if _, err := client.JVM().InvokeVirtual(display, "pushCard", "(Lorg/kwis/msp/lcdui/Card;)V", jvm.ReferenceValue(card)); err != nil {
		t.Fatal(err)
	}
	runner := &sessionRunner{
		server:     newTestServer(t, Options{LogRoot: t.TempDir()}),
		game:       game,
		gameCtx:    context.Background(),
		heldKeys:   map[int32]struct{}{141: {}},
		outText:    make(chan outboundMessage, 3),
		statsSince: time.Now(),
	}
	runner.handleTextInput(clientMessage{Kind: clientText, Action: "open", ID: 9})
	if reply := textInputReply(t, runner); reply.Kind != serverExited {
		t.Fatalf("exit event = %+v", reply)
	}
	answer := textInputReply(t, runner)
	if answer.Kind != serverError || answer.ID != 9 || !answer.Exited {
		t.Fatalf("open answer = %+v", answer)
	}
	if runner.game != nil {
		t.Fatal("runner retained game after held-input release exit")
	}
	if len(runner.heldKeys) != 0 {
		t.Fatal("runner retained held key after release")
	}
}

func textInputExitArchive(t *testing.T) []byte {
	t.Helper()
	const (
		classOffset       = 0x200
		descriptorOffset  = 0x220
		constructorOffset = 0x250
		startOffset       = 0x270
		methodTableOffset = 0x290
		fieldTableOffset  = 0x29c
		vtableOffset      = 0x2a0
		classNameOffset   = 0x2b0
		constructorName   = 0x2d0
		startName         = 0x2e0
	)
	address := func(offset uint32) uint32 { return ktf.ImageBase + offset }
	putWords := func(destination []byte, words ...uint32) {
		for index, word := range words {
			binary.LittleEndian.PutUint32(destination[index*4:], word)
		}
	}
	putHalfwords := func(destination []byte, halfwords ...uint16) {
		for index, halfword := range halfwords {
			binary.LittleEndian.PutUint16(destination[index*2:], halfword)
		}
	}

	client := make([]byte, 0x320)
	// Entry returns the executable descriptor at +0x40.
	putHalfwords(client[0x00:], 0xb500, 0x4801, 0xbd00, 0x46c0)
	putWords(client[0x08:], address(0x40))
	putWords(client[0x40:], address(0x80), address(0xc0), 0, 0, 0, address(0x3d), 0, 0, 0, 0)
	putWords(client[0x80:], address(0xa0), address(0xd0), 0, 0, 0, 0, 0, 0)
	putWords(client[0xa0:], 0, 0, address(0x21), 0, address(0x151), 0, 0)
	copy(client[0xc0:], "WIPI_exe\x00")
	copy(client[0xd0:], "ExeInterface\x00")

	// Interface initialization asks the platform allocator for one small block.
	putHalfwords(client[0x20:],
		0xb510, 0x9c02, 0x6ae4, 0x2010, 0x467b, 0x3305, 0x469e,
		0x4720, 0x2800, 0xd001, 0x2000, 0xbd10, 0x2001, 0xbd10,
	)
	putHalfwords(client[0x3c:], 0x2000, 0x4770)
	// GetClass returns the one authored application class below.
	putHalfwords(client[0x150:], 0x4800, 0x4770)
	putWords(client[0x154:], address(classOffset))
	putHalfwords(client[0x160:], 0x4770)
	putHalfwords(client[0x168:], 0x4770)

	putWords(client[classOffset:], address(classOffset+4), 0, address(descriptorOffset), address(vtableOffset))
	binary.LittleEndian.PutUint16(client[classOffset+16:], 2)
	putWords(client[descriptorOffset:], address(classNameOffset), 0, 0, address(methodTableOffset), 0, address(fieldTableOffset))
	binary.LittleEndian.PutUint16(client[descriptorOffset+26:], 4)
	binary.LittleEndian.PutUint16(client[descriptorOffset+28:], 0x0021)
	putWords(client[constructorOffset:], address(0x161), address(classOffset), 0, address(constructorName), 0, 0x0001<<16, 0)
	putWords(client[startOffset:], address(0x169), address(classOffset), 0, address(startName), 0, 0x0001<<16, 0)
	putWords(client[methodTableOffset:], address(constructorOffset), address(startOffset), 0)
	putWords(client[fieldTableOffset:], 0)
	putWords(client[vtableOffset:], address(constructorOffset), address(startOffset))
	copy(client[classNameOffset:], "fixture/Main\x00")
	copy(client[constructorName:], "\x00()V+<init>\x00")
	copy(client[startName:], "\x00([Ljava/lang/String;)V+startApp\x00")

	pack := func(entries map[string][]byte) []byte {
		var packed bytes.Buffer
		writer := zip.NewWriter(&packed)
		for name, contents := range entries {
			entry, err := writer.Create(name)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := entry.Write(contents); err != nil {
				t.Fatal(err)
			}
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		return packed.Bytes()
	}
	jar := pack(map[string][]byte{"client.bin0": client})
	return pack(map[string][]byte{
		"__adf__":     []byte("AID:fixture\nPID:P0001\nMClass:fixture.Main\n"),
		"fixture.jar": jar,
	})
}

func textInputReply(t *testing.T, runner *sessionRunner) serverMessage {
	t.Helper()
	var reply serverMessage
	if err := json.Unmarshal([]byte((<-runner.outText).text), &reply); err != nil {
		t.Fatal(err)
	}
	return reply
}

// Exercise the actual WebSocket and guest text field, including ownership
// changes. Reopening the editor reads the committed value back from the guest.
func TestTextInputSessionRoundTripAndResume(t *testing.T) {
	connection, logs := sessionFixture(t)
	data, err := os.ReadFile(filepath.Join("..", "platform", "skt", "testdata", "text-input.jar"))
	if err != nil {
		t.Fatal(err)
	}
	var packed bytes.Buffer
	writer := zip.NewWriter(&packed)
	for name, contents := range map[string][]byte{
		"text-input.jar": data,
		"text-input.msd": []byte("MIDlet-Name: Text Input Fixture\nMIDlet-1: Text Input Fixture, , TextInputMIDlet\n"),
	} {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(contents); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(logs), "games", "text-input.zip"), packed.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	expectMessage(t, connection, serverReady)
	send(t, connection, clientMessage{Kind: clientText, Action: "open", ID: 1})
	if reply := expectMessage(t, connection, serverError); reply.Message != backend.ErrNoTextInput.Error() {
		t.Fatalf("no-game reply = %+v", reply)
	}
	send(t, connection, clientMessage{Kind: clientStart, Game: "games/text-input.zip", ID: 2})
	started := expectMessage(t, connection, serverStarted)
	open := func(id uint64) *textInputMessage {
		t.Helper()
		send(t, connection, clientMessage{Kind: clientText, Action: "open", ID: id})
		reply := expectMessage(t, connection, serverResult)
		if reply.TextInput == nil || reply.TextInput.Edit == 0 {
			t.Fatalf("missing editor: %+v", reply)
		}
		return reply.TextInput
	}
	first := open(3)
	if first.Text != "" || first.MaxLength != 16 {
		t.Fatalf("initial editor = %+v", first)
	}
	send(t, connection, clientMessage{Kind: clientText, Action: "commit", Edit: first.Edit, Text: "한글 이름 😀", ID: 4})
	expectMessage(t, connection, serverResult)
	second := open(5)
	if second.Text != "한글 이름 😀" {
		t.Fatalf("guest text = %q", second.Text)
	}
	send(t, connection, clientMessage{Kind: clientText, Action: "commit", Edit: second.Edit, Text: strings.Repeat("한", 17), ID: 6})
	if reply := expectMessage(t, connection, serverError); reply.Message != backend.ErrInvalidTextInput.Error() {
		t.Fatalf("limit reply = %+v", reply)
	}
	send(t, connection, clientMessage{Kind: clientPark, ID: 7})
	expectMessage(t, connection, serverResult)
	send(t, connection, clientMessage{Kind: clientResume, Token: started.Started.Token, ID: 8})
	expectMessage(t, connection, serverStarted)
	send(t, connection, clientMessage{Kind: clientText, Action: "commit", Edit: second.Edit, Text: "stale", ID: 9})
	if reply := expectMessage(t, connection, serverError); reply.Message != backend.ErrTextInputChanged.Error() {
		t.Fatalf("stale reply = %+v", reply)
	}
	resumed := open(10)
	if resumed.Text != "한글 이름 😀" {
		t.Fatalf("resumed guest text = %q", resumed.Text)
	}
	send(t, connection, clientMessage{Kind: clientStop, ID: 11})
	expectMessage(t, connection, serverResult)
}
