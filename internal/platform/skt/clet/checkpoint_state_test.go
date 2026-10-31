package clet

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
	"github.com/movingwoo/wfeature/internal/testfixture"
)

// fixtureProgram lays the authored SKT fixture out the way the session's
// loader does: the code at the image base and the data at the link distance.
func fixtureProgram(t *testing.T, options testfixture.SKTCletOptions) CletProgram {
	t.Helper()
	if testfixture.SKTCletImageBase != ImageBase {
		t.Fatalf("fixture image base %#x differs from the engine's %#x", testfixture.SKTCletImageBase, ImageBase)
	}
	parts, err := testfixture.SKTCletParts(options)
	if err != nil {
		t.Fatal(err)
	}
	data := ImageBase + 0x01001000
	entry := func(offset uint32) uint32 {
		if offset == 0 {
			return 0
		}
		return ImageBase + offset
	}
	return CletProgram{Name: testfixture.SKTCletID + ".daf", ProgramName: testfixture.SKTCletID, Identity: [32]byte{1, 2, 3},
		SaveOwner: testfixture.SKTCletID, Code: parts.Code, Data: parts.Data, DataAddress: data, BSSSize: parts.BSS, RootSlot: data,
		Entries: CletEntries{Start: entry(parts.Entries[0]), Pause: entry(parts.Entries[1]), Resume: entry(parts.Entries[2]),
			Destroy: entry(parts.Entries[3]), Paint: entry(parts.Entries[4]), Event: entry(parts.Entries[5])}}
}

func fixtureSession(t *testing.T, program CletProgram, store backend.SaveStore) *Session {
	t.Helper()
	session, err := StartCletSession(t.Context(), program, SessionOptions{SaveStore: store, Clock: wipicrt.NewManualClock(time.Time{})})
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func fixtureTicks(t *testing.T, session *Session, count int) {
	t.Helper()
	for range count {
		if _, err := session.Tick(t.Context()); err != nil {
			t.Fatal(err)
		}
		session.SkipToNextDeadline()
	}
}

// Every field of the client and the runtime is either in the record or says
// why it is not. A field added without a line here fails, which is the point:
// a field a checkpoint forgets is a load that quietly runs on a default.
func TestCheckpointAccountsForEveryField(t *testing.T) {
	const (
		recorded = "recorded"
		// rebuilt: derived again from the archive, the program or another
		// recorded field.
		rebuilt = "rebuilt"
		// store: a copy of what a save holds, read again from the store the
		// load commits over. Recording one is how a load puts an older save
		// over a newer one.
		store = "store"
		// host: the Host's own — a lock, a logger, a sink, a clock, scratch.
		host = "host"
		// boundary: fixed wherever a checkpoint can be taken, which capture
		// checks rather than records.
		boundary = "boundary"
		// reset: started again in its initial state, which a load is allowed
		// to answer differently from the session it replaces.
		reset = "reset"
		// diagnostic: counted for a report and started again at zero.
		diagnostic = "diagnostic"
	)
	policy := map[reflect.Type]map[string]string{
		reflect.TypeFor[Client](): {
			"Client": recorded, "authentication": recorded, "image": rebuilt, "clet": recorded, "initializationStarted": boundary,
			"workersStopped": boundary, "runtime": recorded, "prepared": boundary,
		},
		reflect.TypeFor[wipicrt.Client](): {
			"Platform": rebuilt, "Hooks": rebuilt, "Runtime": recorded, "Prepared": boundary, "SubscriberNumber": recorded, "core": recorded, "Thread": recorded,
			"Run": host, "Resources": rebuilt, "Files": rebuilt, "SaveStore": store, "ProgramName": rebuilt,
			"FrameRGBA": recorded, "FrameWidth": recorded, "FrameHeight": recorded, "FlushCount": recorded,
			"FramePending": recorded, "PartialFrame": recorded, "FrameSink": host, "FrameSampleAt": host,
			"FrameSampleClock": host, "LastSample": host, "ScreenWidth": recorded, "ScreenHeight": recorded,
			"ThreadSliceSteps": boundary, "Costs": diagnostic, "timePhases": host, "NextRoundPaint": recorded,
			"PaintsDrop": diagnostic, "PaintLoad": recorded, "PaintCost": recorded, "EntryCost": recorded,
			"WorkBaseline": recorded, "ServingDue": boundary, "ActiveTimer": boundary, "ServiceSteps": boundary,
			"ServiceWait": boundary, "ServiceRemaining": boundary, "ServiceDepth": boundary, "serviceStartedAt": boundary,
			"serviceWindowAt": boundary, "serviceWindowReads": boundary, "serviceStepMark": boundary, "GuestClockReads": reset,
			"Clock": host, "SpeedMultiplier": recorded, "ClientWakeAt": recorded, "TraceLimit": host, "Logger": host,
			"Audio": recorded, "Vibrator": recorded, "imageSize": rebuilt,
		},
		reflect.TypeFor[initializationRuntime](): {
			"Runtime": recorded, "client": rebuilt,
		},
		reflect.TypeFor[wipicrt.Runtime](): {
			"Hooks": rebuilt, "Client": rebuilt, "noteScratch": host, "Arena": recorded, "arenaShadow": diagnostic, "shadowWindow": diagnostic,
			"ShadowedBlocks": diagnostic, "CheckedBlocks": diagnostic, "hookWindow": host, "CodeCursor": recorded,
			"Stubs": recorded, "StubKeys": rebuilt, "CurrentThread": boundary, "CurrentContext": boundary,
			"VirtualBaseMillis": recorded, "ClockBase": recorded, "Trace": diagnostic, "CallCounts": diagnostic,
			"SaveReadError": boundary, "WIPICAllocations": recorded, "ImageSourceBuffers": recorded, "ReleasedWIPIC": recorded,
			"ReleasedWIPICOrder": recorded, "ScreenFramebuffer": recorded, "FramebufferOpacity": recorded,
			"PixelOps": recorded, "PendingTimers": recorded, "RoundTimers": boundary, "CFiles": recorded,
			"CFileHandles": recorded, "NextCDatabaseHandle": recorded, "GuestFiles": boundary, "GuestFileWrites": boundary,
			"RemovedFiles": store, "MadeDirectories": store, "RemovedCDatabases": store, "WIPICClips": recorded,
			"WIPICClipOrder": recorded, "WIPICMutedSources": recorded, "UserMemoryInterface": recorded, "UserMemoryPools": recorded,
			"InputModeTableAddress": recorded, "CInput": recorded, "Events": recorded, "GuestEventLoop": recorded,
			"Leds": recorded, "AOTCallDepth": boundary, "DetachedCFiles": rebuilt, "PendingNetCallbacks": recorded,
			"RepaintPending": recorded, "RepaintServicing": boundary, "GuestFlushedOwnFrame": recorded, "CollectAt": rebuilt,
		},
		reflect.TypeFor[cletState](): {
			"program": rebuilt, "root": recorded, "destroyStarted": boundary, "repaint": recorded,
			"timeRecord": recorded, "tokenNext": recorded,
		},
		reflect.TypeFor[wipicrt.CInputState](): {
			"Owner": recorded, "Active": recorded, "Mode": recorded, "Revision": recorded, "Calls": recorded,
			"Activations": recorded, "Pending": recorded, "DiscardCarrier": recorded, "ClearPending": recorded,
			"TimerPointer": recorded, "TimerCallback": recorded,
			"PendingValid": boundary, "Delivering": boundary,
			// cInputUsed reads the use off the calls, the activations and the
			// mode table, which are recorded.
			"Used": rebuilt, "Unreached": reset,
		},
		reflect.TypeFor[wipicrt.GuestArena](): {
			"Base": recorded, "Limit": recorded, "Cursor": recorded, "Free": recorded, "Freed": recorded, "HighWater": recorded,
			"RecordReleased": diagnostic, "CheckReused": diagnostic,
		},
	}
	for kind, fields := range policy {
		seen := make(map[string]bool)
		for index := range kind.NumField() {
			name := kind.Field(index).Name
			seen[name] = true
			if _, ok := fields[name]; !ok {
				t.Errorf("%s.%s has no checkpoint policy", kind.Name(), name)
			}
		}
		for name := range fields {
			if !seen[name] {
				t.Errorf("%s.%s is in the policy but not in the type", kind.Name(), name)
			}
		}
	}
}

func fixtureCheckpoint(t *testing.T) (CletProgram, backend.Checkpoint, *backend.MemorySaveStore) {
	t.Helper()
	program := fixtureProgram(t, testfixture.SKTCletOptions{FieldAtStart: true})
	store, _ := backend.NewMemorySaveStore(nil)
	session := fixtureSession(t, program, store)
	defer session.Close()
	fixtureTicks(t, session, 3)
	for _, key := range []int32{'6', '7', '1'} {
		if err := session.SendKey(t.Context(), wipicrt.KeyPressed, key); err != nil {
			t.Fatal(err)
		}
	}
	fixtureTicks(t, session, 3)
	checkpoint, err := session.CaptureCheckpoint(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return program, checkpoint, store
}

// A record that names the wrong program, settings or layout is refused before
// anything of it is used.
func TestCheckpointRefusesAnotherProgramOrSetting(t *testing.T) {
	program, checkpoint, _ := fixtureCheckpoint(t)
	options := SessionOptions{Clock: wipicrt.NewManualClock(time.Time{})}
	other := program
	other.Identity = [32]byte{9}
	if _, err := PrepareCheckpoint(other, checkpoint, options); !errors.Is(err, backend.ErrCheckpointIdentity) {
		t.Fatalf("another archive's checkpoint: %v", err)
	}
	changed := program
	changed.Code = append([]byte(nil), program.Code...)
	changed.Code[len(changed.Code)-1] ^= 1
	if _, err := PrepareCheckpoint(changed, checkpoint, options); !errors.Is(err, backend.ErrCheckpointIdentity) {
		t.Fatalf("another program's checkpoint: %v", err)
	}
	for name, mutate := range map[string]func(*SessionOptions){
		"timer limit":    func(options *SessionOptions) { options.TimerLimit = 9 },
		"authentication": func(options *SessionOptions) { options.DisableAuthentication = true },
		"service policy": func(options *SessionOptions) { options.ServiceSteps = 7 },
	} {
		changed := options
		mutate(&changed)
		if _, err := PrepareCheckpoint(program, checkpoint, changed); !errors.Is(err, backend.ErrCheckpointVersion) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	wrong := checkpoint
	wrong.Variant = backend.CheckpointKTFModule
	if _, err := PrepareCheckpoint(program, wrong, options); !errors.Is(err, backend.ErrCheckpointVersion) {
		t.Fatalf("another variant: %v", err)
	}
}

// Each table of the record is checked against the others and against the
// restored memory; a record that disagrees with itself is refused.
func TestCheckpointRefusesAnInconsistentRecord(t *testing.T) {
	program, checkpoint, _ := fixtureCheckpoint(t)
	var saved checkpointState
	if err := backend.DecodeCheckpointRecord(checkpoint.Runtime, &saved); err != nil {
		t.Fatal(err)
	}
	if len(saved.Runtime.WIPICAllocations) == 0 || len(saved.Runtime.Stubs) == 0 || len(saved.Runtime.Timers) == 0 || len(saved.Storage.FileHandles) == 0 {
		t.Fatalf("the fixture's record is missing a table: %+v", saved.Storage)
	}
	cases := map[string]func(*checkpointState){
		"version": func(state *checkpointState) { state.Version++ },
		"mappings": func(state *checkpointState) {
			state.Client.Core.Memory.Mappings[0].Permission = armcore.PermissionReadWrite
		},
		"stack pointer": func(state *checkpointState) { state.Client.Thread.Context.Registers[armcore.RegisterSP] = 4 },
		"root":          func(state *checkpointState) { state.Client.Root = 4 },
		"frame shape":   func(state *checkpointState) { state.Client.FrameWidth++ },
		"speed":         func(state *checkpointState) { state.Client.Speed = 99 },
		"deadline":      func(state *checkpointState) { state.Client.ClientWake = deadlineState{Remaining: 5} },
		"audio":         func(state *checkpointState) { state.Client.Audio = nil },
		"stub order":    func(state *checkpointState) { state.Runtime.Stubs[1].Key = state.Runtime.Stubs[0].Key },
		"stub address":  func(state *checkpointState) { state.Runtime.Stubs[0].Address = wipicrt.PlatformDataBase | 1 },
		"stub cursor":   func(state *checkpointState) { state.Runtime.CodeCursor = uint64(wipicrt.PlatformCodeBase) },
		"block over free": func(state *checkpointState) {
			state.Runtime.WIPICAllocations[0].Address = uint32(state.Runtime.Arena.Cursor)
		},
		"arena free bytes": func(state *checkpointState) { state.Runtime.Arena.Freed++ },
		"released live": func(state *checkpointState) {
			state.Runtime.ReleasedWIPIC = []uint32{state.Runtime.WIPICAllocations[0].Address}
		},
		"clock anchor":      func(state *checkpointState) { state.Runtime.ClockAge = 2 * maxDeadlineOffset },
		"timer callback":    func(state *checkpointState) { state.Runtime.Timers[0].Callback = wipicrt.PlatformDataBase | 1 },
		"timer deadline":    func(state *checkpointState) { state.Runtime.Timers[0].Due = deadlineState{} },
		"input mode":        func(state *checkpointState) { state.Runtime.CInput.Mode = 9 },
		"network callback":  func(state *checkpointState) { state.Runtime.Network = []netCallbackState{{}} },
		"clip without data": func(state *checkpointState) { state.Runtime.Clips = []clipRecord{{Address: 8}} },
		"opacity unheld": func(state *checkpointState) {
			state.Runtime.Opacities = append(state.Runtime.Opacities, opacityState{Width: 1, Height: 1, Opaque: []byte{1}})
		},
		"file handle store": func(state *checkpointState) { state.Storage.FileHandles[0].Store = 99 },
		"file handle tag":   func(state *checkpointState) { state.Storage.FileHandles[0].Handle = 7 },
		"file name":         func(state *checkpointState) { state.Storage.Files[0].Name = []byte("../x") },
		"file held by none": func(state *checkpointState) {
			state.Storage.Files = append(state.Storage.Files, fileSnapshot{Name: []byte("orphan")})
		},
	}
	// The same record re-encoded unchanged loads, so every refusal below is
	// the mutation's.
	cases["unchanged"] = func(*checkpointState) {}
	for name, mutate := range cases {
		var damaged checkpointState
		if err := backend.DecodeCheckpointRecord(checkpoint.Runtime, &damaged); err != nil {
			t.Fatal(err)
		}
		mutate(&damaged)
		record, err := backend.EncodeCheckpointRecord(damaged)
		if err != nil {
			t.Fatal(err)
		}
		changed := checkpoint
		changed.Runtime = record
		prepared, err := PrepareCheckpoint(program, changed, SessionOptions{Clock: wipicrt.NewManualClock(time.Time{})})
		if err == nil {
			prepared.Discard()
		}
		if (err == nil) != (name == "unchanged") {
			t.Errorf("%s: prepared with %v", name, err)
		}
	}
}

// Capture is taken only between whole rounds: a session caught inside a call
// is refused and keeps running.
func TestCheckpointCaptureRefusesABusySession(t *testing.T) {
	program := fixtureProgram(t, testfixture.SKTCletOptions{})
	store, _ := backend.NewMemorySaveStore(nil)
	session := fixtureSession(t, program, store)
	defer session.Close()
	fixtureTicks(t, session, 2)
	client := session.Client
	for name, busy := range map[string]func() func(){
		"service": func() func() { client.ServiceDepth = 1; return func() { client.ServiceDepth = 0 } },
		"timer":   func() func() { client.ActiveTimer = &wipicrt.WIPICTimer{}; return func() { client.ActiveTimer = nil } },
		"text": func() func() {
			client.runtime.CInput.Delivering = true
			return func() { client.runtime.CInput.Delivering = false }
		},
		"destroyed": func() func() { client.clet.destroyStarted = true; return func() { client.clet.destroyStarted = false } },
		"save read": func() func() {
			client.runtime.SaveReadError = errors.New("unreadable")
			return func() { client.runtime.SaveReadError = nil }
		},
	} {
		restore := busy()
		if _, err := session.CaptureCheckpoint(t.Context()); err == nil {
			t.Errorf("%s: a busy session was captured", name)
		}
		restore()
	}
	client.Run.Lock()
	_, err := session.CaptureCheckpoint(t.Context())
	client.Run.Unlock()
	if !errors.Is(err, ErrCheckpointBusy) {
		t.Fatalf("a session whose run lock is held: %v", err)
	}
	if _, err := session.CaptureCheckpoint(t.Context()); err != nil {
		t.Fatalf("the idle session after the refusals: %v", err)
	}
	fixtureTicks(t, session, 2)
}

// A load reads the saves only when it commits: validation reaches no store,
// and a store that cannot be read refuses the load with the saves untouched.
func TestCheckpointLoadReadsTheStoreOnlyAtCommit(t *testing.T) {
	program, checkpoint, store := fixtureCheckpoint(t)
	prepared, err := PrepareCheckpoint(program, checkpoint, SessionOptions{Clock: wipicrt.NewManualClock(time.Time{}), SaveStore: store})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Discard()
	if calls, first := prepared.PreparationStoreCalls(); calls != 0 {
		t.Fatalf("validation asked the store %d times, first %s", calls, first)
	}
	if _, err := prepared.Commit(t.Context(), nil, failingStore{}); !errors.Is(err, backend.ErrCheckpointSaveRead) {
		t.Fatalf("an unreadable store: %v", err)
	}
	restored, err := prepared.Commit(t.Context(), nil, store)
	if err != nil {
		t.Fatalf("a commit after a refused one: %v", err)
	}
	defer restored.Close()
	fixtureTicks(t, restored, 3)
	if !strings.Contains(strings.Join(keysOf(t, store), ","), "db/log.dat") {
		t.Fatal("the fixture's log is not in the store")
	}
}

type failingStore struct{}

func (failingStore) LoadSave(string) ([]byte, bool) { return nil, false }
func (failingStore) ReadSave(string) ([]byte, bool, error) {
	return nil, false, errors.New("the store cannot be read")
}
func (failingStore) StoreSave(string, []byte) error { return errors.New("the store cannot be written") }

func keysOf(t *testing.T, store *backend.MemorySaveStore) []string {
	t.Helper()
	entries, err := store.SnapshotSaves()
	if err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(entries))
	for _, entry := range entries {
		keys = append(keys, entry.Key)
	}
	return keys
}

// A removal or a directory an earlier session stored is what a new session
// starts from. The Clet engine used to build its runtime with the removal and
// directory lists already made, empty, so they were never read from the store:
// a packaged file the program had deleted came back the next time it ran.
func TestASessionReadsTheRemovalsAnEarlierOneStored(t *testing.T) {
	program := fixtureProgram(t, testfixture.SKTCletOptions{})
	program.Files = map[string][]byte{"save.dat": []byte("packaged")}
	store, err := backend.NewMemorySaveStore([]backend.SaveEntry{
		{Key: "db/.removed", Data: []byte("save.dat\n")},
		{Key: "db/.dirs", Data: []byte("made\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	session := fixtureSession(t, program, store)
	defer session.Close()
	runtime := session.Client.runtime
	if !runtime.RemovedDatabases()["save.dat"] {
		t.Error("a file removed in an earlier session is not in the removal list")
	}
	if !runtime.CreatedDirectories()["made"] {
		t.Error("a directory made in an earlier session is not in the directory list")
	}
}

// The engine before the shared one wrote the removal list and never read it
// back, so a program that deleted its save in one session and wrote it again in
// a later one left the name listed over the bytes, and that engine answered the
// save from them. A session now answers the same save, and the next write of
// the list leaves the name off. A name over the empty file a removal stores is
// still removed.
func TestASessionKeepsASaveTheEarlierEngineLeftListed(t *testing.T) {
	program := fixtureProgram(t, testfixture.SKTCletOptions{})
	store, err := backend.NewMemorySaveStore([]backend.SaveEntry{
		{Key: "db/.removed", Data: []byte("save.dat\ngone.dat\n")},
		{Key: "db/save.dat", Data: []byte("progress")},
		{Key: "db/gone.dat", Data: []byte{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	session := fixtureSession(t, program, store)
	defer session.Close()
	runtime := session.Client.runtime
	call := func(function uint32, registers ...uint32) uint32 {
		t.Helper()
		thread := armcore.NewThread(armcore.NewContext())
		for index, value := range registers {
			if err := thread.SetRegister(index, value); err != nil {
				t.Fatal(err)
			}
		}
		result, err := runtime.HandleWIPICCall(thread, wipicrt.WIPICTableFilesystem<<16|function)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	name := func(text string) uint32 {
		t.Helper()
		address, err := runtime.AllocateBytes([]byte(text + "\x00"))
		if err != nil {
			t.Fatal(err)
		}
		return address
	}
	save, gone := name("save.dat"), name("gone.dat")
	if result := call(wipicrt.WIPICFileExists, save); result != 0 {
		t.Errorf("MC_fsIsExist of the save = %#x, want 0", result)
	}
	if handle := call(wipicrt.WIPICFileOpen, save, 1); handle == wipicrt.WIPICErrorNotFound {
		t.Error("the save does not open for reading")
	}
	if seed, found := runtime.DatabaseSeed("save.dat"); !found || string(seed) != "progress" {
		t.Errorf("the save reads %q, %v; want the stored progress", seed, found)
	}
	if result := call(wipicrt.WIPICFileExists, gone); result != wipicrt.WIPICErrorNotFound {
		t.Errorf("MC_fsIsExist of the removed file = %#x, want M_E_NOENT", result)
	}
	if result := call(wipicrt.WIPICFileDelete, gone); result != wipicrt.WIPICErrorNotFound {
		t.Errorf("MC_fsRemove of the removed file = %#x, want M_E_NOENT", result)
	}
	if err := runtime.MarkDatabaseRemoved("other.dat", true); err != nil {
		t.Fatal(err)
	}
	listed, _ := store.LoadSave("db/.removed")
	if got := strings.Join(wipicrt.SplitRemovalList(listed), ","); got != "gone.dat,other.dat" {
		t.Errorf("the list stored next is %q, want gone.dat,other.dat", got)
	}
	if saved, _ := store.LoadSave("db/save.dat"); string(saved) != "progress" {
		t.Errorf("db/save.dat = %q, want the progress kept", saved)
	}
}
