package jvm

import (
	"bytes"
	"fmt"
)

const (
	maxInternedStrings     = 1 << 16
	maxInternedStringBytes = 16 << 20
)

// InternString returns this VM's canonical object for a literal or native
// string constant. NewString and Java constructors still create distinct
// objects. Interned objects live until the VM is discarded; guest code cannot
// grow the pool beyond its count and text budgets.
func (vm *VM) InternString(text string) (*Object, error) {
	return vm.internString(text, nil)
}

func (vm *VM) internString(text string, receiver *Object) (*Object, error) {
	if vm == nil {
		return nil, fmt.Errorf("intern string without a VM")
	}
	vm.mu.Lock()
	defer vm.mu.Unlock()
	if object := vm.internedStrings[text]; object != nil {
		return object, nil
	}
	if len(vm.internedStrings) >= maxInternedStrings || len(text) > maxInternedStringBytes-vm.internedStringBytes {
		return nil, guestException("java/lang/OutOfMemoryError", "interned string pool limit exceeded")
	}
	if receiver == nil {
		receiver = vm.NewString(text)
		// Use the owned payload as the map key too, rather than retaining a
		// potentially large backing allocation supplied by a native caller.
		text, _ = StringText(receiver)
	} else {
		vm.objectIdentity(receiver)
	}
	vm.internedStrings[text] = receiver
	vm.internedStringBytes += len(text)
	return receiver, nil
}

func stringIntern(vm *VM, arguments []Value) (Value, error) {
	receiver, err := nativeReference(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	text, ok := StringText(receiver)
	if !ok {
		return VoidValue(), fmt.Errorf("String.intern receiver is not a string")
	}
	object, err := vm.internString(text, receiver)
	return ReferenceValue(object), err
}

// Pool roots are ordered by their text, rather than allocation order. Validate
// the complete index before restoring any VM-owned state.
func (saved HeapState) validateInternedStrings() error {
	if len(saved.InternedStrings) > maxInternedStrings {
		return fmt.Errorf("JVM interned string count exceeds limit")
	}
	var previous []byte
	total := 0
	for index, ref := range saved.InternedStrings {
		if ref == 0 || uint64(ref) > uint64(len(saved.Objects)) {
			return fmt.Errorf("JVM interned string reference is out of range")
		}
		object := saved.Objects[ref-1]
		if object.Class != StringClass || object.Identity == 0 || object.Native == 0 || uint64(object.Native) > uint64(len(saved.Payloads)) {
			return fmt.Errorf("JVM interned string object is invalid")
		}
		payload := saved.Payloads[object.Native-1]
		if payload.Kind != "string" || index > 0 && bytes.Compare(previous, payload.Data) >= 0 {
			return fmt.Errorf("JVM interned strings have invalid payloads, duplicate text, or order")
		}
		if len(payload.Data) > maxInternedStringBytes-total {
			return fmt.Errorf("JVM interned string text exceeds limit")
		}
		total += len(payload.Data)
		previous = payload.Data
	}
	return nil
}
