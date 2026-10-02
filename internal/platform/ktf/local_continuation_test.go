package ktf

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// This opt-in probe compares a restored client with its source at subsequent
// manual-clock rounds. Save stores are private; it never reads or writes
// the person's save directory and does not exercise live session replacement.
func TestLocalKTFClientCheckpoint(t *testing.T) {
	path := os.Getenv("WFEATURE_KTF_CALL_ARCHIVE")
	if path == "" {
		t.Skip("set WFEATURE_KTF_CALL_ARCHIVE to a local archive")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	store, err := backend.NewMemorySaveStore(nil)
	if err != nil {
		t.Fatal(err)
	}
	options := SessionOptions{Clock: NewManualClock(time.Unix(1700000000, 0)), SaveStore: store}
	source, err := StartSession(t.Context(), data, options)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	for i := 0; i < 300; i++ {
		if _, err := source.Tick(t.Context()); err != nil {
			t.Fatal(err)
		}
		source.SkipToNextDeadline()
	}
	source.Frame()
	saved, err := source.CaptureCheckpoint(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	// Bytes separate all mutable source state.
	encoded, err := backend.EncodeCheckpoint(saved)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := backend.DecodeCheckpoint(encoded, backend.SaveIdentity(data))
	if err != nil {
		t.Fatal(err)
	}
	// A slot carries no save. The restored client gets a copy of what the
	// source's store held at the boundary: the same disk, seen by another
	// process.
	boundary, err := store.SnapshotSaves()
	if err != nil {
		t.Fatal(err)
	}
	restoredStore, err := backend.NewMemorySaveStore(boundary)
	if err != nil {
		t.Fatal(err)
	}
	options.Clock, options.SaveStore = NewManualClock(time.Unix(1900000000, 0)), restoredStore
	prepared, err := PrepareSessionCheckpoint(data, decoded, options)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Discard()
	restored, err := prepared.Commit(t.Context(), nil, restoredStore)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	client := restored.Client
	parked := len(client.workers)
	for tick := 0; tick < 100; tick++ {
		want, wantWidth, wantHeight, wantFlush := source.Frame()
		got, width, height, flush := restored.Frame()
		if !bytes.Equal(got, want) || width != wantWidth || height != wantHeight || flush != wantFlush || client.core.Steps() != source.Client.core.Steps() || client.runtime.guestElapsed() != source.Client.runtime.guestElapsed() || len(client.workers) != len(source.Client.workers) {
			t.Fatalf("client continuation differs at round %d: pixels=%t dimensions=%t flush=%d/%d steps=%d/%d elapsed=%v/%v workers=%d/%d", tick, bytes.Equal(got, want), width == wantWidth && height == wantHeight, flush, wantFlush, client.core.Steps(), source.Client.core.Steps(), client.runtime.guestElapsed(), source.Client.runtime.guestElapsed(), len(client.workers), len(source.Client.workers))
		}
		for _, session := range []*Session{source, restored} {
			if _, err := session.Tick(t.Context()); err != nil {
				t.Fatal(err)
			}
			session.SkipToNextDeadline()
		}
	}
	if _, err := restored.CaptureCheckpoint(t.Context()); err != nil {
		t.Fatal(err)
	}
	wantSaves, err := store.SnapshotSaves()
	if err != nil {
		t.Fatal(err)
	}
	gotSaves, err := restoredStore.SnapshotSaves()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(wantSaves, gotSaves) {
		t.Fatal("restored writes differ from the source's isolated saves")
	}
	digest := sha256.Sum256(data)
	t.Logf("archive=%x variant=%d module=%t checkpoint_bytes=%d compared_rounds=100 parked_workers=%d", digest[:6], saved.Variant, source.Client.IsModule(), len(encoded), parked)
}

// This opt-in inventory reads a local archive and uses only in-memory saves.
// It reports the pending call shapes; it does not produce a session checkpoint.
func TestLocalKTFCallCheckpointInventory(t *testing.T) {
	path := os.Getenv("WFEATURE_KTF_CALL_ARCHIVE")
	if path == "" {
		t.Skip("set WFEATURE_KTF_CALL_ARCHIVE to a local archive")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	session, err := StartSession(t.Context(), data, SessionOptions{Clock: NewManualClock(time.Unix(1700000000, 0))})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	shapes, refusals, workerRefusals := make(map[string]int), make(map[string]int), make(map[string]int)
	accepted, workers := 0, 0
	heaps := 0
	heapRefusals := make(map[string]int)
	metadata := 0
	metadataRefusals := make(map[string]int)
	for tick := 0; tick < 300; tick++ {
		if _, err := session.Tick(t.Context()); err != nil {
			t.Fatal(err)
		}
		session.SkipToNextDeadline()
		if tick%25 != 0 {
			continue
		}
		client := session.Client
		client.run.Lock()
		var roots []*jvm.Object
		for _, worker := range client.workers {
			roots = append(roots, worker.javaThread)
		}
		if _, err := client.runtime.captureHeapState(roots); err != nil {
			heapRefusals[err.Error()]++
		} else {
			heaps++
		}
		if _, err := client.runtime.captureMetadataState(); err != nil {
			metadataRefusals[err.Error()]++
		} else {
			metadata++
		}
		for _, worker := range client.workers {
			shape := ""
			_, err := client.core.InspectParkedCalls(worker.armThread, func(_ *armcore.Thread, frame armcore.CallFrame) error {
				if frame.Stop == armcore.CallStoppedAtLimit {
					shape += "limit;"
				} else {
					shape += fmt.Sprintf("svc:%d/%d;", frame.SupervisorCall.Immediate, frame.Context.Registers[12])
				}
				return nil
			})
			if err != nil {
				shape = "no inspectable ARM calls"
			}
			shapes[shape]++
			if _, err := client.runtime.captureAOTCalls(worker); err != nil {
				refusals[err.Error()]++
			} else {
				accepted++
			}
			if _, err := client.runtime.captureWorkerContinuation(worker); err != nil {
				workerRefusals[err.Error()]++
			} else {
				workers++
			}
		}
		client.run.Unlock()
	}
	t.Logf("accepted_call_components=%d accepted_worker_continuations=%d shapes=%v refusals=%v worker_refusals=%v", accepted, workers, shapes, refusals, workerRefusals)
	t.Logf("accepted_heap_components=%d heap_refusals=%v", heaps, heapRefusals)
	t.Logf("accepted_metadata_components=%d metadata_refusals=%v", metadata, metadataRefusals)
}
