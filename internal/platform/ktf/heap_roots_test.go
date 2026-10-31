package ktf

import (
	"encoding/json"
	"runtime"
	"testing"
	"time"
	"weak"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
)

func TestHeapPlatformRootsPreserveOwnershipQueuesAndTimers(t *testing.T) {
	client, source := newTestRuntime(t)
	now := time.Unix(1700000000, 0)
	client.Clock = wipicrt.NewManualClock(now)
	card := &jvm.Object{ClassName: "org/kwis/msp/lcdui/Card", Fields: make(map[string]jvm.Value)}
	runnable := &jvm.Object{ClassName: jvm.ObjectClass}
	owner := &jvm.Object{ClassName: runtimeTimerClass}
	clip := &jvm.Object{ClassName: "org/kwis/msp/media/Clip"}
	card.SetFieldValue("clip", jvm.ReferenceValue(clip))
	source.displayCards = []*jvm.Object{card, card}
	source.dockedCard, source.activeJlet = card, card
	source.runtimeObjects = map[string]*jvm.Object{"screen": card, "clock": owner}
	source.pendingThreads = []*jvm.Object{runnable}
	source.pendingSerial = []*jvm.Object{runnable, runnable}
	source.serialDueAt = now.Add(23 * time.Millisecond)
	source.serialPaintOwners = map[*jvm.Object]*jvm.Object{runnable: card}
	source.jletListeners = []*jvm.Object{runnable}
	source.grabbedKeys = map[int32]*jvm.Object{17: card}
	source.kfcOwnedKeys = map[int32]kfcKeyOwner{18: {
		form: card, field: card, visibilityRevision: jvm.IntValue(4), childrenRevision: jvm.IntValue(9),
		event: runtimeComponentEventState{revision: jvm.IntValue(2), hasListener: true, listenerValue: jvm.ReferenceValue(runnable), listener: runnable, hasData: true, dataValue: jvm.ReferenceValue(card), data: card},
	}}
	source.PendingTimers = []wipicrt.WIPICTimer{{Task: guestValue(runnable), Owner: guestValue(owner), PaintedCard: guestValue(card), Due: now.Add(37 * time.Millisecond), Period: time.Second}}
	source.Events = []wipicrt.GuestEvent{{Kind: wipicrt.EventKindKey, Param1: 17, Param2: -3, Param3: 7}}
	source.clips = map[weak.Pointer[jvm.Object]]*javaClipState{weak.Make(clip): {Data: []byte("authored sound bytes"), Handle: backend.AudioHandle(17), Loaded: true, Played: true}}
	source.cInputCardOwner = card
	client.focusedText = card
	saved, err := source.captureHeapState([]*jvm.Object{card, nil})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	var parsed runtimeHeapState
	if err := json.Unmarshal(encoded, &parsed); err != nil {
		t.Fatal(err)
	}
	freshClient, fresh := newTestRuntime(t)
	freshNow := time.Unix(1900000000, 0)
	freshClient.Clock = wipicrt.NewManualClock(freshNow)
	roots, err := fresh.restoreHeapState(parsed)
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 2 || roots[1] != nil || roots[0] == card {
		t.Fatal("caller roots changed")
	}
	restoredCard := roots[0]
	if len(fresh.displayCards) != 2 || fresh.displayCards[0] != restoredCard || fresh.displayCards[1] != restoredCard || fresh.dockedCard != restoredCard || fresh.activeJlet != restoredCard || fresh.runtimeObjects["screen"] != restoredCard || freshClient.focusedText != restoredCard || fresh.cInputCardOwner != restoredCard {
		t.Fatal("platform root sharing changed")
	}
	restoredRunnable := fresh.pendingSerial[0]
	if restoredRunnable == runnable || fresh.pendingSerial[1] != restoredRunnable || fresh.pendingThreads[0] != restoredRunnable || fresh.serialPaintOwners[restoredRunnable] != restoredCard || fresh.jletListeners[0] != restoredRunnable || fresh.grabbedKeys[17] != restoredCard {
		t.Fatal("queued ownership changed")
	}
	if fresh.serialDueAt.Sub(freshNow) != 23*time.Millisecond || fresh.PendingTimers[0].Due.Sub(freshNow) != 37*time.Millisecond || guestObject(fresh.PendingTimers[0].PaintedCard) != restoredCard || guestObject(fresh.PendingTimers[0].Task) != restoredRunnable {
		t.Fatal("timer ownership or relative deadlines changed")
	}
	key := fresh.kfcOwnedKeys[18]
	if key.form != restoredCard || key.field != restoredCard || key.event.listener != restoredRunnable || key.event.data != restoredCard || key.visibilityRevision != jvm.IntValue(4) || key.childrenRevision != jvm.IntValue(9) || !key.event.hasListener || !key.event.hasData {
		t.Fatal("key ownership changed")
	}
	if got, ok := fresh.NextGuestEvent(); !ok || got != source.Events[0] || len(source.Events) != 1 {
		t.Fatal("restored event queue changed or shares its source")
	}
	restoredClip, _ := restoredCard.Fields["clip"].Reference()
	clipData := fresh.clips[weak.Make(restoredClip)]
	if clipData == nil || clipData.Handle != 17 || !clipData.Loaded || !clipData.Played || string(clipData.Data) != "authored sound bytes" {
		t.Fatal("weak clip ownership or metadata changed")
	}
	clipData.Data[0] = 'A'
	if source.clips[weak.Make(clip)].Data[0] != 'a' {
		t.Fatal("restored media bytes share their source")
	}
	if _, err := fresh.captureHeapState(roots); err != nil {
		t.Fatalf("platform roots recapture: %v", err)
	}
	if _, err := runtimeTimerCancel(fresh, freshClient.vm, []jvm.Value{jvm.ReferenceValue(fresh.runtimeObjects["clock"])}); err != nil {
		t.Fatal(err)
	}
	if len(fresh.PendingTimers) != 0 || len(source.PendingTimers) != 1 {
		t.Fatal("Timer.cancel did not retain its restored owner or changed the source")
	}
}

func TestHeapPlatformRootsRejectMalformedBindingsBeforeAdoption(t *testing.T) {
	_, source := newTestRuntime(t)
	card := &jvm.Object{ClassName: jvm.ObjectClass}
	source.displayCards = []*jvm.Object{card}
	source.runtimeObjects = map[string]*jvm.Object{"screen": card}
	source.PendingTimers = []wipicrt.WIPICTimer{{Task: guestValue(card)}}
	saved, err := source.captureHeapState(nil)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*runtimeHeapState){
		func(s *runtimeHeapState) { s.Roots.CallerCount = 1 << 30 },
		func(s *runtimeHeapState) { s.Roots.DisplayCards[0] = 1 << 30 },
		func(s *runtimeHeapState) { s.Roots.Objects = append(s.Roots.Objects, s.Roots.Objects[0]) },
		func(s *runtimeHeapState) { s.Roots.Timers[0].Period = -1 },
	} {
		var bad runtimeHeapState
		if err := json.Unmarshal(encoded, &bad); err != nil {
			t.Fatal(err)
		}
		change(&bad)
		client, fresh := newTestRuntime(t)
		marker := client.vm.NewString("old target")
		fresh.displayCards = []*jvm.Object{marker}
		if err := client.vm.BindAOTObject(0x1000, marker); err != nil {
			t.Fatal(err)
		}
		if _, err := fresh.restoreHeapState(bad); err == nil {
			t.Fatal("malformed platform roots accepted")
		}
		if fresh.displayCards[0] != marker {
			t.Fatal("failed restoration changed the platform roots")
		}
		if got, _ := client.vm.AOTObjectAt(0x1000); got != marker {
			t.Fatal("failed restoration changed the VM heap")
		}
	}
}

func TestHeapPlatformWeakOwnersRemainCollectible(t *testing.T) {
	_, source := newTestRuntime(t)
	clip := &jvm.Object{ClassName: "org/kwis/msp/media/Clip"}
	source.clips = map[weak.Pointer[jvm.Object]]*javaClipState{weak.Make(clip): {Data: []byte("authored")}}
	saved, err := source.captureHeapState(nil)
	if err != nil {
		t.Fatal(err)
	}
	runtime.KeepAlive(clip)
	_, fresh := newTestRuntime(t)
	roots, err := fresh.restoreHeapState(saved)
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 0 || len(fresh.clips) != 1 {
		t.Fatal("weak fixture shape changed")
	}
	var owner weak.Pointer[jvm.Object]
	for key := range fresh.clips {
		owner = key
	}
	for range 10 {
		runtime.GC()
		if owner.Value() == nil {
			runtime.KeepAlive(roots)
			return
		}
	}
	runtime.KeepAlive(roots)
	t.Fatal("restored caller roots retained a weak-only platform owner")
}
