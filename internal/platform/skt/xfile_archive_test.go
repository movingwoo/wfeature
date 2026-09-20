package skt

import (
	"bytes"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/api/skvm"
	"github.com/movingwoo/wfeature/internal/jvm"
)

func TestXFileReadsInstalledArchiveEntry(t *testing.T) {
	payload := []byte{7, 11, 19, 23}
	sidecar := makeJAR(t, map[string][]byte{"maps/0.bin": payload, "r": {41}})
	container := makeJAR(t, map[string][]byte{
		"game.msd": []byte("MIDlet-1: Canvas Fixture,,CanvasMIDlet\n"),
		"game.jar": canvasJAR,
		"maps.jar": sidecar,
	})
	archive, err := Open(container)
	if err != nil {
		t.Fatal(err)
	}
	if data, ok := archive.Resource("maps.jar"); !ok || !bytes.Equal(data, sidecar) {
		t.Fatal("installed resource JAR was discarded")
	}
	if _, ok := archive.Resource("game.jar"); ok {
		t.Fatal("primary JAR was installed a second time")
	}
	runtime, err := Start(archive, Options{Framebuffer: newTestFramebuffer(t, 4, 3)})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Destroy(true)
	for _, entry := range []string{"maps/0.bin", "/maps/0.bin", "r"} {
		file, err := runtime.VM.NewObject(skvm.XFileClass, "(Ljava/lang/String;Ljava/lang/String;)V",
			jvm.ReferenceValue(runtime.VM.NewString("maps.jar")), jvm.ReferenceValue(runtime.VM.NewString(entry)))
		if err != nil {
			t.Fatalf("open entry %q: %v", entry, err)
		}
		want := payload
		if entry == "r" {
			want = []byte{41}
		}
		buffer := jvm.NewByteArray(make([]byte, len(want)))
		read, err := runtime.VM.InvokeVirtual(file, "read", "([BII)I", jvm.ReferenceValue(buffer), jvm.IntValue(0), jvm.IntValue(int32(len(want))))
		if err != nil {
			t.Fatal(err)
		}
		count, _ := read.Int32()
		got, _ := jvm.ByteArraySnapshot(buffer)
		if int(count) != len(want) || !bytes.Equal(got, want) {
			t.Fatalf("entry %q: count=%d bytes=%v, want %v", entry, count, got, want)
		}
		written, err := runtime.VM.InvokeVirtual(file, "write", "([BII)I", jvm.ReferenceValue(buffer), jvm.IntValue(0), jvm.IntValue(1))
		count, _ = written.Int32()
		if err != nil || count != -1 {
			t.Fatalf("write returned %d, %v; want -1", count, err)
		}
		if !bytes.Equal(archive.Entries["maps.jar"], sidecar) {
			t.Fatal("archive bytes changed")
		}
	}
}

func TestXFileArchiveRejectsMissingAndUnsafeEntries(t *testing.T) {
	archive, err := Open(canvasJAR)
	if err != nil {
		t.Fatal(err)
	}
	archive.Entries["maps.jar"] = makeJAR(t, map[string][]byte{"map.bin": {1}})
	archive.Entries["broken.jar"] = []byte("not a zip")
	archive.Entries["unsafe.jar"] = makeJAR(t, map[string][]byte{"../map.bin": {1}})
	runtime, err := Start(archive, Options{Framebuffer: newTestFramebuffer(t, 4, 3)})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Destroy(true)
	for _, test := range [][2]string{{"absent.jar", "map.bin"}, {"broken.jar", "map.bin"}, {"unsafe.jar", "map.bin"}, {"maps.jar", "absent.bin"}, {"maps.jar", "../map.bin"}, {"../maps.jar", "map.bin"}} {
		_, err := runtime.VM.NewObject(skvm.XFileClass, "(Ljava/lang/String;Ljava/lang/String;)V",
			jvm.ReferenceValue(runtime.VM.NewString(test[0])), jvm.ReferenceValue(runtime.VM.NewString(test[1])))
		if err == nil || !strings.Contains(err.Error(), "java/io/IOException") {
			t.Fatalf("open %v: %v, want IOException", test, err)
		}
	}
}
