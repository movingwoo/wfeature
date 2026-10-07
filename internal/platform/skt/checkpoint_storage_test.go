package skt

import (
	"errors"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
)

func TestJavaCheckpointSaveCacheCopiesShareAnAllocationBudget(t *testing.T) {
	for _, records := range []bool{false, true} {
		t.Run(map[bool]string{false: "files", true: "records"}[records], func(t *testing.T) {
			memory, _ := backend.NewMemorySaveStore(nil)
			store := &javaCheckpointFailStore{MemorySaveStore: memory}
			runtime := &Runtime{Archive: &Archive{Entries: map[string][]byte{}}}
			heap := newCheckpointHeap(runtime, time.Now())
			if records {
				_ = memory.StoreSave(rmsIndexKey, []byte("same"))
				_ = memory.StoreSave("rms/same", backend.EncodeSaveRecords([][]byte{{1, 2, 3, 4, 5, 6, 7, 8}}))
				runtime.rmsState = &rmsState{stores: make(map[string]*recordStore)}
				for range 8 {
					heap.stores = append(heap.stores, &recordStore{name: "same", open: 1})
				}
			} else {
				_ = memory.StoreSave("fs/same", make([]byte, 16))
				for range 8 {
					heap.files = append(heap.files, &xFileData{name: "same", open: true})
				}
			}
			if err := heap.rebuildCheckpointSavesWithin(store, 64); !errors.Is(err, backend.ErrCheckpointSaveRead) {
				t.Fatalf("unbounded reconstruction: %v", err)
			}
			if store.writes != 0 || store.reads > 2 {
				t.Fatalf("cache rebuild wrote or reread saves: %d/%d", store.writes, store.reads)
			}
		})
	}
}
