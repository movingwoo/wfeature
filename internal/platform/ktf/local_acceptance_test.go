package ktf

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/jvm"
)

const localInitializationAcceptanceMaxSteps = 10_000_000

// The startApp probe allows more work: real games run tens of millions of
// instructions of table decompression before first returning.
const localStartAcceptanceMaxSteps = 50_000_000

// localAcceptanceMaxSteps allows WFEATURE_KTF_MAX_STEPS to widen the probe
// ceiling when investigating long-running initialization loops.
func localAcceptanceMaxSteps(t *testing.T) uint64 {
	value := os.Getenv("WFEATURE_KTF_MAX_STEPS")
	if value == "" {
		return localStartAcceptanceMaxSteps
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil || parsed == 0 {
		t.Fatalf("invalid WFEATURE_KTF_MAX_STEPS %q", value)
	}
	return parsed
}

// isNativePackageArchive reports whether an outer archive is the earlier KTF
// package — a module information file beside a raw module — rather than the
// descriptor package these probes drive. The local corpus holds both, and the
// earlier one has no __adf__ to open, so a probe that does not tell them apart
// reports a format it was never asked to handle as a parse failure. See
// native_archive.go.
func isNativePackageArchive(data []byte) bool {
	files, err := readOuterZIP(data)
	if err != nil {
		return false
	}
	return IsNativePackage(files)
}

// TestLocalKTFArchivesParse is opt-in because real games are ignored local
// data, not repository fixtures. It validates every local KTF outer archive
// without executing third-party guest code.
func TestLocalKTFArchivesParse(t *testing.T) {
	if os.Getenv("WFEATURE_KTF_ACCEPTANCE") != "1" {
		t.Skip("set WFEATURE_KTF_ACCEPTANCE=1 to parse ignored local KTF archives")
	}
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate KTF acceptance test source")
	}
	gameDirectory := filepath.Join(filepath.Dir(source), "..", "..", "..", "var", "games", "ktf")
	entries, err := os.ReadDir(gameDirectory)
	if err != nil {
		t.Fatalf("read local KTF game directory: %v", err)
	}
	// One subtest per archive, like the four probes below it. What the ladder
	// answers is where an archive stops rather than whether one did, and a
	// loop of `t.Errorf` says a number where the report needs a name.
	found := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".zip") {
			continue
		}
		found++
		t.Run(entry.Name(), func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(gameDirectory, entry.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if isNativePackageArchive(data) {
				t.Skip("the earlier KTF package, which carries no JAR to parse")
			}
			if _, err := Open(data); err != nil {
				t.Fatalf("parse: %v", err)
			}
		})
	}
	if found == 0 {
		t.Fatal("no local KTF archives to parse")
	}
}

// TestLocalKTFArchivesInitialize is a separate opt-in probe because it runs
// ignored third-party guest code. It stops after each client has performed its
// self-relocation and returned the top-level executable descriptor, then calls
// its bounded interface and WIPI initialization functions. No game lifecycle
// callback is invoked.
func TestLocalKTFArchivesInitialize(t *testing.T) {
	if os.Getenv("WFEATURE_KTF_EXECUTE_ACCEPTANCE") != "1" {
		t.Skip("set WFEATURE_KTF_EXECUTE_ACCEPTANCE=1 to execute ignored local KTF client entry points")
	}
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate KTF acceptance test source")
	}
	gameDirectory := filepath.Join(filepath.Dir(source), "..", "..", "..", "var", "games", "ktf")
	entries, err := os.ReadDir(gameDirectory)
	if err != nil {
		t.Fatalf("read local KTF game directory: %v", err)
	}
	executed := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".zip") {
			continue
		}
		t.Run(entry.Name(), func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(gameDirectory, entry.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if isNativePackageArchive(data) {
				t.Skip("the earlier KTF package, which these probes do not drive")
			}
			archive, err := Open(data)
			if err != nil {
				t.Fatal(err)
			}
			client, err := LoadClient(archive.JAR.Client, armcore.CoreOptions{MaxSteps: localInitializationAcceptanceMaxSteps})
			if err != nil {
				t.Fatal(err)
			}
			summary, err := client.ExecuteEntry(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			result := summary.Context.Registers[0]
			imageEnd := uint64(ImageBase) + archive.JAR.Client.MappedSize()
			if uint64(result) < uint64(ImageBase) || uint64(result) >= imageEnd || result&3 != 0 {
				t.Fatalf("entry result = %#x, want aligned descriptor in [%#x, %#x)", result, ImageBase, imageEnd)
			}
			executable, err := client.ReadExecutable(result)
			if err != nil {
				t.Fatal(err)
			}
			initialization, err := client.Initialize(context.Background(), result)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf(
				"initialized %q/%q descriptor %#x: entry=%d interface=%d WIPI=%d callbacks=%+v",
				executable.Name,
				executable.Interface.Name,
				result,
				summary.Steps,
				initialization.Interface.Steps,
				initialization.WIPI.Steps,
				initialization.Callbacks,
			)
		})
		executed++
	}
	if executed == 0 {
		t.Fatal("no local KTF archive entry points executed")
	}
}

// TestLocalKTFArchivesLoadMainClass extends the explicit third-party execution
// probe through ExeInterface.GetClass for each archive's ADF MClass. It does
// not instantiate the class or invoke lifecycle methods.
func TestLocalKTFArchivesLoadMainClass(t *testing.T) {
	if os.Getenv("WFEATURE_KTF_LIFECYCLE_ACCEPTANCE") != "1" {
		t.Skip("set WFEATURE_KTF_LIFECYCLE_ACCEPTANCE=1 to load ignored local KTF main classes")
	}
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate KTF acceptance test source")
	}
	gameDirectory := filepath.Join(filepath.Dir(source), "..", "..", "..", "var", "games", "ktf")
	entries, err := os.ReadDir(gameDirectory)
	if err != nil {
		t.Fatalf("read local KTF game directory: %v", err)
	}
	executed := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".zip") {
			continue
		}
		t.Run(entry.Name(), func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(gameDirectory, entry.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if isNativePackageArchive(data) {
				t.Skip("the earlier KTF package, which these probes do not drive")
			}
			archive, err := Open(data)
			if err != nil {
				t.Fatal(err)
			}
			client, err := LoadClient(archive.JAR.Client, armcore.CoreOptions{MaxSteps: localInitializationAcceptanceMaxSteps})
			if err != nil {
				t.Fatal(err)
			}
			entrySummary, err := client.ExecuteEntry(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.Initialize(context.Background(), entrySummary.Context.Registers[0]); err != nil {
				t.Fatal(err)
			}
			loaded, err := client.LoadClass(context.Background(), archive.Descriptor.MainClass)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf(
				"loaded MClass %s at %#x: steps=%d methods=%v fields=%d callbacks=%+v",
				loaded.Metadata.Name,
				loaded.Metadata.Address,
				loaded.Run.Steps,
				aotMethodNames(loaded.Metadata.Methods),
				len(loaded.Metadata.Fields),
				loaded.Callbacks,
			)
		})
		executed++
	}
	if executed == 0 {
		t.Fatal("no local KTF main classes loaded")
	}
}

// TestLocalKTFArchivesConstructMainClass extends the explicit local lifecycle
// probe through allocation and the ADF MClass no-argument constructor. It does
// not call startApp or any display/input lifecycle callback.
func TestLocalKTFArchivesConstructMainClass(t *testing.T) {
	if os.Getenv("WFEATURE_KTF_CONSTRUCT_ACCEPTANCE") != "1" {
		t.Skip("set WFEATURE_KTF_CONSTRUCT_ACCEPTANCE=1 to construct ignored local KTF main classes")
	}
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate KTF acceptance test source")
	}
	gameDirectory := filepath.Join(filepath.Dir(source), "..", "..", "..", "var", "games", "ktf")
	entries, err := os.ReadDir(gameDirectory)
	if err != nil {
		t.Fatalf("read local KTF game directory: %v", err)
	}
	executed := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".zip") {
			continue
		}
		t.Run(entry.Name(), func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(gameDirectory, entry.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if isNativePackageArchive(data) {
				t.Skip("the earlier KTF package, which these probes do not drive")
			}
			archive, err := Open(data)
			if err != nil {
				t.Fatal(err)
			}
			client, err := LoadClient(archive.JAR.Client, armcore.CoreOptions{MaxSteps: localInitializationAcceptanceMaxSteps})
			if err != nil {
				t.Fatal(err)
			}
			client.AttachResources(archive.JAR.Entries)
			client.AttachFilesystem(archive.GuestFiles())
			entrySummary, err := client.ExecuteEntry(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.Initialize(context.Background(), entrySummary.Context.Registers[0]); err != nil {
				t.Fatal(err)
			}
			object, constructed, err := client.NewObject(context.Background(), archive.Descriptor.MainClass, "()V")
			if err != nil {
				t.Fatalf("%v; runs=%+v callbacks=%+v\ncounts:\n%s", err, constructed.Runs, client.runtime.callbacks, formatDiagnosticCounts(client.runtime.diagnosticCounts(), 2000))
			}
			address, ok := client.JVM().AOTAddress(object)
			if !ok {
				t.Fatal("constructed KTF main object has no guest address")
			}
			steps := uint64(0)
			for _, run := range constructed.Runs {
				steps += run.Steps
			}
			t.Logf("constructed MClass %s at %#x: runs=%d steps=%d callbacks=%+v", object.ClassName, address, len(constructed.Runs), steps, client.runtime.callbacks)
		})
		executed++
	}
	if executed == 0 {
		t.Fatal("no local KTF main classes constructed")
	}
}

// TestLocalKTFArchivesStartMainClass extends the local lifecycle probe through
// startApp([Ljava/lang/String;)V with an empty argument array, matching the
// original runtime's launcher call.
func TestLocalKTFArchivesStartMainClass(t *testing.T) {
	if os.Getenv("WFEATURE_KTF_START_ACCEPTANCE") != "1" {
		t.Skip("set WFEATURE_KTF_START_ACCEPTANCE=1 to start ignored local KTF main classes")
	}
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate KTF acceptance test source")
	}
	gameDirectory := filepath.Join(filepath.Dir(source), "..", "..", "..", "var", "games", "ktf")
	entries, err := os.ReadDir(gameDirectory)
	if err != nil {
		t.Fatalf("read local KTF game directory: %v", err)
	}
	executed := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".zip") {
			continue
		}
		t.Run(entry.Name(), func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(gameDirectory, entry.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if isNativePackageArchive(data) {
				t.Skip("the earlier KTF package, which these probes do not drive")
			}
			archive, err := Open(data)
			if err != nil {
				t.Fatal(err)
			}
			client, err := LoadClient(archive.JAR.Client, armcore.CoreOptions{MaxSteps: localAcceptanceMaxSteps(t)})
			if err != nil {
				t.Fatal(err)
			}
			client.SetProgramName(ProgramNameForAID(archive.Descriptor.AID))
			client.AttachResources(archive.JAR.Entries)
			client.AttachFilesystem(archive.GuestFiles())
			entrySummary, err := client.ExecuteEntry(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.Initialize(context.Background(), entrySummary.Context.Registers[0]); err != nil {
				t.Fatal(err)
			}
			object, _, err := client.NewObject(context.Background(), archive.Descriptor.MainClass, "()V")
			if err != nil {
				t.Fatalf("construct: %v\ncounts:\n%s", err, formatDiagnosticCounts(client.runtime.diagnosticCounts(), 2000))
			}
			classAddress, err := client.runtime.ensureJavaClass("[Ljava/lang/String;")
			if err != nil {
				t.Fatal(err)
			}
			metadata, ok := client.JVM().AOTClassAt(classAddress)
			if !ok {
				t.Fatal("[Ljava/lang/String; is not registered")
			}
			argumentsAddress, err := client.runtime.allocateAOTArrayObject(metadata, 0)
			if err != nil {
				t.Fatal(err)
			}
			argumentsObject, ok := client.JVM().AOTObject(argumentsAddress)
			if !ok {
				t.Fatal("startApp arguments array is not bound")
			}
			started, err := client.InvokeVirtual(context.Background(), object, "startApp", "([Ljava/lang/String;)V", jvm.ReferenceValue(argumentsObject))
			if err != nil {
				t.Fatalf("startApp: %v; runs=%d\nsite hints:\n%s\ncounts:\n%s",
					err, len(started.Runs),
					errorSiteHints(client.JVM(), err),
					formatDiagnosticCounts(client.runtime.diagnosticCounts(), 2000))
			}
			steps := uint64(0)
			for _, run := range started.Runs {
				steps += run.Steps
			}
			t.Logf("started MClass %s: runs=%d steps=%d callbacks=%+v", object.ClassName, len(started.Runs), steps, client.runtime.callbacks)
		})
		executed++
	}
	if executed == 0 {
		t.Fatal("no local KTF main classes started")
	}
}

// TestLocalKTFArchivesRenderFirstFrame starts every JAR-packaged local
// archive and ticks it the way a Host does until the guest flushes a screen
// with something lit in it. It reports how many reach a first frame; at least
// one must.
//
// The loop is `Session.Tick` rather than a service loop of its own. An earlier
// version called ServiceTimers, ServiceThreads and ServicePaint by hand and
// broke out of the first round that ran none of them, which reported one
// archive as drawing nothing while the same archive under `runktf` flushed a
// full screen on its second tick: what that title waits for is a deadline, and
// a round before the first one is due does nothing at all. Ticking is also the
// only way the probe answers for the path a Host actually takes — a paint that
// a round skips, a wait the client thread declared, the guest exit.
func TestLocalKTFArchivesRenderFirstFrame(t *testing.T) {
	if os.Getenv("WFEATURE_KTF_FRAME_ACCEPTANCE") != "1" {
		t.Skip("set WFEATURE_KTF_FRAME_ACCEPTANCE=1 to render ignored local KTF first frames")
	}
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate KTF acceptance test source")
	}
	gameDirectory := filepath.Join(filepath.Dir(source), "..", "..", "..", "var", "games", "ktf")
	entries, err := os.ReadDir(gameDirectory)
	if err != nil {
		t.Fatalf("read local KTF game directory: %v", err)
	}
	only := os.Getenv("WFEATURE_KTF_ONLY")
	// How many ticks a game needs before it paints anything is set by how much
	// loading it does first, and that grows as the runtime gains the surfaces a
	// game was waiting on. The loop stops as soon as a game draws or has
	// nothing left due, so a ceiling well clear of the slowest costs the
	// others nothing.
	ticks := 512
	if value := os.Getenv("WFEATURE_KTF_FRAME_ROUNDS"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed <= 0 {
			t.Fatalf("invalid WFEATURE_KTF_FRAME_ROUNDS %q", value)
		}
		ticks = parsed
	}
	rendered := 0
	attempted := 0
	skipped := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".zip") {
			continue
		}
		if only != "" && !strings.Contains(entry.Name(), only) {
			continue
		}
		name := entry.Name()
		// One subtest per archive, like the rungs below this one. **An
		// archive that draws nothing fails here rather than being logged**:
		// this probe used to pass as long as any archive drew, which made a
		// title that stopped painting a line in a log nobody reads. The
		// count it printed is what `make acceptance` writes down now, per
		// archive and with the date on it.
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(gameDirectory, name))
			if err != nil {
				t.Fatal(err)
			}
			// The earlier native package carries no JAR and no main class, so
			// it has nothing for this probe to start;
			// TestLocalKTFNativePackageRuns is what exercises that generation.
			if isNativePackageArchive(data) {
				skipped++
				t.Skip("the earlier KTF package, which this probe does not drive")
			}
			attempted++
			// A probe measures what the guest computes, not how long it takes,
			// so it runs a manual clock it jumps to each next deadline: the
			// same sequence of guest work at no real cost. This is what
			// `runktf` does without -play.
			clock := NewManualClock(time.Time{})
			session, err := StartSession(context.Background(), data, SessionOptions{
				MaxSteps: localAcceptanceMaxSteps(t),
				Clock:    clock,
			})
			if err != nil {
				t.Fatalf("start: %v", err)
			}
			defer session.Close()
			ran := 0
			var tickErr error
			for ; ran < ticks; ran++ {
				frame, _, _, flushes := session.Frame()
				if flushes > 0 && frameHasContent(frame) {
					break
				}
				progressed, err := session.Tick(context.Background())
				if err != nil {
					// A game that ended on its own terms is not a failure, and
					// what it drew before it did still counts.
					if !errors.Is(err, ErrGuestExited) {
						tickErr = err
					}
					break
				}
				session.SkipToNextDeadline()
				if !progressed {
					// A round that did nothing is only an idle game when
					// nothing is due: a title whose loop is one repeating timer
					// spends every round before the first one is due doing
					// nothing.
					if _, pending := session.NextDeadline(); !pending {
						break
					}
				}
			}
			frame, width, height, flushes := session.Frame()
			drawn := 0
			for offset := 0; offset+3 < len(frame); offset += 4 {
				if frame[offset] != 0 || frame[offset+1] != 0 || frame[offset+2] != 0 {
					drawn++
				}
			}
			if flushes == 0 || drawn == 0 {
				t.Fatalf("no frame (ticks=%d flushes=%d drawn=%d tickErr=%v)\ncounts:\n%s",
					ran, flushes, drawn, tickErr, formatDiagnosticCounts(session.Client.runtime.diagnosticCounts(), 60))
			}
			rendered++
			t.Logf("%dx%d frame after %d ticks, %d flushes, %d lit pixels", width, height, ran, flushes, drawn)
			if directory := os.Getenv("WFEATURE_KTF_FRAME_DIR"); directory != "" {
				writeFramePNG(t, filepath.Join(directory, strings.TrimSuffix(name, ".zip")+".png"), frame, width, height)
			}
		})
	}
	if attempted == 0 {
		t.Fatal("no local KTF archives attempted")
	}
	// The denominator is the archives this probe can start. Archives of the
	// earlier generation are reported beside it rather than inside it.
	t.Logf("rendered first frames: %d of %d JAR-packaged archives (%d earlier-package archives skipped)",
		rendered, attempted, skipped)
}

func frameHasContent(frame []byte) bool {
	for offset := 0; offset+3 < len(frame); offset += 4 {
		if frame[offset] != 0 || frame[offset+1] != 0 || frame[offset+2] != 0 {
			return true
		}
	}
	return false
}

// writeFramePNG saves one probe frame for visual inspection when
// WFEATURE_KTF_FRAME_DIR is set.
func writeFramePNG(t *testing.T, path string, frame []byte, width, height int) {
	t.Helper()
	if width <= 0 || height <= 0 || len(frame) < width*height*4 {
		return
	}
	target := image.NewRGBA(image.Rect(0, 0, width, height))
	copy(target.Pix, frame)
	file, err := os.Create(path)
	if err != nil {
		t.Logf("write frame %s: %v", path, err)
		return
	}
	defer file.Close()
	if err := png.Encode(file, target); err != nil {
		t.Logf("encode frame %s: %v", path, err)
	}
}

// errorSiteHints maps every guest address mentioned in an execution error to
// the nearest registered AOT method, so crash sites name their methods.
func errorSiteHints(vm *jvm.VM, err error) string {
	if err == nil {
		return ""
	}
	seen := map[uint64]bool{}
	var builder strings.Builder
	for _, match := range regexp.MustCompile(`0x[0-9a-fA-F]+`).FindAllString(err.Error(), 16) {
		address, parseErr := strconv.ParseUint(strings.TrimPrefix(match, "0x"), 16, 64)
		if parseErr != nil || address == 0 || address > 0xffffffff || seen[address] {
			continue
		}
		seen[address] = true
		fmt.Fprintf(&builder, "  %s: %s\n", match, nearestAOTMethod(vm, uint32(address)))
	}
	return builder.String()
}

// nearestAOTMethod names the registered method whose body starts closest at
// or below the supplied guest address, for mapping crash sites to methods.
func nearestAOTMethod(vm *jvm.VM, address uint32) string {
	best := ""
	bestBody := uint32(0)
	for _, class := range vm.AOTClasses() {
		for _, method := range class.Methods {
			for _, body := range []uint32{method.Body &^ 1, method.NativeBody &^ 1} {
				if body == 0 || body > address || address-body > 0x4000 {
					continue
				}
				if body > bestBody {
					bestBody = body
					best = fmt.Sprintf("%s.%s%s body=%#x (+%#x)", class.Name, method.Name, method.Descriptor, body, address-body)
				}
			}
		}
	}
	if best == "" {
		return fmt.Sprintf("no method near %#x", address)
	}
	return best
}

func formatDiagnosticCounts(counts map[string]uint32, limit int) string {
	type entry struct {
		name  string
		count uint32
	}
	entries := make([]entry, 0, len(counts))
	for name, count := range counts {
		entries = append(entries, entry{name, count})
	}
	sort.Slice(entries, func(left, right int) bool {
		if entries[left].count != entries[right].count {
			return entries[left].count > entries[right].count
		}
		return entries[left].name < entries[right].name
	})
	if len(entries) > limit {
		entries = entries[:limit]
	}
	var builder strings.Builder
	for _, current := range entries {
		fmt.Fprintf(&builder, "  %8d %s\n", current.count, current.name)
	}
	return builder.String()
}

func aotMethodNames(methods []jvm.AOTMethodMetadata) []string {
	names := make([]string, len(methods))
	for index, method := range methods {
		names[index] = fmt.Sprintf("%s%s", method.Name, method.Descriptor)
	}
	return names
}
