package clet

import (
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/cheat"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
	"github.com/movingwoo/wfeature/internal/testfixture"
)

// A quick save is refused while a cheat holds a value or a patch, as on every
// other platform: the slot would keep the frozen value as the program's own, or
// the patched bytes, with nothing left that put them there. Once both are let
// go the save is taken, and a load that displaces the session lets go of its
// cheat engine with its client.
func TestCheckpointRefusesActiveCheatsAndALoadDropsThem(t *testing.T) {
	program, checkpoint, store := fixtureCheckpoint(t)
	session := fixtureSession(t, program, store)
	defer session.Close()
	fixtureTicks(t, session, 3)
	engine := session.Cheat()
	if session.CheatConsole() == nil {
		t.Fatal("the session has no cheat console")
	}
	u32, _ := cheat.ParseValueType("u32")
	if _, err := engine.Freeze(testfixture.SKTCletCounter, u32, 7, "frames"); err != nil {
		t.Fatal(err)
	}
	if _, err := session.CaptureCheckpoint(t.Context()); err == nil {
		t.Error("a checkpoint was captured with a freeze active")
	}
	engine.UnfreezeAll()

	held := make([]byte, 4)
	if err := session.Client.Core().Memory().Read(testfixture.SKTCletCounter, held); err != nil {
		t.Fatal(err)
	}
	if err := engine.ApplyPatch(cheat.PatchEntry{Name: "counter", Patches: []cheat.Patch{{
		Address: cheat.Address(testfixture.SKTCletCounter), Expect: held, Replace: cheat.HexBytes{7, 0, 0, 0},
	}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := session.CaptureCheckpoint(t.Context()); err == nil {
		t.Error("a checkpoint was captured with a patch applied")
	}
	if _, err := engine.RevertAllPatches(); err != nil {
		t.Fatal(err)
	}
	if _, err := session.CaptureCheckpoint(t.Context()); err != nil {
		t.Fatalf("a checkpoint with the cheats let go: %v", err)
	}

	prepared, err := PrepareCheckpoint(program, checkpoint, SessionOptions{Clock: wipicrt.NewManualClock(time.Time{}), SaveStore: store})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Discard()
	restored, err := prepared.Commit(t.Context(), session, store)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if session.cheat != nil || session.cheatConsole != nil {
		t.Error("the displaced session still holds its cheat engine")
	}
}
