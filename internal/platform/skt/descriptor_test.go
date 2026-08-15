package skt

import "testing"

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
