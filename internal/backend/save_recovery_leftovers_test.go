package backend

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
)

// Recovery of what an earlier build's quick load can leave beside a title's
// saves, tested from the bytes on disk and from nothing else. No state here is
// made by calling the code that used to write it, so these tests go on
// describing those leftovers after that code is gone.
//
// The states are read off the replacement writer, DirectorySaveStore.replaceSaves,
// and the lock it runs under. Line numbers are those of the tree this file was
// added to:
//
//	save_replace.go:36-38   the reserved sibling of an owner directory,
//	                        .wfeature-quicksave/owners/<owner>, holds "next"
//	                        (staging), "previous" (the displaced tree) and
//	                        "intent" (the record)
//	save_lock.go:19,27,50   the same directory holds the empty files "lock" and
//	                        "session"; neither is ever unlinked
//	checkpoint_slot.go:43   and the slot the load read, <archive SHA-256>.wfq
//	save_replace.go:198     whether a live tree existed when the load began
//	save_replace.go:205-216 staging is emptied, made again and filled with the
//	                        slot's saves, one file per key
//	save_replace.go:241     the "previous" of an older load is removed
//	save_replace.go:244-251 the record is written: the eight bytes of
//	                        save_replace.go:14 and one more, 1 when a live tree
//	                        existed and 0 when it did not
//	save_replace.go:273     the live tree is renamed to "previous", if there
//	                        was one
//	save_replace.go:280     staging is renamed to the live tree
//	save_replace.go:287     the record is removed
//
// A process that stops between two of those steps leaves one of the states
// below. Permission bits are not part of a state: recovery asks of a path only
// whether it is a directory and not a link (save_replace.go:41-53).
const (
	leftoverReserved = ".wfeature-quicksave/owners/owner"
	leftoverStaging  = leftoverReserved + "/next"
	leftoverPrevious = leftoverReserved + "/previous"
)

var (
	// The saves a title had when the earlier build's load began, and the saves
	// that load brought from its slot.
	leftoverOld = []string{"db/index=old", "fs/slot/old.dat=old data", "rms/.index=old stores"}
	leftoverNew = []string{"db/index=new", "fs/slot/new.dat=new data"}

	// What the reserved directory holds beside the journal in every state.
	leftoverKept = leftoverAt(leftoverReserved, "lock=", "session=", strings.Repeat("5a", 32)+".wfq=a slot from an earlier build")

	// Stopped after the record (save_replace.go:249) and before the first
	// rename (:273): everything is staged and the live tree has not moved.
	leftoverPrepared = leftoverJoin(
		leftoverAt("owner", leftoverOld...),
		leftoverKept,
		leftoverAt(leftoverReserved, "intent=WFRSTR01\x01"),
		leftoverAt(leftoverStaging, leftoverNew...),
	)
	// Stopped between the two renames (:273 and :280): the live tree is
	// "previous" and nothing is at the live path.
	leftoverHalfSwapped = leftoverJoin(
		leftoverKept,
		leftoverAt(leftoverReserved, "intent=WFRSTR01\x01"),
		leftoverAt(leftoverStaging, leftoverNew...),
		leftoverAt(leftoverPrevious, leftoverOld...),
	)
	// Stopped after the second rename (:280) and before the record was removed
	// (:287): the load is complete and only its record is left over.
	leftoverSwapped = leftoverJoin(
		leftoverAt("owner", leftoverNew...),
		leftoverKept,
		leftoverAt(leftoverReserved, "intent=WFRSTR01\x01"),
		leftoverAt(leftoverPrevious, leftoverOld...),
	)
	// A load that ran to its end (:287): this is what every finished load of
	// an earlier build left, and what stays on disk for good.
	leftoverFinished = leftoverJoin(
		leftoverAt("owner", leftoverNew...),
		leftoverKept,
		leftoverAt(leftoverPrevious, leftoverOld...),
	)
)

// leftoverAt places files in one directory of a state. A file is its name, "="
// and its bytes.
func leftoverAt(directory string, files ...string) []string {
	placed := make([]string, len(files))
	for index, file := range files {
		placed[index] = directory + "/" + file
	}
	return placed
}

func leftoverJoin(parts ...[]string) []string {
	return slices.Concat(parts...)
}

// leftoverBuild writes a state under parent. A line is one of
//
//	path=bytes    a file and what it holds
//	path/         a directory that holds nothing
//	path->target  a symbolic link
//
// and the directories above a path are implied.
func leftoverBuild(t *testing.T, parent string, state []string) {
	t.Helper()
	for _, line := range state {
		name, data, file := strings.Cut(line, "=")
		link, target, linked := strings.Cut(line, "->")
		switch {
		case file:
			path := filepath.Join(parent, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
				t.Fatal(err)
			}
		case linked:
			path := filepath.Join(parent, filepath.FromSlash(link))
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, path); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
		case strings.HasSuffix(line, "/"):
			if err := os.MkdirAll(filepath.Join(parent, filepath.FromSlash(line)), 0o755); err != nil {
				t.Fatal(err)
			}
		default:
			t.Fatalf("leftover line %q is not a file, a directory or a link", line)
		}
	}
}

// leftoverListing reads back everything under parent in the notation
// leftoverBuild takes: each file with its bytes, each link with its target and
// each directory that holds nothing. Anything else is listed with its type.
func leftoverListing(t *testing.T, parent string) []string {
	t.Helper()
	listing := []string{}
	err := filepath.WalkDir(parent, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || path == parent {
			return err
		}
		relative, err := filepath.Rel(parent, path)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(relative)
		switch {
		case entry.IsDir():
			children, err := os.ReadDir(path)
			if err != nil {
				return err
			}
			if len(children) == 0 {
				listing = append(listing, name+"/")
			}
		case entry.Type().IsRegular():
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			listing = append(listing, name+"="+string(data))
		case entry.Type()&fs.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			listing = append(listing, name+"->"+target)
		default:
			listing = append(listing, name+" "+entry.Type().String())
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(listing)
	return listing
}

// leftoverExpect requires the tree under parent to be exactly the state, and
// prints the lines that differ when it is not.
func leftoverExpect(t *testing.T, parent string, state []string, when string) {
	t.Helper()
	got := leftoverListing(t, parent)
	want := slices.Clone(state)
	sort.Strings(want)
	if slices.Equal(got, want) {
		return
	}
	var difference strings.Builder
	for _, line := range got {
		if !slices.Contains(want, line) {
			fmt.Fprintf(&difference, "\n\tunexpected %q", line)
		}
	}
	for _, line := range want {
		if !slices.Contains(got, line) {
			fmt.Fprintf(&difference, "\n\tmissing    %q", line)
		}
	}
	t.Fatalf("%s the tree is not the expected one:%s", when, difference.String())
}

// leftoverOperation is one ordinary store operation. Any of them can be the
// first to meet a leftover, and there is no repair command to run before it.
type leftoverOperation struct {
	name string
	run  func(root string) (index string, found bool, err error)
	// reads is set when run answers with the entry db/index. A case then
	// requires that answer to come from the settled tree, and not from the
	// tree the journal was found in.
	reads bool
	// silent is set for the one read that has no way to report an error.
	silent bool
	// adds is what a successful run puts into the live tree.
	adds []string
}

func leftoverIndex(entries []SaveEntry, err error) (string, bool, error) {
	for _, entry := range entries {
		if entry.Key == "db/index" {
			return string(entry.Data), true, err
		}
	}
	return "", false, err
}

// leftoverOperations lists the operations in three groups: those that leave a
// settled tree as it is, those that add to the live tree, and those that
// replace something and are only tried where they must be refused.
func leftoverOperations(t *testing.T) (reads, writes, others []leftoverOperation) {
	t.Helper()
	// An archive with no slot in any of these states: the slot operations are
	// here because they run recovery, not for what they find.
	identity := SaveIdentity([]byte("an archive with no slot in these states"))
	envelope, err := EncodeCheckpoint(Checkpoint{Identity: identity, Variant: CheckpointKTFJava, Runtime: []byte("{}")})
	if err != nil {
		t.Fatal(err)
	}
	reads = []leftoverOperation{
		{name: "an entry read", reads: true, run: func(root string) (string, bool, error) {
			data, found, err := NewDirectorySaveStore(root).ReadSave("db/index")
			return string(data), found, err
		}},
		{name: "a read that cannot report an error", reads: true, silent: true, run: func(root string) (string, bool, error) {
			data, found := NewDirectorySaveStore(root).LoadSave("db/index")
			return string(data), found, nil
		}},
		{name: "a bounded read", reads: true, run: func(root string) (string, bool, error) {
			data, found, err := ReadSaveLimit(NewDirectorySaveStore(root), "db/index", 64)
			return string(data), found, err
		}},
		{name: "a snapshot", reads: true, run: func(root string) (string, bool, error) {
			return leftoverIndex(NewDirectorySaveStore(root).SnapshotSaves())
		}},
		{name: "an export", reads: true, run: func(root string) (string, bool, error) {
			return leftoverIndex(ReadSaveTree(root))
		}},
		{name: "an explicit recovery", run: func(root string) (string, bool, error) {
			return "", false, RecoverSaveTree(root)
		}},
		{name: "a slot query", run: func(root string) (string, bool, error) {
			_, err := NewDirectorySaveStore(root).HasCheckpoint(identity)
			return "", false, err
		}},
		{name: "a slot read", run: func(root string) (string, bool, error) {
			_, _, err := NewDirectorySaveStore(root).LoadCheckpoint(identity)
			return "", false, err
		}},
		{name: "a displaced-generation query", run: func(root string) (string, bool, error) {
			_, err := NewDirectorySaveStore(root).DisplacedGeneration()
			return "", false, err
		}},
	}
	writes = []leftoverOperation{
		{name: "an entry write", adds: []string{"owner/db/after=written afterwards"}, run: func(root string) (string, bool, error) {
			return "", false, NewDirectorySaveStore(root).StoreSave("db/after", []byte("written afterwards"))
		}},
		{name: "a batch write", adds: []string{"owner/db/batch=first of two", "owner/fs/batch=second of two"}, run: func(root string) (string, bool, error) {
			return "", false, NewDirectorySaveStore(root).StoreSaves(map[string][]byte{"db/batch": []byte("first of two"), "fs/batch": []byte("second of two")})
		}},
	}
	others = []leftoverOperation{
		{name: "an import", run: func(root string) (string, bool, error) {
			_, _, err := WriteSaveTree(root, []SaveEntry{{Key: "db/imported", Data: []byte("imported")}})
			return "", false, err
		}},
		{name: "a slot write", run: func(root string) (string, bool, error) {
			return "", false, NewDirectorySaveStore(root).StoreCheckpoint(identity, envelope)
		}},
	}
	return reads, writes, others
}

func TestSaveRecoveryOfLeftoversFromEarlierBuilds(t *testing.T) {
	reads, writes, others := leftoverOperations(t)

	// An interrupted load is settled by whichever operation comes first, and
	// settled the same way by each: a load that had not moved the live tree is
	// undone, one that had moved it and installed nothing gets the tree back,
	// and one that had installed its saves is left complete.
	for _, state := range []struct {
		name          string
		before, after []string
		// index is what db/index holds once the state is settled.
		index string
		found bool
		// returns is set when the directory at "previous" must itself become
		// the live tree, moved and not copied.
		returns bool
	}{
		{
			name:   "an intent with staging and a live tree",
			before: leftoverPrepared,
			// Staging is dropped and the live tree is the one that was there.
			after: leftoverJoin(leftoverAt("owner", leftoverOld...), leftoverKept),
			index: "old", found: true,
		},
		{
			name:   "an intent with staging and previous but no live tree",
			before: leftoverHalfSwapped,
			// The displaced tree is live again; staging is dropped.
			after: leftoverJoin(leftoverAt("owner", leftoverOld...), leftoverKept),
			index: "old", found: true, returns: true,
		},
		{
			name:   "an intent with a live tree and previous and no staging",
			before: leftoverSwapped,
			// Both trees stay where they are; only the record goes.
			after: leftoverFinished,
			index: "new", found: true,
		},
		{
			// A title with no save yet (save_replace.go:198 answers false),
			// stopped after the record and before the only rename (:280).
			name: "an intent with staging and no tree at all",
			before: leftoverJoin(
				leftoverKept,
				leftoverAt(leftoverReserved, "intent=WFRSTR01\x00"),
				leftoverAt(leftoverStaging, leftoverNew...),
			),
			// Staging is dropped, and there is still no live tree.
			after: leftoverKept,
		},
		{
			// The same title, stopped after that rename and before the record
			// was removed (:287).
			name: "an intent with a live tree and nothing else",
			before: leftoverJoin(
				leftoverAt("owner", leftoverNew...),
				leftoverKept,
				leftoverAt(leftoverReserved, "intent=WFRSTR01\x00"),
			),
			after: leftoverJoin(leftoverAt("owner", leftoverNew...), leftoverKept),
			index: "new", found: true,
		},
		{
			// A slot taken before the title first saved carries no save, so
			// its staging is an empty directory. Stopped between the renames,
			// "previous" is every save the title has.
			name: "an intent with empty staging and previous but no live tree",
			before: leftoverJoin(
				leftoverKept,
				leftoverAt(leftoverReserved, "intent=WFRSTR01\x01"),
				[]string{leftoverStaging + "/"},
				leftoverAt(leftoverPrevious, leftoverOld...),
			),
			after: leftoverJoin(leftoverAt("owner", leftoverOld...), leftoverKept),
			index: "old", found: true, returns: true,
		},
	} {
		t.Run(state.name, func(t *testing.T) {
			for _, operation := range slices.Concat(reads, writes) {
				t.Run(operation.name, func(t *testing.T) {
					parent := t.TempDir()
					root := filepath.Join(parent, "owner")
					leftoverBuild(t, parent, state.before)
					leftoverExpect(t, parent, state.before, "as built")
					var displaced os.FileInfo
					if state.returns {
						var err error
						if displaced, err = os.Lstat(filepath.Join(parent, filepath.FromSlash(leftoverPrevious))); err != nil {
							t.Fatal(err)
						}
						// Windows reads a directory's identity on first use
						// and by path, so it is asked for while the path
						// still names the directory.
						_ = os.SameFile(displaced, displaced)
					}

					index, found, err := operation.run(root)
					if err != nil {
						t.Fatalf("the first operation failed: %v", err)
					}
					if operation.reads && (found != state.found || index != state.index) {
						t.Fatalf("the read answered %q, %t; the settled tree holds %q, %t", index, found, state.index, state.found)
					}
					settled := leftoverJoin(state.after, operation.adds)
					leftoverExpect(t, parent, settled, "after the first operation")
					if state.returns {
						live, err := os.Lstat(root)
						if err != nil || !os.SameFile(displaced, live) {
							t.Fatalf("the live tree is not the directory that was displaced: %v", err)
						}
					}

					// Settled once is settled: no later operation finds
					// anything more to do.
					for _, again := range reads {
						index, found, err := again.run(root)
						if err != nil {
							t.Fatalf("%s after recovery: %v", again.name, err)
						}
						if again.reads && (found != state.found || index != state.index) {
							t.Fatalf("%s after recovery answered %q, %t", again.name, index, found)
						}
					}
					leftoverExpect(t, parent, settled, "after every read was repeated")
				})
			}
		})
	}

	// A journal that cannot be settled stops everything. Guessing would mean
	// reading or writing beside a load whose state is unknown, so every
	// operation reports an error and the disk stays exactly as it was found:
	// no read answers from "previous", no write makes a live tree beside it.
	refuse := func(t *testing.T, name string, before []string) {
		t.Run(name, func(t *testing.T) {
			parent := t.TempDir()
			root := filepath.Join(parent, "owner")
			leftoverBuild(t, parent, before)
			leftoverExpect(t, parent, before, "as built")
			for _, operation := range slices.Concat(reads, writes, others) {
				index, found, err := operation.run(root)
				if operation.silent {
					if found {
						t.Fatalf("%s answered %q from beside a journal nothing settled", operation.name, index)
					}
				} else if err == nil {
					t.Fatalf("%s went ahead beside a journal it could not settle", operation.name)
				}
				leftoverExpect(t, parent, before, "after "+operation.name)
			}
		})
	}
	corrupt := []struct {
		name   string
		intent []string
	}{
		{"an empty intent", []string{"intent="}},
		{"an intent cut short", []string{"intent=WFRSTR01"}},
		{"an intent with a byte too many", []string{"intent=WFRSTR01\x01\x00"}},
		{"an intent with another magic", []string{"intent=WFRSTR02\x01"}},
		{"an intent with an unknown flag", []string{"intent=WFRSTR01\x02"}},
		{"an intent that is a directory", []string{"intent/"}},
		// The record it points at is well formed, and the link's own length
		// is a record's nine bytes. The writer makes a file, so a link was
		// put there by something else and is not followed.
		{"an intent that is a link to a record", []string{"intent->elsewhere", "elsewhere=WFRSTR01\x01"}},
	}
	// The state around the record is the one where a wrong guess costs most:
	// the title's saves are in "previous" and nothing is live.
	t.Run("a corrupt intent", func(t *testing.T) {
		for _, record := range corrupt {
			refuse(t, record.name, leftoverJoin(
				leftoverKept,
				leftoverAt(leftoverReserved, record.intent...),
				leftoverAt(leftoverStaging, leftoverNew...),
				leftoverAt(leftoverPrevious, leftoverOld...),
			))
		}
	})
	// And the one where a write would have somewhere to land.
	t.Run("a corrupt intent beside a live tree", func(t *testing.T) {
		for _, record := range corrupt {
			refuse(t, record.name, leftoverJoin(
				leftoverAt("owner", leftoverOld...),
				leftoverKept,
				leftoverAt(leftoverReserved, record.intent...),
				leftoverAt(leftoverStaging, leftoverNew...),
			))
		}
	})
	// A well-formed record and three directories that are there or not make
	// sixteen combinations. The writer leaves the five settled above and
	// recovery accepts no other (save_replace.go:122-133): in each of the
	// remaining eleven something the record promises is missing, or something
	// it rules out is there.
	t.Run("a journal no writer step leaves", func(t *testing.T) {
		type journal struct{ hadOld, staging, live, previous bool }
		left := map[journal]bool{
			{true, true, true, false}:   true, // staging and a live tree
			{true, true, false, true}:   true, // staging and previous but no live tree
			{true, false, true, true}:   true, // a live tree and previous and no staging
			{false, true, false, false}: true, // staging and no tree at all
			{false, false, true, false}: true, // a live tree and nothing else
		}
		refused := 0
		for combination := 0; combination < 16; combination++ {
			state := journal{combination&8 != 0, combination&4 != 0, combination&2 != 0, combination&1 != 0}
			if left[state] {
				continue
			}
			refused++
			before, flag := slices.Clone(leftoverKept), 0
			if state.hadOld {
				flag = 1
			}
			before = append(before, leftoverReserved+"/intent=WFRSTR01"+string(rune(flag)))
			if state.staging {
				before = append(before, leftoverAt(leftoverStaging, leftoverNew...)...)
			}
			if state.live {
				before = append(before, leftoverAt("owner", leftoverNew...)...)
			}
			if state.previous {
				before = append(before, leftoverAt(leftoverPrevious, leftoverOld...)...)
			}
			refuse(t, fmt.Sprintf("flag %d, staging %t, live %t, previous %t", flag, state.staging, state.live, state.previous), before)
		}
		if refused != 11 {
			t.Fatalf("%d combinations were tried as refusals, want 11", refused)
		}
	})

	// With no intent there is nothing to settle, and what an earlier build
	// left stays byte for byte through everything a session does: reads,
	// writes, batches and an import that replaces the whole live tree.
	for _, state := range []struct {
		name      string
		before    []string
		index     string
		displaced bool
	}{
		{"previous with no intent", leftoverFinished, "new", true},
		{
			// Stopped while staging was being filled (save_replace.go:208-216)
			// by a first load, before any record: nothing had moved.
			name: "staging with no intent",
			before: leftoverJoin(
				leftoverAt("owner", leftoverOld...),
				leftoverKept,
				leftoverAt(leftoverStaging, "db/index=new"),
			),
			index: "old",
		},
		{
			// The same stop in a second load, before it removed the first
			// load's "previous" (:241).
			name: "previous and staging with no intent",
			before: leftoverJoin(
				leftoverFinished,
				leftoverAt(leftoverStaging, "db/index=newer"),
			),
			index: "new", displaced: true,
		},
	} {
		t.Run(state.name, func(t *testing.T) {
			parent := t.TempDir()
			root := filepath.Join(parent, "owner")
			leftoverBuild(t, parent, state.before)
			leftoverExpect(t, parent, state.before, "as built")
			for _, operation := range reads {
				index, found, err := operation.run(root)
				if err != nil {
					t.Fatalf("%s: %v", operation.name, err)
				}
				if operation.reads && (!found || index != state.index) {
					t.Fatalf("%s answered %q, %t; the live tree holds %q", operation.name, index, found, state.index)
				}
				leftoverExpect(t, parent, state.before, "after "+operation.name)
			}
			if displaced, err := NewDirectorySaveStore(root).DisplacedGeneration(); err != nil || displaced != state.displaced {
				t.Fatalf("displaced generation = %t, %v; want %t", displaced, err, state.displaced)
			}

			want := state.before
			for _, operation := range writes {
				if _, _, err := operation.run(root); err != nil {
					t.Fatalf("%s: %v", operation.name, err)
				}
				want = leftoverJoin(want, operation.adds)
				leftoverExpect(t, parent, want, "after "+operation.name)
			}

			if _, _, err := WriteSaveTree(root, []SaveEntry{{Key: "db/imported", Data: []byte("imported")}}); err != nil {
				t.Fatal(err)
			}
			want = []string{"owner/db/imported=imported"}
			for _, line := range state.before {
				if !strings.HasPrefix(line, "owner/") {
					want = append(want, line)
				}
			}
			leftoverExpect(t, parent, want, "after an import")
		})
	}
}
