package cheat

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A console nobody enabled files on is the one the browser host drives over
// the session socket, so its `save` and `load` must not reach the file system:
// a path there is a path on the server chosen by whoever is connected.
func TestConsoleSaveAndLoadStayOffTheFileSystemByDefault(t *testing.T) {
	directory := t.TempDir()
	console := NewConsole(NewSession(newTestMemory()))
	if _, err := console.Session().Freeze(0x1100, u32Type(t), 7, "gold"); err != nil {
		t.Fatal(err)
	}

	target := filepath.Join(directory, "written.json")
	output := console.Execute("save " + target)
	if !strings.HasPrefix(output, "cheat: ") {
		t.Fatalf("save output = %q, want a refusal", output)
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("save wrote %s (stat err %v)", target, err)
	}

	table := filepath.Join(directory, "table.json")
	if err := os.WriteFile(table, []byte(`{"entries":[{"address":4352,"value":99,"type":"u32","frozen":true}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	output = console.Execute("load " + table)
	if !strings.HasPrefix(output, "cheat: ") {
		t.Fatalf("load output = %q, want a refusal", output)
	}
	if entries := console.Session().Freezes().Entries(); len(entries) != 1 || entries[0].Value != 7 {
		t.Fatalf("load changed the freezes: %+v", entries)
	}
}

func TestConsoleSaveAndLoadWorkOnceFilesAreAllowed(t *testing.T) {
	directory := t.TempDir()
	memory := newTestMemory()
	console := NewConsole(NewSession(memory))
	console.AllowFiles()
	if _, err := console.Session().Freeze(0x1100, u32Type(t), 7, "gold"); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(directory, "table.json")
	if output := console.Execute("save " + target); !strings.Contains(output, "saved 1 frozen value") {
		t.Fatalf("save output = %q", output)
	}
	console.Session().UnfreezeAll()
	if output := console.Execute("load " + target); !strings.Contains(output, "applied 1 frozen value") {
		t.Fatalf("load output = %q", output)
	}
	if value := binary.LittleEndian.Uint32(memory.data[0x100:]); value != 7 {
		t.Fatalf("loaded value = %d, want 7", value)
	}
}

// Even a local console reads a table through a bound: `load /dev/zero` must
// not allocate until the process dies.
func TestConsoleLoadRefusesAnOversizedFile(t *testing.T) {
	directory := t.TempDir()
	console := NewConsole(NewSession(newTestMemory()))
	console.AllowFiles()
	target := filepath.Join(directory, "huge.json")
	if err := os.WriteFile(target, make([]byte, maxTableFileBytes+1), 0o644); err != nil {
		t.Fatal(err)
	}
	output := console.Execute("load " + target)
	if !strings.Contains(output, "larger than") {
		t.Fatalf("load output = %q, want a size refusal", output)
	}
}

// A decimal typed with a leading zero is the decimal it reads as, not octal,
// and a digit string that is not octal is not quietly read as hex.
func TestConsoleNumbersWithALeadingZeroAreDecimal(t *testing.T) {
	for _, test := range []struct {
		text string
		want int64
	}{
		{"010", 10},
		{"0100", 100},
		{"0250", 250},
		{"019", 19},
		{"-010", -10},
		{"0x10", 16},
		{"-0x10", -16},
		{"ffff0000", 0xffff0000},
		{"0", 0},
		{"42", 42},
	} {
		value, ok := parseNumber(test.text)
		if !ok || value != test.want {
			t.Errorf("parseNumber(%q) = %d/%t, want %d", test.text, value, ok, test.want)
		}
	}

	memory := newTestMemory()
	console := NewConsole(NewSession(memory))
	if output := console.Execute("set 0x1100 010 u32"); !strings.Contains(output, "= 10 ") {
		t.Fatalf("set output = %q", output)
	}
	if value := binary.LittleEndian.Uint32(memory.data[0x100:]); value != 10 {
		t.Fatalf("set wrote %d, want 10", value)
	}
}

// An alignment that does not fit the 32-bit stride the sweep walks at used to
// truncate to zero and divide by it.
func TestConsoleRefusesAnAlignTheSweepCannotUse(t *testing.T) {
	console := NewConsole(NewSession(newTestMemory()))
	for _, align := range []string{"4294967296", "8589934592", "4097"} {
		if output := console.Execute("align " + align); !strings.HasPrefix(output, "cheat: ") {
			t.Fatalf("align %s output = %q, want a refusal", align, output)
		}
	}
	if output := console.Execute("scan u"); strings.HasPrefix(output, "cheat: ") {
		t.Fatalf("scan after refused align = %q", output)
	}

	scanner := NewScanner(u32Type(t))
	scanner.SetAlign(1 << 32)
	if scanner.Align() > maxScanAlign {
		t.Fatalf("SetAlign kept %d, above %d", scanner.Align(), maxScanAlign)
	}
	if _, err := scanner.Scan(newTestMemory(), ScanFilter{Op: FilterUnknown}); err != nil {
		t.Fatal(err)
	}
}

// flatMemory is a region of zeroes the size a test asks for.
type flatMemory struct{ size uint32 }

func (memory flatMemory) ReadMemory(address uint32, destination []byte) error {
	clear(destination)
	return nil
}
func (memory flatMemory) WriteMemory(uint32, []byte) error { return nil }
func (memory flatMemory) Regions() []Region {
	return []Region{{Base: 0x10000, Size: memory.size, Label: "flat"}}
}

// An unknown-value search seeds one candidate per address, so its size follows
// guest memory; the search refuses a set it cannot hold rather than growing
// the heap without bound.
func TestUnknownScanRefusesMoreCandidatesThanItCanHold(t *testing.T) {
	valueType, _ := ParseValueType("u8")
	scanner := NewScanner(valueType)
	_, err := scanner.Scan(flatMemory{size: maxScanCandidates + 1}, ScanFilter{Op: FilterUnknown})
	if !errors.Is(err, ErrTooManyCandidates) {
		t.Fatalf("scan err = %v, want ErrTooManyCandidates", err)
	}
	if scanner.Started() || scanner.Len() != 0 {
		t.Fatalf("a refused sweep left a search behind: started %t, %d candidates", scanner.Started(), scanner.Len())
	}
	session := NewSession(flatMemory{size: maxScanCandidates + 1})
	if _, err := PanelScan(session, "u8", "unknown", 0); !errors.Is(err, ErrPanelTooManyCandidates) {
		t.Fatalf("panel err = %v, want the panel's own words", err)
	}
}

// Undo history holds what it can afford: repeated `unchanged` scans over a
// wide search keep at most the budget, dropping the oldest steps first.
func TestScanHistoryStaysWithinItsBudget(t *testing.T) {
	valueType, _ := ParseValueType("u8")
	scanner := NewScanner(valueType)
	memory := flatMemory{size: maxScanHistoryCandidates/2 + 1}
	if _, err := scanner.Scan(memory, ScanFilter{Op: FilterUnknown}); err != nil {
		t.Fatal(err)
	}
	for range 4 {
		if _, err := scanner.Scan(memory, ScanFilter{Op: FilterUnchanged}); err != nil {
			t.Fatal(err)
		}
	}
	retained := 0
	for _, snapshot := range scanner.history {
		retained += len(snapshot)
	}
	if retained > maxScanHistoryCandidates {
		t.Fatalf("history holds %d candidates, budget %d", retained, maxScanHistoryCandidates)
	}
	if !scanner.Undo() || scanner.Len() != int(memory.size) {
		t.Fatalf("undo after a trimmed history: len %d, want %d", scanner.Len(), memory.size)
	}
}
