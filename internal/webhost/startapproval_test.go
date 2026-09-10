package webhost

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/session"
)

func seedRetained(t *testing.T, server *Server, count int) []context.Context {
	t.Helper()
	contexts := make([]context.Context, count)
	for i := range contexts {
		token, directory := fmt.Sprintf("%032x", i+1), fmt.Sprintf("save-%d", i)
		ctx, cancel := context.WithCancel(context.Background())
		contexts[i] = ctx
		server.claimSaveDirectory(directory, "fixture")
		server.parkSession(token, &parkedSession{game: &session.Session{}, context: ctx, cancel: cancel, saveDirectory: directory, label: "fixture"})
		server.parked[token].parkedAt = time.Unix(int64(i), 0)
	}
	return contexts
}

func TestCapacityApprovalPreservesGamesUntilConfirmed(t *testing.T) {
	server, url := resumeFixture(t)
	contexts := seedRetained(t, server, maxRetainedSessions)
	conn := dialSession(t, url)
	expectMessage(t, conn, serverReady)
	request := clientMessage{Kind: clientStart, Game: "games/skt/canvas.zip", Token: fmt.Sprintf("%032x", 99)}
	send(t, conn, request)
	question := expectMessage(t, conn, serverResult)
	if question.Confirmation == "" {
		t.Fatal("capacity did not ask for approval")
	}
	for _, ctx := range contexts {
		if ctx.Err() != nil {
			t.Fatal("unconfirmed start discarded progress")
		}
	}
	request.Confirmation = "forged"
	send(t, conn, request)
	question = expectMessage(t, conn, serverResult)
	if contexts[0].Err() != nil {
		t.Fatal("forged approval discarded progress")
	}
	request.Confirmation = question.Confirmation
	send(t, conn, request)
	expectMessage(t, conn, serverStarted)
	expectFrame(t, conn)
	for i, ctx := range contexts {
		if (ctx.Err() != nil) != (i == 0) {
			t.Fatalf("wrong victim: %d", i)
		}
	}
	send(t, conn, clientMessage{Kind: clientPark, ID: 5})
	expectMessage(t, conn, serverResult)
	if server.parkedCount() != 4 {
		t.Fatal("disconnect did not retain all admitted games")
	}
	for _, ctx := range contexts[1:] {
		if ctx.Err() != nil {
			t.Fatal("parking silently evicted a game")
		}
	}
}

func TestCapacityApprovalDoesNotAuthorizeAChangedVictim(t *testing.T) {
	server, url := resumeFixture(t)
	contexts := seedRetained(t, server, 4)
	conn := dialSession(t, url)
	expectMessage(t, conn, serverReady)
	request := clientMessage{Kind: clientStart, Game: "games/skt/canvas.zip", Token: fmt.Sprintf("%032x", 99)}
	send(t, conn, request)
	question := expectMessage(t, conn, serverResult)
	// The oldest game became active while the question was open.
	owner := &sessionRunner{}
	parked, ok := server.resumeSession(fmt.Sprintf("%032x", 1), owner)
	if !ok {
		t.Fatal("resume failed")
	}
	defer parked.cancel()
	request.Confirmation = question.Confirmation
	send(t, conn, request)
	changed := expectMessage(t, conn, serverResult)
	if changed.Confirmation == "" || changed.Confirmation == question.Confirmation {
		t.Fatal("changed victim reused approval")
	}
	for _, ctx := range contexts {
		if ctx.Err() != nil {
			t.Fatal("stale approval discarded a game")
		}
	}
	server.releaseSession(fmt.Sprintf("%032x", 1), owner)
}

func TestAllActiveGamesRefuseAnotherStart(t *testing.T) {
	server, url := resumeFixture(t)
	server.attached = make(map[string]*sessionRunner)
	for i := 0; i < 4; i++ {
		server.attached[fmt.Sprintf("%032x", i+1)] = &sessionRunner{admitted: true}
	}
	conn := dialSession(t, url)
	expectMessage(t, conn, serverReady)
	send(t, conn, clientMessage{Kind: clientStart, Game: "games/skt/canvas.zip"})
	if reply := expectMessage(t, conn, serverError); reply.Confirmation != "" {
		t.Fatal("active games offered for eviction")
	}
}

func TestConcurrentStartsCannotOverbookTheLastPlace(t *testing.T) {
	server := newTestServer(t, Options{})
	server.attached = make(map[string]*sessionRunner)
	for i := 0; i < 3; i++ {
		server.attached[fmt.Sprintf("%032x", i+1)] = &sessionRunner{admitted: true}
	}
	runners := []*sessionRunner{
		{server: server, outText: make(chan outboundMessage, 1)},
		{server: server, outText: make(chan outboundMessage, 1)},
	}
	for i, runner := range runners {
		server.reserveSession(fmt.Sprintf("%032x", i+10), runner)
	}
	start := make(chan struct{})
	results := make(chan bool, 2)
	for i, runner := range runners {
		go func(i int, runner *sessionRunner) {
			<-start
			results <- runner.admitStart(clientMessage{Game: "fixture"}, fmt.Sprintf("save-%d", i), "fixture")
		}(i, runner)
	}
	close(start)
	first, second := <-results, <-results
	if first == second {
		t.Fatal("exactly one start must get the last place")
	}
	server.parkedMu.Lock()
	defer server.parkedMu.Unlock()
	if server.retainedCountLocked() != 4 {
		t.Fatal("concurrent starts exceeded capacity")
	}
}
