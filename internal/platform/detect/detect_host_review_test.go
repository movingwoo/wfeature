package detect_test

import (
	"archive/zip"
	"bytes"
	"testing"

	"github.com/movingwoo/wfeature/internal/platform/detect"
)

// A class entry stored with a compression method nothing registers cannot be
// opened, and that says nothing about the platform: another class in the same
// JAR still can. One such entry used to refuse the whole archive before the
// class that names the SKVM surface was ever read.
func TestAnUnopenableClassDoesNotAbortClassification(t *testing.T) {
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	broken, err := writer.CreateRaw(&zip.FileHeader{
		Name:               "A.class",
		Method:             99,
		CompressedSize64:   4,
		UncompressedSize64: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := broken.Write([]byte("junk")); err != nil {
		t.Fatal(err)
	}
	entry, err := writer.Create("Main.class")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("\xca\xfe\xba\xbecom/skt/m/Device")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	platform, reason, err := detect.Classify(buffer.Bytes())
	if err != nil {
		t.Fatalf("Classify() error = %v, want the readable class to decide", err)
	}
	if platform != detect.SKT || reason != detect.ReasonClaimed {
		t.Fatalf("Classify() = %q, %v; want SKT claimed", platform, reason)
	}
}
