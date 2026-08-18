package ktf

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// buildNativeInfo assembles a module information file the way a real one is
// laid out, so the parser is exercised against the shape rather than against
// one recorded byte string.
func buildNativeInfo(t *testing.T, spans [][]byte, trailer []uint32) []byte {
	t.Helper()
	const tableOffset = 0x48
	header := make([]byte, tableOffset)
	table := make([]byte, (len(spans)+1)*4)
	body := &bytes.Buffer{}
	start := uint32(tableOffset + len(table))
	for index, span := range spans {
		binary.LittleEndian.PutUint32(table[index*4:], start+uint32(body.Len()))
		body.Write(span)
		_ = index
	}
	binary.LittleEndian.PutUint32(table[len(spans)*4:], start+uint32(body.Len()))
	// The two header sections sit between the fixed fields and the span table.
	// A fixture that leaves them empty would leave the record decoder with
	// nothing to read, so the second one is placed where a real file puts it.
	binary.LittleEndian.PutUint32(header[0x08:], 0x20)
	binary.LittleEndian.PutUint32(header[0x0c:], 0x28)
	binary.LittleEndian.PutUint32(header[0x10:], tableOffset)
	binary.LittleEndian.PutUint32(header[0x14:], uint32(len(spans)))
	binary.LittleEndian.PutUint32(header[0x18:], binary.LittleEndian.Uint32(table))
	out := &bytes.Buffer{}
	out.Write(header)
	out.Write(table)
	out.Write(body.Bytes())
	for _, word := range trailer {
		_ = binary.Write(out, binary.LittleEndian, word)
	}
	out.WriteString(nativeInfoTrailerMagic)
	return out.Bytes()
}

func nativeTextSpan(text string) []byte {
	return append([]byte{0xfe, 0xfe}, encodeEUCKR(text)...)
}

func nativeTypedSpan(mime string, payload []byte) []byte {
	span := make([]byte, 2)
	binary.LittleEndian.PutUint16(span, uint16(len(mime)+3))
	span = append(span, mime...)
	span = append(span, 0)
	return append(span, payload...)
}

func nativeNumericSpan(words ...uint32) []byte {
	span := make([]byte, len(words)*4)
	for index, word := range words {
		binary.LittleEndian.PutUint32(span[index*4:], word)
	}
	return span
}

// TestParseNativeInfoReadsHeaderFieldsOnTheirStride covers the record stride
// the header sections are written at. Reading them as bare aligned words is
// what made an earlier pass map four times the module's size: the boundary
// between two records reads as a value of its own.
func TestParseNativeInfoReadsHeaderFieldsOnTheirStride(t *testing.T) {
	data := buildNativeInfo(t, [][]byte{nativeNumericSpan(1)}, []uint32{0, 1})
	// Two records in the second section: one carrying nothing established,
	// and the module's page-rounded length under its own tag.
	binary.LittleEndian.PutUint32(data[0x28:], 0x03e80001)
	binary.LittleEndian.PutUint16(data[0x2c:], 0)
	binary.LittleEndian.PutUint16(data[0x2e:], 4)
	binary.LittleEndian.PutUint32(data[0x30:], 2*nativePageSize)
	binary.LittleEndian.PutUint16(data[0x34:], 0)
	binary.LittleEndian.PutUint16(data[0x36:], NativeFieldModuleLength)

	info, err := ParseNativeInfo(data)
	if err != nil {
		t.Fatalf("parse native info: %v", err)
	}
	field, ok := info.Field(NativeFieldModuleLength)
	if !ok || field.Value != 2*nativePageSize {
		t.Fatalf("tag %d = %+v (%v), want the module length", NativeFieldModuleLength, field, ok)
	}
	if other, ok := info.Field(4); !ok || other.Value != 0x03e80001 {
		t.Errorf("tag 4 = %+v (%v), want the record beside it", other, ok)
	}
	if _, ok := info.Field(3); ok {
		t.Error("a tag no record carries was answered")
	}
	// The size the loader maps comes from the tag, and the search that used to
	// find it still corroborates: both answer the same number.
	mapped, ok := info.ModuleSpan(2*nativePageSize - 8)
	if !ok || mapped != 2*nativePageSize {
		t.Fatalf("ModuleSpan = %#x (%v), want %#x", mapped, ok, 2*nativePageSize)
	}
}

func TestParseNativeInfo(t *testing.T) {
	icon := []byte("BM\x00\x01icon bytes")
	data := buildNativeInfo(t, [][]byte{
		nativeTypedSpan("image/bmp", icon),
		nativeTextSpan("A Vendor"),
		nativeTextSpan("A Vendor"),
		nativeTextSpan("제목"),
		nativeNumericSpan(1, 2, ImageBase),
	}, []uint32{0x1234, 18933, 0xff})

	info, err := ParseNativeInfo(data)
	if err != nil {
		t.Fatalf("parse native info: %v", err)
	}
	if info.Vendor != "A Vendor" {
		t.Errorf("vendor = %q, want %q", info.Vendor, "A Vendor")
	}
	if info.Name != "제목" {
		t.Errorf("name = %q, want %q", info.Name, "제목")
	}
	if info.ApplicationID != 18933 {
		t.Errorf("application id = %d, want %d", info.ApplicationID, 18933)
	}
	if len(info.Icons) != 1 || info.Icons[0].MIME != "image/bmp" || !bytes.Equal(info.Icons[0].Data, icon) {
		t.Errorf("icons = %+v, want one image/bmp carrying the payload", info.Icons)
	}
	if len(info.Records) != 1 || len(info.Records[0]) != 3 || info.Records[0][2] != ImageBase {
		t.Errorf("records = %+v, want one three-word record ending at the image base", info.Records)
	}
}

func TestParseNativeInfoRejectsDamage(t *testing.T) {
	good := buildNativeInfo(t, [][]byte{nativeNumericSpan(1)}, []uint32{0, 1})
	for _, testCase := range []struct {
		name string
		data []byte
		want string
	}{
		{name: "short", data: good[:0x10], want: "too short"},
		{name: "no trailer", data: append(append([]byte{}, good[:len(good)-4]...), "zzzz"...), want: "does not end with"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := ParseNativeInfo(testCase.data); err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("error = %v, want one containing %q", err, testCase.want)
			}
		})
	}

	t.Run("span table past the end", func(t *testing.T) {
		damaged := append([]byte{}, good...)
		binary.LittleEndian.PutUint32(damaged[0x14:], maxNativeInfoSpans)
		if _, err := ParseNativeInfo(damaged); err == nil {
			t.Fatal("a span table running past the file parsed")
		}
	})

	t.Run("spans out of order", func(t *testing.T) {
		damaged := append([]byte{}, good...)
		table := binary.LittleEndian.Uint32(damaged[0x10:])
		binary.LittleEndian.PutUint32(damaged[table+4:], 0)
		if _, err := ParseNativeInfo(damaged); err == nil {
			t.Fatal("a span ending before it starts parsed")
		}
	})
}

// TestLocalKTFNativePackageParses is opt-in because real games are ignored
// local data. It parses without executing anything.
func TestLocalKTFNativePackageParses(t *testing.T) {
	if os.Getenv("WFEATURE_KTF_NATIVE_ACCEPTANCE") != "1" {
		t.Skip("set WFEATURE_KTF_NATIVE_ACCEPTANCE=1 to parse ignored local KTF native packages")
	}
	for _, path := range localNativePackages(t) {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", filepath.Base(path), err)
		}
		archive, err := OpenNative(data)
		if err != nil {
			t.Fatalf("open %s: %v", filepath.Base(path), err)
		}
		t.Logf("%s: module %q %d bytes, id %d, vendor %q, name %q, %d icons, %d numeric records, trailer %v",
			filepath.Base(path), archive.ModuleName, len(archive.Module), archive.Info.ApplicationID,
			archive.Info.Vendor, archive.Info.Name, len(archive.Info.Icons), len(archive.Info.Records), archive.Info.Trailer)
		for _, field := range archive.Info.Fields {
			t.Logf("  header field tag %d extra %d value %#x", field.Tag, field.Extra, field.Value)
		}
		for index, record := range archive.Info.Records {
			t.Logf("  record %d: %#x", index, record)
		}
		for name, contents := range archive.Files {
			t.Logf("  file %s: %d bytes", name, len(contents))
		}
	}
}

// localNativePackages lists the ignored local archives that carry this package
// rather than the descriptor package.
func localNativePackages(t *testing.T) []string {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate KTF test source")
	}
	directory := filepath.Join(filepath.Dir(source), "..", "..", "..", "var", "games", "ktf")
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Skipf("read local KTF game directory: %v", err)
	}
	found := []string{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".zip") {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		files, err := readOuterZIP(data)
		if err != nil {
			continue
		}
		if IsNativePackage(files) {
			found = append(found, path)
		}
	}
	if len(found) == 0 {
		t.Skip("no local KTF native package present")
	}
	return found
}

// TestLocalKTFNativePackageRuns boots a local module against the platform half
// and runs the frame the title registers.
//
// It asserts what is established: the start-up protocol completes, the title
// loads the files it names, it asks to be called back, and its frames run and
// draw. It asserts nothing about the slots that are still traps — that list is
// the output, and it is the specification for what implementing this package
// still costs. **Read the log as well as the exit status.**
func TestLocalKTFNativePackageRuns(t *testing.T) {
	if os.Getenv("WFEATURE_KTF_NATIVE_ACCEPTANCE") != "1" {
		t.Skip("set WFEATURE_KTF_NATIVE_ACCEPTANCE=1 to execute ignored local KTF native modules")
	}
	for _, path := range localNativePackages(t) {
		name := filepath.Base(path)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		archive, err := OpenNative(data)
		if err != nil {
			t.Fatalf("open %s: %v", name, err)
		}
		client, err := LoadNativeClient(archive, armcore.CoreOptions{MaxSteps: localAcceptanceMaxSteps(t)})
		if err != nil {
			t.Fatalf("load %s: %v", name, err)
		}
		// A manual clock makes the frame the title asked for cost nothing:
		// this measures what the guest computes, not how long it takes.
		clock := NewManualClock(time.Time{})
		platform := NewNativePlatform(client, archive, clock)
		// The title's music is the only thing a run can check about sound
		// without a person listening, and a count of nothing is what a silent
		// platform produced before the player existed.
		sink := &countingAudioSink{}
		platform.AttachAudio(sink)
		if err := platform.Boot(context.Background()); err != nil {
			t.Fatalf("%s: boot: %v", name, err)
		}
		if platform.FrameInterval() == 0 {
			t.Fatalf("%s: the title registered no frame", name)
		}
		t.Logf("%s: booted, frame every %v", name, platform.FrameInterval())
		for _, open := range platform.FileOpens() {
			t.Logf("  opened %-16q mode %d found=%v", open.Name, open.Mode, open.Found)
		}
		frames := 0
		for round := 0; round < localNativeFrames; round++ {
			clock.Advance(platform.FrameInterval())
			ran, err := platform.Tick(context.Background())
			if err != nil {
				t.Fatalf("%s: frame %d: %v", name, frames, err)
			}
			if ran {
				frames++
			}
		}
		_, presents := platform.Frame()
		t.Logf("%s: %d frames, %d draws, %d frames ended, %d images", name, frames, platform.Draws(), presents, len(platform.images))
		if frames != localNativeFrames {
			t.Errorf("%s: %d of %d frames ran", name, frames, localNativeFrames)
		}
		if platform.Draws() == 0 {
			t.Errorf("%s: the title drew nothing", name)
		}
		if presents == 0 {
			t.Errorf("%s: the title ended no frame", name)
		}
		if platform.Missed() != 0 {
			t.Errorf("%s: %d blits named an image the platform did not build", name, platform.Missed())
		}

		// The route into the game, in the title's own terms: the select key
		// opens the menu from the title screen, again enters the first item,
		// the left key moves to the second of its two choices, and select
		// twice more picks a difficulty and starts. Driving it is what proves
		// the input contract, because every screen after the first is one the
		// title only reaches by being played.
		images, draws := len(platform.images), platform.Draws()
		for _, step := range []struct {
			key    uint32
			frames int
		}{
			{NativeKeySelect, 60},
			{NativeKeySelect, 60},
			{NativeKeyLeft, 30},
			{NativeKeySelect, 120},
			{NativeKeySelect, 400},
		} {
			if err := platform.Key(context.Background(), step.key, true); err != nil {
				t.Fatalf("%s: key %#x down: %v", name, step.key, err)
			}
			if err := platform.Key(context.Background(), step.key, false); err != nil {
				t.Fatalf("%s: key %#x up: %v", name, step.key, err)
			}
			for round := 0; round < step.frames; round++ {
				clock.Advance(platform.FrameInterval())
				if _, err := platform.Tick(context.Background()); err != nil {
					t.Fatalf("%s: frame after key %#x: %v", name, step.key, err)
				}
			}
		}
		t.Logf("%s: after the menu route, %d draws and %d images", name, platform.Draws(), len(platform.images))
		if len(platform.images) <= images {
			t.Errorf("%s: the menu route loaded no further images (%d, was %d)", name, len(platform.images), images)
		}
		if platform.Draws() <= draws*4 {
			t.Errorf("%s: the menu route drew %d, barely more than the %d before it", name, platform.Draws(), draws)
		}

		if sink.messages == 0 {
			t.Errorf("%s: the title played no note", name)
		}
		if platform.ClipRefusals() != 0 {
			t.Errorf("%s: %d clips were refused", name, platform.ClipRefusals())
		}
		t.Logf("%s: %d MIDI messages, %d wave samples, %d status messages %q",
			name, sink.messages, sink.samples, len(platform.Messages()), platform.Messages())

		summary := client.SlotSummary()
		t.Logf("%s: %d calls over %d distinct slots", name, len(client.Calls()), len(summary))
		served := map[nativeSlotKey]bool{}
		for _, call := range client.Calls() {
			if call.Served {
				served[nativeSlotKey{surface: call.Surface, slot: call.Slot}] = true
			}
		}
		for _, entry := range summary {
			mark := "trap"
			if served[nativeSlotKey{surface: entry.Surface, slot: entry.Slot}] {
				mark = "served"
			}
			t.Logf("  %-6s %-20s offset %#04x (slot %3d) called %d times, first from %#x",
				mark, entry.Surface, entry.Offset, entry.Slot, entry.Count, entry.First)
		}
	}
}

// TestLocalKTFNativePackageScreenshot writes what the title draws to a PNG,
// after driving a key route into it. It is the eye on this package: the
// acceptance run above says a title drew and how much, and only a picture says
// whether what it drew is the screen it should be.
//
//	WFEATURE_KTF_NATIVE_ACCEPTANCE=1 \
//	WFEATURE_KTF_NATIVE_SHOT=/tmp/frame.png \
//	WFEATURE_KTF_NATIVE_ROUTE=e035:60,e035:60,e034:30,e035:120,e035:400 \
//	go test ./internal/platform/ktf -run TestLocalKTFNativePackageScreenshot
//
// A route step is a key code in hex and how many of the title's frames to run
// after it. WFEATURE_KTF_NATIVE_FRAMES sets the frames before the route.
func TestLocalKTFNativePackageScreenshot(t *testing.T) {
	out := os.Getenv("WFEATURE_KTF_NATIVE_SHOT")
	if os.Getenv("WFEATURE_KTF_NATIVE_ACCEPTANCE") != "1" || out == "" {
		t.Skip("set WFEATURE_KTF_NATIVE_ACCEPTANCE=1 and WFEATURE_KTF_NATIVE_SHOT")
	}
	frames := localNativeFrames
	if value := os.Getenv("WFEATURE_KTF_NATIVE_FRAMES"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			t.Fatalf("invalid WFEATURE_KTF_NATIVE_FRAMES %q", value)
		}
		frames = parsed
	}
	for _, path := range localNativePackages(t) {
		name := filepath.Base(path)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		archive, err := OpenNative(data)
		if err != nil {
			t.Fatal(err)
		}
		client, err := LoadNativeClient(archive, armcore.CoreOptions{MaxSteps: localAcceptanceMaxSteps(t)})
		if err != nil {
			t.Fatal(err)
		}
		clock := NewManualClock(time.Time{})
		platform := NewNativePlatform(client, archive, clock)
		if err := platform.Boot(context.Background()); err != nil {
			t.Fatal(err)
		}
		run := func(count int) {
			for round := 0; round < count; round++ {
				clock.Advance(platform.FrameInterval())
				if _, err := platform.Tick(context.Background()); err != nil {
					t.Fatalf("%s: frame: %v", name, err)
				}
			}
		}
		run(frames)
		for _, step := range strings.Split(os.Getenv("WFEATURE_KTF_NATIVE_ROUTE"), ",") {
			step = strings.TrimSpace(step)
			if step == "" {
				continue
			}
			codeText, framesText, _ := strings.Cut(step, ":")
			code, err := strconv.ParseUint(strings.TrimPrefix(codeText, "0x"), 16, 32)
			if err != nil {
				t.Fatalf("route step %q: %v", step, err)
			}
			hold, err := strconv.Atoi(framesText)
			if err != nil {
				t.Fatalf("route step %q: %v", step, err)
			}
			if err := platform.Key(context.Background(), uint32(code), true); err != nil {
				t.Fatal(err)
			}
			if err := platform.Key(context.Background(), uint32(code), false); err != nil {
				t.Fatal(err)
			}
			run(hold)
		}
		frame, presents := platform.Frame()
		t.Logf("%s: %d draws, %d frames ended, %d images", name, platform.Draws(), presents, len(platform.images))
		file, err := os.Create(out)
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(file, frame); err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		t.Logf("%s: wrote %s", name, out)
	}
}

// countingAudioSink counts what a title played without keeping any of it.
type countingAudioSink struct {
	messages int
	samples  int
}

func (sink *countingAudioSink) PlayWave(_ uint8, _ uint32, samples []int16) {
	sink.samples += len(samples)
}
func (sink *countingAudioSink) MIDINoteOn(uint8, uint8, uint8)        { sink.messages++ }
func (sink *countingAudioSink) MIDINoteOff(uint8, uint8, uint8)       { sink.messages++ }
func (sink *countingAudioSink) MIDIProgramChange(uint8, uint8)        { sink.messages++ }
func (sink *countingAudioSink) MIDIControlChange(uint8, uint8, uint8) { sink.messages++ }
func (sink *countingAudioSink) MIDIPitchBend(uint8, uint16)           { sink.messages++ }
func (sink *countingAudioSink) MIDISysEx([]byte)                      { sink.messages++ }

// localNativeFrames is how many of the title's own frames the acceptance run
// drives. It is well past the point the title finishes loading and starts
// drawing, and short enough that the run stays under a second.
const localNativeFrames = 300

// logTraceShape reports where a traced run spent its instructions, as module
// offsets. A run that never leaves a few hundred bytes has not started a game.
func logTraceShape(t *testing.T, trace []uint32) {
	t.Helper()
	if len(trace) == 0 {
		return
	}
	low, high := trace[0], trace[0]
	regions := map[uint32]int{}
	for _, pc := range trace {
		if pc < low {
			low = pc
		}
		if pc > high {
			high = pc
		}
		regions[pc&^0xff]++
	}
	t.Logf("  trace spans %#x..%#x (module %#x..%#x) over %d distinct 256-byte regions",
		low, high, low-ImageBase, high-ImageBase, len(regions))
	if len(trace) <= 200 {
		offsets := make([]string, 0, len(trace))
		for _, pc := range trace {
			offsets = append(offsets, fmt.Sprintf("%#x", pc-ImageBase))
		}
		t.Logf("  path: %s", strings.Join(offsets, " "))
	}
	type region struct {
		base  uint32
		count int
	}
	ranked := make([]region, 0, len(regions))
	for base, count := range regions {
		ranked = append(ranked, region{base: base, count: count})
	}
	sort.Slice(ranked, func(a, b int) bool { return ranked[a].count > ranked[b].count })
	for index, entry := range ranked {
		if index >= 8 {
			break
		}
		t.Logf("    module %#08x : %d instructions", entry.base-ImageBase, entry.count)
	}
}
