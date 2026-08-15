package cheat

import (
	"encoding/binary"
	"fmt"
	"strings"
	"testing"
)

type testMemory struct {
	base uint32
	data []byte
}

func (memory *testMemory) ReadMemory(address uint32, destination []byte) error {
	offset := int(address - memory.base)
	if offset < 0 || offset+len(destination) > len(memory.data) {
		return fmt.Errorf("read outside test memory")
	}
	copy(destination, memory.data[offset:])
	return nil
}

func (memory *testMemory) WriteMemory(address uint32, data []byte) error {
	offset := int(address - memory.base)
	if offset < 0 || offset+len(data) > len(memory.data) {
		return fmt.Errorf("write outside test memory")
	}
	copy(memory.data[offset:], data)
	return nil
}

func (memory *testMemory) Regions() []Region {
	return []Region{{Base: memory.base, Size: uint32(len(memory.data)), Label: "test"}}
}

func newTestMemory() *testMemory {
	return &testMemory{base: 0x1000, data: make([]byte, 0x400)}
}

func (memory *testMemory) writeU32(address, value uint32) {
	var bytes [4]byte
	binary.LittleEndian.PutUint32(bytes[:], value)
	if err := memory.WriteMemory(address, bytes[:]); err != nil {
		panic(err)
	}
}

func u32Type(t *testing.T) ValueType {
	t.Helper()
	valueType, ok := ParseValueType("u32")
	if !ok {
		t.Fatal("u32 did not parse")
	}
	return valueType
}

func TestValueTypeParseDecodeEncode(t *testing.T) {
	if valueType, ok := ParseValueType("u16be"); !ok || valueType.Kind != KindU16 || valueType.Endian != Big {
		t.Fatalf("ParseValueType(u16be) = %+v/%t", valueType, ok)
	}
	if _, ok := ParseValueType("f32"); ok {
		t.Fatal("ParseValueType(f32) succeeded")
	}
	be, _ := ParseValueType("u32be")
	if value, ok := be.Decode([]byte{0x12, 0x34, 0x56, 0x78}); !ok || value != 0x12345678 {
		t.Fatalf("u32be decode = %#x/%t", value, ok)
	}
	i16le, _ := ParseValueType("i16")
	if value, ok := i16le.Decode([]byte{0xff, 0xff}); !ok || value != -1 {
		t.Fatalf("i16 decode = %d/%t", value, ok)
	}
	u16le, _ := ParseValueType("u16")
	if value, ok := u16le.Decode([]byte{0xff, 0xff}); !ok || value != 65535 {
		t.Fatalf("u16 decode = %d/%t", value, ok)
	}
	if _, ok := u16le.Decode([]byte{0xff}); ok {
		t.Fatal("short decode succeeded")
	}
	if bytes, err := be.Encode(0x12345678); err != nil || string(bytes) != string([]byte{0x12, 0x34, 0x56, 0x78}) {
		t.Fatalf("u32be encode = %x/%v", bytes, err)
	}
	i8Type, _ := ParseValueType("i8")
	if bytes, err := i8Type.Encode(-1); err != nil || string(bytes) != string([]byte{0xff}) {
		t.Fatalf("i8 encode = %x/%v", bytes, err)
	}
	if _, err := i8Type.Encode(200); err == nil {
		t.Fatal("i8 encode accepted 200")
	}
}

func TestScannerExactScanNarrows(t *testing.T) {
	memory := newTestMemory()
	memory.writeU32(0x1100, 1200)
	memory.writeU32(0x1200, 1200)
	memory.writeU32(0x1300, 1200)

	scanner := NewScanner(u32Type(t))
	count, err := scanner.Scan(memory, ScanFilter{Op: FilterEq, A: 1200})
	if err != nil || count != 3 {
		t.Fatalf("first scan = %d/%v, want 3", count, err)
	}

	memory.writeU32(0x1200, 980)
	memory.writeU32(0x1300, 980)
	count, err = scanner.Scan(memory, ScanFilter{Op: FilterEq, A: 980})
	if err != nil || count != 2 {
		t.Fatalf("second scan = %d/%v, want 2", count, err)
	}
	if scanner.Candidates()[0].Address != 0x1200 || scanner.Candidates()[1].Address != 0x1300 {
		t.Fatalf("candidates = %+v", scanner.Candidates())
	}
}

func TestScannerUnknownThenDecreased(t *testing.T) {
	memory := newTestMemory()
	memory.writeU32(0x1100, 500)
	memory.writeU32(0x1200, 500)

	scanner := NewScanner(u32Type(t))
	count, err := scanner.Scan(memory, ScanFilter{Op: FilterUnknown})
	if err != nil || count != 0x400/4 {
		t.Fatalf("unknown scan = %d/%v, want %d", count, err, 0x400/4)
	}

	memory.writeU32(0x1100, 400)
	count, err = scanner.Scan(memory, ScanFilter{Op: FilterDecreased})
	if err != nil || count != 1 {
		t.Fatalf("decreased scan = %d/%v, want 1", count, err)
	}
	if candidate := scanner.Candidates()[0]; candidate.Address != 0x1100 || candidate.Value != 400 {
		t.Fatalf("candidate = %+v", candidate)
	}
}

func TestScannerUnchangedAndDecreasedBy(t *testing.T) {
	memory := newTestMemory()
	memory.writeU32(0x1100, 100)
	memory.writeU32(0x1180, 100)

	scanner := NewScanner(u32Type(t))
	if _, err := scanner.Scan(memory, ScanFilter{Op: FilterEq, A: 100}); err != nil {
		t.Fatal(err)
	}
	count, err := scanner.Scan(memory, ScanFilter{Op: FilterUnchanged})
	if err != nil || count != 2 {
		t.Fatalf("unchanged scan = %d/%v, want 2", count, err)
	}

	memory.writeU32(0x1180, 93)
	count, err = scanner.Scan(memory, ScanFilter{Op: FilterDecreasedBy, A: 7})
	if err != nil || count != 1 {
		t.Fatalf("decreased-by scan = %d/%v, want 1", count, err)
	}
	if scanner.Candidates()[0].Address != 0x1180 {
		t.Fatalf("candidate = %+v", scanner.Candidates()[0])
	}
}

func TestScannerPreviousValueFilterRejectedOnFirstScan(t *testing.T) {
	scanner := NewScanner(u32Type(t))
	if _, err := scanner.Scan(newTestMemory(), ScanFilter{Op: FilterChanged}); err != ErrNeedsPreviousValue {
		t.Fatalf("first changed scan error = %v", err)
	}
	if scanner.Started() {
		t.Fatal("rejected scan marked the scanner started")
	}
}

func TestScannerUndo(t *testing.T) {
	memory := newTestMemory()
	memory.writeU32(0x1100, 7)
	memory.writeU32(0x1200, 7)

	scanner := NewScanner(u32Type(t))
	if _, err := scanner.Scan(memory, ScanFilter{Op: FilterEq, A: 7}); err != nil {
		t.Fatal(err)
	}
	if scanner.Len() != 2 {
		t.Fatalf("scan hits = %d, want 2", scanner.Len())
	}
	if _, err := scanner.Scan(memory, ScanFilter{Op: FilterEq, A: 99999}); err != nil {
		t.Fatal(err)
	}
	if scanner.Len() != 0 {
		t.Fatalf("scan hits = %d, want 0", scanner.Len())
	}
	if !scanner.Undo() {
		t.Fatal("undo failed")
	}
	if scanner.Len() != 2 {
		t.Fatalf("undo hits = %d, want 2", scanner.Len())
	}
}

func TestScannerUnalignedAndBigEndian(t *testing.T) {
	memory := newTestMemory()
	var bytes [4]byte
	binary.LittleEndian.PutUint32(bytes[:], 1234)
	if err := memory.WriteMemory(0x1101, bytes[:]); err != nil {
		t.Fatal(err)
	}

	scanner := NewScanner(u32Type(t))
	if count, err := scanner.Scan(memory, ScanFilter{Op: FilterEq, A: 1234}); err != nil || count != 0 {
		t.Fatalf("aligned scan = %d/%v, want 0", count, err)
	}
	scanner.Reset()
	scanner.SetAlign(1)
	count, err := scanner.Scan(memory, ScanFilter{Op: FilterEq, A: 1234})
	if err != nil || count != 1 || scanner.Candidates()[0].Address != 0x1101 {
		t.Fatalf("unaligned scan = %d/%v %+v", count, err, scanner.Candidates())
	}

	memory = newTestMemory()
	binary.BigEndian.PutUint32(bytes[:], 1234)
	if err := memory.WriteMemory(0x1100, bytes[:]); err != nil {
		t.Fatal(err)
	}
	beType, _ := ParseValueType("u32be")
	beScanner := NewScanner(beType)
	count, err = beScanner.Scan(memory, ScanFilter{Op: FilterEq, A: 1234})
	if err != nil || count != 1 || beScanner.Candidates()[0].Address != 0x1100 {
		t.Fatalf("big-endian scan = %d/%v %+v", count, err, beScanner.Candidates())
	}
}

func TestFreezeApplyRewritesValue(t *testing.T) {
	memory := &testMemory{data: make([]byte, 0x100)}
	var freezes FreezeList
	freezes.Insert(FreezeEntry{Address: 0x10, ValueType: u32Type(t), Value: 9999})

	if failed := freezes.Apply(memory); len(failed) != 0 {
		t.Fatalf("apply failed at %v", failed)
	}
	if value := binary.LittleEndian.Uint32(memory.data[0x10:]); value != 9999 {
		t.Fatalf("frozen value = %d, want 9999", value)
	}

	// The game overwrites it; the next apply puts it back.
	binary.LittleEndian.PutUint32(memory.data[0x10:], 5)
	freezes.Apply(memory)
	if value := binary.LittleEndian.Uint32(memory.data[0x10:]); value != 9999 {
		t.Fatalf("reapplied value = %d, want 9999", value)
	}
}

func TestFreezeInsertReplacesAndRemove(t *testing.T) {
	var freezes FreezeList
	valueType, _ := ParseValueType("u16")
	if freezes.Insert(FreezeEntry{Address: 0x20, ValueType: valueType, Value: 1}) {
		t.Fatal("first insert reported replacement")
	}
	if !freezes.Insert(FreezeEntry{Address: 0x20, ValueType: valueType, Value: 2}) {
		t.Fatal("second insert did not report replacement")
	}
	if freezes.Len() != 1 || freezes.Entries()[0].Value != 2 {
		t.Fatalf("entries = %+v", freezes.Entries())
	}
	if !freezes.Remove(0x20) || freezes.Remove(0x20) || freezes.Len() != 0 {
		t.Fatalf("remove behaved unexpectedly: %+v", freezes.Entries())
	}
}

func TestConsoleScanFreezeFlow(t *testing.T) {
	memory := newTestMemory()
	memory.writeU32(0x1100, 1200)
	console := NewConsole(NewSession(memory))

	output := console.Execute("scan u32 = 1200")
	if !strings.Contains(output, "1 hit(s)") || !strings.Contains(output, "0x00001100 = 1200") {
		t.Fatalf("scan output = %q", output)
	}
	output = console.Execute("freeze 0x1100 77")
	if !strings.Contains(output, "froze 0x00001100 = 77") {
		t.Fatalf("freeze output = %q", output)
	}
	if value := binary.LittleEndian.Uint32(memory.data[0x100:]); value != 77 {
		t.Fatalf("frozen memory = %d, want 77", value)
	}
	memory.writeU32(0x1100, 5)
	if failed := console.Session().Tick(); len(failed) != 0 {
		t.Fatalf("tick failures = %v", failed)
	}
	if value := binary.LittleEndian.Uint32(memory.data[0x100:]); value != 77 {
		t.Fatalf("post-tick memory = %d, want 77", value)
	}
	if output := console.Execute("read 0x1100 i32"); !strings.Contains(output, "= 77") {
		t.Fatalf("read output = %q", output)
	}
	if output := console.Execute("unfreeze all"); !strings.Contains(output, "cleared") {
		t.Fatalf("unfreeze output = %q", output)
	}
	if output := console.Execute("nonsense"); !strings.Contains(output, "unknown command") {
		t.Fatalf("unknown command output = %q", output)
	}
}
