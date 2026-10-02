package backend

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSaveReplacementRollbackSurvivesMissingJournal(t *testing.T) {
	root := filepath.Join(t.TempDir(), "owner")
	store := NewDirectorySaveStore(root)
	if err := store.ReplaceSaves(saveGeneration("old")); err != nil {
		t.Fatal(err)
	}
	paths, _ := replacementPaths(root)
	calls := 0
	injected := errors.New("injected missing preparation")
	err := store.replaceSaves(saveGeneration("new"), func(from, to string) error {
		calls++
		if calls == 2 {
			if err := os.Remove(paths.intent); err != nil {
				return err
			}
			if err := os.RemoveAll(paths.next); err != nil {
				return err
			}
			return injected
		}
		return os.Rename(from, to)
	})
	if !errors.Is(err, injected) {
		t.Fatalf("replacement = %v", err)
	}
	checkSaveGeneration(t, root, "old")
}

func TestSaveDirectoryAliasesShareTransaction(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "OwNer-é")
	if err := NewDirectorySaveStore(root).ReplaceSaves(saveGeneration("old")); err != nil {
		t.Fatal(err)
	}
	aliases := map[string]string{}
	linked := filepath.Join(t.TempDir(), "linked")
	if err := os.Symlink(parent, linked); err == nil {
		aliases["symlink-parent"] = filepath.Join(linked, filepath.Base(root))
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if relative, err := filepath.Rel(cwd, root); err == nil {
		aliases["relative"] = relative
	}
	// Some supported filesystems fold case or Unicode in filenames. Those
	// aliases must select the same journal as well as the same kernel lock.
	info, _ := os.Stat(root)
	for name, alias := range map[string]string{"case": strings.ToLower(root), "unicode": filepath.Join(parent, "OwNer-e\u0301")} {
		if other, err := os.Stat(alias); err == nil && os.SameFile(info, other) {
			aliases[name] = alias
		}
	}
	for name, alias := range aliases {
		t.Run(name, func(t *testing.T) {
			unlock, err := lockSaveTree(root)
			if err != nil {
				t.Fatal(err)
			}
			defer unlock()
			started, done := make(chan struct{}), make(chan error, 1)
			go func() {
				close(started)
				entries, err := ReadSaveTree(alias)
				if err == nil && !reflect.DeepEqual(entries, saveGeneration("old")) {
					err = fmt.Errorf("alias read a different generation")
				}
				done <- err
			}()
			<-started
			select {
			case err := <-done:
				t.Fatalf("alias bypassed active transaction: %v", err)
			case <-time.After(30 * time.Millisecond):
			}
			unlock()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("alias read did not finish")
			}
		})
	}
}

func TestSaveTransactionExcludesAnotherProcess(t *testing.T) {
	if root := os.Getenv("WFEATURE_SAVE_TRANSACTION_READER"); root != "" {
		fmt.Println("reader ready")
		entries, err := ReadSaveTree(root)
		if err != nil || !reflect.DeepEqual(entries, saveGeneration("old")) {
			t.Fatalf("child read = %+v, %v", entries, err)
		}
		return
	}
	root := filepath.Join(t.TempDir(), "owner")
	if err := NewDirectorySaveStore(root).ReplaceSaves(saveGeneration("old")); err != nil {
		t.Fatal(err)
	}
	unlock, err := lockSaveTree(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	child := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestSaveTransactionExcludesAnotherProcess$", "-test.timeout=15s")
	child.Env = append(os.Environ(), "WFEATURE_SAVE_TRANSACTION_READER="+root)
	var stderr bytes.Buffer
	child.Stderr = &stderr
	stdout, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer child.Process.Kill()
	ready, done := make(chan struct{}), make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			if scanner.Text() == "reader ready" {
				close(ready)
			}
		}
		done <- errors.Join(scanner.Err(), child.Wait())
	}()
	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("reader did not start: %v: %s", err, stderr.String())
	case <-time.After(5 * time.Second):
		t.Fatal("reader did not start")
	}
	select {
	case err := <-done:
		t.Fatalf("reader bypassed active transaction: %v: %s", err, stderr.String())
	case <-time.After(50 * time.Millisecond):
	}
	unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("reader failed: %v: %s", err, stderr.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("reader did not finish")
	}
}

func TestSaveClaimsExcludeSessionsButAllowReadsAndReleaseOnExit(t *testing.T) {
	if root := os.Getenv("WFEATURE_SAVE_CLAIM_CHILD"); root != "" {
		release, err := ClaimSaveDirectory(root)
		if os.Getenv("WFEATURE_SAVE_CLAIM_EXIT") == "yes" {
			if err != nil {
				t.Fatal(err)
			}
			_ = release
			os.Exit(77)
		}
		if release != nil {
			release()
		}
		if !errors.Is(err, ErrSaveDirectoryBusy) {
			t.Fatalf("second process claim = %v", err)
		}
		return
	}
	root := filepath.Join(t.TempDir(), "owner")
	release, err := ClaimSaveDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if entries, err := ReadSaveTree(root); err != nil || len(entries) != 0 {
		t.Fatalf("claim blocked save listing: %v", err)
	}
	if other, err := ClaimSaveDirectory(root); !errors.Is(err, ErrSaveDirectoryBusy) {
		if other != nil {
			other()
		}
		t.Fatalf("second store claim = %v", err)
	}
	child := func(exit bool) error {
		command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestSaveClaimsExcludeSessionsButAllowReadsAndReleaseOnExit$", "-test.timeout=15s")
		command.Env = append(os.Environ(), "WFEATURE_SAVE_CLAIM_CHILD="+root)
		if exit {
			command.Env = append(command.Env, "WFEATURE_SAVE_CLAIM_EXIT=yes")
		}
		output, err := command.CombinedOutput()
		if err != nil {
			return fmt.Errorf("child claim: %w: %s", err, output)
		}
		return nil
	}
	if err := child(false); err != nil {
		t.Fatal(err)
	}
	release()
	var exit *exec.ExitError
	if err := child(true); !errors.As(err, &exit) || exit.ExitCode() != 77 {
		t.Fatalf("child did not exit with a held claim: %v", err)
	}
	release, err = ClaimSaveDirectory(root)
	if err != nil {
		t.Fatalf("process exit left a stale claim: %v", err)
	}
	release()
}

// A read may perform crash recovery, so it must wait for the complete active
// replacement even when its DirectorySaveStore is a different Go object.
func TestSaveReplacementExcludesIndependentReaders(t *testing.T) {
	for _, window := range []int{1, 2} {
		for _, refuse := range []bool{false, true} {
			for _, reader := range []string{"export", "snapshot", "entry", "recovery"} {
				t.Run(fmt.Sprintf("rename%d/refuse=%t/%s", window, refuse, reader), func(t *testing.T) {
					root := filepath.Join(t.TempDir(), "owner")
					store := NewDirectorySaveStore(root)
					if err := store.ReplaceSaves(saveGeneration("old")); err != nil {
						t.Fatal(err)
					}
					ready, release := make(chan struct{}), make(chan struct{})
					defer func() {
						select {
						case <-release:
						default:
							close(release)
						}
					}()
					injected := errors.New("injected replacement failure")
					replaced := make(chan error, 1)
					go func() {
						calls := 0
						replaced <- store.replaceSaves(saveGeneration("new"), func(from, to string) error {
							calls++
							if calls == window {
								close(ready)
								<-release
								if refuse {
									return injected
								}
							}
							return os.Rename(from, to)
						})
					}()
					select {
					case <-ready:
					case err := <-replaced:
						t.Fatalf("replacement did not reach boundary: %v", err)
					case <-time.After(5 * time.Second):
						t.Fatal("replacement did not reach boundary")
					}
					started, read := make(chan struct{}), make(chan error, 1)
					want := "new"
					if refuse {
						want = "old"
					}
					go func() {
						other := NewDirectorySaveStore(root)
						close(started)
						var entries []SaveEntry
						var err error
						switch reader {
						case "export":
							entries, err = ReadSaveTree(root)
						case "snapshot":
							entries, err = other.SnapshotSaves()
						case "entry":
							data, exists, readErr := other.ReadSave("db/index")
							err = readErr
							if err == nil && (!exists || string(data) != want) {
								err = fmt.Errorf("entry = %q, exists=%t; want %q", data, exists, want)
							}
						case "recovery":
							err = RecoverSaveTree(root)
						}
						if err == nil && (reader == "export" || reader == "snapshot") && !reflect.DeepEqual(entries, saveGeneration(want)) {
							err = fmt.Errorf("read incomplete or wrong generation: %+v", entries)
						}
						read <- err
					}()
					<-started
					early := false
					select {
					case err := <-read:
						early = true
						t.Errorf("reader entered an active replacement: %v", err)
					case <-time.After(30 * time.Millisecond):
					}
					close(release)
					select {
					case err := <-replaced:
						if refuse && !errors.Is(err, injected) || !refuse && err != nil {
							t.Errorf("replacement = %v", err)
						}
					case <-time.After(5 * time.Second):
						t.Fatal("replacement did not finish")
					}
					if !early {
						select {
						case err := <-read:
							if err != nil {
								t.Error(err)
							}
						case <-time.After(5 * time.Second):
							t.Fatal("reader did not finish")
						}
					}
					checkSaveGeneration(t, root, want)
				})
			}
		}
	}
}
