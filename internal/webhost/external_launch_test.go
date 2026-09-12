package webhost

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/platform/skt"
	"github.com/movingwoo/wfeature/internal/session"
	"github.com/movingwoo/wfeature/internal/wsproto"
)

func TestExternalLaunchMailboxIsBoundedAndAcknowledgedByRequest(t *testing.T) {
	mailbox := &externalLaunchMailbox{}
	mailbox.request("http://first.invalid/item")
	mailbox.request("https://ignored.invalid/item")
	pending, ok := mailbox.snapshot()
	if !ok || pending.Request == 0 || pending.URL != "http://first.invalid/item" {
		t.Fatalf("pending = %+v, %v", pending, ok)
	}

	runner := &sessionRunner{
		game:           &session.Session{},
		externalLaunch: mailbox,
		outText:        make(chan outboundMessage, 4),
	}
	runner.flushExternalLaunchRequest()
	event := externalLaunchReply(t, runner)
	if event.Kind != serverExternalLaunch || event.ExternalLaunch == nil || *event.ExternalLaunch != pending {
		t.Fatalf("event = %+v", event)
	}
	runner.flushExternalLaunchRequest()
	if len(runner.outText) != 0 {
		t.Fatal("standing request was announced twice")
	}

	runner.handleExternalLaunch(clientMessage{Kind: clientExternal, Request: 99, ID: 2})
	if reply := externalLaunchReply(t, runner); reply.Kind != serverResult || reply.ID != 2 {
		t.Fatalf("stale acknowledgement reply = %+v", reply)
	}
	if still, ok := mailbox.snapshot(); !ok || still != pending {
		t.Fatalf("stale acknowledgement changed pending request: %+v, %v", still, ok)
	}

	runner.handleExternalLaunch(clientMessage{Kind: clientExternal, Request: pending.Request, ID: 3})
	if reply := externalLaunchReply(t, runner); reply.Kind != serverResult || reply.ID != 3 {
		t.Fatalf("acknowledgement reply = %+v", reply)
	}
	if _, ok := mailbox.snapshot(); ok {
		t.Fatal("matching acknowledgement did not clear request")
	}

	mailbox.request("https://next.invalid/item")
	runner.flushExternalLaunchRequest()
	next := externalLaunchReply(t, runner)
	if next.ExternalLaunch == nil || next.ExternalLaunch.Request == pending.Request || next.ExternalLaunch.URL != "https://next.invalid/item" {
		t.Fatalf("next event = %+v", next)
	}
}

func TestScriptExternalLaunchSurvivesResumeAndAcknowledgesWithoutGuestCallback(t *testing.T) {
	root := t.TempDir()
	gameRoot := filepath.Join(root, "games")
	if err := os.MkdirAll(gameRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gameRoot, "external.zip"), scriptExternalArchive(t), 0o600); err != nil {
		t.Fatal(err)
	}
	server := newTestServer(t, Options{GameRoot: gameRoot, SaveRoot: filepath.Join(root, "savedata"), LogRoot: filepath.Join(root, "logs")})
	httpServer := httptest.NewServer(server)
	t.Cleanup(httpServer.Close)
	connection, _, err := wsproto.Dial("ws://"+strings.TrimPrefix(httpServer.URL, "http://")+"/api/session", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })

	expectMessage(t, connection, serverReady)
	send(t, connection, clientMessage{Kind: clientStart, Game: "games/external.zip", ID: 1})
	started := expectMessage(t, connection, serverStarted)
	first := expectMessage(t, connection, serverExternalLaunch)
	if started.Started == nil || first.ExternalLaunch == nil || first.ExternalLaunch.Request == 0 ||
		first.ExternalLaunch.URL != "http://example.invalid/download" {
		t.Fatalf("start = %+v, external launch = %+v", started, first)
	}

	// A stale page cannot clear the standing link. Parking detaches the page,
	// and resume gives the new owner one fresh announcement of the same token.
	send(t, connection, clientMessage{Kind: clientExternal, Request: first.ExternalLaunch.Request + 1, ID: 2})
	expectMessage(t, connection, serverResult)
	send(t, connection, clientMessage{Kind: clientPark, ID: 3})
	expectMessage(t, connection, serverResult)
	send(t, connection, clientMessage{Kind: clientResume, Token: started.Started.Token, ID: 4})
	expectMessage(t, connection, serverStarted)
	resumed := expectMessage(t, connection, serverExternalLaunch)
	if resumed.ExternalLaunch == nil || *resumed.ExternalLaunch != *first.ExternalLaunch {
		t.Fatalf("resumed external launch = %+v, want %+v", resumed.ExternalLaunch, first.ExternalLaunch)
	}

	// Acknowledgement clears Host bookkeeping only. A later key callback can
	// make a new request, proving no completion callback was invented here.
	send(t, connection, clientMessage{Kind: clientExternal, Request: resumed.ExternalLaunch.Request, ID: 5})
	expectMessage(t, connection, serverResult)
	send(t, connection, clientMessage{Kind: clientKey, Action: session.KeyPress, Code: skt.KeyCodeFire})
	next := expectMessage(t, connection, serverExternalLaunch)
	if next.ExternalLaunch == nil || next.ExternalLaunch.Request == resumed.ExternalLaunch.Request {
		t.Fatalf("next external launch = %+v", next.ExternalLaunch)
	}
	send(t, connection, clientMessage{Kind: clientStop, ID: 6})
	expectMessage(t, connection, serverResult)

	// A new game gets an identity outside the old mailbox's lifetime. A late
	// acknowledgement from the stopped game cannot consume its first request.
	send(t, connection, clientMessage{Kind: clientStart, Game: "games/external.zip", ID: 7})
	restarted := expectMessage(t, connection, serverStarted)
	replacement := expectMessage(t, connection, serverExternalLaunch)
	if restarted.Started == nil || replacement.ExternalLaunch == nil ||
		replacement.ExternalLaunch.Request == next.ExternalLaunch.Request {
		t.Fatalf("restarted external launch = %+v", replacement.ExternalLaunch)
	}
	send(t, connection, clientMessage{Kind: clientExternal, Request: next.ExternalLaunch.Request, ID: 8})
	expectMessage(t, connection, serverResult)
	send(t, connection, clientMessage{Kind: clientPark, ID: 9})
	expectMessage(t, connection, serverResult)
	send(t, connection, clientMessage{Kind: clientResume, Token: restarted.Started.Token, ID: 10})
	expectMessage(t, connection, serverStarted)
	stillPending := expectMessage(t, connection, serverExternalLaunch)
	if stillPending.ExternalLaunch == nil || *stillPending.ExternalLaunch != *replacement.ExternalLaunch {
		t.Fatalf("old acknowledgement changed new game request: %+v", stillPending.ExternalLaunch)
	}
	send(t, connection, clientMessage{Kind: clientStop, ID: 11})
	expectMessage(t, connection, serverResult)
}

func scriptExternalArchive(t *testing.T) []byte {
	t.Helper()
	data := make([]byte, 52)
	data[0] = 1
	copy(data[10:26], "External fixture")
	entry := func(index int, code ...byte) {
		binary.LittleEndian.PutUint16(data[28+index*2:], uint16(len(data)))
		data = append(data, code...)
	}
	launch := []byte{5, 0, 0xc4, 5, 9, 0x0a, 16, 0xff}
	entry(0, launch...)
	entry(3, launch...)
	vd := len(data)
	for range 17 {
		data = append(data, 1, 1, 0, 0)
	}
	vi, rd := len(data), len(data)
	resource := []byte("http://example.invalid/download\x00")
	data = append(data, 0, 0, byte(len(resource)), byte(len(resource)>>8))
	ri := len(data)
	data = append(data, resource...)
	for index, value := range []int{vd, vi, rd, ri} {
		binary.LittleEndian.PutUint16(data[44+index*2:], uint16(value))
	}

	var descriptor []byte
	for _, value := range []string{"application/x-gnex-sgs", "SGS"} {
		descriptor = binary.LittleEndian.AppendUint32(descriptor, uint32(len(value)))
		descriptor = append(descriptor, value...)
	}
	var packed bytes.Buffer
	writer := zip.NewWriter(&packed)
	for name, value := range map[string][]byte{"fixture.mod": descriptor, "fixture.sgs": data} {
		file, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write(value); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return packed.Bytes()
}

func externalLaunchReply(t *testing.T, runner *sessionRunner) serverMessage {
	t.Helper()
	var reply serverMessage
	if err := json.Unmarshal([]byte((<-runner.outText).text), &reply); err != nil {
		t.Fatal(err)
	}
	return reply
}
