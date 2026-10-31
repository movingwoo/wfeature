package session

import (
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/cheat"
	"github.com/movingwoo/wfeature/internal/testfixture"
)

// A native SKT program runs on the ARM core like the other native titles, so
// the cheat engine attaches to it the same way: a search for the value the
// program counts finds where it keeps it, a freeze holds it through the rounds
// that would move it, and a watch names the store that moves it once it is
// let go.
func TestACletAnswersTheCheatEngine(t *testing.T) {
	store, _ := backend.NewMemorySaveStore(nil)
	s := sktCletStart(t, sktCletArchive(t, testfixture.SKTCletOptions{}), store)
	defer s.Close()
	engine := s.Cheat()
	if engine == nil || !engine.CanWatch() {
		t.Fatal("an SKT Clet has no cheat engine that can watch")
	}
	if s.CheatConsole() == nil {
		t.Fatal("an SKT Clet has no cheat console")
	}
	swept := false
	for _, region := range engine.Regions() {
		if region.Base <= testfixture.SKTCletCounter && testfixture.SKTCletCounter < region.Base+region.Size {
			swept = region.Label == "client" && !region.Code
		}
	}
	if !swept {
		t.Fatalf("the program's data is not swept as the client's: %+v", engine.Regions())
	}

	u32, _ := cheat.ParseValueType("u32")
	engine.Scanner().SetValueType(u32)
	for range 3 {
		sktCletStep(t, s)
	}
	counted := sktCletWord(t, s, testfixture.SKTCletCounter)
	if counted == 0 {
		t.Fatal("the fixture's timer did not count")
	}
	if _, err := engine.Scan(cheat.ScanFilter{Op: cheat.FilterEq, A: int64(counted)}); err != nil {
		t.Fatal(err)
	}
	sktCletStep(t, s)
	if _, err := engine.Scan(cheat.ScanFilter{Op: cheat.FilterIncreased}); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, candidate := range engine.Candidates() {
		found = found || candidate.Address == testfixture.SKTCletCounter
	}
	if !found {
		t.Fatalf("a search for the counter did not find it at %#x: %+v", testfixture.SKTCletCounter, engine.Candidates())
	}

	if _, err := engine.Freeze(testfixture.SKTCletCounter, u32, 7, "frames"); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		sktCletStep(t, s)
		if got := sktCletWord(t, s, testfixture.SKTCletCounter); got != 7 {
			t.Fatalf("the frozen counter reads %d, want 7", got)
		}
	}

	engine.Unfreeze(testfixture.SKTCletCounter)
	if err := engine.Watch(testfixture.SKTCletCounter); err != nil {
		t.Fatal(err)
	}
	sktCletStep(t, s)
	hits, _, err := engine.WatchHits()
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 || hits[0].Address != testfixture.SKTCletCounter || hits[0].Origin != cheat.OriginGuest {
		t.Fatalf("the program's own store to the counter was not reported: %+v", hits)
	}
}
