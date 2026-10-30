package skt

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"testing"
)

// nativeDAF builds a minimal but complete `.daf` from its regions, so a test
// can exercise the parser without a real executable. The caller gives the
// code, data, the two relocation lists and the resource archive; this lays the
// header out around them.
type dafParts struct {
	code        []byte
	data        []byte
	bss         uint32
	entries     [6]uint32
	codeRelocs  []byte
	dataRelocs  []byte
	resource    []byte
	firstBlock  uint32 // 0 means the valid 0x20
	secondBlock uint32 // 0 means the valid 0x50
}

func (parts dafParts) bytes() []byte {
	relocation := make([]byte, 8+len(parts.codeRelocs)+len(parts.dataRelocs))
	binary.LittleEndian.PutUint32(relocation[0:], uint32(len(parts.codeRelocs)))
	binary.LittleEndian.PutUint32(relocation[4:], uint32(len(parts.dataRelocs)))
	copy(relocation[8:], parts.codeRelocs)
	copy(relocation[8+len(parts.codeRelocs):], parts.dataRelocs)

	header := make([]byte, nativeHeaderSize)
	first := parts.firstBlock
	if first == 0 {
		first = nativeFirstBlock
	}
	second := parts.secondBlock
	if second == 0 {
		second = nativeSecondBlock
	}
	binary.LittleEndian.PutUint32(header[0:], first)
	binary.LittleEndian.PutUint32(header[nativeFirstBlock+8:], second)
	binary.LittleEndian.PutUint32(header[nativeSizesOffset+0:], uint32(len(parts.code)))
	binary.LittleEndian.PutUint32(header[nativeSizesOffset+4:], uint32(len(parts.data)))
	binary.LittleEndian.PutUint32(header[nativeSizesOffset+8:], parts.bss)
	binary.LittleEndian.PutUint32(header[nativeSizesOffset+12:], uint32(len(relocation)))
	binary.LittleEndian.PutUint32(header[nativeSizesOffset+20:], uint32(len(parts.resource)))
	for index, entry := range parts.entries {
		binary.LittleEndian.PutUint32(header[nativeEntryOffset+4*index:], entry)
	}

	image := append([]byte(nil), header...)
	image = append(image, parts.code...)
	image = append(image, parts.data...)
	image = append(image, relocation...)
	image = append(image, parts.resource...)
	return image
}

// validParts is a small executable: a word in the code that points into data,
// a word in the data that points into code, and a start entry.
func validParts() dafParts {
	code := make([]byte, 0x20)
	binary.LittleEndian.PutUint32(code[0x08:], NativeDataLink) // a data address
	data := make([]byte, 8)
	binary.LittleEndian.PutUint32(data[0:], nativeRootMarker)
	binary.LittleEndian.PutUint32(data[4:], 0x11) // a code address
	return dafParts{
		code:       code,
		data:       data,
		entries:    [6]uint32{0x11},     // start, Thumb, inside the code
		codeRelocs: []byte{0x40 | 0x08}, // advance 8, data address
		dataRelocs: []byte{0x04},        // advance 4, code address
	}
}

func TestParseNativeExecutable(t *testing.T) {
	executable, err := ParseNativeExecutable(validParts().bytes())
	if err != nil {
		t.Fatalf("ParseNativeExecutable: %v", err)
	}
	if executable.Entries.Start != 0x11 {
		t.Errorf("start entry = %#x, want 0x11", executable.Entries.Start)
	}
	if executable.BSSSize != 0 || len(executable.Data) != 8 {
		t.Errorf("data %d bytes bss %d, want 8 and 0", len(executable.Data), executable.BSSSize)
	}
	if len(executable.Resources) != 0 {
		t.Errorf("resources = %d, want 0", len(executable.Resources))
	}

	const base = 0x00100000
	code, data, err := executable.Load(base)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := binary.LittleEndian.Uint32(code[0x08:]); got != base+NativeDataLink {
		t.Errorf("relocated code data address = %#x, want %#x", got, base+NativeDataLink)
	}
	if got := binary.LittleEndian.Uint32(data[4:]); got != base+0x11 {
		t.Errorf("relocated data code address = %#x, want %#x", got, base+0x11)
	}
	if got := binary.LittleEndian.Uint32(data[0:]); got != nativeRootMarker+base {
		// The marker word is a data word, so the loader does not move it unless
		// a relocation names it; here none does, so it stays the marker.
		if got != nativeRootMarker {
			t.Errorf("marker word = %#x, want it left alone", got)
		}
	}
}

func TestParseNativeExecutableRefusals(t *testing.T) {
	cases := []struct {
		name  string
		parts func(dafParts) dafParts
	}{
		{"wrong first block", func(p dafParts) dafParts { p.firstBlock = 0x30; return p }},
		{"wrong second block", func(p dafParts) dafParts { p.secondBlock = 0x40; return p }},
		{"no start entry", func(p dafParts) dafParts { p.entries[0] = 0; return p }},
		{"even start entry", func(p dafParts) dafParts { p.entries[0] = 0x10; return p }},
		{"start past code", func(p dafParts) dafParts { p.entries[0] = 0x1001; return p }},
		{"no marker", func(p dafParts) dafParts {
			p.data = append([]byte(nil), p.data...)
			binary.LittleEndian.PutUint32(p.data[0:], 0)
			return p
		}},
		{"code relocation overruns", func(p dafParts) dafParts {
			p.codeRelocs = []byte{0x40 | 0x0f, 0x0f} // walks past the 0x20 code
			return p
		}},
		{"unterminated relocation entry", func(p dafParts) dafParts {
			p.codeRelocs = []byte{0x80} // a 28-bit entry needs three more bytes
			return p
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			image := test.parts(validParts()).bytes()
			if _, err := ParseNativeExecutable(image); err == nil {
				t.Fatalf("ParseNativeExecutable accepted %s", test.name)
			}
		})
	}
}

func TestParseNativeExecutableSizeMismatch(t *testing.T) {
	image := validParts().bytes()
	// Claim one more code byte than the file holds.
	binary.LittleEndian.PutUint32(image[nativeSizesOffset:], uint32(0x21))
	if _, err := ParseNativeExecutable(image); err == nil {
		t.Fatal("ParseNativeExecutable accepted a size that does not add up")
	}
}

func TestNativeResources(t *testing.T) {
	// One resource named "a.dat" holding four bytes.
	name := []byte("a.dat\x00")
	const count = 1
	directory := make([]byte, 0)
	header := make([]byte, 28)
	binary.LittleEndian.PutUint32(header[0:], 0x10)
	binary.LittleEndian.PutUint32(header[4:], 1)
	binary.LittleEndian.PutUint32(header[8:], 0x10)
	copy(header[12:], []byte{'K', 'O'})
	entry := make([]byte, 8)
	binary.LittleEndian.PutUint32(entry[0:], 0) // offset
	binary.LittleEndian.PutUint32(entry[4:], 4) // size
	dir := append(append(append([]byte(nil), header...), entry...), name...)
	binary.LittleEndian.PutUint32(dir[16:], uint32(len(dir)-16)) // directory size
	binary.LittleEndian.PutUint32(dir[24:], count)
	payload := []byte{1, 2, 3, 4}
	archive := append(dir, payload...)
	_ = directory

	parts := validParts()
	parts.resource = archive
	executable, err := ParseNativeExecutable(parts.bytes())
	if err != nil {
		t.Fatalf("ParseNativeExecutable with resources: %v", err)
	}
	got, ok := executable.Resources["a.dat"]
	if !ok || !bytes.Equal(got, payload) {
		t.Fatalf("resource a.dat = %v (%v), want %v", got, ok, payload)
	}
}

// nativeZIP packs an executable and files into a package the loader accepts.
func nativeZIP(t *testing.T, id string, image []byte, files map[string][]byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	add := func(name string, data []byte) {
		w, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	add(id+"/"+id+".daf", image)
	add(id+"/"+id+".mod", []byte("descriptor"))
	add(id+"/"+id+".wmr", []byte("icons"))
	for name, data := range files {
		add(id+"/"+name, data)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestOpenNative(t *testing.T) {
	image := validParts().bytes()
	data := nativeZIP(t, "0123456789", image, map[string][]byte{
		"save.dat":      {1, 2, 3},
		"sub/thing.dat": {4, 5},
	})
	if !IsNativeArchive(data) {
		t.Fatal("IsNativeArchive = false for a native package")
	}
	opened, err := OpenNative(data)
	if err != nil {
		t.Fatalf("OpenNative: %v", err)
	}
	if opened.ID != "0123456789" {
		t.Errorf("id = %q, want 0123456789", opened.ID)
	}
	if NativeSaveOwner(opened) != "0123456789" {
		t.Errorf("save owner = %q, want 0123456789", NativeSaveOwner(opened))
	}
	if !bytes.Equal(opened.Files["save.dat"], []byte{1, 2, 3}) {
		t.Errorf("save.dat = %v", opened.Files["save.dat"])
	}
	if !bytes.Equal(opened.Files["sub/thing.dat"], []byte{4, 5}) {
		t.Errorf("sub/thing.dat = %v", opened.Files["sub/thing.dat"])
	}
	// The descriptor, executable and icons are not the title's files.
	for _, name := range []string{"0123456789.daf", "0123456789.mod", "0123456789.wmr"} {
		if _, ok := opened.Files[name]; ok {
			t.Errorf("%s was filed as a title file", name)
		}
	}
}

func TestOpenNativeRejectsFlat(t *testing.T) {
	// A `.daf` with no `.mod` of the same name is not a native package: a
	// bare `.daf` could be anything.
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	w, _ := writer.Create("0123456789.daf")
	w.Write(validParts().bytes())
	writer.Close()
	if IsNativeArchive(buffer.Bytes()) {
		t.Fatal("IsNativeArchive = true for a lone .daf")
	}
}

func TestProgramFilesPrefersOwnFiles(t *testing.T) {
	// A resource and a title file share a name; the title file wins, because
	// that is where the title writes.
	name := []byte("shared.dat\x00")
	header := make([]byte, 28)
	binary.LittleEndian.PutUint32(header[0:], 0x10)
	binary.LittleEndian.PutUint32(header[4:], 1)
	binary.LittleEndian.PutUint32(header[8:], 0x10)
	copy(header[12:], []byte{'K', 'O'})
	entry := make([]byte, 8)
	binary.LittleEndian.PutUint32(entry[4:], 3)
	dir := append(append(append([]byte(nil), header...), entry...), name...)
	binary.LittleEndian.PutUint32(dir[16:], uint32(len(dir)-16))
	binary.LittleEndian.PutUint32(dir[24:], 1)
	archive := append(dir, []byte{9, 9, 9}...) // the resource's bytes

	parts := validParts()
	parts.resource = archive
	image := parts.bytes()
	data := nativeZIP(t, "0123456789", image, map[string][]byte{"shared.dat": {1, 2, 3}})
	opened, err := OpenNative(data)
	if err != nil {
		t.Fatalf("OpenNative: %v", err)
	}
	files := opened.ProgramFiles()
	if !bytes.Equal(files["shared.dat"], []byte{1, 2, 3}) {
		t.Errorf("shared.dat = %v, want the title's own file", files["shared.dat"])
	}
}

// An SKT Java package (a .msd descriptor beside a .jar, no .daf) is not a
// native package, so IsNativeArchive is false and the session's native branch
// is never entered for it. This, with the skt package's execution files being
// unchanged, is why the SKT Java path is unaffected by native support.
func TestIsNativeArchiveFalseForSKTJava(t *testing.T) {
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, e := range []struct{ name, body string }{
		{"0123456789.msd", "MIDlet-1: Game,,Main\n"},
		{"0123456789.jar", "PK\x03\x04 not really a jar"},
		{"0123456789.mod", "descriptor"},
	} {
		w, _ := writer.Create(e.name)
		w.Write([]byte(e.body))
	}
	writer.Close()
	if IsNativeArchive(buffer.Bytes()) {
		t.Fatal("IsNativeArchive = true for an SKT Java package (.msd + .jar, no .daf)")
	}
}
