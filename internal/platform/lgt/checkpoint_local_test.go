package lgt

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"golang.org/x/text/unicode/norm"

	"github.com/movingwoo/wfeature/internal/backend"
)

// checkpointFingerprint is everything two sessions that are the same session
// agree about after the same tick.
type checkpointFingerprint struct {
	Frame   uint64
	Flushes uint64
	Steps   uint64
	Elapsed int64
}

func fingerprintOf(session *Session) checkpointFingerprint {
	return checkpointFingerprint{session.FrameDigest(), session.Flushes(), session.Steps(), int64(session.GuestElapsed())}
}

// memoryDigest hashes every committed page of guest memory, which is where a
// difference the screen has not shown yet is.
func memoryDigest(t testing.TB, client *Client) string {
	t.Helper()
	state, err := client.core.CaptureState()
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.New()
	for _, page := range state.Memory.Pages {
		fmt.Fprintf(hash, "%08x", page.Address)
		hash.Write(page.Data)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

// roundTripCheckpoint takes a checkpoint through the bytes a Host stores, so a
// comparison is against what a restart would read rather than against the
// structures that were just built.
func roundTripCheckpoint(t testing.TB, archive []byte, session *Session) backend.Checkpoint {
	t.Helper()
	checkpoint, err := session.CaptureCheckpoint(context.Background())
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	encoded, err := backend.EncodeCheckpoint(checkpoint)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := backend.DecodeCheckpoint(encoded, backend.SaveIdentity(archive))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return decoded
}

// boundarySaves copies what a store holds now into a store of its own. A slot
// carries no save, so a test that restores into a second session gives it this
// copy: the same disk, seen by another process.
func boundarySaves(t testing.TB, store *backend.MemorySaveStore) *backend.MemorySaveStore {
	t.Helper()
	entries, err := store.SnapshotSaves()
	if err != nil {
		t.Fatalf("snapshot the saves: %v", err)
	}
	copied, err := backend.NewMemorySaveStore(entries)
	if err != nil {
		t.Fatalf("copy the saves: %v", err)
	}
	return copied
}

// TestLocalLGTCheckpointsContinueIdentically is the acceptance probe for quick
// save and load over real archives: run a title, take a checkpoint, restore it
// into a second session with saves of its own, then tick both and compare what
// every tick did. It is opt-in for the reason every local probe is.
//
//	WFEATURE_LGT_CHECKPOINT=1 go test -run TestLocalLGTCheckpointsContinueIdentically -v ./internal/platform/lgt
//
// WFEATURE_LGT_CHECKPOINT_WARM and _ROUNDS set how far the source runs before
// the checkpoint and how long the two are compared; _MATCH keeps the archives
// whose name contains it; _DIR names another directory under var/games.
//
// WFEATURE_LGT_CHECKPOINT_SWEEP=1 asks a different question of the same runs:
// instead of comparing one checkpoint, it takes one after every round and
// rebuilds a session from it, and reports every boundary that was refused. A
// title is only ever at a few kinds of boundary, and this is what finds the
// rare one.
func TestLocalLGTCheckpointsContinueIdentically(t *testing.T) {
	if os.Getenv("WFEATURE_LGT_CHECKPOINT") != "1" {
		t.Skip("set WFEATURE_LGT_CHECKPOINT=1 to run ignored local LGT archives")
	}
	number := func(name string, fallback int) int {
		if value := os.Getenv(name); value != "" {
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed < 0 {
				t.Fatalf("%s=%q is not a count", name, value)
			}
			return parsed
		}
		return fallback
	}
	warm, rounds := number("WFEATURE_LGT_CHECKPOINT_WARM", 200), number("WFEATURE_LGT_CHECKPOINT_ROUNDS", 150)
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate the probe source")
	}
	root := filepath.Join(filepath.Dir(source), "..", "..", "..", "var", "games")
	groups := []string{"lgt", "LGT WIPI 2.X"}
	if directory := os.Getenv("WFEATURE_LGT_CHECKPOINT_DIR"); directory != "" {
		groups = []string{directory}
	}
	var files []string
	for _, group := range groups {
		entries, err := os.ReadDir(filepath.Join(root, group))
		if err != nil {
			continue
		}
		for _, entry := range entries {
			// A file name read back from this filesystem may be decomposed
			// where the one typed into a shell is not.
			if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".zip") ||
				!strings.Contains(norm.NFC.String(entry.Name()), norm.NFC.String(os.Getenv("WFEATURE_LGT_CHECKPOINT_MATCH"))) {
				continue
			}
			files = append(files, filepath.Join(root, group, entry.Name()))
		}
	}
	if len(files) == 0 {
		t.Skip("no local LGT archives")
	}
	type outcome struct {
		name, kind, result string
		size               int
	}
	var mutex sync.Mutex
	var outcomes []outcome
	t.Run("archives", func(t *testing.T) {
		for _, file := range files {
			name := filepath.Base(filepath.Dir(file)) + "/" + filepath.Base(file)
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				kind, size, result := compareLocalCheckpoint(t, file, warm, rounds, os.Getenv("WFEATURE_LGT_CHECKPOINT_SWEEP") == "1")
				mutex.Lock()
				outcomes = append(outcomes, outcome{name, kind, result, size})
				mutex.Unlock()
			})
		}
	})
	sort.Slice(outcomes, func(i, j int) bool { return outcomes[i].name < outcomes[j].name })
	counts := map[string]int{}
	for _, entry := range outcomes {
		counts[entry.kind+" "+strings.SplitN(entry.result, ":", 2)[0]]++
		t.Logf("%-8s %9d  %-60s %s", entry.kind, entry.size, entry.name, entry.result)
	}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		t.Logf("%4d  %s", counts[key], key)
	}
}

func compareLocalCheckpoint(t *testing.T, file string, warm, rounds int, sweep bool) (kind string, size int, result string) {
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	ctx := context.Background()
	sourceStore, _ := backend.NewMemorySaveStore(nil)
	keys := []uint32{'5', 0xFFFFFFFE, '5', 0xFFFFFFFC, '5', 0xFFFFFFFF, '5', 0xFFFFFFFD}
	press := func(session *Session, tick int) {
		// A key every so often moves a title off the screen it would otherwise
		// sit on for the whole run, and the same keys go to both sessions.
		if tick%25 == 10 {
			session.SendKey(true, keys[tick/25%len(keys)])
		}
		if tick%25 == 13 {
			session.SendKey(false, keys[tick/25%len(keys)])
		}
	}
	// A title whose first run installs itself and asks to be restarted has to
	// be started again over the saves that run left, the way a person would.
	var source *Session
	for run := 1; source == nil; run++ {
		started, err := StartSession(ctx, data, SessionOptions{SaveStore: sourceStore})
		if err != nil {
			if errors.Is(err, ErrGuestExited) && run < 3 {
				continue
			}
			return kind, 0, "skipped: start: " + firstLine(err)
		}
		kind = "clet"
		if started.client.javaApplication {
			kind = "java"
		}
		var failure error
		for tick := 0; tick < warm && failure == nil; tick++ {
			press(started, tick)
			failure = started.Tick(ctx)
		}
		if failure == nil {
			source = started
			break
		}
		_ = started.Close(ctx)
		if !errors.Is(failure, ErrGuestExited) {
			return kind, 0, "skipped: tick: " + firstLine(failure)
		}
		if run == 3 {
			return kind, 0, "skipped: exited before the checkpoint on three runs"
		}
	}
	defer source.Close(ctx)
	if sweep {
		return kind, 0, sweepLocalCheckpoints(t, data, source, press, warm, rounds)
	}
	checkpoint, err := source.CaptureCheckpoint(ctx)
	if err != nil {
		t.Errorf("capture refused: %v", err)
		return kind, 0, "REFUSED: " + firstLine(err)
	}
	encoded, err := backend.EncodeCheckpoint(checkpoint)
	if err != nil {
		t.Errorf("encode: %v", err)
		return kind, 0, "FAILED: encode: " + firstLine(err)
	}
	size = len(encoded)
	decoded, err := backend.DecodeCheckpoint(encoded, backend.SaveIdentity(data))
	if err != nil {
		t.Errorf("decode: %v", err)
		return kind, size, "FAILED: decode: " + firstLine(err)
	}
	restoredStore := boundarySaves(t, sourceStore)
	prepared, err := PrepareSessionCheckpoint(data, decoded, SessionOptions{SaveStore: restoredStore})
	if err != nil {
		t.Errorf("prepare: %v", err)
		return kind, size, "FAILED: prepare: " + firstLine(err)
	}
	if calls, first := prepared.PreparationStoreCalls(); calls != 0 {
		t.Errorf("validation made %d save store calls, the first being %s", calls, first)
	}
	restored, err := prepared.Commit(ctx, nil, restoredStore)
	if err != nil {
		prepared.Discard()
		t.Errorf("commit: %v", err)
		return kind, size, "FAILED: commit: " + firstLine(err)
	}
	defer restored.Close(ctx)
	if before, after := fingerprintOf(source), fingerprintOf(restored); before != after {
		t.Errorf("restored session differs before any tick: %+v, want %+v", after, before)
		return kind, size, "DIVERGED: at restore"
	}
	if before, after := memoryDigest(t, source.client), memoryDigest(t, restored.client); before != after {
		t.Errorf("restored memory differs before any tick")
		return kind, size, "DIVERGED: memory at restore"
	}
	for round := 0; round < rounds; round++ {
		tick := warm + round
		press(source, tick)
		press(restored, tick)
		sourceErr, restoredErr := source.Tick(ctx), restored.Tick(ctx)
		if (sourceErr == nil) != (restoredErr == nil) {
			t.Errorf("round %d: source %v, restored %v", round, sourceErr, restoredErr)
			return kind, size, fmt.Sprintf("DIVERGED: errors at round %d", round)
		}
		if sourceErr != nil {
			if errors.Is(sourceErr, ErrGuestExited) != errors.Is(restoredErr, ErrGuestExited) {
				t.Errorf("round %d: source %v, restored %v", round, sourceErr, restoredErr)
				return kind, size, fmt.Sprintf("DIVERGED: endings at round %d", round)
			}
			return kind, size, fmt.Sprintf("ok: both ended at round %d", round)
		}
		if before, after := fingerprintOf(source), fingerprintOf(restored); before != after {
			t.Errorf("round %d: restored %+v, want %+v", round, after, before)
			return kind, size, fmt.Sprintf("DIVERGED: at round %d", round)
		}
	}
	if before, after := memoryDigest(t, source.client), memoryDigest(t, restored.client); before != after {
		t.Errorf("guest memory differs after %d rounds", rounds)
		return kind, size, "DIVERGED: memory"
	}
	sourceSaves, _ := sourceStore.SnapshotSaves()
	restoredSaves, _ := restoredStore.SnapshotSaves()
	if difference := describeSaveDifference(sourceSaves, restoredSaves); difference != "" {
		t.Errorf("saves differ after %d rounds: %s", rounds, difference)
		return kind, size, "DIVERGED: saves"
	}
	// A restored session has to be able to be saved again.
	if _, err := restored.CaptureCheckpoint(ctx); err != nil {
		t.Errorf("recapture refused: %v", err)
		return kind, size, "REFUSED: recapture: " + firstLine(err)
	}
	return kind, size, "ok"
}

func firstLine(err error) string {
	text := err.Error()
	if cut := strings.IndexByte(text, '\n'); cut >= 0 {
		text = text[:cut]
	}
	if len(text) > 160 {
		text = text[:160]
	}
	return text
}

// describeSaveDifference names the keys two save sets disagree about, which is
// what says whether a divergence is in the title's own data or in a list this
// platform keeps beside it.
func describeSaveDifference(source, restored []backend.SaveEntry) string {
	index := func(entries []backend.SaveEntry) map[string][]byte {
		table := make(map[string][]byte, len(entries))
		for _, entry := range entries {
			table[entry.Key] = entry.Data
		}
		return table
	}
	left, right := index(source), index(restored)
	var parts []string
	for key, data := range left {
		other, found := right[key]
		switch {
		case !found:
			parts = append(parts, fmt.Sprintf("%q only in the source (%d bytes)", key, len(data)))
		case string(data) != string(other):
			parts = append(parts, fmt.Sprintf("%q differs (%d and %d bytes)", key, len(data), len(other)))
		}
	}
	for key, data := range right {
		if _, found := left[key]; !found {
			parts = append(parts, fmt.Sprintf("%q only in the restored session (%d bytes)", key, len(data)))
		}
	}
	sort.Strings(parts)
	return strings.Join(parts, "; ")
}

// sweepLocalCheckpoints takes a checkpoint after every round and prepares a
// session from each, without running it. What it answers is how often a
// boundary is one a checkpoint cannot be taken at, and why.
func sweepLocalCheckpoints(
	t *testing.T, data []byte, source *Session, press func(*Session, int), warm, rounds int,
) string {
	ctx := context.Background()
	refusals := map[string]int{}
	taken := 0
	for round := 0; round < rounds; round++ {
		checkpoint, err := source.CaptureCheckpoint(ctx)
		if err != nil {
			refusals[firstLine(err)]++
		} else {
			taken++
			store, _ := backend.NewMemorySaveStore(nil)
			prepared, err := PrepareSessionCheckpoint(data, checkpoint, SessionOptions{SaveStore: store})
			if err != nil {
				t.Errorf("round %d: a checkpoint that was taken could not be prepared: %v", round, err)
				return "FAILED: prepare: " + firstLine(err)
			}
			prepared.Discard()
		}
		press(source, warm+round)
		if err := source.Tick(ctx); err != nil {
			if errors.Is(err, ErrGuestExited) {
				break
			}
			return "skipped: tick: " + firstLine(err)
		}
	}
	if len(refusals) == 0 {
		return fmt.Sprintf("ok: %d checkpoints", taken)
	}
	reasons := make([]string, 0, len(refusals))
	for reason, count := range refusals {
		reasons = append(reasons, fmt.Sprintf("%d x %s", count, reason))
	}
	sort.Strings(reasons)
	t.Errorf("%d of %d boundaries refused: %s", len(refusals), taken+len(refusals), strings.Join(reasons, "; "))
	return fmt.Sprintf("REFUSED: %d boundaries: %s", rounds-taken, strings.Join(reasons, "; "))
}
