package lgt

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
)

// Closing a session is what a Host does to every game it holds when it stops,
// and on this platform it is the last moment for a file the title wrote and
// never closed: those bytes are in the handle's buffer and nowhere else. A
// Host that ends a game without closing it loses them, which is why a stopping
// server closes attached games as well as parked ones.
func TestClosingASessionStoresAnUnclosedWrite(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "saves")
	archive, err := Open(fixtureArchive(t))
	if err != nil {
		t.Fatal(err)
	}
	client, err := Load(archive, Options{
		Width: 16, Height: 8,
		SaveStore: backend.NewDirectorySaveStore(directory),
	})
	if err != nil {
		t.Fatal(err)
	}
	client.files = map[uint32]*openFile{
		1: {name: "progress.dat", fileContent: &fileContent{data: []byte("written and left open"), dirty: true}, writable: true},
	}
	session := &Session{client: client, archive: archive}
	if err := session.Close(context.Background()); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Read through a store of its own, the way the next run of the game would.
	key, err := fileSaveKey("progress.dat")
	if err != nil {
		t.Fatalf("the file name has no save key: %v", err)
	}
	stored, found, err := backend.ReadSave(backend.NewDirectorySaveStore(directory), key)
	if err != nil || !found || !bytes.Equal(stored, []byte("written and left open")) {
		t.Fatalf("after the close the store holds %q (found=%t, err=%v), want the bytes the title wrote", stored, found, err)
	}
}

// What a title writes in its own destroy entry, into a file it never closes,
// reaches the store when the session closes.
func TestClosingASessionStoresWhatDestroyWrote(t *testing.T) {
	store := newFaultStore(t, nil)
	fixture := newJavaThreadFixture(t, store)
	client := fixture.client
	files := newGuestFiles(t, client)
	handle := files.open("final.dat", fileOpenReadWrite)
	if err := client.core.Memory().Write(files.buffer, []byte("FINAL")); err != nil {
		t.Fatal(err)
	}
	write, err := client.stub(svcCategoryWIPIC, slotFsWrite)
	if err != nil {
		t.Fatal(err)
	}
	client.clet.Destroy = fixture.install(func(a *assembler) {
		a.emit(armPushLR)
		a.literal(0, handle)
		a.literal(1, files.buffer)
		a.literal(2, 5)
		a.literal(4, write)
		a.call(4)
		a.emit(armPopPC)
	})
	if err := fixture.session.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := store.held("fs/final.dat"); got != "FINAL" {
		t.Fatalf("after the close the store holds %q, want what destroyClet wrote", got)
	}
}

// Bytes a title wrote through a stream opened on a File, and never flushed,
// reach the store when the session ends.
func TestTheEndingDrainsAFileOutputStream(t *testing.T) {
	ctx := context.Background()
	store := newFaultStore(t, nil)
	fixture := newJavaThreadFixture(t, store)
	client := fixture.client
	object := openJavaFile(t, client, "stream.dat", 4)
	sink, err := javaFileOpenOutputStream(client, ctx, client.thread, []uint32{object})
	if err != nil {
		t.Fatal(err)
	}
	array, err := client.newJavaByteArray([]byte("MARKER-stream"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := javaByteSinkWriteAll(client, ctx, client.thread, []uint32{sink, array}); err != nil {
		t.Fatal(err)
	}
	if err := fixture.session.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if got := store.held("fs/stream.dat"); got != "MARKER-stream" {
		t.Fatalf("after the close the store holds %q, want what the stream was given", got)
	}
}
