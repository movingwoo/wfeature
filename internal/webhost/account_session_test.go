package webhost

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"github.com/movingwoo/wfeature/internal/accounts"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/session"
	"github.com/movingwoo/wfeature/internal/wsproto"
)

const sharedFixtureGame = "games/skt/checkpoint.zip"

// A contended start can answer with an occupied game or a replacement
// question. Read the response by request ID, as the browser does.
func sharedReply(t *testing.T, page *wsproto.Conn, id uint64) serverMessage {
	t.Helper()
	if err := page.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	defer page.SetReadDeadline(time.Time{})
	for {
		op, data, err := page.ReadMessage()
		if err != nil {
			t.Fatalf("reading response %d: %v", id, err)
		}
		if op != wsproto.OpText {
			continue
		}
		var reply serverMessage
		if err := json.Unmarshal(data, &reply); err != nil {
			t.Fatal(err)
		}
		if reply.ID == id {
			return reply
		}
	}
}

func TestSharedConcurrentStartsReserveOneAccountSlot(t *testing.T) {
	s, addr := sharedHost(t)
	pages := []*wsproto.Conn{
		accountSocket(t, addr, signIn(t, s, "register", "player")),
		accountSocket(t, addr, signIn(t, s, "login", "player")),
	}
	send(t, pages[0], clientMessage{Kind: clientStart, Game: "games/missing.zip", ID: 1})
	if reply := sharedReply(t, pages[0], 1); reply.Kind != serverError {
		t.Fatalf("missing game: %+v", reply)
	}
	gate := make(chan struct{})
	sent := make(chan error, len(pages))
	for i, page := range pages {
		message := clientMessage{Kind: clientStart, Game: sharedFixtureGame, Token: fmt.Sprintf("%032x", i+1), ID: 2}
		data, err := json.Marshal(message)
		if err != nil {
			t.Fatal(err)
		}
		go func() {
			<-gate
			sent <- page.WriteText(string(data))
		}()
	}
	close(gate)
	for range pages {
		if err := <-sent; err != nil {
			t.Fatal(err)
		}
	}
	starts := 0
	for _, page := range pages {
		reply := sharedReply(t, page, 2)
		switch {
		case reply.Kind == serverStarted && reply.Started != nil:
			starts++
			expectFrame(t, page)
		case reply.Occupied, reply.Confirmation != "":
		default:
			t.Fatalf("contended start: %+v", reply)
		}
	}
	if starts != 1 || s.claimCount() != 1 {
		t.Fatalf("simultaneous starts: %d games, %d claims", starts, s.claimCount())
	}
}

func TestSharedFiveAccountsKeepConnectedAndParkedGames(t *testing.T) {
	s, addr := sharedHost(t)
	const token = "10000000000000000000000000000000"
	var pages []*wsproto.Conn
	for i := range 5 {
		page := accountSocket(t, addr, signIn(t, s, "register", fmt.Sprintf("player%d", i)))
		pages = append(pages, page)
		send(t, page, clientMessage{Kind: clientStart, Game: sharedFixtureGame, Token: token, ID: 1})
		if reply := sharedReply(t, page, 1); reply.Started == nil {
			t.Fatalf("account %d could not start: %+v", i, reply)
		}
		expectFrame(t, page)
		if i == 0 || i == 2 {
			send(t, page, clientMessage{Kind: clientPark, ID: 2})
			expectMessage(t, page, serverResult)
		}
	}
	if s.parkedCount() != 2 || s.claimCount() != 5 {
		t.Fatalf("before resuming: %d parked, %d claims", s.parkedCount(), s.claimCount())
	}
	for i, page := range pages {
		send(t, page, clientMessage{Kind: clientResume, Token: token, ID: 3})
		if reply := sharedReply(t, page, 3); reply.Started == nil || reply.Started.Game != sharedFixtureGame {
			t.Fatalf("account %d lost its game: %+v", i, reply)
		}
		expectFrame(t, page)
	}
}

func TestSharedTakeoverAndStaleReplacement(t *testing.T) {
	s, addr := sharedHost(t)
	cookie := signIn(t, s, "register", "player")
	login, err := s.accounts.Lookup(t.Context(), cookie.Value)
	if err != nil {
		t.Fatal(err)
	}
	phone := accountSocket(t, addr, cookie)
	desktop := accountSocket(t, addr, signIn(t, s, "login", "player"))
	const phoneToken = "20000000000000000000000000000000"
	const desktopToken = "30000000000000000000000000000000"
	send(t, phone, clientMessage{Kind: clientStart, Game: sharedFixtureGame, Token: phoneToken, ID: 1})
	expectMessage(t, phone, serverStarted)
	expectFrame(t, phone)
	send(t, desktop, clientMessage{Kind: clientResume, Token: desktopToken, ID: 2})
	if reply := sharedReply(t, desktop, 2); !reply.Occupied {
		t.Fatalf("a new device did not find the existing controller: %+v", reply)
	}
	send(t, desktop, clientMessage{Kind: clientResume, Token: desktopToken, Takeover: true, ID: 3})
	expectMessage(t, phone, serverDetached)
	expectMessage(t, desktop, serverStarted)
	expectFrame(t, desktop)
	send(t, phone, clientMessage{Kind: clientStop, ID: 4})
	expectMessage(t, phone, serverResult)
	send(t, desktop, clientMessage{Kind: clientQuickSave, ID: 4})
	expectMessage(t, desktop, serverResult)

	// Both pages ask about the same instance. Only one may use its answer.
	send(t, desktop, clientMessage{Kind: clientKey, Action: session.KeyPress, Code: '3'})
	send(t, desktop, clientMessage{Kind: clientPark, ID: 5})
	expectMessage(t, desktop, serverResult)
	parked := s.parkedGame(login.User.ID)
	if parked == nil || !parked.Paused() || len(parked.HeldKeys()) != 0 {
		t.Fatal("parking did not pause the guest and release its held key")
	}
	request := clientMessage{Kind: clientStart, Game: "games/ktf/checkpoint.zip", Token: phoneToken, ID: 6}
	send(t, phone, request)
	phoneApproval := sharedReply(t, phone, 6).Confirmation
	request.Token = desktopToken
	send(t, desktop, request)
	desktopApproval := sharedReply(t, desktop, 6).Confirmation
	if phoneApproval == "" || desktopApproval == "" {
		t.Fatal("replacement did not ask both pages to confirm")
	}
	request.Confirmation = desktopApproval
	request.ID = 7
	send(t, desktop, request)
	if reply := sharedReply(t, desktop, 7); reply.Started == nil || reply.Started.Game != request.Game {
		t.Fatalf("confirmed replacement: %+v", reply)
	}
	send(t, desktop, clientMessage{Kind: clientPark, ID: 8})
	expectMessage(t, desktop, serverResult)
	replacement := s.parkedGame(login.User.ID)
	request.Token = phoneToken
	request.Confirmation = phoneApproval
	send(t, phone, request)
	if reply := sharedReply(t, phone, 7); reply.Started != nil || reply.Confirmation == phoneApproval {
		t.Fatalf("obsolete confirmation affected the replacement: %+v", reply)
	}
	if replacement == nil || s.parkedGame(login.User.ID) != replacement || s.claimCount() != 1 {
		t.Fatal("the newer instance or its save claim was lost")
	}
	send(t, phone, clientMessage{Kind: clientResume, Token: phoneToken, ID: 9})
	expectMessage(t, phone, serverStarted)
	send(t, phone, clientMessage{Kind: clientQuickSave, ID: 10})
	expectMessage(t, phone, serverResult)

	// Changing games from the current controller also keeps the old game
	// playable while its confirmation is open.
	request = clientMessage{Kind: clientStart, Game: sharedFixtureGame, Token: phoneToken, ID: 11}
	send(t, phone, request)
	request.Confirmation = sharedReply(t, phone, 11).Confirmation
	if request.Confirmation == "" {
		t.Fatal("the controller could replace its active game without confirmation")
	}
	send(t, phone, clientMessage{Kind: clientQuickSave, ID: 12})
	expectMessage(t, phone, serverResult)
	request.ID = 13
	send(t, phone, request)
	if reply := sharedReply(t, phone, 13); reply.Started == nil || reply.Started.Game != sharedFixtureGame {
		t.Fatalf("confirmed active replacement: %+v", reply)
	}
	expectFrame(t, phone)
}

func TestSharedParkDeadlineChecksInstanceAndParkTime(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		if sharedParkLifetime != 30*time.Minute {
			t.Fatalf("default park lifetime: %v", sharedParkLifetime)
		}
		s := newTestServer(t, Options{Accounts: new(accounts.Store)})
		const owner = "40000000000000000000000000000000"
		directory := filepath.Join(t.TempDir(), "skt", "owner")
		if held, reason := s.claimSaveDirectory(directory, "fixture"); !held {
			t.Fatal(reason)
		}
		defer s.releaseSaveDirectory(directory)
		saves := backend.NewDirectorySaveStore(directory)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		archive := checkpointSKTBrowserArchive(t, "sgs")
		game, err := session.Start(ctx, archive, session.Options{SaveStore: saves})
		if err != nil {
			t.Fatal(err)
		}
		defer game.Close()
		if err := saves.StoreSave("progress", []byte("saved")); err != nil {
			t.Fatal(err)
		}
		checkpoint, err := game.CaptureCheckpoint(ctx)
		if err != nil {
			t.Fatal(err)
		}
		identity := game.ArchiveIdentity()
		if err := saves.StoreCheckpoint(identity, checkpoint); err != nil {
			t.Fatal(err)
		}
		p := &parkedSession{game: game, context: ctx, cancel: cancel, label: "fixture", saveDirectory: directory}
		s.parkSession(owner, p)
		firstPark := p.parkedAt
		time.Sleep(sharedParkLifetime - time.Nanosecond)
		controller := new(sessionRunner)
		if got, ok := s.resumeSession(owner, controller); !ok || got != p {
			t.Fatal("could not resume just before expiry")
		}
		time.Sleep(time.Minute)
		s.expireParked(owner, p, firstPark)
		if ctx.Err() != nil || !game.Running() || s.claimCount() != 1 {
			t.Fatal("a queued timer ended a resumed game")
		}
		if err := game.SendKey(ctx, session.KeyPress, 1); err != nil {
			t.Fatalf("playing after the old deadline: %v", err)
		}
		if _, err := game.Tick(ctx, time.Millisecond); err != nil {
			t.Fatal(err)
		}
		s.parkSession(owner, p)
		s.expireParked(owner, p, firstPark)
		time.Sleep(sharedParkLifetime - time.Nanosecond)
		if s.parkedGame(owner) != game || ctx.Err() != nil {
			t.Fatal("the next park did not receive its full lifetime")
		}
		time.Sleep(time.Nanosecond)
		synctest.Wait()
		if s.parkedCount() != 0 || s.claimCount() != 0 || ctx.Err() == nil || game.Running() {
			t.Fatal("the idle timer did not finish closing the parked game")
		}
		if !s.reserveSession(owner, controller) {
			t.Fatal("the expired account still owns its slot")
		}
		s.releaseSession(owner, controller)
		release, err := backend.ClaimSaveDirectory(directory)
		if err != nil {
			t.Fatalf("the expired game still owns the save lock: %v", err)
		}
		defer release()
		if got, ok := saves.LoadSave("progress"); !ok || string(got) != "saved" {
			t.Fatal("expiration lost an ordinary save")
		}
		if got, ok, err := saves.LoadCheckpoint(identity); err != nil || !ok || !bytes.Equal(got, checkpoint) {
			t.Fatalf("expiration changed the manual quick slot: %v", err)
		}
	})
}
