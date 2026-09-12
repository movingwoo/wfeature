package sgsvm

import "testing"

func TestResourceByteSharesOnlyOriginalModuleRegion(t *testing.T) {
	p, err := Parse(parserFixture(1))
	if err != nil {
		t.Fatal(err)
	}
	first, second := New(p, nil), New(p, nil)
	if got := first.ResourceByte(0, 3); got != 40 {
		t.Fatalf("adjacent module byte=%d", got)
	}
	first.Resources[1].Data[0] = 99
	if got := first.ResourceByte(0, 3); got != 40 {
		t.Fatalf("mutable runtime copy changed module byte: %d", got)
	}
	first.Resources[0].Data[0] = 77
	if got := second.ResourceByte(0, 0); got != 10 || p.Resources[0].Data[0] != 10 {
		t.Fatal("VM resource bytes alias other instance or program")
	}
	if first.ResourceByte(0, 5) != 0 || first.Error() == nil {
		t.Fatal("read escaped module region")
	}
}

func TestResourceByteAdjacentImmutableWritesAndDetachedBounds(t *testing.T) {
	p := &Program{Resources: []Resource{{Data: []byte{1, 2}}, {Data: []byte{3, 4}}}}
	vm := New(p, nil)
	vm.Resources[1].Data[0] = 9
	if vm.ResourceByte(0, 2) != 9 {
		t.Fatal("adjacent immutable bank lost shared ownership")
	}
	vm.Resources[0].Data = append([]byte(nil), vm.Resources[0].Data...)
	if vm.ResourceByte(0, 2) != 0 || vm.Error() == nil {
		t.Fatal("detached allocation retained module bounds")
	}
	for _, offset := range []int{-1, 2} {
		vm = New(&Program{Resources: []Resource{{Mutable: true, Data: []byte{1, 2}}, {Data: []byte{3}}}}, nil)
		if vm.ResourceByte(0, offset) != 0 || vm.Error() == nil {
			t.Fatal("mutable resource escaped allocation")
		}
	}
}

func TestResourceBytePackedMutableRegionTracksResize(t *testing.T) {
	p := &Program{Resources: []Resource{
		{Mutable: true, Data: []byte{1, 2}},
		{Data: []byte{99}},
		{Mutable: true},
		{Mutable: true, Data: []byte{0xc0, 4}},
	}}
	vm := New(p, nil)
	if got := vm.ResourceByte(0, 2); got != 0xc0 {
		t.Fatalf("packed mutable byte=%d", got)
	}
	vm.Resources[0].Data = append(vm.Resources[0].Data, 5, 6)
	if got := vm.ResourceByte(0, 4); got != 0xc0 {
		t.Fatalf("resized mutable byte=%d", got)
	}
	vm.Resources[3].Data[0] = 7
	if got := vm.ResourceByte(0, 4); got != 7 {
		t.Fatalf("cross-bank read ignored write: %d", got)
	}
	other := New(p, nil)
	if other.ResourceByte(0, 2) != 0xc0 {
		t.Fatal("mutable arena leaked between instances")
	}
	if vm.ResourceByte(0, 6) != 0 || vm.Error() == nil {
		t.Fatal("read escaped packed mutable region")
	}
}
