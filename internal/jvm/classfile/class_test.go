package classfile

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

func TestParseMinimalClass(t *testing.T) {
	parsed, err := Parse(minimalClass("example/Main"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if parsed.Name != "example/Main" {
		t.Fatalf("Name = %q", parsed.Name)
	}
	if parsed.SuperName != "java/lang/Object" {
		t.Fatalf("SuperName = %q", parsed.SuperName)
	}
	if parsed.MajorVersion != 48 || parsed.MinorVersion != 0 {
		t.Fatalf("version = %d.%d", parsed.MajorVersion, parsed.MinorVersion)
	}
	if len(parsed.Methods) != 1 {
		t.Fatalf("len(Methods) = %d", len(parsed.Methods))
	}
	method := parsed.Methods[0]
	if method.Name != "<init>" || method.Descriptor != "()V" {
		t.Fatalf("method = %s%s", method.Name, method.Descriptor)
	}
	if len(method.Attributes) != 1 || method.Attributes[0].Code == nil {
		t.Fatal("constructor has no parsed Code attribute")
	}
	wantCode := []byte{0x2a, 0xb7, 0x00, 0x08, 0xb1}
	if !bytes.Equal(method.Attributes[0].Code.Bytecode, wantCode) {
		t.Fatalf("bytecode = %x", method.Attributes[0].Code.Bytecode)
	}
}

func TestParseRejectsMalformedInput(t *testing.T) {
	valid := minimalClass("Main")
	tests := map[string][]byte{
		"empty":     nil,
		"truncated": valid[:len(valid)/2],
		"trailing":  append(append([]byte(nil), valid...), 0xff),
		"bad magic": append([]byte{0, 0, 0, 0}, valid[4:]...),
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := Parse(data)
			if !errors.Is(err, ErrInvalidFormat) {
				t.Fatalf("Parse() error = %v, want ErrInvalidFormat", err)
			}
		})
	}
}

func TestParseReportsUnsupportedVersion(t *testing.T) {
	data := minimalClass("Main")
	binary.BigEndian.PutUint16(data[6:8], MaxSupportedMajorVersion+1)
	_, err := Parse(data)
	var versionError *UnsupportedVersionError
	if !errors.As(err, &versionError) {
		t.Fatalf("Parse() error = %v", err)
	}
	if versionError.Major != MaxSupportedMajorVersion+1 {
		t.Fatalf("Major = %d", versionError.Major)
	}
}

func TestDecodeModifiedUTF8(t *testing.T) {
	data := []byte{'a', 0xc0, 0x80, 0xed, 0xa0, 0xbd, 0xed, 0xb8, 0x80}
	decoded, err := decodeModifiedUTF8(data)
	if err != nil {
		t.Fatalf("decodeModifiedUTF8() error = %v", err)
	}
	if decoded != "a\x00😀" {
		t.Fatalf("decoded = %q", decoded)
	}
}

func TestDecodeModifiedUTF8RejectsUnpairedSurrogate(t *testing.T) {
	_, err := decodeModifiedUTF8([]byte{0xed, 0xa0, 0xbd})
	if err == nil {
		t.Fatal("decodeModifiedUTF8() accepted an unpaired surrogate")
	}
}

func FuzzParseNeverPanics(f *testing.F) {
	f.Add(minimalClass("Main"))
	f.Add([]byte{0xca, 0xfe, 0xba, 0xbe})
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = Parse(data)
	})
}

func minimalClass(name string) []byte {
	var out bytes.Buffer
	putU4(&out, 0xcafebabe)
	putU2(&out, 0)
	putU2(&out, 48)
	putU2(&out, 10)
	putUTF8(&out, name)               // 1
	putClass(&out, 1)                 // 2
	putUTF8(&out, "java/lang/Object") // 3
	putClass(&out, 3)                 // 4
	putUTF8(&out, "<init>")           // 5
	putUTF8(&out, "()V")              // 6
	putUTF8(&out, "Code")             // 7
	putU1(&out, ConstantMethodRef)    // 8
	putU2(&out, 4)
	putU2(&out, 9)
	putU1(&out, ConstantNameAndType) // 9
	putU2(&out, 5)
	putU2(&out, 6)

	putU2(&out, 0x0021)
	putU2(&out, 2)
	putU2(&out, 4)
	putU2(&out, 0) // interfaces
	putU2(&out, 0) // fields
	putU2(&out, 1) // methods
	putU2(&out, 0x0001)
	putU2(&out, 5)
	putU2(&out, 6)
	putU2(&out, 1)
	putU2(&out, 7)
	putU4(&out, 17)
	putU2(&out, 1)
	putU2(&out, 1)
	putU4(&out, 5)
	out.Write([]byte{0x2a, 0xb7, 0x00, 0x08, 0xb1})
	putU2(&out, 0) // exception table
	putU2(&out, 0) // code attributes
	putU2(&out, 0) // class attributes
	return out.Bytes()
}

func putUTF8(out *bytes.Buffer, value string) {
	putU1(out, ConstantUTF8)
	putU2(out, uint16(len(value)))
	out.WriteString(value)
}

func putClass(out *bytes.Buffer, nameIndex uint16) {
	putU1(out, ConstantClass)
	putU2(out, nameIndex)
}

func putU1(out *bytes.Buffer, value uint8) {
	out.WriteByte(value)
}

func putU2(out *bytes.Buffer, value uint16) {
	var data [2]byte
	binary.BigEndian.PutUint16(data[:], value)
	out.Write(data[:])
}

func putU4(out *bytes.Buffer, value uint32) {
	var data [4]byte
	binary.BigEndian.PutUint32(data[:], value)
	out.Write(data[:])
}
