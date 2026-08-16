package lgt

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"testing"
)

// rewrap repacks an archive with every entry inside one folder, which is what
// a copy that was unpacked and zipped up again looks like.
func rewrap(t *testing.T, data []byte, folder string) []byte {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	buffer := &bytes.Buffer{}
	writer := zip.NewWriter(buffer)
	for _, file := range reader.File {
		opened, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		contents, err := io.ReadAll(opened)
		opened.Close()
		if err != nil {
			t.Fatal(err)
		}
		entry, err := writer.Create(folder + "/" + file.Name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(contents); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

// The loader looks app_info up by exact name, so a wrapping folder used to
// leave it reporting an archive with no descriptor. It is the same archive.
func TestOpenReadsAnArchiveInsideAFolder(t *testing.T) {
	wrapped := rewrap(t, fixtureArchive(t), "레전드 오브 무언가")

	opened, err := Open(wrapped)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if opened.Descriptor.AID != "0102ABCD" {
		t.Fatalf("AID = %q, want 0102ABCD", opened.Descriptor.AID)
	}
	if len(opened.Module) == 0 {
		t.Fatal("the module inside the JAR was not read")
	}
	// The JAR's own entries keep their names: a JAR never gains a wrapper, and
	// the loader deliberately does not look for one there.
	if _, ok := opened.Resource("data/hello.txt"); !ok {
		t.Fatal("a JAR resource lost its name to the unwrap")
	}
}

// And it runs, which is the only proof that nothing downstream was reading a
// name the unwrap moved.
func TestASessionStartsFromAWrappedArchive(t *testing.T) {
	wrapped := rewrap(t, fixtureArchive(t), "wrapper")
	session, err := StartSession(context.Background(), wrapped, SessionOptions{
		Width: 16, Height: 8, MaxSteps: 1 << 20,
	})
	if err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}
	if err := session.Tick(context.Background()); err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
}
