package ktf

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

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

// TestLocalKTFNativePackageStarts performs the package's start-up protocol
// against a local module and reports what the title then asks the platform for.
//
// It asserts that the handshake completes, because that is now understood and a
// regression in it is a defect. It asserts nothing about the slots the title
// goes on to call: that list is the output, and it is the specification for
// what implementing this package costs. **Read the log rather than the exit
// status** — the run is expected to stop at the first slot that matters, and it
// says which one.
func TestLocalKTFNativePackageStarts(t *testing.T) {
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
		// Two slots are identified and answered; everything else stays a trap.
		client.ServeAllocator(NativePlatformTable, 0x68)
		client.ServeQueryInterface(NativeEntryObject, 0x8)

		if err := client.Start(context.Background()); err != nil {
			t.Fatalf("%s: entry: %v", name, err)
		}
		record, err := client.ReadEntryRecord()
		if err != nil {
			t.Fatalf("%s: entry record: %v", name, err)
		}
		t.Logf("%s: entry returned a factory at %#x exporting %d functions", name, record.Address, len(record.Functions))

		identifier, ok := archive.ApplicationIdentifier()
		if !ok {
			t.Fatalf("%s: information file carries no application identifier", name)
		}
		object, err := client.CreateApplication(context.Background(), identifier)
		if err != nil {
			t.Fatalf("%s: create application %#x: %v", name, identifier, err)
		}
		t.Logf("%s: application %#x created at %#x", name, identifier, object)

		result, err := client.SendEvent(context.Background(), object, 0, 0, 0)
		t.Logf("%s: event 0 returned %#x, and stopped with: %v", name, result, err)

		summary := client.SlotSummary()
		t.Logf("%s: %d calls over %d distinct slots", name, len(client.Calls()), len(summary))
		for _, entry := range summary {
			t.Logf("  %-20s offset %#04x (slot %3d) called %d times, first from %#x",
				entry.Surface, entry.Offset, entry.Slot, entry.Count, entry.First)
		}
		for index, call := range client.Calls() {
			t.Logf("  #%02d %-20s offset %#04x (%#x, %#x, %#x, %#x) from %#x", index, call.Surface, call.Offset,
				call.Arguments[0], call.Arguments[1], call.Arguments[2], call.Arguments[3], call.Caller)
		}
	}
}

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
