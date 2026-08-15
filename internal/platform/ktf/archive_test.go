package ktf

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
)

func TestOpenKTFArchive(t *testing.T) {
	data := makeSyntheticArchive(t, "16", syntheticClientCode())
	archive, err := Open(data)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if archive.Descriptor.AID != "fixture" || archive.Descriptor.PID != "P0001" || archive.Descriptor.MainClass != "fixture/Main" {
		t.Fatalf("descriptor = %+v", archive.Descriptor)
	}
	if got := archive.Descriptor.Properties["MCLASS"]; got != "fixture.Main" {
		t.Fatalf("descriptor property MCLASS = %q", got)
	}
	if archive.JARName != "fixture.jar" {
		t.Fatalf("JARName = %q", archive.JARName)
	}
	if archive.JAR.Client.Name != "client.bin16" || archive.JAR.Client.BSSSize != 16 {
		t.Fatalf("client = %+v", archive.JAR.Client)
	}
	if got := string(archive.JAR.Entries["fixture.txt"]); got != "newly authored" {
		t.Fatalf("fixture resource = %q", got)
	}
}

func TestSyntheticClientRunsFromArchiveThroughSVC(t *testing.T) {
	archive, err := Open(makeSyntheticArchive(t, "16", syntheticClientCode()))
	if err != nil {
		t.Fatal(err)
	}
	client, err := LoadClient(archive.JAR.Client, armcore.CoreOptions{Quantum: 2, MaxSteps: 20})
	if err != nil {
		t.Fatal(err)
	}

	bss := make([]byte, archive.JAR.Client.BSSSize)
	if err := client.Core().Memory().Read(ImageBase+uint32(len(archive.JAR.Client.Data)), bss); err != nil {
		t.Fatalf("read BSS: %v", err)
	}
	if !bytes.Equal(bss, make([]byte, len(bss))) {
		t.Fatalf("BSS is not zero-filled: %v", bss)
	}

	var calls int
	summary, err := client.ExecuteEntry(context.Background(), func(_ context.Context, thread *armcore.Thread, call armcore.SupervisorCall) error {
		calls++
		if call.Immediate != 0x2a || call.Address != ImageBase+4 || call.ResumePC != ImageBase+6 {
			t.Fatalf("supervisor call = %+v", call)
		}
		registers := thread.Context().Registers
		if registers[0] != 17 {
			t.Fatalf("entry r0 at SVC = %d, want BSS size + 1", registers[0])
		}
		if registers[armcore.RegisterSP] != ThreadStackBase+uint32(ThreadStackSize)-4 {
			t.Fatalf("entry sp at SVC = %#x", registers[armcore.RegisterSP])
		}
		return thread.SetRegister(0, 40)
	})
	if err != nil {
		t.Fatalf("ExecuteEntry() error = %v", err)
	}
	if calls != 1 || summary.Context.Registers[0] != 42 || summary.Steps != 5 {
		t.Fatalf("calls=%d r0=%d steps=%d, want 1/42/5", calls, summary.Context.Registers[0], summary.Steps)
	}
	if client.Core().Steps() != 5 {
		t.Fatalf("core steps = %d, want 5", client.Core().Steps())
	}
}

func TestExecuteEntryRequiresSVCHandler(t *testing.T) {
	archive, err := Open(makeSyntheticArchive(t, "0", syntheticClientCode()))
	if err != nil {
		t.Fatal(err)
	}
	client, err := LoadClient(archive.JAR.Client, armcore.CoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ExecuteEntry(context.Background(), nil)
	if !errors.Is(err, armcore.ErrUnhandledSupervisorCall) {
		t.Fatalf("ExecuteEntry() error = %v, want ErrUnhandledSupervisorCall", err)
	}
}

func TestOpenRejectsInvalidKTFContainers(t *testing.T) {
	validADF := []byte("AID:fixture\nPID:P0001\nMClass:fixture.Main\n")
	validJAR := makeZIP(t, []zipEntry{{name: "client.bin0", data: syntheticClientCode()}})
	tests := []struct {
		name string
		data []byte
		want string
	}{
		{name: "not zip", data: []byte("not a zip"), want: "open KTF archive"},
		{name: "missing adf", data: makeZIP(t, []zipEntry{{name: "fixture.jar", data: validJAR}}), want: "has no __adf__"},
		{name: "missing aid jar", data: makeZIP(t, []zipEntry{{name: adfPath, data: validADF}}), want: "has no AID JAR"},
		{name: "unsafe outer path", data: makeZIP(t, []zipEntry{{name: "../escape", data: nil}}), want: "unsafe entry"},
		{name: "case duplicate", data: makeZIP(t, []zipEntry{{name: "FILE", data: nil}, {name: "file", data: nil}}), want: "duplicate entries"},
		{name: "missing client", data: makeZIP(t, []zipEntry{{name: adfPath, data: validADF}, {name: "fixture.jar", data: makeZIP(t, []zipEntry{{name: "resource", data: nil}})}}), want: "has no client.bin"},
		{name: "unsafe jar path", data: makeZIP(t, []zipEntry{{name: adfPath, data: validADF}, {name: "fixture.jar", data: makeZIP(t, []zipEntry{{name: "../client.bin0", data: []byte{1}}})}}), want: "unsafe entry"},
		{name: "multiple clients", data: makeZIP(t, []zipEntry{{name: adfPath, data: validADF}, {name: "fixture.jar", data: makeZIP(t, []zipEntry{{name: "client.bin0", data: []byte{1}}, {name: "client.bin1", data: []byte{1}}})}}), want: "multiple client.bin"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Open(test.data)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Open() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestOpenJARRejectsInvalidClientNamesAndSizes(t *testing.T) {
	tests := []struct {
		name       string
		clientName string
		data       []byte
		want       string
	}{
		{name: "missing suffix", clientName: "client.bin", data: []byte{1}, want: "no decimal BSS size"},
		{name: "nondigit suffix", clientName: "client.binABC", data: []byte{1}, want: "invalid BSS size"},
		{name: "overflow suffix", clientName: "client.bin4294967296", data: []byte{1}, want: "invalid BSS size"},
		{name: "empty image", clientName: "client.bin0", want: "is empty"},
		{name: "mapped size", clientName: "client.bin268435456", data: []byte{1}, want: "maps 268435457 bytes"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := OpenJAR(makeZIP(t, []zipEntry{{name: test.clientName, data: test.data}}))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("OpenJAR() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestOpenJARAcceptsLegacyIncorrectDataDescriptorCRC(t *testing.T) {
	jar := makeZIP(t, []zipEntry{{name: "client.bin0", data: syntheticClientCode()}})
	descriptor := bytes.Index(jar, []byte{0x50, 0x4b, 0x07, 0x08})
	if descriptor < 0 || descriptor+8 > len(jar) {
		t.Fatal("generated ZIP has no signed data descriptor")
	}
	jar[descriptor+4] ^= 0xff
	opened, err := OpenJAR(jar)
	if err != nil {
		t.Fatalf("OpenJAR() rejected data whose actual and central CRC agree: %v", err)
	}
	if !bytes.Equal(opened.Client.Data, syntheticClientCode()) {
		t.Fatal("legacy descriptor compatibility changed client contents")
	}
}

func TestParseDescriptorValidatesRequiredNames(t *testing.T) {
	valid, err := ParseDescriptor([]byte("Name:\xff\nAID:fixture\r\nPID:P0001\r\nMClass:fixture.Main\r\n"))
	if err != nil {
		t.Fatalf("ParseDescriptor() error = %v", err)
	}
	if valid.MainClass != "fixture/Main" {
		t.Fatalf("MainClass = %q", valid.MainClass)
	}
	tests := []struct {
		name string
		data string
		want string
	}{
		{name: "missing aid", data: "PID:p\nMClass:m\n", want: "has no AID"},
		{name: "unsafe aid", data: "AID:../x\nPID:p\nMClass:m\n", want: "not a safe file name"},
		{name: "missing pid", data: "AID:a\nMClass:m\n", want: "has no PID"},
		{name: "missing class", data: "AID:a\nPID:p\n", want: "has no MClass"},
		{name: "invalid class", data: "AID:a\nPID:p\nMClass:a..b\n", want: "MClass"},
		{name: "duplicate", data: "AID:a\nAID:b\nPID:p\nMClass:m\n", want: "repeats field AID"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseDescriptor([]byte(test.data))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ParseDescriptor() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestLoadClientRejectsOversizedManualImage(t *testing.T) {
	_, err := LoadClient(ClientImage{Name: "client.bin268435456", Data: []byte{1}, BSSSize: 268435456}, armcore.CoreOptions{})
	if err == nil || !strings.Contains(err.Error(), "maps 268435457 bytes") {
		t.Fatalf("LoadClient() error = %v", err)
	}
}

func TestLoadClientRejectsInconsistentManualImage(t *testing.T) {
	_, err := LoadClient(ClientImage{Name: "client.bin7", Data: []byte{1}, BSSSize: 8}, armcore.CoreOptions{})
	if err == nil || !strings.Contains(err.Error(), "suffix names BSS 7 but image specifies 8") {
		t.Fatalf("LoadClient() error = %v", err)
	}
}

func FuzzOpenNeverPanics(f *testing.F) {
	f.Add(makeSyntheticArchiveForFuzz("0", syntheticClientCode()))
	f.Add([]byte("not a zip"))
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = Open(data)
	})
}

// syntheticClientCode is newly authored Thumb code:
// push {lr}; adds r0,#1; svc #0x2a; adds r0,#2; pop {pc}.
func syntheticClientCode() []byte {
	instructions := []uint16{0xb500, 0x3001, 0xdf2a, 0x3002, 0xbd00}
	data := make([]byte, len(instructions)*2)
	for index, instruction := range instructions {
		binary.LittleEndian.PutUint16(data[index*2:], instruction)
	}
	return data
}

func makeSyntheticArchive(t *testing.T, bssSuffix string, code []byte) []byte {
	t.Helper()
	jar := makeZIP(t, []zipEntry{
		{name: "client.bin" + bssSuffix, data: code},
		{name: "fixture.txt", data: []byte("newly authored")},
	})
	return makeZIP(t, []zipEntry{
		{name: adfPath, data: []byte("AID:fixture\nPID:P0001\nMClass:fixture.Main\n")},
		{name: "fixture.jar", data: jar},
	})
}

func makeSyntheticArchiveForFuzz(bssSuffix string, code []byte) []byte {
	var jar bytes.Buffer
	jarWriter := zip.NewWriter(&jar)
	client, _ := jarWriter.Create("client.bin" + bssSuffix)
	_, _ = client.Write(code)
	_ = jarWriter.Close()
	var archive bytes.Buffer
	archiveWriter := zip.NewWriter(&archive)
	adf, _ := archiveWriter.Create(adfPath)
	_, _ = adf.Write([]byte("AID:fixture\nPID:P0001\nMClass:fixture.Main\n"))
	jarEntry, _ := archiveWriter.Create("fixture.jar")
	_, _ = jarEntry.Write(jar.Bytes())
	_ = archiveWriter.Close()
	return archive.Bytes()
}

type zipEntry struct {
	name string
	data []byte
}

func makeZIP(t *testing.T, entries []zipEntry) []byte {
	t.Helper()
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for _, item := range entries {
		entry, err := writer.Create(item.name)
		if err != nil {
			t.Fatalf("Create(%q) error = %v", item.name, err)
		}
		if _, err := entry.Write(item.data); err != nil {
			t.Fatalf("Write(%q) error = %v", item.name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	return output.Bytes()
}
