package ktf

import (
	"encoding/binary"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/testfixture"
)

func TestAuthoredCheckpointArchiveStartsAndRestoresWithoutStartup(t *testing.T) {
	archive, err := testfixture.KTFCheckpointArchive()
	if err != nil {
		t.Fatal(err)
	}
	store, err := backend.NewMemorySaveStore(nil)
	if err != nil {
		t.Fatal(err)
	}
	options := SessionOptions{SaveStore: store}
	source, err := StartSession(t.Context(), archive, options)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	if got := binary.LittleEndian.Uint32(readTestBytes(t, source.Client, testfixture.KTFCheckpointStartupCounter, 4)); got != 1 {
		t.Fatalf("fixture startup count = %d", got)
	}
	saved, err := source.CaptureCheckpoint(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	writeTestWords(t, source.Client, testfixture.KTFCheckpointStartupCounter, []uint32{9})
	prepared, err := PrepareSessionCheckpoint(archive, saved, options)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Discard()
	restored, err := prepared.Commit(t.Context(), source, store)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if got := binary.LittleEndian.Uint32(readTestBytes(t, restored.Client, testfixture.KTFCheckpointStartupCounter, 4)); got != 1 {
		t.Fatalf("fixture startup reran or old memory survived: %d", got)
	}
	if _, err := restored.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
}
