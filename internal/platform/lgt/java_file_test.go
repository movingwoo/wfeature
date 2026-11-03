package lgt

import (
	"bytes"
	"context"
	"testing"
)

func TestJavaFileModesPreserveReadWriteAndTruncateExplicitly(t *testing.T) {
	for _, tc := range []struct {
		name     string
		mode     uint32
		want     []byte
		cursor   int
		writable bool
	}{
		{"read-only", 1, []byte{1, 1, 2, 1, 1}, 0, false},
		{"append", 2, []byte{1, 1, 2, 1, 1}, 5, true},
		{"truncate", 3, nil, 0, true},
		{"read-write", 4, []byte{1, 1, 2, 1, 1}, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := fixtureClient(t)
			client.saveStore = newMemorySaveStore()
			client.writeFile("settings.dat", []byte{1, 1, 2, 1, 1})
			name, err := client.newJavaString("settings.dat")
			if err != nil {
				t.Fatal(err)
			}
			const object = 0x1000
			if _, err := javaFileOpen(client, nil, nil, []uint32{object, name, tc.mode}); err != nil {
				t.Fatal(err)
			}
			_, file, err := client.javaFileHandle(object)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(file.data, tc.want) || file.cursor != tc.cursor || file.writable != tc.writable {
				t.Fatalf("mode %d: data %v cursor %d writable %v", tc.mode, file.data, file.cursor, file.writable)
			}
			if tc.mode == 4 {
				buffer, err := client.newJavaByteArray(make([]byte, 5))
				if err != nil {
					t.Fatal(err)
				}
				n, err := javaFileRead(client, nil, nil, []uint32{object, buffer})
				if err != nil || n != 5 {
					t.Fatalf("read = %d, %v", n, err)
				}
			}
		})
	}
}

// A DataInputStream is the stream it wraps, so closing the wrapper lets go of
// the file the stream under it holds open after its File closed, at once
// rather than at a collection.
func TestClosingAWrapperLetsTheFileUnderItGo(t *testing.T) {
	ctx := context.Background()
	store := newFaultStore(t, map[string]string{"fs/save.dat": "0123"})
	fixture := newJavaThreadFixture(t, store)
	client := fixture.client
	object := openJavaFile(t, client, "save.dat", 1)
	stream, err := javaFileOpenInputStream(client, ctx, client.thread, []uint32{object})
	if err != nil {
		t.Fatal(err)
	}
	wrapper, err := newTestObject(t, client, javaDataInputStreamClass)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := javaWrapStream(client, ctx, client.thread, []uint32{wrapper, stream}); err != nil {
		t.Fatal(err)
	}
	if _, err := javaFileClose(client, ctx, client.thread, []uint32{object}); err != nil {
		t.Fatal(err)
	}
	if _, err := javaStreamClose(client, ctx, client.thread, []uint32{wrapper}); err != nil {
		t.Fatal(err)
	}
	if len(client.files) != 0 || len(client.javaRun.files) != 0 {
		t.Fatalf("closing the wrapper left %d handles and %d bindings", len(client.files), len(client.javaRun.files))
	}
	if _, err := javaStreamClose(client, ctx, client.thread, []uint32{stream}); err != nil {
		t.Fatalf("closing the stream after its wrapper answered %v", err)
	}
}
