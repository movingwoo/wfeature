package ktf

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
)

// Local archives remain ignored. Every run uses isolated temporary saves;
// reports identify an archive by its digest instead of its title or filename.
func TestLocalNativeCheckpointContinuation(t *testing.T) {
	if os.Getenv("WFEATURE_KTF_NATIVE_CHECKPOINT_ACCEPTANCE") != "1" {
		t.Skip("set WFEATURE_KTF_NATIVE_CHECKPOINT_ACCEPTANCE=1 to exercise local native archives")
	}
	for _, file := range localNativePackages(t) {
		archive, err := os.ReadFile(file)
		if err != nil {
			t.Fatal("read local native archive")
		}
		identity := backend.SaveIdentity(archive)
		t.Run(fmt.Sprintf("%x", identity[:6]), func(t *testing.T) {
			clock := NewManualClock(time.Time{})
			store := backend.NewDirectorySaveStore(filepath.Join(t.TempDir(), "owner"))
			options := NativeSessionOptions{Clock: clock, SaveStore: store, MaxSteps: 1_000_000_000}
			source, err := StartNativeSession(t.Context(), archive, options)
			if err != nil {
				t.Fatal(err)
			}
			defer source.Close()
			step := func(session *NativeSession, clock *ManualClock, round int) {
				t.Helper()
				if round == 5 || round == 19 {
					if err := session.SendKey(t.Context(), KeyPressed, KeyNum0+5); err != nil {
						t.Fatal(err)
					}
				}
				if round == 6 || round == 20 {
					if err := session.SendKey(t.Context(), KeyReleased, KeyNum0+5); err != nil {
						t.Fatal(err)
					}
				}
				if !session.SkipToNextDeadline() {
					clock.Advance(16 * time.Millisecond)
				}
				if _, err := session.Tick(t.Context()); err != nil {
					t.Fatalf("round %d: %v", round, err)
				}
			}
			for i := range 40 {
				step(source, clock, i)
			}
			began := time.Now()
			saved, err := source.CaptureCheckpoint(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := backend.EncodeCheckpoint(saved)
			if err != nil {
				t.Fatal(err)
			}
			captureCost := time.Since(began)
			saved, err = backend.DecodeCheckpoint(encoded, identity)
			if err != nil {
				t.Fatal(err)
			}
			freshClock := NewManualClock(time.Unix(1900000000, 0))
			freshStore := backend.NewDirectorySaveStore(filepath.Join(t.TempDir(), "owner"))
			options.Clock, options.SaveStore = freshClock, freshStore
			began = time.Now()
			prepared, err := PrepareNativeSessionCheckpoint(archive, saved, options)
			if err != nil {
				t.Fatal(err)
			}
			defer prepared.Discard()
			restored, err := prepared.Commit(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer restored.Close()
			restoreCost := time.Since(began)
			for i := range 100 {
				step(source, clock, i)
				step(restored, freshClock, i)
				if source.FrameDigest() != restored.FrameDigest() || source.Flushes() != restored.Flushes() || source.Client.Steps() != restored.Client.Steps() || source.GuestElapsed() != restored.GuestElapsed() {
					t.Fatalf("round %d changed pixels, frame count, instructions or guest time", i)
				}
			}
			a, err := source.CaptureCheckpoint(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			b, err := restored.CaptureCheckpoint(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(a.Runtime, b.Runtime) || !reflect.DeepEqual(a.Saves, b.Saves) {
				t.Fatal("continued native state or durable saves differ")
			}
			t.Logf("bytes=%d capture=%v restore=%v images=%d files=%d frames=%d steps=%d", len(encoded), captureCost, restoreCost, len(source.platform.images), len(source.platform.files), source.Flushes(), source.Client.Steps())
		})
	}
}
