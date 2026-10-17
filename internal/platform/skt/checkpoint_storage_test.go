package skt

import (
	"errors"
	"fmt"
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
			reads := 2
			if records {
				_ = memory.StoreSave(rmsIndexKey, []byte("same"))
				_ = memory.StoreSave("rms/same", backend.EncodeSaveRecords([][]byte{{1, 2, 3, 4, 5, 6, 7, 8}}))
				runtime.rmsState = &rmsState{stores: make(map[string]*recordStore)}
				for range 8 {
					heap.stores = append(heap.stores, &recordStore{name: "same", open: 1})
				}
			} else {
				// Handles on one path share its one file, so a copy is made per
				// path rather than per handle: eight paths overrun the budget,
				// after the removal list and the five paths read before it ran
				// out.
				for index := range 8 {
					name := fmt.Sprintf("file%d", index)
					_ = memory.StoreSave("fs/"+name, make([]byte, 16))
					heap.files = append(heap.files, &xFileData{name: name, open: true})
				}
				reads = 6
			}
			if err := heap.rebuildCheckpointSavesWithin(store, 64); !errors.Is(err, backend.ErrCheckpointSaveRead) {
				t.Fatalf("unbounded reconstruction: %v", err)
			}
			if store.writes != 0 || store.reads > reads {
				t.Fatalf("cache rebuild wrote or reread saves: %d/%d", store.writes, store.reads)
			}
		})
	}
}

// Eight handles on one path rebuild one shared copy: the budget a single copy
// fits is enough, the path is read once, and every handle reads the same file.
func TestJavaCheckpointHandlesOnOnePathShareOneCopy(t *testing.T) {
	memory, _ := backend.NewMemorySaveStore(nil)
	store := &javaCheckpointFailStore{MemorySaveStore: memory}
	_ = memory.StoreSave("fs/same", make([]byte, 16))
	runtime := &Runtime{Archive: &Archive{Entries: map[string][]byte{}}}
	heap := newCheckpointHeap(runtime, time.Now())
	for _, name := range []string{"same", "/same", "./same", "same", "same", "same", "same", "same"} {
		heap.files = append(heap.files, &xFileData{name: name, open: true})
	}
	if err := heap.rebuildCheckpointSavesWithin(store, 40); err != nil {
		t.Fatal(err)
	}
	if store.writes != 0 || store.reads > 2 {
		t.Fatalf("cache rebuild wrote or reread saves: %d/%d", store.writes, store.reads)
	}
	for _, file := range heap.files {
		if file.node == nil || file.node != heap.files[0].node || len(file.node.data) != 16 {
			t.Fatal("handles on one path were not rebuilt onto one file")
		}
	}
}
