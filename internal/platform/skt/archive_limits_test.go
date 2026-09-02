package skt

import (
	"archive/zip"
	"bytes"
	"strconv"
	"testing"
)

// An SKT title is two zips, one inside the other, and each of the four bounds
// has to stop an archive that crosses it in both of them — while stopping none
// that does not. They are exercised through the limits the readers take rather
// than through the real ones: the far side of the real total is half a
// gigabyte of test data, and a bound nobody can afford to test is a bound
// nobody tests.
func TestTheJARIsBoundedFourWays(t *testing.T) {
	small := archiveLimits{input: 4096, entry: 64, total: 160, count: 4}
	// Three entries of sixty bytes: inside every bound but the total, which
	// two of them already fill.
	content := bytes.Repeat([]byte("x"), 60)
	three := makeJAR(t, map[string][]byte{"a": content, "b": content, "c": content})
	if _, err := readJARWithin(three, small); err == nil {
		t.Fatal("readJARWithin() accepted a JAR past the total it may expand to")
	}
	// The same JAR under a total that fits it is read, so the failure above is
	// the bound rather than the fixture.
	roomy := small
	roomy.total = 4096
	entries, err := readJARWithin(three, roomy)
	if err != nil {
		t.Fatalf("readJARWithin() = %v over a JAR inside every bound", err)
	}
	if len(entries) != 3 {
		t.Fatalf("readJARWithin() read %d entries, want 3", len(entries))
	}

	// One entry past the per-entry bound, with room in the total for it.
	big := makeJAR(t, map[string][]byte{"a": bytes.Repeat([]byte("x"), 65)})
	if _, err := readJARWithin(big, roomy); err == nil {
		t.Fatal("readJARWithin() accepted an entry past the per-entry bound")
	}

	// More entries than may be declared, each of them tiny.
	many := map[string][]byte{}
	for index := 0; index <= roomy.count; index++ {
		many["entry"+strconv.Itoa(index)] = []byte("x")
	}
	if _, err := readJARWithin(makeJAR(t, many), roomy); err == nil {
		t.Fatal("readJARWithin() accepted more entries than the count allows")
	}

	// An archive larger than the loader will even open. The check is on the
	// length rather than on the contents, so the bytes need not be a zip.
	if _, err := readJARWithin(make([]byte, roomy.input+1), roomy); err == nil {
		t.Fatal("readJARWithin() accepted a JAR past the input bound")
	}
}

// The outer container reads its entries in three passes — the descriptor, the
// JAR, then the installed files — so its total is the one bound that cannot be
// counted inside a single loop, and the one worth pinning here.
func TestTheContainerIsBoundedFourWays(t *testing.T) {
	inner := makeJAR(t, map[string][]byte{"Main.class": []byte("x")})
	content := bytes.Repeat([]byte("d"), 60)
	container := func(t *testing.T) []byte {
		return makeJAR(t, map[string][]byte{
			"game.msd":  []byte("MIDlet-1: Title,,Main\n"),
			"game.jar":  inner,
			"data1.dat": content,
			"data2.dat": content,
		})
	}
	roomy := archiveLimits{input: 1 << 20, entry: 1 << 20, total: 1 << 20, count: 64}
	descriptor, jar, installed, err := unpackArchiveWithin(container(t), roomy)
	if err != nil {
		t.Fatalf("unpackArchiveWithin() = %v over a container inside every bound", err)
	}
	if descriptor.MainClass == "" || jar == nil || len(installed) != 2 {
		t.Fatalf("unpackArchiveWithin() = %+v, %d jar bytes, %d installed", descriptor, len(jar), len(installed))
	}

	// A total that the descriptor and the JAR leave no room in: the installed
	// files are read last and are what crosses it.
	tight := roomy
	tight.total = uint64(len(inner)) + 64
	if _, _, _, err := unpackArchiveWithin(container(t), tight); err == nil {
		t.Fatal("unpackArchiveWithin() accepted a container past the total it may expand to")
	}

	perEntry := roomy
	perEntry.entry = 32
	if _, _, _, err := unpackArchiveWithin(container(t), perEntry); err == nil {
		t.Fatal("unpackArchiveWithin() accepted an entry past the per-entry bound")
	}

	count := roomy
	count.count = 3
	if _, _, _, err := unpackArchiveWithin(container(t), count); err == nil {
		t.Fatal("unpackArchiveWithin() accepted more entries than the count allows")
	}

	if _, _, _, err := unpackArchiveWithin(make([]byte, roomy.input+1), roomy); err == nil {
		t.Fatal("unpackArchiveWithin() accepted a container past the input bound")
	}
}

// Bytes that are not a container at all are still not an error: the caller
// reads them as a bare MIDlet JAR, which is the shape every fixture has. A
// bound must not turn that fallback into a failure.
func TestBytesThatAreNotAContainerStayTheCallersProblem(t *testing.T) {
	descriptor, jar, installed, err := unpackArchiveWithin([]byte("not a zip at all"), defaultArchiveLimits)
	if err != nil || jar != nil || installed != nil || descriptor.MainClass != "" {
		t.Fatalf("unpackArchiveWithin(not a zip) = %+v, %v, %v, %v", descriptor, jar, installed, err)
	}
}

// Two entries under one name is an ambiguity about which one runs, so the
// reader refuses it rather than picking a side. A map-keyed fixture cannot
// express it, which is why this one writes the entry list itself.
func TestADuplicateNameIsRefused(t *testing.T) {
	var out bytes.Buffer
	writer := zip.NewWriter(&out)
	for _, body := range []string{"first", "second"} {
		entry, err := writer.Create("a")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := readJARWithin(out.Bytes(), defaultArchiveLimits); err == nil {
		t.Fatal("readJARWithin() accepted two entries under one name")
	}
}

// A container whose inner JAR is broken has to fail as a container rather than
// be quietly retried as a bare JAR.
func TestAContainerWithABrokenJARFails(t *testing.T) {
	broken := makeJAR(t, map[string][]byte{
		"game.msd": []byte("MIDlet-1: Title,,Main\n"),
		"game.jar": []byte("this is not a JAR"),
	})
	if _, err := Open(broken); err == nil {
		t.Fatal("Open() accepted a container whose JAR is not a JAR")
	}
}

func FuzzUnpackArchiveNeverPanics(f *testing.F) {
	f.Add(makeJARBytes(f, map[string][]byte{"game.msd": []byte("MIDlet-1: T,,M\n"), "game.jar": []byte("x")}))
	f.Add(makeJARBytes(f, map[string][]byte{"a": []byte("x"), "a/../a": []byte("y")}))
	f.Add([]byte("not a zip"))
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _, _, _ = unpackArchiveWithin(data, archiveLimits{input: 1 << 20, entry: 1 << 16, total: 1 << 18, count: 64})
	})
}
