package skt

import (
	"bytes"
	goruntime "runtime"
	"testing"
)

func TestParseDescriptor(t *testing.T) {
	data := []byte("Manifest-Version: 1.0\r\nMIDlet-Name: Example\r\nMIDlet-1: Example, /icon.png, com.example.\r\n Main\r\n\r\nName: ignored.class\r\nMIDlet-1: Wrong, , wrong.Main\r\n")
	descriptor, err := ParseDescriptor(data)
	if err != nil {
		t.Fatalf("ParseDescriptor() error = %v", err)
	}
	if descriptor.Name != "Example" {
		t.Fatalf("Name = %q", descriptor.Name)
	}
	if descriptor.MainClass != "com/example/Main" {
		t.Fatalf("MainClass = %q", descriptor.MainClass)
	}
	if descriptor.Properties["Manifest-Version"] != "1.0" {
		t.Fatalf("Manifest-Version = %q", descriptor.Properties["Manifest-Version"])
	}
}

func TestParseDescriptorRequiresMIDletClass(t *testing.T) {
	_, err := ParseDescriptor([]byte("MIDlet-Name: Example\n"))
	if err == nil {
		t.Fatal("ParseDescriptor() accepted a descriptor without MIDlet-1")
	}
}

func TestParseDescriptorRejectsOrphanContinuation(t *testing.T) {
	_, err := ParseDescriptor([]byte(" continuation\n"))
	if err == nil {
		t.Fatal("ParseDescriptor() accepted an orphan continuation")
	}
}

// A manifest's continuation lines are joined in linear time: a long logical
// line built one copy at a time cost seconds per megabyte and hours for an
// archive entry at the size limit.
func TestDescriptorContinuationLinesJoinInLinearTime(t *testing.T) {
	var manifest bytes.Buffer
	manifest.WriteString("MIDlet-1: a,b,c\nMIDlet-Description: d\n")
	const lines = 20000
	for range lines {
		manifest.WriteString(" 0123456789\n")
	}
	var before, after goruntime.MemStats
	goruntime.GC()
	goruntime.ReadMemStats(&before)
	descriptor, err := ParseDescriptor(manifest.Bytes())
	goruntime.ReadMemStats(&after)
	if err != nil {
		t.Fatal(err)
	}
	if value := descriptor.Properties["MIDlet-Description"]; len(value) != 1+lines*10 {
		t.Fatalf("joined description is %d bytes, want %d", len(value), 1+lines*10)
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 64<<20 {
		t.Fatalf("parsing a %d-byte manifest allocated %d bytes", manifest.Len(), allocated)
	}
}

// A descriptor carrying two spellings of one key answers the exact spelling,
// and the same one every time: the save owner is read from it.
func TestDescriptorPropertyPrefersTheExactKeyDeterministically(t *testing.T) {
	descriptor := Descriptor{Properties: map[string]string{"DD-PROGNAME": "5678", "DD-ProgName": "1234", "dd-progname": "9999"}}
	for range 64 {
		if value, ok := descriptor.Property("DD-ProgName"); !ok || value != "1234" {
			t.Fatalf("Property(DD-ProgName) = %q, %v, want the exact key", value, ok)
		}
		if owner := SaveOwner(descriptor); owner != "1234" {
			t.Fatalf("SaveOwner() = %q, want the exact key's program number", owner)
		}
		if value, _ := descriptor.Property("Dd-PrOgNaMe"); value != "5678" {
			t.Fatalf("Property(Dd-PrOgNaMe) = %q, want the first spelling in byte order", value)
		}
	}
}
