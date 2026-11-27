package lgt

import (
	"bytes"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/wipic"
)

func TestCheckpointRestoresALiveDateComponent(t *testing.T) {
	archive := fixtureArchive(t)
	store, _ := backend.NewMemorySaveStore(nil)
	source := checkpointFixtureSession(t, archive, store)
	name := writeGuest(t, source.client, append([]byte("DateTimeComponent"), 0))
	class := callSlot(t, source.client, 0x321, name)
	first := callSlot(t, source.client, 0x322, uicApplicationContext, class)
	handle := callSlot(t, source.client, 0x322, uicApplicationContext, class)
	callSlot(t, source.client, slotUicDestroy, first)
	callSlot(t, source.client, 0x337, handle, 12345678)
	out := writeGuest(t, source.client, make([]byte, wipic.TimeRecordSize))
	checkpoint := roundTripCheckpoint(t, archive, source)
	prepared := prepareFixtureCheckpoint(t, archive, checkpoint, store)
	restored, err := prepared.Commit(t.Context(), nil, store)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close(t.Context())
	callSlot(t, restored.client, 0x338, handle, out)
	want := wipic.TimeRecord(time.Unix(12345678, 0).In(time.Local))
	got := make([]byte, len(want))
	if err := restored.client.core.Memory().Read(out, got); err != nil || !bytes.Equal(got, want) {
		t.Fatalf("restored component time = %x (%v), want %x", got, err, want)
	}
	if id := callSlot(t, restored.client, slotUicCreate, uicApplicationContext, class); id != first {
		t.Fatalf("restored table assigned %#x, want freed handle %#x", id, first)
	}
	callSlot(t, restored.client, slotUicDestroy, first)
	callSlot(t, restored.client, slotUicDestroy, handle)
	empty := roundTripCheckpoint(t, archive, restored)
	if v, ok := leadingRecordVersion(empty.Runtime); !ok || v >= sessionDateCheckpointVersion {
		t.Fatalf("session without date components wrote version %d", v)
	}
}

func TestDateCheckpointRejectsInvalidHandleTables(t *testing.T) {
	oversized := make([]*uicDate, maxDateComponents+1)
	oversized[maxDateComponents] = new(uicDate)
	for _, dates := range [][]*uicDate{nil, {new(uicDate), nil}, oversized} {
		record, err := backend.EncodeCheckpointRecord(sessionDateCheckpointState{
			Version: sessionDateCheckpointVersion,
			Image:   sessionImageCheckpointState{Version: sessionImageCheckpointVersion},
			Dates:   dates,
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := decodeLGTDateRecord(record); err == nil {
			t.Fatalf("accepted an invalid handle table of length %d", len(dates))
		}
	}
}
