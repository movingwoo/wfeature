package ktf

import (
	"archive/zip"
	"bytes"
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

// This loader looks __adf__ up by exact name, so a wrapping folder left it
// reporting an archive with no descriptor. It is the same archive.
func TestOpenReadsAnArchiveInsideAFolder(t *testing.T) {
	wrapped := rewrap(t, makeSyntheticArchive(t, "16", syntheticClientCode()), "게임 폴더")

	opened, err := Open(wrapped)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if opened.Descriptor.AID == "" {
		t.Fatal("the descriptor inside the folder was not read")
	}
	if opened.JAR == nil || len(opened.JAR.Client.Data) == 0 {
		t.Fatal("the client image inside the JAR was not read")
	}
	// The JAR's own entries keep their names: a JAR never gains a wrapper, and
	// the loader deliberately does not look for one there.
	if _, ok := opened.JAR.Entries[opened.JAR.Client.Name]; !ok {
		t.Fatal("a JAR entry lost its name to the unwrap")
	}
}

// ReadDescriptor is the other door into an archive — the save importer uses it
// — and it has to agree with Open about what the archive is.
func TestReadDescriptorReadsAnArchiveInsideAFolder(t *testing.T) {
	wrapped := rewrap(t, makeSyntheticArchive(t, "16", syntheticClientCode()), "folder")
	descriptor, err := ReadDescriptor(wrapped)
	if err != nil {
		t.Fatalf("ReadDescriptor() error = %v", err)
	}
	if descriptor.AID == "" {
		t.Fatal("ReadDescriptor found no AID through the folder")
	}
}
