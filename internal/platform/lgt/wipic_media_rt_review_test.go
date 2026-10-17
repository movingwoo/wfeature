package lgt

import "testing"

// MC_mdaClipFree gives the clip's record back to the platform arena, so a
// title that creates and frees a clip for every sound effect runs in the same
// space however long it plays.
func TestFreeingAClipReturnsItsRecord(t *testing.T) {
	client := mediaClient(t, nil)
	clip := callSlot(t, client, slotClipCreate, 0, 0, 0)
	used := client.arena.used()
	if code := int32(callSlot(t, client, slotClipFree, clip)); code != wipiSuccess {
		t.Fatalf("MC_mdaClipFree = %d", code)
	}
	if after := client.arena.used(); after != used-mediaClipRecordSize {
		t.Fatalf("the arena holds %d bytes after the free, want %d", after, used-mediaClipRecordSize)
	}
	for range 1000 {
		next := callSlot(t, client, slotClipCreate, 0, 0, 0)
		if next != clip {
			t.Fatalf("a new clip took %#x rather than the freed record at %#x", next, clip)
		}
		callSlot(t, client, slotClipFree, next)
	}
	if after := client.arena.used(); after != used-mediaClipRecordSize {
		t.Fatalf("create and free cycles left the arena at %d bytes, want %d", after, used-mediaClipRecordSize)
	}
	// A handle freed twice is a handle the platform no longer holds.
	if code := int32(callSlot(t, client, slotClipFree, clip)); code != wipiError {
		t.Fatalf("freeing a freed clip = %d, want an error code", code)
	}
}

// The queue of undelivered transitions is bounded to cap memory, and the cap
// drops the oldest transition rather than ending the run: a title that stops
// and plays an effect many times inside one tick has done nothing wrong.
func TestAClipsTransitionQueueDropsTheOldestWhenFull(t *testing.T) {
	clip := &mediaClip{callback: 0x1000}
	statuses := []uint32{mediaStarted, mediaStopped}
	for index := range 200 {
		if err := clip.notify(statuses[index%2]); err != nil {
			t.Fatalf("transition %d: %v", index, err)
		}
	}
	if len(clip.pending) != maxClipTransitions {
		t.Fatalf("%d transitions are queued, want %d", len(clip.pending), maxClipTransitions)
	}
	if last := clip.pending[len(clip.pending)-1]; last != statuses[199%2] {
		t.Fatalf("the newest queued transition is %d, want %d", last, statuses[199%2])
	}
	if first := clip.pending[0]; first != statuses[(200-maxClipTransitions)%2] {
		t.Fatalf("the oldest queued transition is %d, want %d", first, statuses[(200-maxClipTransitions)%2])
	}
}
