package lgt

import (
	"bytes"
	"context"
	"errors"
	"math/rand"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/audio/smaf"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/cheat"
)

// The checkpoint fixture is the authored Clet with one thing added: a timer
// whose callback counts, draws the count, presents it and arms itself again.
// That is the shape of a real title's frame loop, and it gives two sessions
// something to disagree about on every tick.
const (
	checkpointTimerRecord uint32 = fixtureDataBase + 0x8e0
	checkpointCounter     uint32 = fixtureDataBase + 0x8f0
	checkpointCodeBase    uint32 = fixtureDataBase + 0x900
)

func checkpointFixtureSession(t *testing.T, archive []byte, store backend.SaveStore) *Session {
	t.Helper()
	session, err := StartSession(context.Background(), archive, SessionOptions{SaveStore: store, Width: 16, Height: 8})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close(context.Background()) })
	client := session.client
	flush, err := client.stub(svcCategoryWIPIC, slotFlushLcd)
	if err != nil {
		t.Fatal(err)
	}
	arm, err := client.stub(svcCategoryWIPIC, slotSetTimer)
	if err != nil {
		t.Fatal(err)
	}
	a := &assembler{base: checkpointCodeBase}
	a.emit(armPushLR)
	a.emit(armMovReg(5, 0)) // r5 = the timer structure
	a.literal(4, checkpointCounter)
	a.emit(armLdr(0, 4, 0), armAddImm(0, 0, 1), armStr(0, 4, 0))
	a.literal(6, fixtureFrameBuffer)
	a.emit(armLdr(6, 6, 0))
	a.emit(armStrh(0, 6)) // the count is the first pixel
	a.literal(4, flush)
	a.emit(armMovImm(0, 0), armMovImm(1, 0))
	a.call(4)
	a.literal(4, arm)
	a.emit(armMovReg(0, 5), armMovImm(1, 20), armMovImm(2, 0), armMovImm(3, 0))
	a.call(4)
	a.emit(armMovImm(0, 0))
	a.emit(armPopPC)
	if err := client.core.Memory().Write(checkpointCodeBase, a.finish()); err != nil {
		t.Fatal(err)
	}
	callSlot(t, client, slotDefTimer, checkpointTimerRecord, checkpointCodeBase)
	callSlot(t, client, slotSetTimer, checkpointTimerRecord, 20, 0, 0)
	return session
}

func tickSession(t *testing.T, session *Session, count int) {
	t.Helper()
	for tick := 0; tick < count; tick++ {
		if err := session.Tick(context.Background()); err != nil {
			t.Fatalf("tick %d: %v", tick, err)
		}
	}
}

func guestWord(t *testing.T, session *Session, address uint32) uint32 {
	t.Helper()
	word, err := session.client.readWord(address)
	if err != nil {
		t.Fatal(err)
	}
	return word
}

func prepareFixtureCheckpoint(t *testing.T, archive []byte, checkpoint backend.Checkpoint, store backend.SaveStore) *PreparedSession {
	t.Helper()
	prepared, err := PrepareSessionCheckpoint(archive, checkpoint, SessionOptions{SaveStore: store})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	return prepared
}

// A checkpoint taken between two ticks restores a session that is the same
// session: the frame, the flush count, the instruction count, the guest clock
// and the title's own memory agree after every tick that follows. It is not
// enough for the screen to match at the moment of the load — a timer restored
// a tick late or a clock restored a millisecond early both look right there.
func TestCheckpointRestoresACletBetweenTicks(t *testing.T) {
	archive := fixtureArchive(t)
	ctx := context.Background()
	store, _ := backend.NewMemorySaveStore(nil)
	source := checkpointFixtureSession(t, archive, store)
	tickSession(t, source, 6)
	source.SendKey(true, '7')
	tickSession(t, source, 1)
	if guestWord(t, source, checkpointCounter) == 0 || guestWord(t, source, fixtureLastEvent) != '7' {
		t.Fatal("the fixture did not run its timer and take its key before the checkpoint")
	}
	checkpoint := roundTripCheckpoint(t, archive, source)
	if checkpoint.Variant != backend.CheckpointLGTClet {
		t.Fatalf("variant = %d, want the Clet one", checkpoint.Variant)
	}

	other := boundarySaves(t, store)
	prepared := prepareFixtureCheckpoint(t, archive, checkpoint, other)
	restored, err := prepared.Commit(ctx, nil, other)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close(ctx)
	if fingerprintOf(restored) != fingerprintOf(source) || memoryDigest(t, restored.client) != memoryDigest(t, source.client) {
		t.Fatal("the restored session differs before any tick")
	}
	if width, height := restored.client.screen.width, restored.client.screen.height; width != 16 || height != 8 {
		t.Fatalf("restored screen is %dx%d, want the 16x8 the checkpoint was taken on", width, height)
	}
	for round := 0; round < 12; round++ {
		if round == 4 {
			source.SendKey(false, '7')
			restored.SendKey(false, '7')
		}
		tickSession(t, source, 1)
		tickSession(t, restored, 1)
		if before, after := fingerprintOf(source), fingerprintOf(restored); before != after {
			t.Fatalf("round %d: restored %+v, want %+v", round, after, before)
		}
	}
	if before, after := guestWord(t, source, checkpointCounter), guestWord(t, restored, checkpointCounter); before != after || before < 10 {
		t.Fatalf("timer counts are %d and %d", before, after)
	}
	if memoryDigest(t, restored.client) != memoryDigest(t, source.client) {
		t.Fatal("guest memory differs after the same ticks")
	}
	// The frame a Host takes is the caller's own and is the whole screen the
	// first time it is asked for.
	frame, width, height, fresh := restored.Frame()
	expected, _, _, _ := source.Frame()
	if !fresh && width*height*4 != len(frame) || !bytes.Equal(frame, expected) {
		t.Fatal("the restored frame is not the source's frame")
	}
}

// A restored session can be saved again, and what it writes is what it was
// restored from. This is the property that makes the codec a codec: nothing is
// lost in a round trip, so nothing accumulates over a hundred of them.
func TestCheckpointRecordSurvivesARoundTrip(t *testing.T) {
	archive := fixtureArchive(t)
	ctx := context.Background()
	store, _ := backend.NewMemorySaveStore(nil)
	source := checkpointFixtureSession(t, archive, store)
	populateCheckpointTables(t, source)
	tickSession(t, source, 3)
	first := roundTripCheckpoint(t, archive, source)

	other := boundarySaves(t, store)
	prepared := prepareFixtureCheckpoint(t, archive, first, other)
	restored, err := prepared.Commit(ctx, nil, other)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close(ctx)
	second := roundTripCheckpoint(t, archive, restored)
	var before, after sessionCheckpointState
	if err := backend.DecodeCheckpointRecord(first.Runtime, &before); err != nil {
		t.Fatal(err)
	}
	if err := backend.DecodeCheckpointRecord(second.Runtime, &after); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("a restored session writes a different record:\n%s", describeStateDifference(before, after))
	}
	// Neither the load nor the second capture stored anything the first
	// session had not stored.
	sourceSaves, _ := store.SnapshotSaves()
	restoredSaves, _ := other.SnapshotSaves()
	if difference := describeSaveDifference(sourceSaves, restoredSaves); difference != "" {
		t.Fatalf("the restored session's saves differ: %s", difference)
	}

	// And the tables behave, not only compare: each of these continues from
	// where the source left it.
	file := uint32(0)
	for handle := range restored.client.files {
		file = handle
	}
	if file == 0 || restored.client.files[file].cursor != source.client.files[file].cursor ||
		!bytes.Equal(restored.client.files[file].data, source.client.files[file].data) {
		t.Fatal("the open file did not keep its bytes and cursor")
	}
	if restored.client.cRandomValue() != source.client.cRandomValue() {
		t.Fatal("the C generator did not continue its sequence")
	}
	if len(restored.client.framebuffers) != len(source.client.framebuffers) {
		t.Fatal("the surfaces were not restored")
	}
	for handle, buffer := range source.client.framebuffers {
		copied := restored.client.framebuffers[handle]
		if copied == nil || copied == buffer || !reflect.DeepEqual(copied.pixels, buffer.pixels) || !reflect.DeepEqual(copied.opaque, buffer.opaque) {
			t.Fatalf("surface %#x was not restored as a copy of its own", handle)
		}
	}
}

// populateCheckpointTables puts something in every table a Clet can fill
// without a module that calls the slots: the slots are driven the way the
// guest would drive them.
func populateCheckpointTables(t *testing.T, session *Session) {
	t.Helper()
	client := session.client
	write := func(text string) uint32 {
		address, err := client.allocateBytes(append([]byte(text), 0))
		if err != nil {
			t.Fatal(err)
		}
		return address
	}
	// An offscreen surface the title draws into itself, and one with a mask.
	surface := callSlot(t, client, slotCreateOffscreen, 4, 3)
	buffer := client.framebuffer(surface)
	buffer.pixels[5] = 0x1234 // differs from guest memory, so it is kept
	masked := client.framebuffer(callSlot(t, client, slotCreateOffscreen, 3, 3))
	masked.opaque = []bool{true, false, true, true, true, false, false, true, true}
	masked.colorKeyed, masked.transparentKey = true, 0xf81f
	if err := client.syncToGuest(masked); err != nil {
		t.Fatal(err)
	}
	// A file open for writing with a cursor in the middle, and a removed path.
	name := write("save.dat")
	handle := callSlot(t, client, slotFsOpen, name, fileOpenReadWrite)
	data := write("written by the guest")
	callSlot(t, client, slotFsWrite, handle, data, 20)
	callSlot(t, client, slotFsSeek, handle, 7, 0)
	gone := write("gone.dat")
	other := callSlot(t, client, slotFsOpen, gone, fileOpenReadWrite)
	callSlot(t, client, slotFsClose, other)
	callSlot(t, client, slotFsRemove, gone)
	// A resource identity, which a title keeps and compares.
	size, err := client.allocate(4)
	if err != nil {
		t.Fatal(err)
	}
	callSlot(t, client, slotGetResourceID, write("data/hello.txt"), size)
	// A clip with data, a volume and a muted source.
	clip := callSlot(t, client, slotClipCreate, write("audio/x-smaf"), 0, 0)
	callSlot(t, client, slotClipPutData, clip, data, 12)
	callSlot(t, client, slotClipSetVolume, clip, 40)
	callSlot(t, client, slotSetVolume, 70)
	callSlot(t, client, slotSetSourceVolume, 11, 55)
	callSlot(t, client, slotSetMuteState, 11, 1)
	callSlot(t, client, slotVibrator, 50, 0)
	// An event and a dial that have not been delivered, an input mode, the C
	// generator part way through its sequence, and a second timer that is not
	// armed.
	callSlot(t, client, slotPostEvent, 0, 600, 3, 4)
	callSlot(t, client, slotNetConnect, checkpointCodeBase+0x80, 9)
	callSlot(t, client, slotIMSetCurrentMode, 3)
	client.seedCRandom(1234)
	for draw := 0; draw < 17; draw++ {
		client.cRandomValue()
	}
	callSlot(t, client, slotDefTimer, checkpointTimerRecord+8, checkpointCodeBase)
	// A pixel operation the guest has answered once.
	operation := installThumb(t, client, 0x1c08, 0x4770) // adds r0, r1, #0; bx lr
	client.installPixelOp(operation)
	if _, err := client.applyPixelOp(context.Background(), client.thread, pixelOp{function: operation, param: 7}, 0x0001, 0x0002); err != nil {
		t.Fatal(err)
	}
	// The dial's callback is the second half of the timer code area: a
	// function that only returns.
	if err := client.core.Memory().Write(checkpointCodeBase+0x80, []byte{0x1e, 0xff, 0x2f, 0xe1}); err != nil { // bx lr
		t.Fatal(err)
	}
}

// describeStateDifference names the first fields two records disagree about.
func describeStateDifference(before, after sessionCheckpointState) string {
	var parts []string
	walk := func(name string, left, right reflect.Value) {
		for index := 0; index < left.NumField(); index++ {
			if !reflect.DeepEqual(left.Field(index).Interface(), right.Field(index).Interface()) {
				parts = append(parts, name+"."+left.Type().Field(index).Name)
			}
		}
	}
	walk("session", reflect.ValueOf(before), reflect.ValueOf(after))
	walk("client", reflect.ValueOf(before.Client), reflect.ValueOf(after.Client))
	if before.Client.Java != nil && after.Client.Java != nil {
		walk("java", reflect.ValueOf(*before.Client.Java), reflect.ValueOf(*after.Client.Java))
	}
	return strings.Join(parts, ", ")
}

// A load into a running session replaces it and leaves the saves alone: what
// was saved after the checkpoint is still there. The session that was running
// is cut off from the store, and closing what is left of it writes nothing.
func TestCheckpointLoadReplacesALiveSessionAndLeavesItsSaves(t *testing.T) {
	archive := fixtureArchive(t)
	ctx := context.Background()
	store, _ := backend.NewMemorySaveStore([]backend.SaveEntry{{Key: "fs/progress", Data: []byte("saved")}})
	source := checkpointFixtureSession(t, archive, store)
	tickSession(t, source, 4)
	checkpoint := roundTripCheckpoint(t, archive, source)
	counted := guestWord(t, source, checkpointCounter)

	// The session carries on and writes a save.
	tickSession(t, source, 5)
	if err := store.StoreSave("fs/progress", []byte("later")); err != nil {
		t.Fatal(err)
	}
	displaced := source.client

	prepared := prepareFixtureCheckpoint(t, archive, checkpoint, store)
	if calls, first := prepared.PreparationStoreCalls(); calls != 0 {
		t.Fatalf("validation made %d save store calls, the first being %s", calls, first)
	}
	restored, err := prepared.Commit(ctx, source, store)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close(ctx)
	prepared.Discard() // after a commit this is nothing
	if guestWord(t, restored, checkpointCounter) != counted {
		t.Fatal("the restored session is not at the checkpoint")
	}
	if saved, _ := store.LoadSave("fs/progress"); string(saved) != "later" {
		t.Fatalf("the save written after the checkpoint reads %q after the load", saved)
	}
	// The displaced session is gone as far as its owner can tell, and what is
	// left of its client cannot reach the store.
	if source.client != nil || source.CanCheckpoint() {
		t.Fatal("the displaced session still has a client")
	}
	if err := source.Close(ctx); err != nil {
		t.Fatal(err)
	}
	before, _ := store.SnapshotSaves()
	name, err := displaced.allocateBytes([]byte("after.dat\x00"))
	if err != nil {
		t.Fatal(err)
	}
	handle := callSlot(t, displaced, slotFsOpen, name, fileOpenReadWrite)
	callSlot(t, displaced, slotFsWrite, handle, name, 4)
	displaced.flushOpenFiles()
	after, _ := store.SnapshotSaves()
	if difference := describeSaveDifference(before, after); difference != "" {
		t.Fatalf("the displaced session wrote into the saves after the load: %s", difference)
	}
	tickSession(t, restored, 3)
	if guestWord(t, restored, checkpointCounter) <= counted {
		t.Fatal("the restored session does not run")
	}
}

// A load that is refused leaves the session and its saves as they were. The
// record is checked in full before the saves are touched, so there is no
// partial load to recover from.
func TestCheckpointRefusalLeavesTheSessionRunning(t *testing.T) {
	archive := fixtureArchive(t)
	ctx := context.Background()
	store, _ := backend.NewMemorySaveStore([]backend.SaveEntry{{Key: "fs/progress", Data: []byte("current")}})
	source := checkpointFixtureSession(t, archive, store)
	tickSession(t, source, 4)
	checkpoint := roundTripCheckpoint(t, archive, source)
	var valid sessionCheckpointState
	if err := backend.DecodeCheckpointRecord(checkpoint.Runtime, &valid); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		mutate func(*sessionCheckpointState)
		// reason is part of the refusal a case has to be refused with, for the
		// ones a later check would otherwise be refusing instead.
		reason string
	}{
		{name: "a newer version", mutate: func(saved *sessionCheckpointState) { saved.Version++ }},
		{name: "another authentication policy", mutate: func(saved *sessionCheckpointState) { saved.DisableAuthentication = true }},
		{name: "an unknown authentication status", mutate: func(saved *sessionCheckpointState) { saved.Authentication = "invented" }},
		{name: "a larger instruction budget", mutate: func(saved *sessionCheckpointState) {
			saved.MaxSteps *= 2
			saved.Client.Core.MaxSteps *= 2
		}},
		{name: "a speed outside the range", mutate: func(saved *sessionCheckpointState) { saved.Speed = 64 }},
		{name: "another module", mutate: func(saved *sessionCheckpointState) { saved.Client.ModuleHash[0] ^= 1 }},
		{name: "a mapping of its own", mutate: func(saved *sessionCheckpointState) {
			saved.Client.Core.Memory.Mappings = append(saved.Client.Core.Memory.Mappings,
				armcore.MappingState{Address: 0x50000000, Size: 0x1000, Permission: armcore.PermissionReadWriteExecute})
		}},
		{name: "a stack pointer outside the stack", mutate: func(saved *sessionCheckpointState) {
			saved.Client.Thread.Context.Registers[armcore.RegisterSP] = heapBase
		}},
		{name: "a clock ahead of the instructions", mutate: func(saved *sessionCheckpointState) { saved.Client.Baseline = saved.Client.Core.Steps + 1 }},
		{name: "an arena block past the cursor", mutate: func(saved *sessionCheckpointState) {
			saved.Client.Heap.Blocks = append(saved.Client.Heap.Blocks, 0, 0, 0, 0x21, 8, 0, 0, 0)
		}},
		{name: "an arena cursor with nothing under it", mutate: func(saved *sessionCheckpointState) { saved.Client.Arena.Cursor += 64 }},
		{name: "a stub outside the stub region", mutate: func(saved *sessionCheckpointState) { saved.Client.Stubs[0].Address = heapBase | 1 }},
		{name: "a screen of another size", mutate: func(saved *sessionCheckpointState) { saved.Client.Width++ }},
		{name: "a presented frame of another size", mutate: func(saved *sessionCheckpointState) {
			saved.Client.Presented = saved.Client.Presented[2:]
		}},
		{name: "a surface with too few pixels", mutate: func(saved *sessionCheckpointState) {
			saved.Client.Screen.Guest, saved.Client.Screen.Pixels = false, []byte{1, 2}
		}},
		{name: "an armed timer with no callback", mutate: func(saved *sessionCheckpointState) { saved.Client.Timers[0].Callback = 0 }},
		{name: "a negative guest clock", mutate: func(saved *sessionCheckpointState) { saved.Client.Elapsed = -time.Second }},
		{name: "a file handle that was never issued", mutate: func(saved *sessionCheckpointState) {
			saved.Client.Files = append(saved.Client.Files, fileState{Handle: saved.Client.NextHandle + 5})
		}},
		{name: "a file read before its start", mutate: func(saved *sessionCheckpointState) {
			saved.Client.Files = append(saved.Client.Files, fileState{Handle: 1, Cursor: -1})
			saved.Client.NextHandle = 2
		}},
		{name: "a loaded clip with no sound", mutate: func(saved *sessionCheckpointState) {
			saved.Client.Clips = append(saved.Client.Clips, clipState{Handle: 8, Loaded: true, Sound: 99, Volume: 10})
		}},
		{name: "a generator with half a state", mutate: func(saved *sessionCheckpointState) {
			saved.Client.Random = &guestRandomState{History: make([]byte, 64)}
		}},
		{name: "a surface the allocator never handed out", mutate: func(saved *sessionCheckpointState) {
			saved.Client.Framebuffers = append(saved.Client.Framebuffers, surfaceState{
				Handle: platformDataBase + 0x800000, Address: surfaceBase + 0x800000, Width: 4096, Height: 4096, Guest: true})
		}},
		{name: "an input mode that does not exist", mutate: func(saved *sessionCheckpointState) { saved.Client.InputMode = 99 }},
		{name: "a Java runtime with a thread on no stack", mutate: func(saved *sessionCheckpointState) {
			saved.Client.Java = &javaState{Workers: []javaWorkerState{{}}}
		}},
		{name: "an adapter this module does not match", mutate: func(saved *sessionCheckpointState) {
			saved.Adapters.Store = adapterStoreOptions
		}},
		{name: "a local service this module does not match", mutate: func(saved *sessionCheckpointState) {
			saved.Adapters.Network = &networkState{}
		}},
		{name: "a screen larger than a session is started with", reason: "an invalid screen", mutate: func(saved *sessionCheckpointState) {
			saved.Client.Width = maxStateScreen + 1
			saved.Client.Screen.Width = saved.Client.Width
			saved.Client.Presented = make([]byte, saved.Client.Width*saved.Client.Height*2)
		}},
		{name: "more spans taken back than there are blocks between them", reason: "invalid blocks or spans", mutate: func(saved *sessionCheckpointState) {
			saved.Client.Heap.Free = make([]arenaSpanState, len(saved.Client.Heap.Blocks)/8+2)
		}},
		{name: "a dial a local service accepted, with no local service", reason: "local service it does not have", mutate: func(saved *sessionCheckpointState) {
			saved.Client.NetConnects = append(saved.Client.NetConnects,
				netConnectState{Offline: true, Generation: saved.Client.NetGeneration, Callback: checkpointCodeBase | 1})
		}},
		{name: "an instruction count an hour past the clock's baseline", reason: "an invalid guest clock", mutate: func(saved *sessionCheckpointState) {
			saved.Client.Core.Steps = saved.Client.Baseline + (uint64(maxGuestWork/time.Millisecond)+1)*guestInstructionsPerMillisecond
		}},
		{name: "a clock past any session", reason: "an invalid guest clock", mutate: func(saved *sessionCheckpointState) {
			saved.Client.Elapsed = maxGuestClock + 1
		}},
		// The first tick adds the work since the baseline to the clock, so a
		// loop that is twenty minutes behind in work is twenty minutes behind.
		{name: "a repeating sound the first tick would replay a million times", reason: "catch-up", mutate: func(saved *sessionCheckpointState) {
			saved.Client.Core.Steps = saved.Client.Baseline + uint64(20*time.Minute/time.Millisecond)*guestInstructionsPerMillisecond
			saved.Client.Audio.Next = 1
			saved.Client.Audio.Sounds = []backend.AudioSoundState{{
				Handle: 1, Events: []smaf.Event{{Time: 0}, {Time: 1}}, Length: time.Millisecond,
				Playing: true, Repeat: true, StartedAt: saved.Client.Elapsed,
			}}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var saved sessionCheckpointState
			if err := backend.DecodeCheckpointRecord(checkpoint.Runtime, &saved); err != nil {
				t.Fatal(err)
			}
			test.mutate(&saved)
			record, err := backend.EncodeCheckpointRecord(saved)
			if err != nil {
				t.Fatal(err)
			}
			damaged := checkpoint
			damaged.Runtime = record
			prepared, err := PrepareSessionCheckpoint(archive, damaged, SessionOptions{SaveStore: store})
			if err == nil {
				prepared.Discard()
				t.Fatal("the record was accepted")
			}
			if !strings.Contains(err.Error(), test.reason) {
				t.Fatalf("the record was refused for another reason: %v", err)
			}
			if saved, _ := store.LoadSave("fs/progress"); string(saved) != "current" {
				t.Fatal("a refused load changed the saves")
			}
		})
	}
	// Another archive and another variant are refused before the record is
	// read at all.
	if _, err := PrepareSessionCheckpoint(append([]byte{0}, archive...), checkpoint, SessionOptions{SaveStore: store}); !errors.Is(err, backend.ErrCheckpointIdentity) {
		t.Fatalf("another archive: %v", err)
	}
	wrong := checkpoint
	wrong.Variant = backend.CheckpointKTFNative
	if _, err := PrepareSessionCheckpoint(archive, wrong, SessionOptions{SaveStore: store}); !errors.Is(err, backend.ErrCheckpointVersion) {
		t.Fatalf("another variant: %v", err)
	}
	wrong.Variant = backend.CheckpointLGTJava
	if _, err := PrepareSessionCheckpoint(archive, wrong, SessionOptions{SaveStore: store}); !errors.Is(err, backend.ErrCheckpointVersion) {
		t.Fatalf("a Clet record under the Java variant: %v", err)
	}
	// A load reads no save while it is checked, so it is checked the same
	// with any store or none. Where the saves are is said when it commits, and
	// a commit with nowhere to find them is refused.
	unbound, err := PrepareSessionCheckpoint(archive, checkpoint, SessionOptions{})
	if err != nil {
		t.Fatalf("a load was checked against a store: %v", err)
	}
	if _, err := unbound.Commit(ctx, source, nil); err == nil {
		t.Fatal("a load was committed with no save store")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := unbound.Commit(cancelled, source, store); !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled load: %v", err)
	}
	unbound.Discard()
	if saved, _ := store.LoadSave("fs/progress"); string(saved) != "current" {
		t.Fatal("a refused load changed the saves")
	}
	// The session the refusals were aimed at is still running, and the record
	// they were made from still loads.
	before := guestWord(t, source, checkpointCounter)
	tickSession(t, source, 3)
	if guestWord(t, source, checkpointCounter) <= before {
		t.Fatal("the session stopped running")
	}
	prepared := prepareFixtureCheckpoint(t, archive, checkpoint, store)
	restored, err := prepared.Commit(ctx, source, store)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close(ctx)
}

// A checkpoint is taken at a boundary and refused anywhere else, and a refusal
// is not a failure: the next request is answered.
func TestCheckpointIsRefusedAwayFromABoundary(t *testing.T) {
	archive := fixtureArchive(t)
	ctx := context.Background()
	store, _ := backend.NewMemorySaveStore(nil)
	session := checkpointFixtureSession(t, archive, store)
	tickSession(t, session, 2)
	client := session.client

	client.javaCallDepth = 1
	if _, err := session.CaptureCheckpoint(ctx); !errors.Is(err, ErrCheckpointBusy) {
		t.Fatalf("inside a guest call: %v", err)
	}
	client.javaCallDepth = 0

	client.saveReadError = errors.New("the disk went away")
	if _, err := session.CaptureCheckpoint(ctx); err == nil || errors.Is(err, ErrCheckpointBusy) {
		t.Fatalf("after a failed save read: %v", err)
	}
	client.saveReadError = nil

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := session.CaptureCheckpoint(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled request: %v", err)
	}

	session.Cheat().Freezes().Insert(cheat.FreezeEntry{Address: checkpointCounter, Value: 1})
	if _, err := session.CaptureCheckpoint(ctx); err == nil {
		t.Fatal("a frozen value was captured")
	}
	session.Cheat().Freezes().Clear()

	if _, err := session.CaptureCheckpoint(ctx); err != nil {
		t.Fatalf("the boundary after the refusals: %v", err)
	}
	// A capture copies no save, so a store that can only be asked for one key
	// at a time is enough. A session with no store at all has nowhere a load
	// could find its saves, and is refused.
	partial, err := StartSession(ctx, archive, SessionOptions{SaveStore: newMemorySaveStore(), Width: 16, Height: 8})
	if err != nil {
		t.Fatal(err)
	}
	defer partial.Close(ctx)
	if _, err := partial.CaptureCheckpoint(ctx); err != nil {
		t.Fatalf("a store that answers one key at a time was refused: %v", err)
	}
	bare, err := StartSession(ctx, archive, SessionOptions{Width: 16, Height: 8})
	if err != nil {
		t.Fatal(err)
	}
	defer bare.Close(ctx)
	if _, err := bare.CaptureCheckpoint(ctx); err == nil || errors.Is(err, ErrCheckpointBusy) {
		t.Fatalf("a session with no save store: %v", err)
	}
	// A title that has ended has nothing to continue.
	client.exited = true
	if _, err := session.CaptureCheckpoint(ctx); err == nil {
		t.Fatal("an exited title was captured")
	}
	client.exited = false
}

// Timers that come due together fire in the order they came due, and two with
// one deadline in the order of their structures — every time, because a
// session and the checkpoint taken from it have to take the same path.
func TestTimersDueTogetherFireInOneOrder(t *testing.T) {
	for attempt := 0; attempt < 20; attempt++ {
		client := fixtureClient(t)
		order, err := client.allocate(64)
		if err != nil {
			t.Fatal(err)
		}
		// callback(structure, param): append the parameter to the list at
		// `order`, whose first word is the count.
		a := &assembler{base: checkpointCodeBase}
		a.literal(2, order)
		a.emit(armLdr(3, 2, 0), armAddImm(3, 3, 1), armStr(3, 2, 0))
		a.emit(0xe7821103) // str r1, [r2, r3, lsl #2]
		a.emit(armBX(14))
		if err := client.core.Memory().Write(checkpointCodeBase, a.finish()); err != nil {
			t.Fatal(err)
		}
		structures := make([]uint32, 6)
		for index := range structures {
			if structures[index], err = client.allocate(8); err != nil {
				t.Fatal(err)
			}
			callSlot(t, client, slotDefTimer, structures[index], checkpointCodeBase)
		}
		// The later structures are armed for the earlier deadline.
		for index, structure := range structures {
			timeout := uint32(40)
			if index >= 3 {
				timeout = 20
			}
			callSlot(t, client, slotSetTimer, structure, timeout, 0, uint32(index+1))
		}
		client.clock.advance(100 * time.Millisecond)
		if err := client.serviceTimers(context.Background()); err != nil {
			t.Fatal(err)
		}
		var fired []uint32
		for index := uint32(1); index <= 6; index++ {
			word, err := client.readWord(order + index*4)
			if err != nil {
				t.Fatal(err)
			}
			fired = append(fired, word)
		}
		if want := []uint32{4, 5, 6, 1, 2, 3}; !reflect.DeepEqual(fired, want) {
			t.Fatalf("attempt %d: timers fired %v, want %v", attempt, fired, want)
		}
	}
}

// A file opened for writing is the handle's own copy. Writing in place through
// the archive's bytes would change what the archive says the file holds, for
// every later reader and for a checkpoint that reopens the archive.
func TestWritingAPackagedFileLeavesTheArchiveAlone(t *testing.T) {
	client := fixtureClient(t)
	name, err := client.allocateBytes([]byte("data/hello.txt\x00"))
	if err != nil {
		t.Fatal(err)
	}
	handle := callSlot(t, client, slotFsOpen, name, fileOpenReadWrite)
	callSlot(t, client, slotFsWrite, handle, name, 4)
	if packaged, _ := client.archive.Resource("data/hello.txt"); string(packaged) != "packaged" {
		t.Fatalf("the archive now says %q", packaged)
	}
	if file := client.files[handle]; string(file.data) != "dataaged" {
		t.Fatalf("the open file holds %q", file.data)
	}
}

// The generator a title seeds is the standard library's sequence, value for
// value, because a seed a title stored has to give the game it gave before.
// It is produced here rather than there so that its state can be held, and
// what that must not change is a single number of it — through the first 607
// values, which come from the library, and far past them, where every value is
// made from the ones before.
func TestGuestRandomIsTheLibrarysSequence(t *testing.T) {
	for _, seed := range []int64{0, 1, -1, 20081231, 1 << 40, -987654321} {
		ours, theirs := newGuestRandom(seed), rand.New(rand.NewSource(seed))
		for draw := 0; draw < 5000; draw++ {
			var left, right int64
			switch draw % 5 {
			case 0:
				left, right = ours.Int63(), theirs.Int63()
			case 1:
				left, right = int64(ours.Uint32()), int64(theirs.Uint32())
			case 2:
				left, right = int64(ours.Intn(97)), int64(theirs.Intn(97))
			case 3:
				left, right = int64(ours.Uint64()), int64(theirs.Uint64())
			default:
				left, right = int64(ours.Int31n(1<<30+7)), int64(theirs.Int31n(1<<30+7))
			}
			if left != right {
				t.Fatalf("seed %d, draw %d: %d, want %d", seed, draw, left, right)
			}
		}
	}
}

// A generator is restored from its state, wherever in its sequence it is:
// before it has handed anything out, part way through the values the library
// gave it, and past them.
func TestGuestRandomContinuesItsSequence(t *testing.T) {
	for _, drawn := range []int{0, 1, 300, 606, 607, 608, 5000} {
		original := newGuestRandom(20081231)
		for draw := 0; draw < drawn; draw++ {
			original.Int63()
		}
		saved, err := original.captureState()
		if err != nil {
			t.Fatal(err)
		}
		restored, err := restoreGuestRandom(saved)
		if err != nil {
			t.Fatal(err)
		}
		for draw := 0; draw < 2000; draw++ {
			if before, after := original.Int63(), restored.Int63(); before != after {
				t.Fatalf("after %d draws, draw %d: %d and %d", drawn, draw, before, after)
			}
		}
	}
	for name, damaged := range map[string]guestRandomState{
		"a short history":            {History: make([]byte, 8)},
		"a place outside the ring":   {History: make([]byte, laggedLength*8), At: laggedLength},
		"more first values than 607": {History: make([]byte, laggedLength*8), Primed: laggedLength + 1},
		"first values out of place":  {History: make([]byte, laggedLength*8), At: 3, Primed: 5},
	} {
		if _, err := restoreGuestRandom(damaged); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

// An arena is restored to hand out the same addresses next, and a record that
// does not describe one — a block counted twice, a gap nothing owns — is not
// an arena.
func TestArenaStateRestoresTheNextAllocation(t *testing.T) {
	source := newArena(0x1000, 0x1000)
	var blocks []uint32
	for _, size := range []uint64{24, 8, 40, 16, 64} {
		address, ok := source.allocate(size)
		if !ok {
			t.Fatal("the fixture arena is full")
		}
		blocks = append(blocks, address)
	}
	source.release(blocks[1])
	source.release(blocks[3])
	saved := source.captureState()
	restored, err := restoreArena(saved, 0x1000, 0x1000)
	if err != nil {
		t.Fatal(err)
	}
	if restored.used() != source.used() {
		t.Fatalf("outstanding is %d, want %d", restored.used(), source.used())
	}
	for _, size := range []uint64{8, 16, 16, 200} {
		before, _ := source.allocate(size)
		after, _ := restored.allocate(size)
		if before != after {
			t.Fatalf("a %d byte block is at %#x and %#x", size, before, after)
		}
	}
	for name, mutate := range map[string]func(*arenaState){
		"a gap":              func(saved *arenaState) { saved.Free = saved.Free[1:] },
		"a block twice":      func(saved *arenaState) { saved.Blocks = append(saved.Blocks, saved.Blocks[:8]...) },
		"a cursor too far":   func(saved *arenaState) { saved.Cursor += 8 },
		"a cursor too short": func(saved *arenaState) { saved.Cursor = 0x0ff8 },
		"a block of nothing": func(saved *arenaState) { saved.Blocks[4], saved.Blocks[5] = 0, 0 },
		"a torn table":       func(saved *arenaState) { saved.Blocks = saved.Blocks[:len(saved.Blocks)-3] },
	} {
		damaged := saved
		damaged.Free, damaged.Blocks = append([]arenaSpanState(nil), saved.Free...), append([]byte(nil), saved.Blocks...)
		mutate(&damaged)
		if _, err := restoreArena(damaged, 0x1000, 0x1000); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

// Every field the runtime holds is one a checkpoint either records, rebuilds
// from something it records, reads again from the save store, or leaves to the
// Host on purpose. A field that is added to one of these structures without a
// line here fails this test, which is the only thing that makes "the
// checkpoint covers the runtime" a claim that stays true: a table nobody
// copied restores as empty and nothing else notices.
func TestCheckpointAccountsForEveryRuntimeField(t *testing.T) {
	const (
		recorded = "recorded"
		// rebuilt: derived again from the archive, the module or another record.
		rebuilt = "rebuilt"
		// store: a copy of what a save holds. It is never recorded: committing
		// a load reads it again from the store the session runs over. A field
		// that is a host copy of a save and is marked anything else is how a
		// load comes to put an older save over a newer one.
		store = "store"
		// host: the Host's own — a logger, a device, a store, a diagnostic knob.
		host = "host"
		// boundary: a value that is fixed wherever a checkpoint can be taken,
		// which capture checks rather than records.
		boundary = "boundary"
		// diagnostic: counted for a report and started again at zero.
		diagnostic = "diagnostic"
	)
	policy := map[string]map[string]string{
		"Client": {
			"wideGraphicsContexts": rebuilt, "wideExclusiveClip": rebuilt, "core": recorded, "thread": recorded,
			"archive": rebuilt, "module": rebuilt, "logger": host, "saveStore": recorded, "saveReadError": boundary,
			"subscriberNumber": recorded, "notificationNetwork": recorded, "mu": rebuilt,
			"arena": recorded, "surfaces": recorded, "heap": recorded, "codeCurse": recorded, "stubs": recorded,
			"clet": recorded, "exited": boundary, "exitedFrom": boundary,
			"screen": recorded, "framePending": rebuilt, "frameRGBA": rebuilt, "flushes": recorded, "presented": recorded,
			"framebuffers": recorded, "timers": recorded, "nextHandle": recorded,
			"pixelOps": recorded, "installedPixelOps": recorded, "uninstalledPixelOps": diagnostic,
			"netConnects": recorded, "netGeneration": recorded, "clock": recorded, "events": recorded,
			"uncaughtCallbacks": diagnostic, "uncaughtFirst": diagnostic,
			"audio": recorded, "clips": recorded, "volume": recorded, "sourceVolume": recorded, "sourceMuted": recorded,
			"vibrator": recorded, "files": recorded, "removed": store, "created": store,
			// The two lists are stored before a checkpoint is taken, and one
			// the store will not take refuses it.
			"removedUnsaved": boundary, "createdUnsaved": boundary,
			// Every restored buffer is level with the store, so the counts
			// start again from nothing.
			"fileEpochs": rebuilt,
			// What a load has still to fill exists only between its restore
			// and its commit.
			"restoredStorage": boundary,
			"traceLive":       host, "traceOut": host, "tmStorage": recorded, "cRandom": recorded, "strtokScan": recorded,
			"inputMode": recorded, "inputModeTableAddress": recorded, "cTextInput": recorded,
			"javaApplication": recorded, "javaClasses": diagnostic, "javaLink": recorded, "javaRun": recorded,
			"javaMediaEvents": recorded,
			"javaTry":         boundary, "javaTryBuffers": recorded, "javaCallDepth": boundary,
			"activeJavaWorker": boundary, "javaThreadsStopped": boundary, "collecting": boundary,
			"collectNanos": diagnostic, "collectorOff": host, "applicationIDAddress": recorded,
			"resourceIDs": recorded, "resourceNames": recorded, "trace": host, "imports": recorded,
		},
		"javaRuntime": {
			"byHandle": recorded, "byObject": recorded, "byName": recorded, "strings": recorded, "singletons": recorded,
			"random": recorded, "streams": recorded, "images": recorded, "files": recorded, "threads": recorded,
			"monitors": recorded, "mainThread": recorded, "keyCallback": boundary, "keyChecks": recorded,
			"vectors": recorded, "calendars": recorded, "sinks": recorded, "wrapped": recorded,
			"sinkFiles": recorded, "streamFiles": recorded, "decodedImages": recorded, "widgets": recorded,
			"focusedWidget": recorded, "widgetGeneration": recorded, "dates": recorded, "databases": recorded,
			"workers": recorded, "threadStacks": recorded, "graphics": recorded, "card": recorded, "cardDirty": recorded,
			"screenGraphics": recorded, "named": diagnostic, "serial": recorded, "jlet": recorded, "objects": recorded,
			"pins": boundary, "collectAt": recorded, "condemned": recorded, "sinceCollection": recorded,
			"collected": recorded, "collections": recorded,
		},
		"javaWorker": {
			"armThread": recorded, "stackBase": recorded, "grant": rebuilt, "events": rebuilt, "wakeAt": recorded,
			"done": recorded, "yields": recorded, "monitors": recorded, "renewals": recorded, "tryFrames": recorded,
			"tryBuffers": recorded, "callDepth": recorded, "waitSite": recorded, "waits": recorded,
			"waitReported": recorded, "waiting": recorded, "restored": recorded,
		},
		"javaThread":  {"object": recorded, "runnable": recorded, "worker": recorded, "priority": recorded},
		"javaMonitor": {"owner": recorded, "count": recorded, "platform": recorded},
		"javaRuntimeClass": {
			"Name": recorded, "Handle": recorded, "Record": recorded, "Object": recorded, "VTable": recorded,
			"Slots": recorded, "Instance": recorded, "Super": recorded, "ElementBytes": recorded,
			"StaticWords": recorded, "Measured": recorded, "initialized": recorded, "dataBlock": recorded,
		},
		"javaTryFrame": {"Buffer": recorded, "Depth": recorded, "Saved": recorded, "Armed": recorded},
		// Data is the title's own bytes or a resource's for most streams, and
		// for a stream opened on a file it is a window on that file, which is
		// the store's: File and Offset say which and where.
		"javaStream": {
			"Name": recorded, "Data": store, "Read": recorded, "Closed": recorded, "Source": recorded,
			"Markable": recorded, "Mark": recorded, "File": recorded, "Offset": recorded,
		},
		"javaWidget": {
			"text": recorded, "maxLength": recorded, "revision": recorded, "kind": recorded, "mode": recorded,
			"listener": recorded, "inputHandler": recorded, "children": recorded, "parent": recorded,
			"shown": recorded, "visibilityRevision": recorded, "focused": recorded,
		},
		// A database travels as its name. A store the store refused is
		// retried before a checkpoint is taken, and the rest of its
		// bookkeeping starts again with the records a load reads.
		"javaDatabase": {
			"name": recorded, "recordSize": recorded, "records": store, "deleted": store,
			"modified": recorded, "closed": recorded, "unsaved": boundary, "synced": rebuilt,
			"rebuilt": rebuilt, "changed": rebuilt,
		},
		"javaGraphics": {
			"surface": recorded, "color": recorded, "translateX": recorded, "translateY": recorded,
			"clipX": recorded, "clipY": recorded, "clipWidth": recorded, "clipHeight": recorded, "clipSet": recorded,
			"alpha": recorded, "packed": recorded, "xor": recorded,
		},
		"javaCalendar":     {"millis": recorded, "pending": recorded},
		"javaObjectRecord": {"block": recorded, "blockSize": recorded, "condemned": recorded},
		"javaLink":         {"surface": recorded, "layout": recorded},
		"javaLayoutClass": {
			"Name": recorded, "Super": recorded, "InstanceSize": recorded, "VTableSize": recorded,
			"Virtual": recorded, "Fields": recorded, "InstanceWords": recorded, "Statics": recorded,
			"Application": recorded, "StaticWords": recorded, "Measured": recorded,
		},
		"framebuffer": {
			"handle": recorded, "width": recorded, "height": recorded, "address": recorded, "pixels": recorded,
			"screen": recorded, "opaque": recorded, "colorKeyed": recorded, "transparentKey": recorded,
			"drawnHere": recorded,
		},
		"timer": {"structure": recorded, "callback": recorded, "param": recorded, "dueAt": recorded, "armed": recorded},
		// A buffer with unstored writes is stored before a checkpoint is taken.
		"openFile": {
			"name": recorded, "data": store, "cursor": recorded, "writable": recorded, "dirty": boundary,
			"truncated": recorded, "synced": rebuilt,
		},
		"mediaClip": {
			"callback": recorded, "status": recorded, "pending": recorded, "mediaType": recorded, "data": recorded,
			"volume": recorded, "handle": recorded, "loaded": recorded, "javaPaused": recorded,
			"javaRepeat": recorded, "listener": recorded, "java": recorded, "completed": recorded,
		},
		"javaMediaEvent": {"clip": recorded, "listener": recorded, "code": recorded},
		"pendingNetConnect": {
			"offline": recorded, "generation": recorded, "callback": recorded, "param": recorded, "dueAt": recorded,
		},
		"pendingEvent":    {"kind": recorded, "param1": recorded, "param2": recorded},
		"cTextInputState": {"active": recorded, "revision": recorded, "calls": recorded, "pending": recorded},
		"pixelOpCache":    {"results": recorded},
		"guestClock":      {"mu": rebuilt, "elapsed": recorded, "origin": rebuilt, "steps": rebuilt, "baseline": recorded},
		"arena": {
			"base": rebuilt, "limit": rebuilt, "cursor": recorded, "free": recorded, "sizes": recorded,
			"outstanding": rebuilt,
		},
		"notificationNetwork": {
			"contract": rebuilt, "identity": rebuilt, "authenticationRequest": rebuilt, "certificateRequests": rebuilt,
			"active": recorded, "next": recorded, "serial": recorded, "sockets": recorded,
		},
		"notificationSocketState": {
			"connected": recorded, "failed": recorded, "stage": recorded, "request": recorded, "response": recorded,
			"pendingResponse": recorded, "connect": recorded, "read": recorded, "write": recorded,
		},
		// What an adapter read from its store it reads again. The volatile
		// copies exist only in a session with no store, which is not captured.
		"authenticationOptionStore": {
			"mu": rebuilt, "base": host, "archive": rebuilt, "original": store, "volatile": boundary,
			"readError": boundary,
		},
		// The lists are the store's with the certificate listed or not as the
		// title left it, and those two answers are what is recorded.
		"authenticationCertificate58Store": {
			"mu": rebuilt, "base": host, "certificate": recorded, "ledgers": store, "originalMembership": store,
		},
		// The flag and the certificate the adapter found in its file are the
		// one copy of save content a record carries: they are read from the
		// file again, and the record's are used only when the file is gone or
		// has no header the adapter reads. See checkpoint_adapters.go.
		"authenticationCertificate100Store": {
			"mu": rebuilt, "base": host, "volatile": boundary, "contract": rebuilt, "key": rebuilt, "active": recorded,
			"publishHeader": rebuilt, "originalFlag": store, "originalCertificate": store, "certificate": recorded,
		},
		"Session": {
			"authentication": recorded, "client": recorded, "archive": rebuilt, "tick": recorded, "speed": recorded,
			"nextDue": rebuilt, "cheat": boundary, "cheatConsole": host, "pad": recorded, "options": host,
			"archiveIdentity": rebuilt,
		},
	}
	for _, value := range []any{
		Client{}, javaRuntime{}, javaWorker{}, javaThread{}, javaMonitor{}, javaRuntimeClass{}, javaTryFrame{},
		javaStream{}, javaWidget{}, javaDatabase{}, javaGraphics{}, javaCalendar{}, javaObjectRecord{}, javaLink{},
		javaLayoutClass{}, framebuffer{}, timer{}, openFile{}, mediaClip{}, javaMediaEvent{}, pendingNetConnect{}, pendingEvent{},
		cTextInputState{}, pixelOpCache{}, guestClock{}, arena{}, notificationNetwork{}, notificationSocketState{},
		authenticationOptionStore{}, authenticationCertificate58Store{}, authenticationCertificate100Store{},
		Session{},
	} {
		structure := reflect.TypeOf(value)
		fields, listed := policy[structure.Name()]
		if !listed {
			t.Errorf("%s has no checkpoint policy", structure.Name())
			continue
		}
		seen := map[string]bool{}
		for index := 0; index < structure.NumField(); index++ {
			name := structure.Field(index).Name
			seen[name] = true
			if fields[name] == "" {
				t.Errorf("%s.%s is not accounted for by the checkpoint", structure.Name(), name)
			}
		}
		for name := range fields {
			if !seen[name] {
				t.Errorf("the checkpoint policy names %s.%s, which the runtime no longer has", structure.Name(), name)
			}
		}
		delete(policy, structure.Name())
	}
	for name := range policy {
		t.Errorf("the checkpoint policy names %s, which is not checked", name)
	}
}
