package skt

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

func TestOpen(t *testing.T) {
	jar := makeJAR(t, map[string][]byte{
		manifestPath:         []byte("Manifest-Version: 1.0\nMIDlet-Name: Example\nMIDlet-1: Example, , example.Main\n"),
		"example/Main.class": minimalClass("example/Main"),
		"resource.txt":       []byte("hello"),
	})
	archive, err := Open(jar)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if archive.Descriptor.MainClass != "example/Main" || archive.MainClass.Name != "example/Main" {
		t.Fatalf("main class = %q / %q", archive.Descriptor.MainClass, archive.MainClass.Name)
	}
	if archive.Summary().Entries != 3 {
		t.Fatalf("Entries = %d", archive.Summary().Entries)
	}
}

func TestOpenRejectsUnsafePath(t *testing.T) {
	for _, name := range []string{"..", "../outside", "/absolute"} {
		t.Run(name, func(t *testing.T) {
			jar := makeJAR(t, map[string][]byte{name: []byte("bad")})
			_, err := Open(jar)
			if err == nil || !strings.Contains(err.Error(), "unsafe entry path") {
				t.Fatalf("Open() error = %v", err)
			}
		})
	}
}

func TestOpenRejectsMismatchedMainClass(t *testing.T) {
	jar := makeJAR(t, map[string][]byte{
		manifestPath:         []byte("MIDlet-Name: Example\nMIDlet-1: Example, , example.Main\n"),
		"example/Main.class": minimalClass("other/Main"),
	})
	_, err := Open(jar)
	if err == nil || !strings.Contains(err.Error(), "class file declares") {
		t.Fatalf("Open() error = %v", err)
	}
}

func FuzzOpenNeverPanics(f *testing.F) {
	f.Add(arithmeticJAR)
	f.Add([]byte("not a zip"))
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = Open(data)
	})
}

func makeJAR(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	var out bytes.Buffer
	writer := zip.NewWriter(&out)
	for name, data := range entries {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("Create(%q) error = %v", name, err)
		}
		if _, err := entry.Write(data); err != nil {
			t.Fatalf("Write(%q) error = %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	return out.Bytes()
}

func minimalClass(name string) []byte {
	var out bytes.Buffer
	writeU4(&out, 0xcafebabe)
	writeU2(&out, 0)
	writeU2(&out, 48)
	writeU2(&out, 10)
	writeUTF8(&out, name)
	writeClass(&out, 1)
	writeUTF8(&out, "java/lang/Object")
	writeClass(&out, 3)
	writeUTF8(&out, "<init>")
	writeUTF8(&out, "()V")
	writeUTF8(&out, "Code")
	out.WriteByte(10)
	writeU2(&out, 4)
	writeU2(&out, 9)
	out.WriteByte(12)
	writeU2(&out, 5)
	writeU2(&out, 6)
	writeU2(&out, 0x0021)
	writeU2(&out, 2)
	writeU2(&out, 4)
	writeU2(&out, 0)
	writeU2(&out, 0)
	writeU2(&out, 1)
	writeU2(&out, 0x0001)
	writeU2(&out, 5)
	writeU2(&out, 6)
	writeU2(&out, 1)
	writeU2(&out, 7)
	writeU4(&out, 17)
	writeU2(&out, 1)
	writeU2(&out, 1)
	writeU4(&out, 5)
	out.Write([]byte{0x2a, 0xb7, 0x00, 0x08, 0xb1})
	writeU2(&out, 0)
	writeU2(&out, 0)
	writeU2(&out, 0)
	return out.Bytes()
}

func writeUTF8(out *bytes.Buffer, value string) {
	out.WriteByte(1)
	writeU2(out, uint16(len(value)))
	out.WriteString(value)
}

func writeClass(out *bytes.Buffer, nameIndex uint16) {
	out.WriteByte(7)
	writeU2(out, nameIndex)
}

func writeU2(out *bytes.Buffer, value uint16) {
	var data [2]byte
	binary.BigEndian.PutUint16(data[:], value)
	out.Write(data[:])
}

func writeU4(out *bytes.Buffer, value uint32) {
	var data [4]byte
	binary.BigEndian.PutUint32(data[:], value)
	out.Write(data[:])
}
