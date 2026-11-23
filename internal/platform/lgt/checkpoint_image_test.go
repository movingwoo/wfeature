package lgt

import (
	"context"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
)

func TestCheckpointPreservesSharedImageSourceLifetime(t *testing.T) {
	archive := fixtureArchive(t)
	store, _ := backend.NewMemorySaveStore(nil)
	session := checkpointFixtureSession(t, archive, store)
	client := session.client
	encoded := encodedTestImage(t, 8, 8)
	size := uint32(len(encoded))
	address := callSlot(t, client, slotCalloc, 2*size)
	if err := client.core.Memory().Write(address, append(encoded, encoded...)); err != nil {
		t.Fatal(err)
	}
	first := imageFromGuestBuffer(t, client, address, 0, size)
	second := imageFromGuestBuffer(t, client, address, size, size)
	checkpoint := roundTripCheckpoint(t, archive, session)
	prepared := prepareFixtureCheckpoint(t, archive, checkpoint, store)
	restored, err := prepared.Commit(context.Background(), nil, store)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close(context.Background())
	callSlot(t, restored.client, slotDestroyImage, second)
	if restored.client.heap.sizes[address] == 0 {
		t.Fatal("destroying one restored image freed the shared allocation")
	}
	callSlot(t, restored.client, slotDestroyImage, first)
	if restored.client.heap.sizes[address] != 0 {
		t.Fatal("the last restored image kept its source")
	}

	record, err := decodeLGTImageRecord(checkpoint.Runtime)
	if err != nil || len(record.Sources) != 2 {
		t.Fatalf("record has %d image sources: %v", len(record.Sources), err)
	}
	for name, entry := range map[string]imageSourceState{
		"duplicate image":  record.Sources[0],
		"non-image handle": {Image: address, Source: address},
		"interior source":  {Image: second, Source: address + 1},
	} {
		t.Run(name, func(t *testing.T) {
			damaged := record
			damaged.Sources = []imageSourceState{record.Sources[0], entry}
			slot := checkpoint
			slot.Runtime, err = damaged.encode()
			if err != nil {
				t.Fatal(err)
			}
			if loaded, err := PrepareSessionCheckpoint(archive, slot, SessionOptions{}); err == nil {
				loaded.Discard()
				t.Fatal("accepted a damaged image source table")
			}
		})
	}

	// The pre-ownership record contains exactly the same surfaces and heap.
	// An older slot cannot identify the allocation to release.
	record.Sources = nil
	checkpoint.Runtime, err = record.encode()
	if err != nil {
		t.Fatal(err)
	}
	legacy := prepareFixtureCheckpoint(t, archive, checkpoint, store)
	defer legacy.Discard()
	old := legacy.session.client
	callSlot(t, old, slotDestroyImage, first)
	callSlot(t, old, slotDestroyImage, second)
	if old.heap.sizes[address] == 0 {
		t.Fatal("destroying an older image freed an allocation it never recorded")
	}
}
