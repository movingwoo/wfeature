package ktf

import "github.com/movingwoo/wfeature/internal/armcore"

// nullPageSize is the first page of the guest address space.
const nullPageSize = 0x1000

// mapNullPage gives a recognized client zero-filled memory at address zero.
//
// Its own code goes through a null pointer on a path it was shipped with: the
// routine that writes a save's defaults looks a value up in a table the title
// has not loaded yet, so the lookup reads the table's row count at offset 0xc
// of nothing, fails its own range check on the zero it gets here, and stores
// its error code at offset 0x10 of the same nothing before answering zero. A
// handset of that generation answers an access to address zero; this platform
// stops the guest, which ended the title inside startApp. The page is read and
// written, since the same routine does both, and it starts as zeroes, the
// answer that takes the lookup's own failure path.
//
// The mapping travels with a checkpoint as every mapping does. It is selected
// by the client's digest alone and applies whatever the handset is, because
// the path is the title's answer to a save it does not accept.
// See docs/ktf.md#null-page-memory.
func mapNullPage(memory *armcore.Memory) error {
	return memory.Map(0, nullPageSize, armcore.PermissionReadWrite)
}
