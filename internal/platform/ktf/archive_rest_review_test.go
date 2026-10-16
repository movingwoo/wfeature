package ktf

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"hash/crc32"
	"io"
	"strings"
	"testing"
)

// zeroReader answers zeros for ever, so a large expanded entry can be built
// without holding its bytes.
type zeroReader struct{}

func (zeroReader) Read(buffer []byte) (int, error) {
	clear(buffer)
	return len(buffer), nil
}

// TestDuplicateEntriesAreChargedToTheExpansionBudget pins the bound the
// duplicate check used to skip: every entry under one name was inflated to be
// compared with the first, and none of them counted against the archive's
// expansion budget, so an archive of identical duplicates could ask for any
// amount of decompression. Nine 64 MiB copies declare 576 MiB, past the
// 512 MiB budget, and the archive is refused rather than inflated in full.
func TestDuplicateEntriesAreChargedToTheExpansionBudget(t *testing.T) {
	const entrySize = 64 << 20
	var compressed bytes.Buffer
	writer, err := flate.NewWriter(&compressed, flate.BestCompression)
	if err != nil {
		t.Fatal(err)
	}
	checksum := crc32.NewIEEE()
	if _, err := io.CopyN(io.MultiWriter(writer, checksum), zeroReader{}, entrySize); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	zipWriter := zip.NewWriter(&archive)
	for range maxExpandedSize/entrySize + 1 {
		raw, err := zipWriter.CreateRaw(&zip.FileHeader{
			Name:               "x",
			Method:             zip.Deflate,
			CRC32:              checksum.Sum32(),
			CompressedSize64:   uint64(compressed.Len()),
			UncompressedSize64: entrySize,
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := raw.Write(compressed.Bytes()); err != nil {
			t.Fatal(err)
		}
	}
	if err := zipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = readZIP(archive.Bytes(), "test archive")
	if err == nil || !strings.Contains(err.Error(), "expands beyond") {
		t.Fatalf("identical duplicates past the budget: error = %v, want the expansion refusal", err)
	}
}

// TestIdenticalDuplicateEntriesStillLoad keeps the case the duplicate check
// exists for: an archive that packs the same small file twice loads.
func TestIdenticalDuplicateEntriesStillLoad(t *testing.T) {
	var archive bytes.Buffer
	zipWriter := zip.NewWriter(&archive)
	for range 2 {
		entry, err := zipWriter.Create("img/a.png")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte("same bytes")); err != nil {
			t.Fatal(err)
		}
	}
	if err := zipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	entries, err := readZIP(archive.Bytes(), "test archive")
	if err != nil {
		t.Fatal(err)
	}
	if string(entries["img/a.png"]) != "same bytes" {
		t.Fatalf("entry = %q", entries["img/a.png"])
	}
}
