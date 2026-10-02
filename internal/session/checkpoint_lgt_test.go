package session

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/testfixture"
)

func lgtCheckpointWord(t *testing.T, s *Session, address uint32) uint32 {
	t.Helper()
	data, err := s.Cheat().ReadBytes(address, 4)
	if err != nil {
		t.Fatal(err)
	}
	return binary.LittleEndian.Uint32(data)
}

func lgtCheckpointTicks(t *testing.T, s *Session, count int) {
	t.Helper()
	for range count {
		if _, err := s.Tick(t.Context(), 0); err != nil {
			t.Fatal(err)
		}
	}
}

// The LGT Clet goes through the same shared path the KTF runtimes do: what the
// Host holds beside the guest — the pause, the held keys, the speed — is
// restored with it, the saves stay as they are, and the session then does what
// the uninterrupted one did.
func TestCheckpointLGTRestoresInputPauseAndContinuation(t *testing.T) {
	archive, err := testfixture.LGTCheckpointArchive()
	if err != nil {
		t.Fatal(err)
	}
	for _, paused := range []bool{false, true} {
		t.Run(fmt.Sprintf("paused=%t", paused), func(t *testing.T) {
			store, _ := backend.NewMemorySaveStore([]backend.SaveEntry{{Key: "fs/progress", Data: []byte("saved")}})
			s, err := Start(t.Context(), archive, Options{SaveStore: store, Speed: 2})
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if !s.CanCheckpoint() {
				t.Fatal("an LGT session cannot be asked for a checkpoint")
			}
			lgtCheckpointTicks(t, s, 3)
			if err := s.SendKey(t.Context(), KeyPress, '5'); err != nil {
				t.Fatal(err)
			}
			lgtCheckpointTicks(t, s, 1)
			if paused {
				if err := s.Pause(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			data, err := s.CaptureCheckpoint(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			counted := lgtCheckpointWord(t, s, testfixture.LGTCheckpointFrameCounter)
			if paused {
				if lgtCheckpointWord(t, s, testfixture.LGTCheckpointLifecycle) != 3 {
					t.Fatal("the fixture was not told it was paused")
				}
				if err := s.Resume(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.SendKey(t.Context(), KeyRelease, '5'); err != nil {
				t.Fatal(err)
			}
			lgtCheckpointTicks(t, s, 5)
			released := lgtCheckpointWord(t, s, testfixture.LGTCheckpointLastEvent)
			expected, width, height, _ := s.Frame()
			flushes, steps, elapsed := s.Flushes(), s.LGT().Steps(), s.LGT().GuestElapsed()
			if err := store.StoreSave("fs/progress", []byte("later")); err != nil {
				t.Fatal(err)
			}

			if err := s.LoadCheckpoint(t.Context(), archive, data); err != nil {
				t.Fatal(err)
			}
			if s.Paused() != paused || !reflect.DeepEqual(s.HeldKeys(), []int32{'5'}) || !s.CanCheckpoint() || s.Speed() != 2 {
				t.Fatal("the shared state was not restored")
			}
			if lgtCheckpointWord(t, s, testfixture.LGTCheckpointStartupCounter) != 1 ||
				lgtCheckpointWord(t, s, testfixture.LGTCheckpointFrameCounter) != counted {
				t.Fatal("the restored guest replayed its startup or is not at the checkpoint")
			}
			if saved, _ := store.LoadSave("fs/progress"); string(saved) != "later" {
				t.Fatalf("the load changed the save written after the checkpoint: %q", saved)
			}
			if paused {
				if _, err := s.Tick(t.Context(), 0); !errors.Is(err, ErrPaused) {
					t.Fatalf("a paused checkpoint ran: %v", err)
				}
				if lgtCheckpointWord(t, s, testfixture.LGTCheckpointLifecycle) != 3 {
					t.Fatal("the restored fixture forgot it was paused")
				}
				if err := s.Resume(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.ReleaseHeldInput(t.Context()); err != nil || len(s.HeldKeys()) != 0 {
				t.Fatalf("the restored hold was not released: %v", err)
			}
			lgtCheckpointTicks(t, s, 5)
			if lgtCheckpointWord(t, s, testfixture.LGTCheckpointLastEvent) != released {
				t.Fatal("the restored guest was not told its key was released")
			}
			actual, w, h, _ := s.Frame()
			if !bytes.Equal(actual, expected) || w != width || h != height || s.Flushes() != flushes ||
				s.LGT().Steps() != steps || s.LGT().GuestElapsed() != elapsed {
				t.Fatalf("the continuation differs: flushes %d/%d steps %d/%d elapsed %v/%v",
					s.Flushes(), flushes, s.LGT().Steps(), steps, s.LGT().GuestElapsed(), elapsed)
			}
		})
	}
}

// A refused load leaves the running session, what the Host holds for it and
// its saves exactly as they were, and the next valid load still works.
func TestCheckpointLGTRefusalPreservesTheLiveSession(t *testing.T) {
	archive, err := testfixture.LGTCheckpointArchive()
	if err != nil {
		t.Fatal(err)
	}
	store := backend.NewDirectorySaveStore(filepath.Join(t.TempDir(), testfixture.LGTCheckpointSaveOwner))
	if err := store.StoreSave("fs/progress", []byte("saved")); err != nil {
		t.Fatal(err)
	}
	s, err := Start(t.Context(), archive, Options{SaveStore: store})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	lgtCheckpointTicks(t, s, 2)
	data, err := s.CaptureCheckpoint(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := backend.DecodeCheckpoint(data, backend.SaveIdentity(archive))
	if err != nil {
		t.Fatal(err)
	}
	if checkpoint.Variant != backend.CheckpointLGTClet {
		t.Fatalf("variant = %d", checkpoint.Variant)
	}
	lgtCheckpointTicks(t, s, 2)
	if err := s.SendKey(t.Context(), KeyPress, '7'); err != nil {
		t.Fatal(err)
	}
	if err := store.StoreSave("fs/progress", []byte("current")); err != nil {
		t.Fatal(err)
	}
	previous, counted, steps := s.LGT(), lgtCheckpointWord(t, s, testfixture.LGTCheckpointFrameCounter), s.LGT().Steps()
	unchanged := func(why string) {
		t.Helper()
		if s.LGT() != previous || !s.Running() || s.Failed() != nil || s.LGT().Steps() != steps ||
			lgtCheckpointWord(t, s, testfixture.LGTCheckpointFrameCounter) != counted ||
			!reflect.DeepEqual(s.HeldKeys(), []int32{'7'}) {
			t.Fatalf("%s changed the live session", why)
		}
		if current, found, err := store.ReadSave("fs/progress"); err != nil || !found || string(current) != "current" {
			t.Fatalf("%s changed the saves: %q, %t, %v", why, current, found, err)
		}
	}

	// A runtime section that decodes and does not describe a session: the
	// envelope's checksum is recomputed, so only the platform can refuse it.
	var record, client map[string]json.RawMessage
	if err := json.Unmarshal(checkpoint.Runtime, &record); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(record["Client"], &client); err != nil {
		t.Fatal(err)
	}
	client["NextHandle"] = json.RawMessage("0")
	if record["Client"], err = json.Marshal(client); err != nil {
		t.Fatal(err)
	}
	damaged := checkpoint
	if damaged.Runtime, err = json.Marshal(record); err != nil {
		t.Fatal(err)
	}
	bad, err := backend.EncodeCheckpoint(damaged)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.LoadCheckpoint(t.Context(), archive, bad); err == nil {
		t.Fatal("a record with no handle counter was adopted")
	}
	unchanged("a malformed record")
	// Another execution variant's number over the same record.
	wrong := checkpoint
	wrong.Variant = backend.CheckpointKTFNative
	other, err := backend.EncodeCheckpoint(wrong)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.LoadCheckpoint(t.Context(), archive, other); !errors.Is(err, ErrCheckpointUnsupported) {
		t.Fatalf("a KTF variant over an LGT archive = %v", err)
	}
	unchanged("another platform's variant")
	if err := s.LoadCheckpoint(t.Context(), append(bytes.Clone(archive), 0), data); !errors.Is(err, backend.ErrCheckpointIdentity) {
		t.Fatalf("another archive = %v", err)
	}
	unchanged("another archive")
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if err := s.LoadCheckpoint(cancelled, archive, data); !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled load = %v", err)
	}
	unchanged("a cancelled load")
	// A KTF checkpoint handed to this session belongs to another archive.
	ktfArchive, err := testfixture.KTFCheckpointArchive()
	if err != nil {
		t.Fatal(err)
	}
	ktfStore, _ := backend.NewMemorySaveStore(nil)
	ktfSession, err := Start(t.Context(), ktfArchive, Options{SaveStore: ktfStore})
	if err != nil {
		t.Fatal(err)
	}
	defer ktfSession.Close()
	foreign, err := ktfSession.CaptureCheckpoint(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.LoadCheckpoint(t.Context(), archive, foreign); !errors.Is(err, backend.ErrCheckpointIdentity) {
		t.Fatalf("a KTF checkpoint = %v", err)
	}
	if err := ktfSession.LoadCheckpoint(t.Context(), ktfArchive, data); !errors.Is(err, backend.ErrCheckpointIdentity) {
		t.Fatalf("an LGT checkpoint in a KTF session = %v", err)
	}
	unchanged("a KTF checkpoint")

	lgtCheckpointTicks(t, s, 1)
	if err := s.LoadCheckpoint(t.Context(), archive, data); err != nil {
		t.Fatalf("the valid checkpoint could not be loaded after the refusals: %v", err)
	}
	if kept, found, err := store.ReadSave("fs/progress"); err != nil || !found || string(kept) != "current" {
		t.Fatalf("the valid load changed the saves: %q, %t, %v", kept, found, err)
	}
	if len(s.HeldKeys()) != 0 {
		t.Fatal("the valid load kept a hold made after the checkpoint")
	}
}

// A checkpoint written to a slot is restored by a process that never ran the
// title: the startup is not replayed, the hold the Host had is still owned,
// and the save written after the checkpoint is still there.
func TestCheckpointLGTSubprocess(t *testing.T) {
	archive, err := testfixture.LGTCheckpointArchive()
	if err != nil {
		t.Fatal(err)
	}
	identity := backend.SaveIdentity(archive)
	if root := os.Getenv("WFEATURE_SHARED_LGT_CHECKPOINT_FIXTURE"); root != "" {
		store := backend.NewDirectorySaveStore(root)
		data, exists, err := store.LoadCheckpoint(identity)
		if err != nil || !exists {
			t.Fatalf("restarted process slot = %t, %v", exists, err)
		}
		restored, err := RestoreCheckpoint(t.Context(), archive, data, Options{SaveStore: store})
		if err != nil {
			t.Fatal(err)
		}
		defer restored.Close()
		count, err := strconv.ParseUint(os.Getenv("WFEATURE_SHARED_LGT_CHECKPOINT_COUNT"), 10, 32)
		if err != nil {
			t.Fatal(err)
		}
		want := uint32(count)
		if lgtCheckpointWord(t, restored, testfixture.LGTCheckpointStartupCounter) != 1 ||
			lgtCheckpointWord(t, restored, testfixture.LGTCheckpointFrameCounter) != want ||
			!reflect.DeepEqual(restored.HeldKeys(), []int32{49}) {
			t.Fatal("the restarted process replayed startup, lost the frame count or lost input")
		}
		if data, _ := store.LoadSave("fs/progress"); string(data) != "written after the checkpoint" {
			t.Fatalf("the restarted process changed the save written after the checkpoint: %q", data)
		}
		if err := restored.ReleaseHeldInput(t.Context()); err != nil {
			t.Fatal(err)
		}
		lgtCheckpointTicks(t, restored, 3)
		if lgtCheckpointWord(t, restored, testfixture.LGTCheckpointFrameCounter) != want+3 {
			t.Fatal("the restarted process does not run on from the checkpoint")
		}
		return
	}
	root := filepath.Join(t.TempDir(), testfixture.LGTCheckpointSaveOwner)
	store := backend.NewDirectorySaveStore(root)
	if err := store.StoreSave("fs/progress", []byte("saved before process exit")); err != nil {
		t.Fatal(err)
	}
	source, err := Start(t.Context(), archive, Options{SaveStore: store})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(source.Close)
	lgtCheckpointTicks(t, source, 4)
	if err := source.SendKey(t.Context(), KeyPress, 49); err != nil {
		t.Fatal(err)
	}
	data, err := source.CaptureCheckpoint(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.StoreCheckpoint(identity, data); err != nil {
		t.Fatal(err)
	}
	count := lgtCheckpointWord(t, source, testfixture.LGTCheckpointFrameCounter)
	source.Close()
	if err := store.StoreSave("fs/progress", []byte("written after the checkpoint")); err != nil {
		t.Fatal(err)
	}
	program := os.Getenv("WFEATURE_SHARED_CHECKPOINT_RESTORE_BINARY")
	if program == "" {
		program = os.Args[0]
	}
	process := exec.CommandContext(t.Context(), program, "-test.run=^TestCheckpointLGTSubprocess$", "-test.timeout=20s")
	process.Env = append(os.Environ(), "WFEATURE_SHARED_LGT_CHECKPOINT_FIXTURE="+root,
		"WFEATURE_SHARED_LGT_CHECKPOINT_COUNT="+strconv.FormatUint(uint64(count), 10))
	if output, err := process.CombinedOutput(); err != nil {
		t.Fatalf("restore shared-session process: %v\n%s", err, output)
	}
}
