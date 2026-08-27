package lgt

import (
	"bytes"
	"strconv"
	"testing"
)

// An archive is untrusted input: it is a file somebody downloaded, and the
// loader reads its zip directory, its descriptor, a zip nested inside it and
// an ELF header before anything has said the file is what it claims to be.
// None of that may panic. An error is the right answer for every byte string
// here; a panic in a Host is the server going down, and in the CLI it is a
// stack trace where a refusal belonged.
//
// The seeds are the shapes the loader has real branches for, so the fuzzer
// starts inside the parser rather than having to discover a zip header.
func FuzzOpenNeverPanics(f *testing.F) {
	f.Add(fixtureArchive(f))
	f.Add(fixtureJAR(f))
	// A descriptor with no JAR beside it, and a JAR with no module in it: the
	// two ways an archive gets past the first check and fails the second.
	f.Add(zipOf(f, map[string][]byte{"app_info": []byte("AID=0102ABCD\n")}))
	f.Add(zipOf(f, map[string][]byte{
		"app_info":     []byte("AID=0102ABCD\nPID=PF000001\n"),
		"0102ABCD.jar": zipOf(f, map[string][]byte{"nothing.txt": []byte("no module")}),
	}))
	// A descriptor that names neither identity, which is the parse error.
	f.Add(zipOf(f, map[string][]byte{"app_info": []byte("Name=Fixture\n")}))
	// An archive whose only entry is a traversal name.
	f.Add(zipOf(f, map[string][]byte{"../../etc/passwd": []byte("x")}))
	f.Add([]byte("not a zip"))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		archive, err := Open(data)
		if err != nil {
			return
		}
		// An archive that opened has to be usable rather than merely
		// non-panicking: a nil module or a nil map here is a crash moved one
		// call further on, into the loader that reads them.
		if archive == nil {
			t.Fatal("Open() returned no archive and no error")
		}
		if archive.Module == nil || archive.Resources == nil || archive.Packaged == nil {
			t.Fatalf("Open() returned an archive with nothing in it: %+v", archive)
		}
		if archive.Descriptor.AID == "" && archive.Descriptor.PID == "" {
			t.Fatal("Open() accepted a descriptor naming neither an AID nor a PID")
		}
	})
}

// The ELF the archive carries is the other untrusted parse, and the one with
// arithmetic in it: section headers name offsets and sizes that a crafted file
// can point anywhere.
func FuzzParseModuleNeverPanics(f *testing.F) {
	// The module is taken back out through Open rather than rebuilt here, so
	// the seed is the same bytes a loader would have handed the parser.
	archive, err := Open(fixtureArchive(f))
	if err != nil {
		f.Fatalf("open the fixture archive: %v", err)
	}
	module := archive.Module
	f.Add(module)
	// The header alone, so a truncation is a seed rather than a discovery.
	if len(module) > 52 {
		f.Add(module[:52])
	}
	f.Add([]byte("\x7fELF"))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		parsed, err := ParseModule(data)
		if err != nil {
			return
		}
		if parsed == nil {
			t.Fatal("ParseModule() returned no module and no error")
		}
		// Span is what the loader maps, so a module that parsed has to answer
		// a range rather than one that runs backwards.
		if low, high := parsed.Span(); high < low {
			t.Fatalf("Span() = [%#x, %#x)", low, high)
		}
	})
}

// The descriptor is plain text and is parsed before anything has checked it is
// text at all.
func FuzzParseDescriptorNeverPanics(f *testing.F) {
	f.Add([]byte("AID=0102ABCD\nPID=PF000001\nMClass=Fixture\n"))
	f.Add([]byte("AID:0102ABCD\r\nPID:PF000001\r\n"))
	f.Add([]byte("# a comment and nothing else\n"))
	f.Add([]byte("=\n:\n\n"))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		descriptor, err := ParseDescriptor(data)
		if err != nil {
			return
		}
		if descriptor.AID == "" && descriptor.PID == "" {
			t.Fatal("ParseDescriptor() accepted a descriptor naming neither an AID nor a PID")
		}
		if descriptor.Fields == nil {
			t.Fatal("ParseDescriptor() returned a descriptor with no field map")
		}
	})
}

// Each of the four bounds has to stop an archive that crosses it, and none of
// them may stop one that does not. They are exercised through the limits the
// loader takes rather than through the real ones: the far side of the real
// total is half a gigabyte of test data, and a bound nobody can afford to test
// is a bound nobody tests.
func TestAnArchiveIsBoundedFourWays(t *testing.T) {
	small := archiveLimits{input: 4096, entry: 64, total: 160, count: 4}
	// Three entries of sixty bytes: inside every bound but the total, which
	// two of them already fill.
	content := bytes.Repeat([]byte("x"), 60)
	three := zipOf(t, map[string][]byte{"a": content, "b": content, "c": content})
	if _, err := readZIPWithin(three, "fixture", small); err == nil {
		t.Fatal("readZIPWithin() accepted an archive past the total it may expand to")
	}
	// The same archive under a total that fits it is read, so the failure
	// above is the bound rather than the fixture.
	roomy := small
	roomy.total = 4096
	entries, err := readZIPWithin(three, "fixture", roomy)
	if err != nil {
		t.Fatalf("readZIPWithin() = %v over an archive inside every bound", err)
	}
	if len(entries) != 3 {
		t.Fatalf("readZIPWithin() read %d entries, want 3", len(entries))
	}

	// One entry past the per-entry bound, with room in the total for it.
	big := zipOf(t, map[string][]byte{"a": bytes.Repeat([]byte("x"), 65)})
	if _, err := readZIPWithin(big, "fixture", roomy); err == nil {
		t.Fatal("readZIPWithin() accepted an entry past the per-entry bound")
	}

	// More entries than may be declared, each of them tiny.
	many := map[string][]byte{}
	for index := 0; index <= roomy.count; index++ {
		many["entry"+strconv.Itoa(index)] = []byte("x")
	}
	if _, err := readZIPWithin(zipOf(t, many), "fixture", roomy); err == nil {
		t.Fatal("readZIPWithin() accepted more entries than the count allows")
	}

	// An archive larger than the loader will even open. The check is on the
	// length rather than on the contents, so the bytes need not be a zip.
	if _, err := readZIPWithin(make([]byte, roomy.input+1), "fixture", roomy); err == nil {
		t.Fatal("readZIPWithin() accepted an archive past the input bound")
	}
}
