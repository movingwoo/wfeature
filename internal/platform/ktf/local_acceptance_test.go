package ktf

import (
	"context"
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
	parsed, native := 0, 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".zip") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(gameDirectory, entry.Name()))
		if err != nil {
			t.Errorf("read %q: %v", entry.Name(), err)
			continue
		}
		if isNativePackageArchive(data) {
			native++
			continue
		}
		if _, err := Open(data); err != nil {
			t.Errorf("parse %q: %v", entry.Name(), err)
			continue
		}
		parsed++
	}
	if parsed == 0 {
		t.Fatal("no local KTF archives parsed")
	}
	t.Logf("parsed %d local KTF archives, and passed over %d of the earlier package", parsed, native)
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

// TestLocalKTFArchivesRenderFirstFrame drives registered timer callbacks
// after startApp until the client flushes its screen framebuffer, then
// checks that the presented RGBA frame contains drawn pixels. It reports how
// many local games reach a first frame; at least one must.
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
	rendered := 0
	attempted := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".zip") {
			continue
		}
		if only != "" && !strings.Contains(entry.Name(), only) {
			continue
		}
		attempted++
		name := entry.Name()
		data, err := os.ReadFile(filepath.Join(gameDirectory, name))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if isNativePackageArchive(data) {
			continue
		}
		archive, err := Open(data)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		client, err := LoadClient(archive.JAR.Client, armcore.CoreOptions{MaxSteps: localAcceptanceMaxSteps(t)})
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		client.SetProgramName(ProgramNameForAID(archive.Descriptor.AID))
		client.AttachResources(archive.JAR.Entries)
		client.AttachFilesystem(archive.GuestFiles())
		entrySummary, err := client.ExecuteEntry(context.Background(), nil)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if _, err := client.Initialize(context.Background(), entrySummary.Context.Registers[0]); err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		object, _, err := client.NewObject(context.Background(), archive.Descriptor.MainClass, "()V")
		if err != nil {
			t.Errorf("%s construct: %v", name, err)
			continue
		}
		classAddress, err := client.runtime.ensureJavaClass("[Ljava/lang/String;")
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		metadata, ok := client.JVM().AOTClassAt(classAddress)
		if !ok {
			t.Errorf("%s: [Ljava/lang/String; is not registered", name)
			continue
		}
		argumentsAddress, err := client.runtime.allocateAOTArrayObject(metadata, 0)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		argumentsObject, _ := client.JVM().AOTObject(argumentsAddress)
		if _, err := client.InvokeVirtual(context.Background(), object, "startApp", "([Ljava/lang/String;)V", jvm.ReferenceValue(argumentsObject)); err != nil {
			t.Errorf("%s startApp: %v", name, err)
			continue
		}
		// How many service rounds a game needs before it paints anything is
		// set by how much loading it does first, and that grows as the runtime
		// gains the surfaces a game was waiting on: one title needed around a
		// hundred once its loading timer ran, and the slowest needs about
		// three hundred and thirty now that it gets as far as reading its
		// option save. The loop breaks out as soon as a game draws or stops making
		// progress, so a ceiling well clear of the slowest costs the others
		// nothing.
		rounds := 512
		if value := os.Getenv("WFEATURE_KTF_FRAME_ROUNDS"); value != "" {
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed <= 0 {
				t.Fatalf("invalid WFEATURE_KTF_FRAME_ROUNDS %q", value)
			}
			rounds = parsed
		}
		var timerErr error
		serviced := 0
		for round := 0; round < rounds; round++ {
			frame, _, _, flushes := client.Frame()
			if flushes > 0 && frameHasContent(frame) {
				break
			}
			ranTimers, err := client.ServiceTimers(context.Background(), 4)
			serviced += ranTimers
			if err != nil {
				timerErr = err
			}
			ranThreads, err := client.ServiceThreads(context.Background(), 1)
			serviced += ranThreads
			if err != nil {
				// A thread hitting the bounded ceiling may still have drawn.
				timerErr = err
			}
			painted, err := client.ServicePaint(context.Background())
			if painted {
				serviced++
			}
			if err != nil {
				timerErr = err
			}
			if ranTimers == 0 && ranThreads == 0 && !painted {
				break
			}
		}
		frame, width, height, flushes := client.Frame()
		drawn := 0
		for offset := 0; offset+3 < len(frame); offset += 4 {
			if frame[offset] != 0 || frame[offset+1] != 0 || frame[offset+2] != 0 {
				drawn++
			}
		}
		if flushes > 0 && drawn > 0 {
			rendered++
			t.Logf("%s: %dx%d frame after %d timers, %d flushes, %d lit pixels", name, width, height, serviced, flushes, drawn)
			if directory := os.Getenv("WFEATURE_KTF_FRAME_DIR"); directory != "" {
				writeFramePNG(t, filepath.Join(directory, strings.TrimSuffix(name, ".zip")+".png"), frame, width, height)
			}
		} else {
			t.Logf("%s: no frame (timers=%d flushes=%d drawn=%d timerErr=%v)\ncounts:\n%s",
				name, serviced, flushes, drawn, timerErr, formatDiagnosticCounts(client.runtime.diagnosticCounts(), 60))
		}
	}
	if attempted == 0 {
		t.Fatal("no local KTF archives attempted")
	}
	if rendered == 0 && only == "" {
		t.Fatal("no local KTF game rendered a first frame")
	}
	t.Logf("rendered first frames: %d of %d", rendered, attempted)
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
