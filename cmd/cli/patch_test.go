package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/cheat"
)

// patchMemory is a flat span of guest memory, which is all the two decisions
// `-patch` makes need to be exercised against: whether the declared bytes are
// there, and whether the table says it was made for this image.
type patchMemory struct {
	base uint32
	data []byte
}

func newPatchMemory() *patchMemory { return &patchMemory{base: 0x1000, data: make([]byte, 0x100)} }

func (memory *patchMemory) offset(address uint32, length int) (int, bool) {
	if address < memory.base {
		return 0, false
	}
	start := int(address - memory.base)
	if start+length > len(memory.data) {
		return 0, false
	}
	return start, true
}

func (memory *patchMemory) ReadMemory(address uint32, destination []byte) error {
	start, ok := memory.offset(address, len(destination))
	if !ok {
		return os.ErrInvalid
	}
	copy(destination, memory.data[start:])
	return nil
}

func (memory *patchMemory) WriteMemory(address uint32, data []byte) error {
	start, ok := memory.offset(address, len(data))
	if !ok {
		return os.ErrInvalid
	}
	copy(memory.data[start:], data)
	return nil
}

func (memory *patchMemory) Regions() []cheat.Region {
	return []cheat.Region{{Base: memory.base, Size: uint32(len(memory.data)), Label: "test"}}
}

func writePatchTable(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// consoleFor builds a console over memory, keyed as a platform with an image
// hash would be. The archive path handed to applyStartPatches is a file that
// does not exist, so the file half of the key stays empty and the image half
// is the only one under test.
func consoleFor(t *testing.T, memory *patchMemory, image string) *cheat.Console {
	t.Helper()
	console := cheat.NewConsole(cheat.NewSession(memory))
	if image != "" {
		console.SetTableKey(cheat.TableKey{Image: image})
	}
	return console
}

const patchTestImage = "1b114584dce8216dd2fae93722feb49f88eb7b8989920caf91903cdef78c8929"

func TestReadPatchTablesRefusesATableWithNoPatches(t *testing.T) {
	path := writePatchTable(t, "empty.json", `{"entries":[]}`)
	if _, err := readPatchTables([]string{path}); err == nil {
		t.Fatal("a table with nothing to apply was accepted")
	} else if !strings.Contains(err.Error(), "no patches") {
		t.Fatalf("the refusal does not say what is missing: %v", err)
	}
}

func TestReadPatchTablesReportsAMalformedFileByName(t *testing.T) {
	path := writePatchTable(t, "broken.json", `{"patches":[{"name":"x","patches":[{"address":"nope"}]}]}`)
	if _, err := readPatchTables([]string{path}); err == nil {
		t.Fatal("a table with an unreadable address was accepted")
	} else if !strings.Contains(err.Error(), path) {
		t.Fatalf("the refusal does not name the file: %v", err)
	}
}

func TestApplyStartPatchesWritesEveryTableInOrder(t *testing.T) {
	memory := newPatchMemory()
	memory.data[0] = 0x01
	memory.data[1] = 0x02
	memory.data[2] = 0x03
	memory.data[3] = 0x04
	first := writePatchTable(t, "first.json", `{"image":"`+patchTestImage+`","patches":[
		{"name":"first","patches":[{"address":"0x1000","expect":"0102","replace":"aabb"}]}]}`)
	second := writePatchTable(t, "second.json", `{"image":"`+patchTestImage+`","patches":[
		{"name":"second","patches":[{"address":"0x1002","expect":"0304","replace":"ccdd"}]}]}`)
	tables, err := readPatchTables([]string{first, second})
	if err != nil {
		t.Fatal(err)
	}

	var notices bytes.Buffer
	console := consoleFor(t, memory, patchTestImage)
	if err := applyStartPatches(console, "no-such-archive.zip", tables, &notices); err != nil {
		t.Fatalf("two matching tables were refused: %v", err)
	}
	if want := []byte{0xaa, 0xbb, 0xcc, 0xdd}; string(memory.data[:4]) != string(want) {
		t.Fatalf("memory = % x, want % x", memory.data[:4], want)
	}
	// Both entries stay visible to the console, so an interactive session
	// started over them can list and revert what the flag put in.
	if held := console.Session().Patches(); len(held) != 2 {
		t.Fatalf("held patches = %d, want 2", len(held))
	}
}

// The declared bytes not being there stops the run. It is the decision that
// separates this flag from a warning: a run that ticked anyway would show
// exactly the behavior the patch was written to change.
func TestApplyStartPatchesRefusesWhenTheDeclaredBytesAreNotThere(t *testing.T) {
	memory := newPatchMemory()
	path := writePatchTable(t, "wrong.json", `{"image":"`+patchTestImage+`","patches":[
		{"name":"gate","patches":[{"address":"0x1000","expect":"deadbeef","replace":"00000000"}]}]}`)
	tables, err := readPatchTables([]string{path})
	if err != nil {
		t.Fatal(err)
	}

	var notices bytes.Buffer
	err = applyStartPatches(consoleFor(t, memory, patchTestImage), "no-such-archive.zip", tables, &notices)
	if err == nil {
		t.Fatal("a patch whose declared bytes are absent did not stop the run")
	}
	if !strings.Contains(err.Error(), "memory holds") {
		t.Fatalf("the refusal does not say what was there instead: %v", err)
	}
}

// A key that positively disagrees stops the run too, which is where this flag
// parts company with the console: the console warns because somebody is about
// to read the warning.
func TestApplyStartPatchesRefusesATableMadeAgainstAnotherImage(t *testing.T) {
	memory := newPatchMemory()
	memory.data[0] = 0x01
	memory.data[1] = 0x02
	path := writePatchTable(t, "elsewhere.json", `{"image":"00","patches":[
		{"name":"gate","patches":[{"address":"0x1000","expect":"0102","replace":"aabb"}]}]}`)
	tables, err := readPatchTables([]string{path})
	if err != nil {
		t.Fatal(err)
	}

	var notices bytes.Buffer
	err = applyStartPatches(consoleFor(t, memory, patchTestImage), "no-such-archive.zip", tables, &notices)
	if err == nil {
		t.Fatal("a table made against another image was applied")
	}
	if memory.data[0] != 0x01 {
		t.Fatal("the mismatched table wrote to memory before it was refused")
	}
}

// A table that says nothing about what it was made for is a hand-written one,
// which is how the first patch against a title always starts. It applies, and
// the note says the addresses were taken on trust.
func TestApplyStartPatchesAcceptsAnUnkeyedTableWithANotice(t *testing.T) {
	memory := newPatchMemory()
	memory.data[0] = 0x01
	memory.data[1] = 0x02
	path := writePatchTable(t, "unkeyed.json", `{"patches":[
		{"name":"gate","patches":[{"address":"0x1000","expect":"0102","replace":"aabb"}]}],
		"entries":[{"name":"gold","address":4096,"value":1,"type":"u32","frozen":true}],
		"watches":[4096]}`)
	tables, err := readPatchTables([]string{path})
	if err != nil {
		t.Fatal(err)
	}

	var notices bytes.Buffer
	if err := applyStartPatches(consoleFor(t, memory, patchTestImage), "no-such-archive.zip", tables, &notices); err != nil {
		t.Fatalf("an unkeyed table was refused: %v", err)
	}
	if memory.data[0] != 0xaa {
		t.Fatal("the unkeyed table did not apply")
	}
	text := notices.String()
	if !strings.Contains(text, "taken on trust") {
		t.Fatalf("nothing said the table is unkeyed: %q", text)
	}
	// The frozen value and the watch are not this flag's to apply, and saying
	// so is what keeps the next hour from starting on a wrong belief.
	if !strings.Contains(text, "frozen value") || !strings.Contains(text, "watch") {
		t.Fatalf("the parts -patch does not apply went unreported: %q", text)
	}
}
