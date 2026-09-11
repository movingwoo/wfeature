package sgsvm

import "fmt"

// ResourceByte reads a signed-offset byte from an instance-owned guest region.
// Original immutable resources are adjacent in the module: a read may pass a
// descriptor's length while remaining inside that validated region. Resized
// immutable resources retain their separate allocation bounds. Mutable banks
// occupy a second packed region whose offsets change when a bank is resized.
func (vm *VM) ResourceByte(index, offset int) byte {
	r := vm.Resource(index)
	if vm.Error() != nil {
		return 0
	}
	if offset >= 0 && offset < len(r.Data) {
		return r.Data[offset]
	}
	if offset >= 0 && !r.Mutable && len(r.Data) > 0 && index < len(vm.resourceViews) {
		view := vm.resourceViews[index]
		if offset < len(view) && &r.Data[0] == &view[0] {
			return view[offset]
		}
	}
	if offset >= 0 && r.Mutable {
		remaining := offset - len(r.Data)
		for i := index + 1; i < len(vm.Resources); i++ {
			if err := vm.ChargeWork(1); err != nil {
				return 0
			}
			next := &vm.Resources[i]
			if !next.Mutable {
				continue
			}
			if remaining < len(next.Data) {
				return next.Data[remaining]
			}
			remaining -= len(next.Data)
		}
	}
	vm.Fail(fmt.Errorf("SGS resource %d byte offset %d out of bounds", index, offset))
	return 0
}
