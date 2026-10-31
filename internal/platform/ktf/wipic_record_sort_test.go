package ktf

import (
	"context"
	"encoding/binary"
	"slices"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
)

// sortCall drives MC_dbSortRecords on the client's own thread, whose stack
// carries the fifth argument, and answers the result and the ids written.
func sortCall(t *testing.T, client *Client, runtime *initializationRuntime, handle, capacity, compare, filter uint32) (uint32, []uint32) {
	t.Helper()
	const buffer = wipicrt.PlatformDataBase + 0xc000
	thread := client.Thread
	runtime.CurrentThread, runtime.CurrentContext = thread, context.Background()
	writeGuestWords(t, runtime, buffer, make([]uint32, 8)...)
	// The thread's stack pointer starts at the top of the stack, which is the
	// first byte past it; the call's fifth argument sits below.
	top, err := thread.Register(armcore.RegisterSP)
	if err != nil {
		t.Fatal(err)
	}
	stack := top - 0x100
	if err := thread.SetRegister(armcore.RegisterSP, stack); err != nil {
		t.Fatal(err)
	}
	defer thread.SetRegister(armcore.RegisterSP, top)
	writeGuestWords(t, runtime, stack, filter)
	for index, value := range []uint32{handle, buffer, capacity, compare} {
		if err := thread.SetRegister(index, value); err != nil {
			t.Fatal(err)
		}
	}
	result, err := runtime.handleWIPICRecordDatabaseCall(thread, wipicRecordDatabaseSort)
	if err != nil {
		t.Fatalf("MC_dbSortRecords: %v", err)
	}
	ids := make([]uint32, 0, 8)
	data := make([]byte, 32)
	if err := runtime.client.Core().Memory().Read(buffer, data); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < int(min(result, 8)); index++ {
		ids = append(ids, binary.LittleEndian.Uint32(data[index*4:]))
	}
	return result, ids
}

// installThumb writes a Thumb function and answers its address.
func installThumb(t *testing.T, runtime *initializationRuntime, address uint32, body ...uint16) uint32 {
	t.Helper()
	data := make([]byte, len(body)*2)
	for index, instruction := range body {
		binary.LittleEndian.PutUint16(data[index*2:], instruction)
	}
	if err := runtime.client.Core().Memory().Write(address, data); err != nil {
		t.Fatal(err)
	}
	return address | 1
}

// The specification's example: four two-byte records come back in the order
// of their bytes, and the stored database is left as it was. The record at
// id 3 was deleted, which is the slot a sort has to skip.
func TestSortRecordsByBytesAndByTheTitlesFunctions(t *testing.T) {
	client, runtime := newTestRuntime(t)
	store := &runtimeRecordDatabase{name: "scores", recordSize: 2, records: [][]byte{
		{0x33, 0x22}, {0x11, 0x22}, nil, {0x22, 0xff}, {0x11, 0x00},
	}}
	const handle = recordDatabaseHandleBit | 1
	runtime.recordDatabases = map[string]*runtimeRecordDatabase{store.name: store}
	runtime.recordDatabaseHandles = map[uint32]*runtimeRecordDatabaseHandle{handle: {store: store}}

	if result, ids := sortCall(t, client, runtime, handle, 8, 0, 0); result != 4 || !slices.Equal(ids, []uint32{5, 2, 4, 1}) {
		t.Fatalf("byte order = %d %v, want 4 [5 2 4 1]", result, ids)
	}
	if store.records[0][0] != 0x33 || store.records[2] != nil {
		t.Fatal("the sort changed the stored database")
	}

	// compare(a, b) = a[0] - b[0]: a positive answer puts the first record
	// first, so this is descending by the first byte, ties in id order.
	compare := installThumb(t, runtime, wipicrt.ImageBase+0x40,
		0x7800, // ldrb r0, [r0]
		0x7809, // ldrb r1, [r1]
		0x1a40, // subs r0, r0, r1
		0x4770, // bx lr
	)
	// filter(rec) = rec[1]: zero leaves a record out.
	filter := installThumb(t, runtime, wipicrt.ImageBase+0x60,
		0x7840, // ldrb r0, [r0, #1]
		0x4770, // bx lr
	)
	if result, ids := sortCall(t, client, runtime, handle, 8, compare, 0); result != 4 || !slices.Equal(ids, []uint32{1, 4, 2, 5}) {
		t.Fatalf("the title's order = %d %v, want 4 [1 4 2 5]", result, ids)
	}
	if result, ids := sortCall(t, client, runtime, handle, 8, 0, filter); result != 3 || !slices.Equal(ids, []uint32{2, 4, 1}) {
		t.Fatalf("filtered byte order = %d %v, want 3 [2 4 1]", result, ids)
	}
	if result, ids := sortCall(t, client, runtime, handle, 8, compare, filter); result != 3 || !slices.Equal(ids, []uint32{1, 4, 2}) {
		t.Fatalf("filtered title order = %d %v, want 3 [1 4 2]", result, ids)
	}

	if result, _ := sortCall(t, client, runtime, handle, 3, 0, 0); result != wipicrt.WIPICErrorShortBuf {
		t.Fatalf("four ids into three = %#x, want M_E_SHORTBUF", result)
	}
	if result, _ := sortCall(t, client, runtime, handle, 0, 0, 0); result != wipicrt.WIPICErrorInvalid {
		t.Fatalf("a zero length = %#x, want M_E_INVALID", result)
	}
	if result, _ := sortCall(t, client, runtime, recordDatabaseHandleBit|9, 8, 0, 0); result != wipicrt.WIPICErrorBadFD {
		t.Fatalf("an unknown handle = %#x, want M_E_BADFD", result)
	}
	if len(runtime.WIPICAllocations) != 0 {
		t.Fatalf("the record copies were not given back: %d allocations left", len(runtime.WIPICAllocations))
	}
}

func TestRecordDatabaseAccessMode(t *testing.T) {
	_, runtime := newTestRuntime(t)
	runtime.client.SaveStore = wipicrt.NewDirectorySaveStore(t.TempDir())
	runtime.recordDatabases = map[string]*runtimeRecordDatabase{"scores": {name: "scores"}}

	call := func(name string, null bool) uint32 {
		t.Helper()
		const address = wipicrt.PlatformDataBase + 0xa000
		if err := runtime.client.Core().Memory().Write(address, append([]byte(name), 0)); err != nil {
			t.Fatal(err)
		}
		thread := armcore.NewThread(armcore.Context{})
		pointer := uint32(address)
		if null {
			pointer = 0
		}
		if err := thread.SetRegister(0, pointer); err != nil {
			t.Fatal(err)
		}
		result, err := runtime.handleWIPICRecordDatabaseCall(thread, wipicRecordDatabaseAccessMode)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	if got := call("scores", false); got != 1 {
		t.Fatalf("MC_dbGetAccessMode(scores) = %#x, want MC_DIR_PRIVATE_ACCESS", got)
	}
	if got := call("missing", false); got != wipicrt.WIPICErrorNotFound {
		t.Fatalf("MC_dbGetAccessMode(missing) = %#x, want M_E_NOENT", got)
	}
	if got := call("", true); got != wipicrt.WIPICErrorInvalid {
		t.Fatalf("MC_dbGetAccessMode(NULL) = %#x, want M_E_INVALID", got)
	}
}
