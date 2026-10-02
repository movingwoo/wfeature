package backend

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
)

func saveGeneration(label string) []SaveEntry {
	return []SaveEntry{{Key: "db/index", Data: []byte(label)}, {Key: "fs/" + label, Data: []byte(label + " data")}}
}

func checkSaveGeneration(t *testing.T, root, label string) {
	t.Helper()
	entries, err := NewDirectorySaveStore(root).SnapshotSaves()
	if err != nil {
		t.Fatal(err)
	}
	want := saveGeneration(label)
	if len(entries) != len(want) {
		t.Fatalf("generation %s contains %d entries", label, len(entries))
	}
	for i := range want {
		if entries[i].Key != want[i].Key || !bytes.Equal(entries[i].Data, want[i].Data) {
			t.Fatalf("generation %s has mixed entries: %#v", label, entries)
		}
	}
}

func TestSaveReplacementKeepsPreviousGenerationAndDeletesLaterKeys(t *testing.T) {
	root := filepath.Join(t.TempDir(), "owner")
	store := NewDirectorySaveStore(root)
	for _, entry := range saveGeneration("old") {
		if err := store.StoreSave(entry.Key, entry.Data); err != nil {
			t.Fatal(err)
		}
	}
	for _, label := range []string{"new", "latest"} {
		if err := store.ReplaceSaves(saveGeneration(label)); err != nil {
			t.Fatal(err)
		}
		checkSaveGeneration(t, root, label)
	}
	paths, err := replacementPaths(root)
	if err != nil {
		t.Fatal(err)
	}
	checkSaveGeneration(t, paths.previous, "new")
	if _, err := os.Stat(paths.intent); !os.IsNotExist(err) {
		t.Fatal("completed replacement kept a recovery intent")
	}
	if err := store.ReplaceSaves(nil); err != nil {
		t.Fatal(err)
	}
	if entries, err := store.SnapshotSaves(); err != nil || len(entries) != 0 {
		t.Fatalf("empty replacement = %v, %v", entries, err)
	}
	checkSaveGeneration(t, paths.previous, "latest")
}

func TestSaveReplacementRollsBackRenameFailures(t *testing.T) {
	for failure := 1; failure <= 2; failure++ {
		t.Run(strconv.Itoa(failure), func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "owner")
			store := NewDirectorySaveStore(root)
			if err := store.ReplaceSaves(saveGeneration("old")); err != nil {
				t.Fatal(err)
			}
			injected := errors.New("injected rename failure")
			calls := 0
			err := store.replaceSaves(saveGeneration("new"), func(from, to string) error {
				calls++
				if calls == failure {
					return injected
				}
				// Destinations must be absent, including on hosts whose rename
				// cannot replace an existing directory.
				if _, err := os.Lstat(to); !os.IsNotExist(err) {
					t.Fatal("replacement relied on overwriting a directory")
				}
				return os.Rename(from, to)
			})
			if !errors.Is(err, injected) {
				t.Fatalf("replacement error = %v", err)
			}
			checkSaveGeneration(t, root, "old")
			if err := store.StoreSave("db/after", []byte("still usable")); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSaveReplacementCrashRecovery(t *testing.T) {
	if root := os.Getenv("WFEATURE_REPLACEMENT_CRASH_ROOT"); root != "" {
		stage, _ := strconv.Atoi(os.Getenv("WFEATURE_REPLACEMENT_CRASH_STAGE"))
		calls := 0
		store := NewDirectorySaveStore(root)
		err := store.replaceSaves(saveGeneration("new"), func(from, to string) error {
			if stage == 0 {
				os.Exit(77) // Intent is durable; no live path has moved.
			}
			if err := os.Rename(from, to); err != nil {
				return err
			}
			calls++
			if calls == stage {
				os.Exit(77) // No defers run, as with abrupt process termination.
			}
			return nil
		})
		t.Fatalf("crash point was not reached: %v", err)
	}
	for _, hadOld := range []bool{false, true} {
		stages := []int{0, 1}
		if hadOld {
			stages = append(stages, 2)
		}
		for _, stage := range stages {
			t.Run(fmt.Sprintf("old=%t/stage=%d", hadOld, stage), func(t *testing.T) {
				root := filepath.Join(t.TempDir(), "owner")
				if hadOld {
					if err := NewDirectorySaveStore(root).ReplaceSaves(saveGeneration("old")); err != nil {
						t.Fatal(err)
					}
				}
				// An already-used object must recover a different process's
				// interruption too, rather than caching "checked" forever.
				store := NewDirectorySaveStore(root)
				if _, _, err := store.ReadSave("db/index"); err != nil {
					t.Fatal(err)
				}
				process := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestSaveReplacementCrashRecovery$", "-test.timeout=15s")
				process.Env = append(os.Environ(), "WFEATURE_REPLACEMENT_CRASH_ROOT="+root, "WFEATURE_REPLACEMENT_CRASH_STAGE="+strconv.Itoa(stage))
				output, err := process.CombinedOutput()
				var exit *exec.ExitError
				if !errors.As(err, &exit) || exit.ExitCode() != 77 {
					t.Fatalf("crash child = %v: %s", err, output)
				}
				// Recovery runs through the normal first save read, not a
				// special repair command the user has to discover.
				_, _, err = store.ReadSave("db/index")
				if err != nil {
					t.Fatal(err)
				}
				committed := hadOld && stage == 2 || !hadOld && stage == 1
				if committed {
					checkSaveGeneration(t, root, "new")
				} else if hadOld {
					checkSaveGeneration(t, root, "old")
				} else if entries, err := store.SnapshotSaves(); err != nil || len(entries) != 0 {
					t.Fatalf("first-run rollback = %v, %v", entries, err)
				}
				if err := RecoverSaveTree(root); err != nil {
					t.Fatalf("repeated recovery: %v", err)
				}
				if committed && hadOld {
					paths, _ := replacementPaths(root)
					checkSaveGeneration(t, paths.previous, "old")
				}
			})
		}
	}
}

func TestSaveReplacementRejectsCorruptRecoveryBeforeWriting(t *testing.T) {
	root := filepath.Join(t.TempDir(), "owner")
	store := NewDirectorySaveStore(root)
	if err := store.ReplaceSaves(saveGeneration("old")); err != nil {
		t.Fatal(err)
	}
	paths, _ := replacementPaths(root)
	if err := os.WriteFile(paths.intent, []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	store = NewDirectorySaveStore(root)
	if err := store.StoreSave("db/index", []byte("new")); err == nil {
		t.Fatal("write ignored a corrupt recovery record")
	}
	if _, err := ReadSaveTree(root); err == nil {
		t.Fatal("export ignored a corrupt recovery record")
	}
	data, err := os.ReadFile(filepath.Join(root, "db", "index"))
	if err != nil || string(data) != "old" {
		t.Fatal("failed recovery changed the live generation")
	}
}

func TestSaveReplacementSnapshotsSeeOneGeneration(t *testing.T) {
	store := NewDirectorySaveStore(filepath.Join(t.TempDir(), "owner"))
	if err := store.ReplaceSaves(saveGeneration("old")); err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	errors := make(chan error, 3)
	for reader := 0; reader < 3; reader++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for i := 0; i < 20; i++ {
				entries, err := store.SnapshotSaves()
				if err != nil {
					errors <- err
					return
				}
				if len(entries) != 2 || entries[0].Key != "db/index" || entries[1].Key != "fs/"+string(entries[0].Data) || string(entries[1].Data) != string(entries[0].Data)+" data" {
					errors <- fmt.Errorf("snapshot mixed save generations")
					return
				}
			}
		}()
	}
	for i := 0; i < 10; i++ {
		if err := store.ReplaceSaves(saveGeneration(fmt.Sprintf("round%d", i))); err != nil {
			t.Fatal(err)
		}
	}
	workers.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
}
