package lgt

import (
	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/platform/compatibility"
)

// nullPageMapping is the first page of the guest address space as
// lgt.null_page_memory maps it: zero-filled and read only.
//
// The page is for a recognized module whose own code reads through a null
// pointer. During the opening of the first game a process starts, the weapon
// key reaches a routine that reads the equipped item of the next weapon type
// through a slot the title fills only once the opening ends. Here the read
// stopped the guest. On the page it reads zeros: the title looks up an item
// that is not there, decodes nothing from address zero and goes on, and a
// later press, after the slots are filled, works as packaged. Zeros are what
// keep that harmless, so the page is read only: a store through a null pointer
// still stops the guest with the faulting instruction. Every other title keeps
// the unmapped page.
//
// The page is not what a handset held at address zero. A handset of that
// generation answered the read with whatever it had there, and filled with the
// values such a handset keeps there the same press ends in the title's own
// loader following a wild pointer. This is a choice to make a missing check in
// the title harmless.
// See docs/lgt.md#null-page-memory.
var nullPageMapping = armcore.MappingState{Address: 0, Size: 0x1000, Permission: armcore.PermissionRead}

func hasNullPageMemory(module []byte) bool {
	return compatibility.HasFix("lgt", compatibility.NativeModule, moduleDigest(module), compatibility.LGTNullPageMemory)
}

func mapNullPage(memory *armcore.Memory) error {
	return memory.Map(nullPageMapping.Address, nullPageMapping.Size, nullPageMapping.Permission)
}

// withNullPage is a recorded memory map with the page in it. A quick save of
// a recognized module taken before the page was mapped for it records a map
// without the page, and the page is the only difference: it holds no bytes,
// and it sorts first because nothing else is mapped below the module.
func withNullPage(mappings []armcore.MappingState) []armcore.MappingState {
	if len(mappings) > 0 && mappings[0] == nullPageMapping {
		return mappings
	}
	return append([]armcore.MappingState{nullPageMapping}, mappings...)
}
