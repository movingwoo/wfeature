package lgt

import (
	"context"
	"testing"
)

// What an LGT title links against exists nowhere except the resolutions it
// makes while it starts, so what this pins is that they are kept — and kept
// apart: a slot that would be serviced and a slot that only resolves are both
// answered with a stub, and only the record says which is which.
func TestResolvedImportsSeparateWhatIsAnsweredFromWhatOnlyResolves(t *testing.T) {
	archive, err := Open(fixtureArchive(t))
	if err != nil {
		t.Fatal(err)
	}
	client, err := Load(archive, Options{Width: 16, Height: 8, MaxSteps: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(client.ResolvedImports()) != 0 {
		t.Fatal("a client that has not started has already resolved something")
	}
	if err := client.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	records := map[uint32]ImportRecord{}
	for _, record := range client.ResolvedImports() {
		if record.Category != svcCategoryWIPIC {
			t.Errorf("the fixture resolved %s, which it does not ask for", record.Describe())
			continue
		}
		records[record.Slot] = record
	}
	// The fixture module resolves these four and nothing else. Each one is
	// serviced, so each one is a record that says so, named rather than
	// numbered.
	for _, slot := range []uint32{
		slotCletRegister, slotGetScreenFramebuffer, slotFramebufferPointer, slotFlushLcd,
	} {
		record, ok := records[slot]
		if !ok {
			t.Fatalf("the module resolved slot %#x and it was not recorded: %v", slot, client.ResolvedImports())
		}
		if !record.Implemented {
			t.Errorf("slot %#x is serviced but the record calls it unimplemented", slot)
		}
		if record.Name != wipicSlotNames[slot] || record.Name == "" {
			t.Errorf("slot %#x is named %q, want %q", slot, record.Name, wipicSlotNames[slot])
		}
	}

	// A slot with no implementation resolves the same way — refusing here would
	// stop a title over a function it never calls — and is what the record has
	// to tell apart.
	const unimplemented uint32 = 0x4c9
	if knownWIPICSlot(unimplemented) || unknownSlotAccepted(unimplemented) {
		t.Fatalf("slot %#x is implemented now; pick another for this test", unimplemented)
	}
	if _, err := client.importFunction(importTableWIPIC, unimplemented); err != nil {
		t.Fatalf("resolving an unimplemented slot = %v, want a stub", err)
	}
	var found bool
	for _, record := range client.ResolvedImports() {
		if record.Category == svcCategoryWIPIC && record.Slot == unimplemented {
			found = true
			if record.Implemented {
				t.Errorf("%s is recorded as implemented", record.Describe())
			}
		}
	}
	if !found {
		t.Errorf("resolving slot %#x recorded nothing", unimplemented)
	}
}

// A Java title resolves its imports from tables this platform packs into one
// slot space of its own. A report that printed the packed number would be
// naming an address in this platform rather than anything the module passed.
func TestAJavaAuxiliaryImportIsDescribedAsTheTableTheModuleAsked(t *testing.T) {
	archive, err := Open(fixtureArchive(t))
	if err != nil {
		t.Fatal(err)
	}
	client, err := Load(archive, Options{Width: 16, Height: 8})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.importFunction(0x1fc, 3); err != nil {
		t.Fatalf("resolving an auxiliary Java table = %v, want a stub", err)
	}
	var described string
	var implemented bool
	for _, record := range client.ResolvedImports() {
		described = record.Describe()
		implemented = record.Implemented
	}
	if described != "java table 0x1fc index 0x3" {
		t.Fatalf("Describe() = %q", described)
	}
	if !implemented {
		t.Fatal("an auxiliary Java call accepted by the dispatcher is reported unimplemented")
	}
}

// A Java slot's metadata is useful for naming a failure, but it does not make
// the call executable. The coverage answer follows the same branches as the
// dispatcher: baked slots may be inherited, and a named metadata entry still
// needs a registered method body.
func TestJavaVirtualImportCoverageMatchesDispatch(t *testing.T) {
	client := fixtureClient(t)
	for _, class := range []string{javaStackClass, javaDataInputStreamClass, "org/kwis/msp/lwc/Component"} {
		if _, err := client.preparePlatformJavaClass(class); err != nil {
			t.Fatal(err)
		}
	}
	layout := newJavaLayout()
	layout.classes["org/kwis/msp/lwc/Component"] = &javaLayoutClass{
		Name:  "org/kwis/msp/lwc/Component",
		Super: "java/lang/Object",
		Virtual: map[string]uint32{
			"getHeight()I":  12,
			"setHeight(I)V": 13,
		},
	}
	client.javaLink = &javaLink{
		layout: layout,
		surface: &javaSurface{
			Classes: []javaAPIClass{{
				Name:           "org/kwis/msp/lwc/Component",
				VirtualMethods: javaRun{Count: 2},
			}},
			VirtualMethods: []javaMemberRef{
				{Name: "getHeight", Descriptor: "()I"},
				{Name: "setHeight", Descriptor: "(I)V"},
			},
		},
	}

	for _, test := range []struct {
		name        string
		slot        uint32
		implemented bool
		called      string
	}{
		{
			name:        "inherited baked slot",
			slot:        javaVirtualSlot(javaStackClass, 15),
			implemented: true,
			called:      javaVectorClass + ".size()I",
		},
		{
			name:        "metadata method with a body",
			slot:        javaVirtualSlot("org/kwis/msp/lwc/Component", 12),
			implemented: true,
			called:      "org/kwis/msp/lwc/Component.getHeight()I",
		},
		{
			name:        "named metadata method without a body",
			slot:        javaVirtualSlot("org/kwis/msp/lwc/Component", 13),
			implemented: false,
			called:      "org/kwis/msp/lwc/Component.setHeight(I)V",
		},
		{
			name:        "unsupported baked slot",
			slot:        javaVirtualSlot(javaDataInputStreamClass, 26),
			implemented: false,
			called:      javaDataInputStreamClass + ".slot26",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			served, _ := client.callJavaPlatformVirtual(context.Background(), client.thread, test.slot)
			if served != test.implemented {
				t.Fatalf("dispatcher served = %v, want %v", served, test.implemented)
			}
			if got := client.importImplemented(svcCategoryJava, test.slot); got != test.implemented {
				t.Errorf("import implemented = %v, want %v", got, test.implemented)
			}
			if got := client.javaSlotName(test.slot); got != test.called {
				t.Errorf("slot name = %q, want %q", got, test.called)
			}
		})
	}
}

// Static entries have two dispatch rules beyond an exact method lookup: the
// unnamed entries at the front of each class run answer with the class, and an
// unnamed method can be identified by a unique descriptor. Both are executable
// contracts; a merely named entry remains only a diagnostic.
func TestJavaStaticImportCoverageMatchesDispatch(t *testing.T) {
	client := fixtureClient(t)
	const class = "org/kwis/msf/io/Network"
	client.javaLink = &javaLink{surface: &javaSurface{
		Classes: []javaAPIClass{{Name: class, StaticMethods: javaRun{Count: 5}}},
		StaticMethods: []javaMemberRef{
			{},
			{},
			{Name: "disconnect", Descriptor: "()V"},
			{Descriptor: "()I"},
			{Name: "missing", Descriptor: "()V"},
		},
	}}

	for _, test := range []struct {
		name        string
		index       uint32
		implemented bool
		called      string
	}{
		{name: "class entry", index: 0, implemented: true, called: class + ".<class>"},
		{name: "named method", index: 2, implemented: true, called: class + ".disconnect()V"},
		{name: "descriptor method", index: 3, implemented: true, called: class + ".connect()I"},
		{name: "named method without a body", index: 4, implemented: false, called: class + ".missing()V"},
	} {
		t.Run(test.name, func(t *testing.T) {
			slot := javaStaticMethodSlot(test.index)
			var served bool
			if _, _, special := client.javaLink.unnamedStaticEntry(test.index); special {
				served = client.handleJavaSVC(context.Background(), client.thread, slot) == nil
			} else {
				served, _ = client.callJavaPlatformStatic(context.Background(), client.thread, test.index)
			}
			if served != test.implemented {
				t.Fatalf("dispatcher served = %v, want %v", served, test.implemented)
			}
			if got := client.importImplemented(svcCategoryJava, slot); got != test.implemented {
				t.Errorf("import implemented = %v, want %v", got, test.implemented)
			}
			if got := client.javaSlotName(slot); got != test.called {
				t.Errorf("slot name = %q, want %q", got, test.called)
			}
		})
	}
}
