package backend

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// linkedOwner makes an owner directory that is a link to a directory kept
// somewhere else, the way a person moves one game's saves to another disk.
func linkedOwner(t *testing.T) (root, target string) {
	t.Helper()
	target = filepath.Join(t.TempDir(), "kept elsewhere")
	if err := os.Mkdir(target, 0755); err != nil {
		t.Fatal(err)
	}
	root = filepath.Join(t.TempDir(), "owner")
	if err := os.Symlink(target, root); err != nil {
		t.Skipf("this platform cannot make the link the case needs: %v", err)
	}
	return root, target
}

// refuseSaveFileLock stands in for a file system that answers the lock call
// with one error. Tests in this package do not run in parallel, so the call is
// replaced for the length of one test and no longer.
func refuseSaveFileLock(t *testing.T, refusal error) {
	t.Helper()
	previous := saveFileLock
	saveFileLock = func(*os.File, bool) error { return refusal }
	t.Cleanup(func() { saveFileLock = previous })
}

// refuseReservedDirectory stands in for a location where the reserved
// directory cannot be made. It answers the same for a path that is already
// there, which is the less convenient of the two orders a file system may
// check in.
func refuseReservedDirectory(t *testing.T, refusal error) {
	t.Helper()
	previous := makeSaveDirectory
	makeSaveDirectory = func(path string, _ fs.FileMode) error {
		return &fs.PathError{Op: "mkdir", Path: path, Err: refusal}
	}
	t.Cleanup(func() { makeSaveDirectory = previous })
}

// saveLockRefusals is every class of failure a location without a usable lock
// answers on this platform: the two every platform names the same way, and the
// codes its own file lists.
func saveLockRefusals() map[string]error {
	refusals := map[string]error{
		"permission":    fs.ErrPermission,
		"no lock built": fmt.Errorf("directory save locking on this operating system: %w", errors.ErrUnsupported),
	}
	for _, code := range saveLockUnavailableErrors {
		refusals[code.Error()] = code
	}
	return refusals
}

func readOnlyDirectories(t *testing.T, paths ...string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not refuse a write by directory mode bits")
	}
	if os.Geteuid() == 0 {
		t.Skip("the superuser is not refused by directory mode bits")
	}
	for _, path := range paths {
		if err := os.Chmod(path, 0555); err != nil {
			t.Fatal(err)
		}
	}
	// The temporary directory is removed after this, which needs them back.
	t.Cleanup(func() {
		for index := len(paths) - 1; index >= 0; index-- {
			_ = os.Chmod(paths[index], 0755)
		}
	})
}

// fallbackWarnings is a Host logger that keeps the directory of every warning
// it is given, so a test reads the report rather than its formatting.
type fallbackWarnings struct{ directories []string }

func (warnings *fallbackWarnings) Enabled(context.Context, slog.Level) bool { return true }

func (warnings *fallbackWarnings) Handle(_ context.Context, record slog.Record) error {
	if record.Level != slog.LevelWarn {
		return nil
	}
	record.Attrs(func(attribute slog.Attr) bool {
		if attribute.Key == "directory" {
			warnings.directories = append(warnings.directories, attribute.Value.String())
		}
		return true
	})
	return nil
}

func (warnings *fallbackWarnings) WithAttrs([]slog.Attr) slog.Handler { return warnings }
func (warnings *fallbackWarnings) WithGroup(string) slog.Handler      { return warnings }

// A per-game save directory may be a link. The releases before the file lock
// read and wrote through one, so the lock must not be what ends that: every
// ordinary operation and the session claim go through the link, and the
// reserved directory stays beside the link rather than following it.
func TestLinkedOwnerDirectoryWorksThroughTheLink(t *testing.T) {
	root, target := linkedOwner(t)
	store := NewDirectorySaveStore(root)
	if err := store.StoreSave("db/slot", []byte("one")); err != nil {
		t.Fatalf("store through a link: %v", err)
	}
	if data, exists, err := store.ReadSave("db/slot"); err != nil || !exists || string(data) != "one" {
		t.Fatalf("load through a link = %q, %t, %v", data, exists, err)
	}
	if err := store.StoreSaves(map[string][]byte{"db/slot": []byte("two"), "rms/.index": []byte("index")}); err != nil {
		t.Fatalf("batch through a link: %v", err)
	}
	inTarget := func(key string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(target, filepath.FromSlash(key)))
		if err != nil {
			t.Fatalf("%s did not land in the directory the link names: %v", key, err)
		}
		return string(data)
	}
	if inTarget("db/slot") != "two" || inTarget("rms/.index") != "index" {
		t.Fatal("the link target holds other bytes than the store wrote")
	}

	// Listing and export are one walk, and it has to enter the link to see
	// anything: a walk that stops at it exports a game that never saved.
	entries, err := ReadSaveTree(root)
	want := []SaveEntry{{Key: "db/slot", Data: []byte("two")}, {Key: "rms/.index", Data: []byte("index")}}
	if err != nil || !reflect.DeepEqual(entries, want) {
		t.Fatalf("list through a link = %+v, %v", entries, err)
	}
	// An import replaces the tree, so it removes through the link as well.
	written, removed, err := WriteSaveTree(root, []SaveEntry{{Key: "db/slot", Data: []byte("restored")}, {Key: "fs/new", Data: []byte("new")}})
	if err != nil || written != 2 || removed != 1 {
		t.Fatalf("import through a link = %d written, %d removed, %v", written, removed, err)
	}
	if inTarget("db/slot") != "restored" || inTarget("fs/new") != "new" {
		t.Fatal("the import did not land in the link target")
	}
	if _, err := os.Lstat(filepath.Join(target, "rms")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the import left an entry the backup does not hold: %v", err)
	}

	release, err := ClaimSaveDirectory(root)
	if err != nil {
		t.Fatalf("claim on a link: %v", err)
	}
	if other, err := ClaimSaveDirectory(root); !errors.Is(err, ErrSaveDirectoryBusy) {
		if other != nil {
			other()
		}
		t.Fatalf("second claim on a link = %v", err)
	}
	release()

	if info, err := os.Lstat(root); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the owner directory is no longer the link it was: %v", err)
	}
	paths, err := replacementPaths(root)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(filepath.Dir(filepath.Dir(paths.directory))) != filepath.Dir(paths.root) || filepath.Base(paths.directory) != "owner" {
		t.Fatalf("the reserved directory %s is not beside the link, named after it", paths.directory)
	}
	if _, err := os.Lstat(filepath.Join(filepath.Dir(target), ".wfeature-quicksave")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a reserved directory followed the link: %v", err)
	}
}

// Replacing a whole generation renames the owner directory, which a link does
// not survive, so it refuses one before staging anything — and so does the
// recovery of a replacement, which would rename through the link the same way.
func TestGenerationReplacementStillRefusesALinkedRoot(t *testing.T) {
	root, target := linkedOwner(t)
	store := NewDirectorySaveStore(root)
	for _, entry := range saveGeneration("old") {
		if err := store.StoreSave(entry.Key, entry.Data); err != nil {
			t.Fatalf("ordinary write through a link: %v", err)
		}
	}
	intact := func(after string) {
		t.Helper()
		if destination, err := os.Readlink(root); err != nil || destination != target {
			t.Fatalf("%s changed the link: %q, %v", after, destination, err)
		}
		for _, entry := range saveGeneration("old") {
			data, err := os.ReadFile(filepath.Join(target, filepath.FromSlash(entry.Key)))
			if err != nil || !bytes.Equal(data, entry.Data) {
				t.Fatalf("%s changed %s in the link target: %q, %v", after, entry.Key, data, err)
			}
		}
	}
	if err := store.ReplaceSaves(saveGeneration("new")); err == nil {
		t.Fatal("replacement accepted a linked root")
	}
	paths, err := replacementPaths(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, staged := range []string{paths.next, paths.previous, paths.intent} {
		if _, err := os.Lstat(staged); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("refused replacement left %s: %v", staged, err)
		}
	}
	intact("a refused replacement")
	if _, err := store.SnapshotSaves(); err == nil {
		t.Fatal("a generation snapshot followed a linked root")
	}
	if _, err := store.HasCheckpoint(SaveIdentity([]byte("authored slot archive"))); err == nil {
		t.Fatal("a checkpoint slot was offered for a linked root")
	}

	// A record that says the old generation was moved aside, over a root that
	// is a link: rolling that back is a rename onto the link.
	if err := os.WriteFile(paths.intent, append([]byte(saveReplaceMagic), 1), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ReadSave("db/index"); err == nil {
		t.Fatal("recovery accepted a linked root")
	}
	intact("a refused recovery")
}

// A location that cannot hold the kernel lock keeps this process's own
// exclusion instead of failing every operation: that is what the releases
// before the lock had, and the registry is at least as strong. The location
// says so at one of two steps, making the reserved directory or locking the
// file in it, and every class of answer is tried at both.
func TestSaveLockFallsBackWhereTheLocationCannotLock(t *testing.T) {
	for _, step := range []struct {
		name   string
		refuse func(*testing.T, error)
	}{{"creating", refuseReservedDirectory}, {"locking", refuseSaveFileLock}} {
		for name, refusal := range saveLockRefusals() {
			t.Run(step.name+"/"+name, func(t *testing.T) {
				step.refuse(t, refusal)
				saveLockFallsBack(t, refusal)
			})
		}
	}
}

func saveLockFallsBack(t *testing.T, refusal error) {
	root := filepath.Join(t.TempDir(), "owner")
	store := NewDirectorySaveStore(root)
	if err := store.StoreSave("db/slot", []byte("one")); err != nil {
		t.Fatalf("store without a file lock: %v", err)
	}
	if data, exists, err := store.ReadSave("db/slot"); err != nil || !exists || string(data) != "one" {
		t.Fatalf("load without a file lock = %q, %t, %v", data, exists, err)
	}
	if err := store.StoreSaves(map[string][]byte{"db/slot": []byte("two"), "fs/file": []byte("file")}); err != nil {
		t.Fatalf("batch without a file lock: %v", err)
	}
	entries, err := ReadSaveTree(root)
	if err != nil || len(entries) != 2 {
		t.Fatalf("list without a file lock = %+v, %v", entries, err)
	}
	if written, removed, err := WriteSaveTree(root, entries[:1]); err != nil || written != 1 || removed != 1 {
		t.Fatalf("import without a file lock = %d written, %d removed, %v", written, removed, err)
	}

	// The claim is held in this process alone, and is still a claim.
	release, err := ClaimSaveDirectory(root)
	if err != nil {
		t.Fatalf("claim without a file lock: %v", err)
	}
	if other, err := ClaimSaveDirectory(root); !errors.Is(err, ErrSaveDirectoryBusy) {
		if other != nil {
			other()
		}
		t.Fatalf("second claim without a file lock = %v", err)
	}
	release()
	if release, err = ClaimSaveDirectory(root); err != nil {
		t.Fatalf("a released claim stayed held: %v", err)
	}
	release()

	// Another store object on the same root waits for a transaction,
	// which the per-object mutex of the earlier releases did not do.
	unlock, err := lockSaveTree(root)
	if err != nil {
		t.Fatal(err)
	}
	read := make(chan error, 1)
	go func() {
		_, _, err := NewDirectorySaveStore(root).ReadSave("db/slot")
		read <- err
	}()
	select {
	case err := <-read:
		unlock()
		t.Fatalf("a second store entered an open transaction: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	unlock()
	select {
	case err := <-read:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the waiting read did not finish")
	}

	cause, fallback := SaveLockFallback(root)
	if !fallback || !errors.Is(cause, refusal) {
		t.Fatalf("fallback = %t, %v; want it recorded with %v", fallback, cause, refusal)
	}
	paths, err := replacementPaths(root)
	if err != nil {
		t.Fatal(err)
	}
	warnings := &fallbackWarnings{}
	for range 3 {
		WarnSaveLockFallback(slog.New(warnings), root)
	}
	if !reflect.DeepEqual(warnings.directories, []string{paths.root}) {
		t.Fatalf("the fallback was reported as %q, want once for %s", warnings.directories, paths.root)
	}
	// Nothing is kept for a root nobody is at.
	saveGates.mutex.Lock()
	defer saveGates.mutex.Unlock()
	for _, name := range []string{"lock", "session"} {
		if _, kept := saveGates.open[saveGateKey{root: paths.root, name: name}]; kept {
			t.Fatalf("the %s gate outlived its last holder", name)
		}
	}
}

// The exclusion the fallback keeps is a real one: writers on one root, each
// through its own acquisition, never overlap. The counter has no other
// guard, so the race detector fails this as well as the count does.
func TestSaveLockFallbackSerializesWritersOnOneRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "owner")
	refuseSaveFileLock(t, fs.ErrPermission)
	const writers, rounds = 8, 40
	inside, overlapped := 0, false
	var group sync.WaitGroup
	for range writers {
		group.Add(1)
		go func() {
			defer group.Done()
			for range rounds {
				unlock, err := lockSaveTree(root)
				if err != nil {
					t.Error(err)
					return
				}
				before := inside
				runtime.Gosched()
				overlapped = overlapped || inside != before
				inside = before + 1
				unlock()
			}
		}()
	}
	group.Wait()
	if overlapped || inside != writers*rounds {
		t.Fatalf("%d of %d transactions counted, overlapped=%t", inside, writers*rounds, overlapped)
	}
	// Each root has its own report, and a root that never fell back has none.
	other := filepath.Join(t.TempDir(), "owner")
	if err := NewDirectorySaveStore(other).StoreSave("db/slot", []byte("x")); err != nil {
		t.Fatal(err)
	}
	warnings := &fallbackWarnings{}
	for _, reported := range []string{root, other, root, other, filepath.Join(t.TempDir(), "untouched")} {
		WarnSaveLockFallback(slog.New(warnings), reported)
	}
	if !reflect.DeepEqual(warnings.directories, []string{root, other}) {
		t.Fatalf("two roots fell back and the report was %q", warnings.directories)
	}
}

// A lock somebody holds is contention. It is answered as busy and never read
// as a location that cannot lock, which would let the second holder in.
func TestSaveLockContentionDoesNotFallBack(t *testing.T) {
	t.Run("answered", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "owner")
		refuseSaveFileLock(t, ErrSaveDirectoryBusy)
		if release, err := ClaimSaveDirectory(root); !errors.Is(err, ErrSaveDirectoryBusy) {
			if release != nil {
				release()
			}
			t.Fatalf("a held lock was claimed: %v", err)
		}
		if cause, fallback := SaveLockFallback(root); fallback {
			t.Fatalf("contention was recorded as a missing capability: %v", cause)
		}
	})
	// The kernel lock itself, reached through a second name for one directory
	// so that this process's registry is not what answers.
	t.Run("held", func(t *testing.T) {
		parent := t.TempDir()
		root := filepath.Join(parent, "owner")
		linked := filepath.Join(t.TempDir(), "linked")
		if err := os.Symlink(parent, linked); err != nil {
			t.Skipf("this platform cannot make the second name the case needs: %v", err)
		}
		release, err := ClaimSaveDirectory(root)
		if err != nil {
			t.Fatal(err)
		}
		defer release()
		if cause, fallback := SaveLockFallback(root); fallback {
			t.Skipf("this location has no file lock to contend for: %v", cause)
		}
		alias := filepath.Join(linked, "owner")
		if other, err := ClaimSaveDirectory(alias); !errors.Is(err, ErrSaveDirectoryBusy) {
			if other != nil {
				other()
			}
			t.Fatalf("a second name claimed a held directory: %v", err)
		}
		if cause, fallback := SaveLockFallback(alias); fallback {
			t.Fatalf("contention was recorded as a missing capability: %v", cause)
		}
	})
}

// A save root that cannot be written still reads, as it did before the lock
// needed a directory beside it, and a write fails for the reason it always
// did: the save file itself cannot be created.
func TestReadOnlySaveRootReadsAndRefusesWritesOrdinarily(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "owner")
	if err := os.MkdirAll(filepath.Join(root, "db"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "db", "slot"), []byte("kept"), 0644); err != nil {
		t.Fatal(err)
	}
	readOnlyDirectories(t, parent, root, filepath.Join(root, "db"))

	store := NewDirectorySaveStore(root)
	if data, exists, err := store.ReadSave("db/slot"); err != nil || !exists || string(data) != "kept" {
		t.Fatalf("read on a read-only root = %q, %t, %v", data, exists, err)
	}
	if _, exists, err := store.ReadSave("db/absent"); err != nil || exists {
		t.Fatalf("absent entry on a read-only root = %t, %v", exists, err)
	}
	entries, err := ReadSaveTree(root)
	if err != nil || len(entries) != 1 || entries[0].Key != "db/slot" {
		t.Fatalf("list on a read-only root = %+v, %v", entries, err)
	}
	for name, write := range map[string]func() error{
		"store": func() error { return store.StoreSave("db/slot", []byte("new")) },
		"batch": func() error {
			return store.StoreSaves(map[string][]byte{"db/slot": []byte("new"), "db/other": []byte("new")})
		},
		"import": func() error {
			_, _, err := WriteSaveTree(root, []SaveEntry{{Key: "db/slot", Data: []byte("new")}})
			return err
		},
	} {
		err := write()
		if !errors.Is(err, fs.ErrPermission) {
			t.Fatalf("%s on a read-only root = %v, want the permission error of the write itself", name, err)
		}
		// The ordinary error names the save tree, not the lock's own directory.
		if strings.Contains(err.Error(), ".wfeature-quicksave") || !strings.Contains(err.Error(), filepath.Base(root)) {
			t.Fatalf("%s on a read-only root reported the lock rather than the write: %v", name, err)
		}
	}
	if data, err := os.ReadFile(filepath.Join(root, "db", "slot")); err != nil || string(data) != "kept" {
		t.Fatalf("a refused write changed the save: %q, %v", data, err)
	}

	release, err := ClaimSaveDirectory(root)
	if err != nil {
		t.Fatalf("claim on a read-only root: %v", err)
	}
	defer release()
	if other, err := ClaimSaveDirectory(root); !errors.Is(err, ErrSaveDirectoryBusy) {
		if other != nil {
			other()
		}
		t.Fatalf("second claim on a read-only root = %v", err)
	}
	if cause, fallback := SaveLockFallback(root); !fallback || !errors.Is(cause, fs.ErrPermission) {
		t.Fatalf("fallback = %t, %v; want the permission failure recorded", fallback, cause)
	}
	if _, err := os.Lstat(filepath.Join(parent, ".wfeature-quicksave")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a read-only root gained a reserved directory: %v", err)
	}
}

// A reserved path that is a link or not a directory was not made by this
// program. That is refused whether or not the location could have held a
// lock: the fallback is for a missing capability, not for a changed tree.
func TestTamperedReservedPathIsStillRefused(t *testing.T) {
	elsewhere := func(t *testing.T) string {
		t.Helper()
		directory := filepath.Join(t.TempDir(), "elsewhere")
		if err := os.Mkdir(directory, 0700); err != nil {
			t.Fatal(err)
		}
		return directory
	}
	link := func(t *testing.T, target, path string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, path); err != nil {
			t.Skipf("this platform cannot make the link the case needs: %v", err)
		}
	}
	cases := []struct {
		name               string
		tamper             func(t *testing.T, paths saveReplacementPaths)
		transaction, claim bool // which of the two locks the change must refuse
	}{
		{"reserved directory is a file", func(t *testing.T, paths saveReplacementPaths) {
			if err := os.WriteFile(filepath.Dir(filepath.Dir(paths.directory)), []byte("file"), 0600); err != nil {
				t.Fatal(err)
			}
		}, true, true},
		{"reserved directory is a link", func(t *testing.T, paths saveReplacementPaths) {
			link(t, elsewhere(t), filepath.Dir(filepath.Dir(paths.directory)))
		}, true, true},
		{"owners directory is a link", func(t *testing.T, paths saveReplacementPaths) {
			link(t, elsewhere(t), filepath.Dir(paths.directory))
		}, true, true},
		{"owner entry is a link", func(t *testing.T, paths saveReplacementPaths) {
			link(t, elsewhere(t), paths.directory)
		}, true, true},
		{"transaction lock is a directory", func(t *testing.T, paths saveReplacementPaths) {
			if err := os.MkdirAll(filepath.Join(paths.directory, "lock"), 0700); err != nil {
				t.Fatal(err)
			}
		}, true, false},
		{"transaction lock is a link", func(t *testing.T, paths saveReplacementPaths) {
			link(t, filepath.Join(elsewhere(t), "lock"), filepath.Join(paths.directory, "lock"))
		}, true, false},
		{"claim is a link", func(t *testing.T, paths saveReplacementPaths) {
			link(t, filepath.Join(elsewhere(t), "session"), filepath.Join(paths.directory, "session"))
		}, false, true},
	}
	for _, location := range []string{"lockable", "without locks", "cannot create", "read-only"} {
		for _, tampered := range cases {
			t.Run(location+"/"+tampered.name, func(t *testing.T) {
				parent := t.TempDir()
				root := filepath.Join(parent, "owner")
				if err := os.MkdirAll(filepath.Join(root, "db"), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, "db", "slot"), []byte("kept"), 0644); err != nil {
					t.Fatal(err)
				}
				paths, err := replacementPaths(root)
				if err != nil {
					t.Fatal(err)
				}
				tampered.tamper(t, paths)
				switch location {
				case "without locks":
					refuseSaveFileLock(t, errors.ErrUnsupported)
				case "cannot create":
					refuseReservedDirectory(t, fs.ErrPermission)
				case "read-only":
					readOnlyDirectories(t, parent)
				}
				store := NewDirectorySaveStore(root)
				if tampered.transaction {
					if _, _, err := store.ReadSave("db/slot"); err == nil {
						t.Fatal("a read went past a tampered reserved path")
					}
					if err := store.StoreSave("db/slot", []byte("new")); err == nil {
						t.Fatal("a write went past a tampered reserved path")
					}
					if _, err := ReadSaveTree(root); err == nil {
						t.Fatal("a listing went past a tampered reserved path")
					}
				}
				if tampered.claim {
					release, err := ClaimSaveDirectory(root)
					if err == nil {
						release()
						t.Fatal("a claim went past a tampered reserved path")
					}
					if errors.Is(err, ErrSaveDirectoryBusy) {
						t.Fatalf("tampering was reported as contention: %v", err)
					}
				}
				if cause, fallback := SaveLockFallback(root); fallback {
					t.Fatalf("tampering was recorded as a missing capability: %v", cause)
				}
				if data, err := os.ReadFile(filepath.Join(root, "db", "slot")); err != nil || string(data) != "kept" {
					t.Fatalf("a refused operation changed the save: %q, %v", data, err)
				}
			})
		}
	}
}
