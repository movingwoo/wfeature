package lgt

import (
	"context"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/testfixture"
)

// dropLGTListenerGuestRoots removes the authored fixture's incidental roots at
// an idle boundary. Object fields are deliberately left alone: the tests must
// distinguish a reachable native edge from an unreachable reference cycle.
func dropLGTListenerGuestRoots(fixture *lgtWIPIListenerFixture) {
	fixture.t.Helper()
	client := fixture.client
	if client.javaCallDepth != 0 || client.activeJavaWorker != nil || len(client.thread.LiveContexts()) != 1 {
		fixture.t.Fatal("guest roots may only be dropped at an idle fixture boundary")
	}
	client.releaseJavaPins(0)
	fixture.writeWord(testfixture.LGTListenerHistoryCount, 0)
	if err := client.core.Memory().Write(testfixture.LGTListenerHistory,
		make([]byte, testfixture.LGTListenerHistoryCapacity*16)); err != nil {
		fixture.t.Fatal(err)
	}
	for register := 0; register <= armcore.RegisterLR; register++ {
		if register != armcore.RegisterSP {
			if err := client.thread.SetRegister(register, 0); err != nil {
				fixture.t.Fatal(err)
			}
		}
	}
	// The collector conservatively scans committed stack pages, including
	// frames that have already returned. No guest frame is live here.
	for _, region := range client.core.Memory().CommittedRegions(armcore.PermissionReadWrite) {
		if region.Base >= stackBase && uint64(region.Base)+region.Size <= uint64(stackBase)+stackSize {
			if err := client.core.Memory().Write(region.Base, make([]byte, int(region.Size))); err != nil {
				fixture.t.Fatal(err)
			}
		}
	}
}

func linkLGTListenerCycle(fixture *lgtWIPIListenerFixture, listener, clip uint32) {
	fixture.t.Helper()
	record, ok := fixture.client.javaRun.objects[listener]
	if !ok || record.blockSize < 12 {
		fixture.t.Fatal("authored listener has no reference field")
	}
	fixture.writeWord(record.block+8, clip)
}

func wantLGTListenerTracked(t *testing.T, client *Client, want bool, objects ...uint32) {
	t.Helper()
	for _, object := range objects {
		if got := tracked(client, object); got != want {
			t.Fatalf("object %#x tracked = %v, want %v", object, got, want)
		}
	}
}

func wantLGTListenerSoundReleased(t *testing.T, client *Client, clip uint32, sound backend.AudioHandle) {
	t.Helper()
	if _, exists := client.clips[clip]; exists {
		t.Fatalf("reclaimed Java Clip %#x still owns a native media entry", clip)
	}
	if sound != 0 {
		if _, err := client.audio.Playback(sound, client.clock.now()); err == nil {
			t.Fatalf("reclaimed Java Clip %#x still owns sound %d", clip, sound)
		}
	}
}

func TestLGTWIPIListenerGCReachableIdleClipKeepsListener(t *testing.T) {
	fixture := newLGTWIPIListenerFixture(t)
	fixture.setListener(fixture.listeners[0])
	linkLGTListenerCycle(fixture, fixture.listeners[0], fixture.clip)
	root, err := fixture.client.allocateWords([]uint32{fixture.clip})
	if err != nil {
		t.Fatal(err)
	}
	dropLGTListenerGuestRoots(fixture)
	collect(t, fixture.client)
	collect(t, fixture.client)
	wantLGTListenerTracked(t, fixture.client, true, fixture.clip, fixture.listeners[0])
	// The unused peer proves allocation pins were not what kept the listener.
	wantLGTListenerTracked(t, fixture.client, false, fixture.listeners[1])
	fixture.writeWord(root, 0)
	collect(t, fixture.client)
	collect(t, fixture.client)
	wantLGTListenerTracked(t, fixture.client, false, fixture.clip, fixture.listeners[0])
	wantLGTListenerSoundReleased(t, fixture.client, fixture.clip, 0)
}

func TestLGTWIPIListenerGCActiveAndPausedClipsKeepUnreferencedCycles(t *testing.T) {
	for _, paused := range []bool{false, true} {
		t.Run(map[bool]string{false: "playing", true: "paused"}[paused], func(t *testing.T) {
			fixture := newLGTWIPIListenerFixture(t)
			fixture.setListener(fixture.listeners[0])
			linkLGTListenerCycle(fixture, fixture.listeners[0], fixture.clip)
			fixture.call("play", true, true)
			if paused {
				fixture.call("pause", true)
				fixture.advance(time.Hour)
			}
			fixture.drain()
			sound := fixture.client.clips[fixture.clip].handle
			dropLGTListenerGuestRoots(fixture)
			collect(t, fixture.client)
			collect(t, fixture.client)
			wantLGTListenerTracked(t, fixture.client, true, fixture.clip, fixture.listeners[0])
			wantLGTListenerTracked(t, fixture.client, false, fixture.listeners[1])
			if fixture.client.audio.Paused(sound) != paused || fixture.client.audio.Playing(sound) == paused {
				t.Fatal("collection changed retained clip playback")
			}
			fixture.call("stop", true)
			fixture.drain()
			dropLGTListenerGuestRoots(fixture)
			collect(t, fixture.client)
			collect(t, fixture.client)
			wantLGTListenerTracked(t, fixture.client, false, fixture.clip, fixture.listeners[0])
			wantLGTListenerSoundReleased(t, fixture.client, fixture.clip, sound)
		})
	}
}

func TestLGTWIPIListenerGCQueuedSnapshotsKeepReplacedAndNullListeners(t *testing.T) {
	fixture := newLGTWIPIListenerFixture(t)
	fixture.setListener(fixture.listeners[0])
	linkLGTListenerCycle(fixture, fixture.listeners[0], fixture.clip)
	linkLGTListenerCycle(fixture, fixture.listeners[1], fixture.clip)
	fixture.call("play", true)
	fixture.setListener(fixture.listeners[1])
	fixture.call("pause", true)
	fixture.setListener(0)
	fixture.call("stop", true)
	sound := fixture.client.clips[fixture.clip].handle
	dropLGTListenerGuestRoots(fixture)
	collect(t, fixture.client)
	collect(t, fixture.client)
	wantLGTListenerTracked(t, fixture.client, true, fixture.clip, fixture.listeners[0], fixture.listeners[1])
	fixture.drain()
	fixture.wantHistory(fixture.event(0, 2), fixture.event(1, 4))
	dropLGTListenerGuestRoots(fixture)
	collect(t, fixture.client)
	collect(t, fixture.client)
	wantLGTListenerTracked(t, fixture.client, false, fixture.clip, fixture.listeners[0], fixture.listeners[1])
	wantLGTListenerSoundReleased(t, fixture.client, fixture.clip, sound)
}

func TestLGTWIPIListenerGCCompletedCycleReleasedAfterEndDelivery(t *testing.T) {
	fixture := newLGTWIPIListenerFixture(t)
	fixture.setListener(fixture.listeners[0])
	linkLGTListenerCycle(fixture, fixture.listeners[0], fixture.clip)
	fixture.call("play", true)
	fixture.drain()
	fixture.advance(fixture.duration() + time.Millisecond)
	sound := fixture.client.clips[fixture.clip].handle
	if fixture.client.audio.Playing(sound) {
		t.Fatal("authored score did not finish")
	}
	dropLGTListenerGuestRoots(fixture)
	collect(t, fixture.client)
	collect(t, fixture.client)
	wantLGTListenerTracked(t, fixture.client, true, fixture.clip, fixture.listeners[0])
	fixture.drain()
	fixture.wantHistory(fixture.event(0, 1))
	dropLGTListenerGuestRoots(fixture)
	collect(t, fixture.client)
	collect(t, fixture.client)
	wantLGTListenerTracked(t, fixture.client, false, fixture.clip, fixture.listeners[0])
	wantLGTListenerSoundReleased(t, fixture.client, fixture.clip, sound)
}

func TestLGTWIPIListenerGCDetachedBatchKeepsEveryRecipient(t *testing.T) {
	fixture := newLGTWIPIListenerFixture(t)
	collections := installLGTListenerCollectingCallback(fixture)
	first := fixture.clip
	fixture.setListener(fixture.listeners[0])
	fixture.call("play", true)
	fixture.setListener(0)
	fixture.call("stop", true)
	firstSound := fixture.client.clips[first].handle
	second := fixture.newClip()
	fixture.clip = second
	fixture.setListener(fixture.listeners[1])
	fixture.call("play", true)
	fixture.setListener(0)
	fixture.call("stop", true)
	secondSound := fixture.client.clips[second].handle
	fixture.clip = first
	dropLGTListenerGuestRoots(fixture)
	collect(t, fixture.client)
	if got := fixture.drain(); got != 2 {
		t.Fatalf("detached batch delivered %d callbacks, want 2", got)
	}
	if *collections != 4 {
		t.Fatalf("guest callbacks triggered %d collections, want 4", *collections)
	}
	fixture.wantHistory(
		lgtWIPIListenerEvent{receiver: fixture.listeners[0], clip: first, event: 2},
		lgtWIPIListenerEvent{receiver: fixture.listeners[1], clip: second, event: 2},
	)
	wantLGTListenerTracked(t, fixture.client, true, first, second, fixture.listeners[0], fixture.listeners[1])
	dropLGTListenerGuestRoots(fixture)
	collect(t, fixture.client)
	collect(t, fixture.client)
	wantLGTListenerTracked(t, fixture.client, false, first, second, fixture.listeners[0], fixture.listeners[1])
	wantLGTListenerSoundReleased(t, fixture.client, first, firstSound)
	wantLGTListenerSoundReleased(t, fixture.client, second, secondSound)
}

// installLGTListenerCollectingCallback replaces only the fixture's executable
// callback bytes. The guest calls a test-only Java SVC twice before recording
// its four arguments; later queued recipients are never copied into guest
// memory before those collections. System.gc is deliberately not involved.
func installLGTListenerCollectingCallback(fixture *lgtWIPIListenerFixture) *int {
	fixture.t.Helper()
	client := fixture.client
	const class, member = "test/ListenerGC", "test/ListenerGC.collect()V"
	if _, exists := javaPlatformMethods[member]; exists {
		fixture.t.Fatal("test collector method is already registered")
	}
	collections := 0
	javaPlatformMethods[member] = javaPlatformMethod{
		Implementat: func(client *Client, _ context.Context, _ *armcore.Thread, _ []uint32) (uint32, error) {
			// The Java dispatcher does not acquire client.mu for a native
			// method. Take it explicitly around this forced collection.
			client.mu.Lock()
			defer client.mu.Unlock()
			if _, err := client.collectJavaObjects(nil); err != nil {
				return 0, err
			}
			collections++
			return 0, nil
		},
	}
	fixture.t.Cleanup(func() { delete(javaPlatformMethods, member) })
	surface := client.javaLink.surface
	slot := uint32(len(surface.StaticMethods))
	surface.StaticMethods = append(surface.StaticMethods, javaMemberRef{Name: "collect", Descriptor: "()V"})
	surface.Classes = append(surface.Classes, javaAPIClass{Name: class, StaticMethods: javaRun{Start: slot, Count: 1}})
	stub, err := client.stub(svcCategoryJava, javaStaticMethodSlot(slot))
	if err != nil {
		fixture.t.Fatal(err)
	}
	address := fixture.word(testfixture.LGTListenerCallbackAddress)
	a := &assembler{base: address}
	a.emit(armPushLR)
	for register := uint32(0); register < 4; register++ {
		a.emit(armMovReg(4+register, register))
	}
	for range 2 {
		a.literal(12, stub)
		a.call(12)
	}
	for register := uint32(0); register < 4; register++ {
		a.emit(armMovReg(register, 4+register))
	}
	a.literal(4, testfixture.LGTListenerHistoryCount)
	a.emit(armLdr(5, 4, 0), armCmpImm(5, testfixture.LGTListenerHistoryCapacity))
	full := len(a.words)
	a.emit(0) // bhs return
	a.literal(6, testfixture.LGTListenerHistory)
	a.emit(0xe0866205) // add r6, r6, r5, lsl #4
	for register := uint32(0); register < 4; register++ {
		a.emit(armStr(register, 6, register*4))
	}
	a.emit(armAddImm(5, 5, 1), armStr(5, 4, 0))
	a.words[full] = 0x2a000000 | uint32(len(a.words)-full-2)&0xffffff
	a.emit(armPopPC)
	code := a.finish()
	inside := false
	for _, section := range client.module.Sections {
		if section.Executable && address >= section.Address &&
			uint64(address)+uint64(len(code)) <= uint64(section.Address)+uint64(len(section.Data)) {
			inside = true
		}
	}
	if !inside {
		fixture.t.Fatalf("collecting callback does not fit its executable section: %d bytes", len(code))
	}
	if err := client.core.Memory().Load(address, code); err != nil {
		fixture.t.Fatal(err)
	}
	return &collections
}
