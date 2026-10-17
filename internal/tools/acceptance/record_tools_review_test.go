package main

import (
	"os"
	"path/filepath"
	"testing"
)

// A record file cut off part of the way through a line is refused rather than
// read as complete: planning or comparing from the part before the cut would
// report every archive after it as gone from the corpus.
func TestATruncatedRecordFileIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cut"+recordExtension)
	run := runRecord{Schema: recordSchema, Kind: runKind, Run: "2026-10-16T10:00:00Z"}
	archives := []archiveRecord{
		{Schema: recordSchema, Kind: archiveKind, Run: run.Run, Platform: "ktf", Archive: "one.zip"},
		{Schema: recordSchema, Kind: archiveKind, Run: run.Run, Platform: "ktf", Archive: "two.zip"},
	}
	if err := writeRecords(path, run, archives); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data[:len(data)-10], 0o644); err != nil {
		t.Fatal(err)
	}
	if _, read, err := readRecords(path); err == nil {
		t.Fatalf("a truncated file read as complete with %d archive(s)", len(read))
	}
	// A whole file still reads, including its last line.
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, read, err := readRecords(path); err != nil || len(read) != 2 {
		t.Fatalf("a whole file read as %d archive(s), err %v", len(read), err)
	}
}
