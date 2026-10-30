package clet

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
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
	session, err := StartCletSession(t.Context(), program, SessionOptions{SaveStore: store, Clock: NewManualClock(time.Time{})})
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
			"subscriberNumber": recorded, "authentication": recorded, "core": recorded, "thread": recorded,
			"image": rebuilt, "mapped": rebuilt, "entry": rebuilt, "clet": recorded,
			"run": host, "initializationStarted": boundary, "workersStopped": boundary, "runtime": recorded, "prepared": boundary,
			"resources": rebuilt, "files": rebuilt, "saveStore": store, "programName": rebuilt,
			"frame": recorded, "frameWidth": recorded, "frameHeight": recorded, "flushCount": recorded,
			"framePending": recorded, "partialFrame": recorded, "frameSink": host,
			"frameSampleAt": host, "frameSampleClock": host, "lastSample": host,
			"screenWidth": recorded, "screenHeight": recorded, "threadSliceSteps": boundary,
			"costs": diagnostic, "timePhases": host, "nextRoundPaint": recorded,
			"paintsDrop": diagnostic, "paintLoad": recorded, "paintCost": recorded, "entryCost": recorded,
			"workBaseline": recorded, "servingDue": boundary, "activeTimer": boundary,
			"serviceSteps": boundary, "serviceWait": boundary,
			// A service call sets these when it begins and reads them only
			// against each other until it ends.
			"serviceRemaining": boundary, "serviceDepth": boundary, "serviceStartedAt": boundary,
			"serviceWindowAt": boundary, "serviceWindowReads": boundary, "serviceStepMark": boundary,
			"guestClockReads": reset,
			"clock":           host, "speed": recorded, "clientWakeAt": recorded, "traceLimit": host, "logger": host,
			"audio": recorded, "vibrator": recorded,
		},
		reflect.TypeFor[initializationRuntime](): {
			"client": rebuilt, "arena": recorded,
			"arenaShadow": diagnostic, "shadowWindow": diagnostic, "shadowedBlocks": diagnostic, "checkedBlocks": diagnostic,
			"hookWindow": host, "codeCursor": recorded, "stubs": recorded, "stubKeys": rebuilt,
			"currentThread": boundary, "currentContext": boundary,
			"virtualBaseMillis": recorded, "clockBase": recorded, "trace": diagnostic, "callCounts": diagnostic,
			"saveReadError":    boundary,
			"wipicAllocations": recorded, "imageSourceBuffers": recorded, "releasedWIPIC": recorded, "releasedWIPICOrder": recorded,
			"screenFramebuffer": recorded, "framebufferOpacity": recorded, "pixelOps": recorded,
			"pendingTimers": recorded, "roundTimers": boundary,
			// The file table is recorded by name and cursor; what a file holds
			// is the store's.
			"cFiles": recorded, "cFileHandles": recorded, "nextCDatabaseHandle": recorded,
			"guestFiles": boundary, "guestFileWrites": boundary,
			"removedFiles": store, "madeDirectories": store, "removedCDatabases": store,
			"wipicClips": recorded, "wipicClipOrder": recorded, "wipicMutedSources": recorded,
			"userMemoryInterface": recorded, "userMemoryPools": recorded,
			"inputModeTableAddress": recorded, "cInput": recorded,
			"events": recorded, "guestEventLoop": recorded, "leds": recorded,
			"aotCallDepth": boundary, "detachedCFiles": rebuilt, "pendingNetCallbacks": recorded,
			"repaintPending": recorded, "repaintServicing": boundary, "guestFlushedOwnFrame": recorded,
			// A Clet has no collector, so the trigger never leaves its floor.
			"collectAt": rebuilt,
		},
		reflect.TypeFor[cletState](): {
			"program": rebuilt, "root": recorded, "destroyStarted": boundary, "repaint": recorded,
			"timeRecord": recorded, "tokenNext": recorded,
		},
		reflect.TypeFor[cInputState](): {
			"owner": recorded, "active": recorded, "mode": recorded, "revision": recorded, "calls": recorded,
			"activations": recorded, "pending": recorded, "discardCarrier": recorded, "clearPending": recorded,
			"timerPointer": recorded, "timerCallback": recorded,
			"pendingValid": boundary, "delivering": boundary,
			// cInputUsed reads the use off the calls, the activations and the
			// mode table, which are recorded.
			"used": rebuilt, "unreached": reset,
		},
		reflect.TypeFor[guestArena](): {
			"base": recorded, "limit": recorded, "cursor": recorded, "free": recorded, "freed": recorded, "highWater": recorded,
			"recordReleased": diagnostic, "checkReused": diagnostic,
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
		if err := session.SendKey(t.Context(), KeyPressed, key); err != nil {
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
	options := SessionOptions{Clock: NewManualClock(time.Time{})}
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
		"stub address":  func(state *checkpointState) { state.Runtime.Stubs[0].Address = platformDataBase | 1 },
		"stub cursor":   func(state *checkpointState) { state.Runtime.CodeCursor = uint64(platformCodeBase) },
		"block over free": func(state *checkpointState) {
			state.Runtime.WIPICAllocations[0].Address = uint32(state.Runtime.Arena.Cursor)
		},
		"arena free bytes": func(state *checkpointState) { state.Runtime.Arena.Freed++ },
		"released live": func(state *checkpointState) {
			state.Runtime.ReleasedWIPIC = []uint32{state.Runtime.WIPICAllocations[0].Address}
		},
		"clock anchor":      func(state *checkpointState) { state.Runtime.ClockAge = 2 * maxDeadlineOffset },
		"timer callback":    func(state *checkpointState) { state.Runtime.Timers[0].Callback = platformDataBase | 1 },
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
		prepared, err := PrepareCheckpoint(program, changed, SessionOptions{Clock: NewManualClock(time.Time{})})
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
		"service": func() func() { client.serviceDepth = 1; return func() { client.serviceDepth = 0 } },
		"timer":   func() func() { client.activeTimer = &wipicTimer{}; return func() { client.activeTimer = nil } },
		"text": func() func() {
			client.runtime.cInput.delivering = true
			return func() { client.runtime.cInput.delivering = false }
		},
		"destroyed": func() func() { client.clet.destroyStarted = true; return func() { client.clet.destroyStarted = false } },
		"save read": func() func() {
			client.runtime.saveReadError = errors.New("unreadable")
			return func() { client.runtime.saveReadError = nil }
		},
	} {
		restore := busy()
		if _, err := session.CaptureCheckpoint(t.Context()); err == nil {
			t.Errorf("%s: a busy session was captured", name)
		}
		restore()
	}
	client.run.Lock()
	_, err := session.CaptureCheckpoint(t.Context())
	client.run.Unlock()
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
	prepared, err := PrepareCheckpoint(program, checkpoint, SessionOptions{Clock: NewManualClock(time.Time{}), SaveStore: store})
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
