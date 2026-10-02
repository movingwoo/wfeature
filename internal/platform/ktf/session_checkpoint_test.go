package ktf

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

func checkpointFixtureSession(t *testing.T, fixture continuationFixture, store backend.SaveStore) *Session {
	t.Helper()
	fixture.client.AttachSaveStore(store)
	return &Session{Archive: &Archive{JAR: &JAR{Client: fixture.client.image}}, Client: fixture.client,
		archiveIdentity: backend.SaveIdentity([]byte("authored checkpoint fixture")),
		options:         SessionOptions{MaxSteps: fixture.client.core.MaxSteps(), SaveStore: store, Clock: fixture.clock}}
}

// A load restores execution state and leaves the saves alone: what was saved
// after the checkpoint is still there, byte for byte, and nothing is staged or
// set aside beside the save directory. A write the displaced runtime issues
// while it unwinds goes to a store of its own.
func TestSessionCheckpointLeavesLaterSavesAndIsolatesDiscardedWrites(t *testing.T) {
	fixture := newContinuationFixture(t, 1700000000, continuationFixtureOptions{Runnable: true})
	fixture.start(t)
	owner := filepath.Join(t.TempDir(), "owner")
	store := backend.NewDirectorySaveStore(owner)
	if err := store.StoreSave("progress", []byte("saved")); err != nil {
		t.Fatal(err)
	}
	source := checkpointFixtureSession(t, fixture, store)
	source.Client.saveStore = newCertificateSaveStore(store, []byte("saved certificate"))
	checkpoint, err := source.CaptureCheckpoint(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := backend.EncodeCheckpoint(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err = backend.DecodeCheckpoint(encoded, source.archiveIdentity)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.StoreSaves(map[string][]byte{"progress": []byte("later"), "later-only": {9}}); err != nil {
		t.Fatal(err)
	}
	// This worker exists only in the displaced timeline. Its native deferred
	// save runs while StopThreads unwinds it, after the replacement committed.
	if err := source.Client.vm.RegisterNative("saved/Discard", "run", "()V", func(*jvm.VM, []jvm.Value) (jvm.Value, error) {
		defer func() {
			if err := source.Client.saveStore.StoreSave("late-discard", []byte("must stay isolated")); err != nil {
				t.Error(err)
			}
		}()
		return jvm.VoidValue(), fixture.runtime.sleepCurrentWorker(time.Hour)
	}); err != nil {
		t.Fatal(err)
	}
	worker, err := source.Client.newGuestWorker(&jvm.Object{ClassName: "saved/Discard"})
	if err != nil {
		t.Fatal(err)
	}
	source.Client.workers = append(source.Client.workers, worker)
	if _, err := source.Client.ServiceThreads(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	options := source.options
	clock := NewManualClock(time.Unix(1900000000, 0))
	options.Clock = clock
	prepared, err := prepareSessionCheckpoint(source.Archive, source.archiveIdentity, checkpoint, options)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Discard()
	if calls, first := prepared.PreparationStoreCalls(); calls != 0 {
		t.Fatalf("validation made %d save store calls, the first being %s", calls, first)
	}
	before, err := store.SnapshotSaves()
	if err != nil {
		t.Fatal(err)
	}
	clock.Advance(10 * time.Second)
	old := source.Client
	restored, err := prepared.Commit(t.Context(), source, store)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restored.Client.StopThreads)
	if source.Client != nil || !old.workersStopped || restored.Client == old {
		t.Fatal("old session remained live after adoption")
	}
	if data, _ := old.saveStore.LoadSave("late-discard"); string(data) != "must stay isolated" {
		t.Fatal("discarded worker did not finish its deferred save")
	}
	after, err := store.SnapshotSaves()
	if err != nil || !reflect.DeepEqual(after, before) {
		t.Fatalf("the load changed the saves: %+v, %v; want them as they were: %+v", after, err, before)
	}
	if data, _ := store.LoadSave("progress"); string(data) != "later" {
		t.Fatalf("the save written after the checkpoint reads %q after the load", data)
	}
	for _, name := range []string{"previous", "next", "intent"} {
		leftover := filepath.Join(filepath.Dir(owner), ".wfeature-quicksave", "owners", "owner", name)
		if _, err := os.Lstat(leftover); !os.IsNotExist(err) {
			t.Fatalf("the load left %s beside the saves: %v", name, err)
		}
	}
	certificate, _ := restored.Client.saveStore.LoadSave(certificateSaveKey)
	if string(certificate) != "saved certificate" {
		t.Fatal("adoption lost authentication adapter state")
	}
	clock.Advance(29 * time.Millisecond)
	if ran, err := restored.Client.ServiceThreads(t.Context(), 1); err != nil || ran != 0 {
		t.Fatalf("early restored worker = %d, %v", ran, err)
	}
	clock.Advance(time.Millisecond)
	if ran, err := restored.Client.ServiceThreads(t.Context(), 1); err != nil || ran != 1 {
		t.Fatalf("restored worker = %d, %v", ran, err)
	}
	if _, err := restored.CaptureCheckpoint(t.Context()); err != nil {
		t.Fatalf("restored session cannot be captured again: %v", err)
	}
}

// A load that is refused changes nothing: the running session keeps running
// and its saves are what they were. Nothing durable happens in a load any
// more, so the refusals left are the ones about the request itself.
func TestSessionCheckpointRefusalKeepsOriginalRunning(t *testing.T) {
	fixture := newContinuationFixture(t, 1700000000, continuationFixtureOptions{})
	fixture.start(t)
	store, err := backend.NewMemorySaveStore([]backend.SaveEntry{{Key: "progress", Data: []byte("current")}})
	if err != nil {
		t.Fatal(err)
	}
	source := checkpointFixtureSession(t, fixture, store)
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := source.CaptureCheckpoint(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled capture = %v", err)
	}
	source.Client.run.Lock()
	_, busy := source.CaptureCheckpoint(t.Context())
	source.Client.run.Unlock()
	if !errors.Is(busy, ErrCheckpointBusy) {
		t.Fatalf("busy capture = %v", busy)
	}
	saved, err := source.CaptureCheckpoint(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	original := source.Client
	for _, refusal := range []struct {
		name   string
		commit func(*PreparedSession) error
	}{
		{"no save store", func(prepared *PreparedSession) error {
			_, err := prepared.Commit(t.Context(), source, nil)
			return err
		}},
		{"canceled", func(prepared *PreparedSession) error {
			_, err := prepared.Commit(canceled, source, store)
			return err
		}},
		{"busy", func(prepared *PreparedSession) error {
			source.Client.run.Lock()
			defer source.Client.run.Unlock()
			_, err := prepared.Commit(t.Context(), source, store)
			return err
		}},
	} {
		prepared, err := prepareSessionCheckpoint(source.Archive, source.archiveIdentity, saved, source.options)
		if err != nil {
			t.Fatal(err)
		}
		if err := refusal.commit(prepared); err == nil {
			t.Fatalf("%s: the load was accepted", refusal.name)
		}
		prepared.Discard()
		if source.Client != original || original.workersStopped {
			t.Fatalf("%s: the refused load changed the original session", refusal.name)
		}
		if data, _ := store.LoadSave("progress"); string(data) != "current" {
			t.Fatalf("%s: the refused load changed live saves", refusal.name)
		}
	}
	fixture.clock.Advance(30 * time.Millisecond)
	if ran, err := source.Client.ServiceThreads(t.Context(), 1); err != nil || ran != 1 {
		t.Fatalf("original stopped after refused load: %d, %v", ran, err)
	}
}

// A session with no save store cannot be captured: its slot would carry no
// save and a load would have nowhere to find one.
func TestSessionCheckpointRequiresASaveStore(t *testing.T) {
	fixture := newContinuationFixture(t, 1700000000, continuationFixtureOptions{})
	fixture.start(t)
	source := &Session{Archive: &Archive{JAR: &JAR{Client: fixture.client.image}}, Client: fixture.client,
		archiveIdentity: backend.SaveIdentity([]byte("authored checkpoint fixture")),
		options:         SessionOptions{MaxSteps: fixture.client.core.MaxSteps(), Clock: fixture.clock}}
	if _, err := source.CaptureCheckpoint(t.Context()); err == nil {
		t.Fatal("a session without a save store was captured")
	}
}

func TestSessionCheckpointSubprocess(t *testing.T) {
	identity := backend.SaveIdentity([]byte("authored checkpoint fixture"))
	if path := os.Getenv("WFEATURE_SESSION_CHECKPOINT_FIXTURE"); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		saved, err := backend.DecodeCheckpoint(data, identity)
		if err != nil {
			t.Fatal(err)
		}
		var probe struct{ Address uint32 }
		if err := backend.DecodeCheckpointRecord(saved.Session, &probe); err != nil {
			t.Fatal(err)
		}
		fresh := newContinuationRestoreFixture(t, 1900000000, continuationFixtureOptions{Runnable: true})
		// The saves of the process that loads are its own: the slot carries none.
		store, err := backend.NewMemorySaveStore([]backend.SaveEntry{{Key: "progress", Data: []byte("written after the checkpoint")}})
		if err != nil {
			t.Fatal(err)
		}
		archive := &Archive{JAR: &JAR{Client: fresh.client.image}}
		prepared, err := prepareSessionCheckpoint(archive, identity, saved, SessionOptions{MaxSteps: fresh.client.core.MaxSteps(), Clock: fresh.clock, SaveStore: store})
		if err != nil {
			t.Fatal(err)
		}
		defer prepared.Discard()
		restored, err := prepared.Commit(t.Context(), nil, store)
		if err != nil {
			t.Fatal(err)
		}
		defer restored.Client.StopThreads()
		for i := 0; i < 2; i++ {
			fresh.clock.Advance(30 * time.Millisecond)
			if ran, err := restored.Client.ServiceThreads(t.Context(), 1); err != nil || ran != 1 {
				t.Fatalf("fresh process slice = %d, %v", ran, err)
			}
		}
		data = readTestBytes(t, restored.Client, probe.Address, 16)
		if binary.LittleEndian.Uint32(data) != 1 || binary.LittleEndian.Uint32(data[4:]) != 42 || binary.LittleEndian.Uint32(data[12:]) != 2 || len(restored.Client.workers) != 0 {
			t.Fatalf("fresh process continuation differs: %x", data)
		}
		if content, _ := store.LoadSave("progress"); string(content) != "written after the checkpoint" {
			t.Fatalf("the fresh process's save reads %q after the load", content)
		}
		return
	}
	fixture := newContinuationFixture(t, 1700000000, continuationFixtureOptions{Runnable: true})
	fixture.start(t)
	store, err := backend.NewMemorySaveStore([]backend.SaveEntry{{Key: "progress", Data: []byte("saved in another process")}})
	if err != nil {
		t.Fatal(err)
	}
	source := checkpointFixtureSession(t, fixture, store)
	saved, err := source.CaptureCheckpoint(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	source.Client.StopThreads()
	saved.Session, err = backend.EncodeCheckpointRecord(struct{ Address uint32 }{fixture.probe})
	if err != nil {
		t.Fatal(err)
	}
	data, err := backend.EncodeCheckpoint(saved)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "session.checkpoint")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	program := os.Getenv("WFEATURE_CALL_CHECKPOINT_RESTORE_BINARY")
	if program == "" {
		program = os.Args[0]
	}
	process := exec.CommandContext(t.Context(), program, "-test.run=^TestSessionCheckpointSubprocess$", "-test.timeout=20s")
	process.Env = append(os.Environ(), "WFEATURE_SESSION_CHECKPOINT_FIXTURE="+path)
	if output, err := process.CombinedOutput(); err != nil {
		t.Fatalf("restore session process: %v\n%s", err, output)
	}
}
