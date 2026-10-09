package lgt

import (
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/testfixture"
)

type lgtListenerRawRecord struct {
	handle, header, methods, callback, interfaces, entry uint32
}

func lgtListenerRecord(fixture *lgtWIPIListenerFixture) lgtListenerRawRecord {
	fixture.t.Helper()
	handle := fixture.word(testfixture.LGTListenerClassHandle)
	header := handle - javaClassHeader
	methods := fixture.word(header + javaClassMethodRun)
	interfaces := fixture.word(header + javaClassInterfaces)
	record := lgtListenerRawRecord{handle: handle, header: header, methods: methods,
		interfaces: interfaces, entry: fixture.word(interfaces + 4)}
	for index := uint32(0); index < fixture.word(methods); index++ {
		member := methods + 4 + index*javaMethodWords*4
		name, _ := fixture.client.readPrintableString(fixture.word(member + 4))
		if name == "playUpdate" {
			record.callback = member
		}
	}
	if record.callback == 0 {
		fixture.t.Fatal("authored raw record has no playUpdate member")
	}
	return record
}

func lgtListenerValidationString(fixture *lgtWIPIListenerFixture, value string) uint32 {
	fixture.t.Helper()
	address, err := fixture.client.allocateBytes(append([]byte(value), 0))
	if err != nil {
		fixture.t.Fatal(err)
	}
	return address
}

// cloneLGTListenerRecord makes another ordinary raw AOT class, with its own
// header and member ownership words. Its method bodies remain the authored
// executable callback; no host function stands in for guest code.
func cloneLGTListenerRecord(fixture *lgtWIPIListenerFixture, name string) uint32 {
	fixture.t.Helper()
	original := lgtListenerRecord(fixture)
	end := original.methods + 4 + fixture.word(original.methods)*javaMethodWords*4
	bytes := make([]byte, end-original.header)
	if err := fixture.client.core.Memory().Read(original.header, bytes); err != nil {
		fixture.t.Fatal(err)
	}
	header, err := fixture.client.allocateBytes(bytes)
	if err != nil {
		fixture.t.Fatal(err)
	}
	handle := header + javaClassHeader
	methods := header + original.methods - original.header
	fixture.writeWord(header+8, lgtListenerValidationString(fixture, name))
	fixture.writeWord(header+javaClassMethodRun, methods)
	fixture.writeWord(handle+8, header)
	for index := uint32(0); index < fixture.word(methods); index++ {
		fixture.writeWord(methods+4+index*javaMethodWords*4, handle)
	}
	return handle
}

func setLGTListenerRawInterfaces(fixture *lgtWIPIListenerFixture, handle uint32, entries ...[2]uint32) {
	fixture.t.Helper()
	if len(entries) == 0 {
		fixture.writeWord(handle-javaClassHeader+javaClassInterfaces, 0)
		return
	}
	words := []uint32{uint32(len(entries))}
	for _, entry := range entries {
		address, err := fixture.client.allocateWords(entry[:])
		if err != nil {
			fixture.t.Fatal(err)
		}
		words = append(words, address)
	}
	table, err := fixture.client.allocateWords(words)
	if err != nil {
		fixture.t.Fatal(err)
	}
	fixture.writeWord(handle-javaClassHeader+javaClassInterfaces, table)
}

func newLGTListenerRawInterface(fixture *lgtWIPIListenerFixture, name string) uint32 {
	fixture.t.Helper()
	handle := cloneLGTListenerRecord(fixture, name)
	header := handle - javaClassHeader
	fixture.writeWord(header, 0x601)
	fixture.writeWord(header+0x10, 0)
	fixture.writeWord(header+javaClassMethodRun, 0)
	setLGTListenerRawInterfaces(fixture, handle)
	return handle
}

func newLGTListenerFromRawClass(fixture *lgtWIPIListenerFixture, handle uint32) uint32 {
	fixture.t.Helper()
	class, err := fixture.client.prepareJavaClass(fixture.t.Context(), fixture.client.thread, handle)
	if err != nil {
		fixture.t.Fatal(err)
	}
	object, err := fixture.client.allocateJavaObject(class)
	if err != nil {
		fixture.t.Fatal(err)
	}
	return object
}

func tryLGTListenerSetter(fixture *lgtWIPIListenerFixture, clip, listener uint32) error {
	fixture.t.Helper()
	thread := armcore.NewThread(armcore.NewContext())
	for index, word := range []uint32{clip, listener} {
		if err := thread.SetRegister(index, word); err != nil {
			fixture.t.Fatal(err)
		}
	}
	const called = "setListener(Lorg/kwis/msp/media/PlayListener;)V"
	return fixture.client.callJavaMethod(fixture.t.Context(), thread, javaClipClass, called,
		javaPlatformMethods[javaClipClass+"."+called])
}

func TestLGTWIPIListenerRejectsInvalidNativeTargetsWithoutReplacement(t *testing.T) {
	for _, test := range []struct {
		name   string
		target func(*lgtWIPIListenerFixture) uint32
	}{
		{"unknown address", func(_ *lgtWIPIListenerFixture) uint32 { return 0xfffffff8 }},
		{"issued Clip instead of listener", func(f *lgtWIPIListenerFixture) uint32 { return f.clip }},
		{"unissued object with copied vtable", func(f *lgtWIPIListenerFixture) uint32 {
			address, err := f.client.allocateWords([]uint32{f.word(f.listeners[1]), 0, f.word(f.listeners[1] + 8)})
			if err != nil {
				f.t.Fatal(err)
			}
			return address
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newLGTWIPIListenerFixture(t)
			fixture.setListener(fixture.listeners[0])
			if err := tryLGTListenerSetter(fixture, fixture.clip, test.target(fixture)); err == nil {
				t.Fatal("invalid listener was accepted")
			}
			if fixture.client.clips[fixture.clip].listener != fixture.listeners[0] {
				t.Fatal("rejected listener replaced the previous recipient")
			}
			fixture.call("play", true)
			fixture.drain()
			fixture.wantHistory(fixture.event(0, 2))
		})
	}
}

func TestLGTWIPIListenerSetterRequiresIssuedClipOwner(t *testing.T) {
	for _, unissued := range []bool{false, true} {
		t.Run(map[bool]string{false: "issued non-Clip", true: "unissued copied Clip"}[unissued], func(t *testing.T) {
			fixture := newLGTWIPIListenerFixture(t)
			owner := fixture.listeners[1]
			if unissued {
				var err error
				owner, err = fixture.client.allocateWords([]uint32{fixture.word(fixture.clip), 0, fixture.word(fixture.clip + 8)})
				if err != nil {
					t.Fatal(err)
				}
			}
			// A native payload entry is insufficient evidence of Java identity.
			fixture.client.clips[owner] = &mediaClip{java: true, listener: fixture.listeners[0]}
			if err := tryLGTListenerSetter(fixture, owner, fixture.listeners[1]); err == nil {
				t.Fatal("listener setter accepted a non-Clip owner")
			}
			if fixture.client.clips[owner].listener != fixture.listeners[0] {
				t.Fatal("rejected owner changed its previous recipient")
			}
		})
	}
}

func TestLGTWIPIListenerRejectsMalformedRawTargetsAtSetterAndDispatch(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*lgtWIPIListenerFixture, lgtListenerRawRecord)
	}{
		{"wrong descriptor", func(f *lgtWIPIListenerFixture, r lgtListenerRawRecord) {
			f.writeWord(r.callback+8, lgtListenerValidationString(f, "(Lorg/kwis/msp/media/Clip;I)V"))
		}},
		{"zero body", func(f *lgtWIPIListenerFixture, r lgtListenerRawRecord) { f.writeWord(r.callback+20, 0) }},
		{"unaligned ARM body", func(f *lgtWIPIListenerFixture, r lgtListenerRawRecord) {
			f.writeWord(r.callback+20, f.word(r.callback+20)+2)
		}},
		{"nonexecutable body", func(f *lgtWIPIListenerFixture, r lgtListenerRawRecord) {
			f.writeWord(r.callback+20, lgtListenerValidationString(f, "data"))
		}},
		{"missing raw interface with stale cache", func(f *lgtWIPIListenerFixture, r lgtListenerRawRecord) {
			f.writeWord(r.header+javaClassInterfaces, 0)
			f.writeWord(r.header+javaClassMethodRun, 0)
		}},
		{"forged cached interface", func(f *lgtWIPIListenerFixture, r lgtListenerRawRecord) {
			f.writeWord(r.entry, lgtListenerValidationString(f, "java/lang/Runnable"))
			f.writeWord(r.header+javaClassMethodRun, 0)
			f.client.javaRun.byHandle[r.handle].Record.Interfaces = []javaInterface{{Name: javaPlayListenerClass}}
		}},
		{"zero interface count", func(f *lgtWIPIListenerFixture, r lgtListenerRawRecord) { f.writeWord(r.interfaces, 0) }},
		{"oversized interface count", func(f *lgtWIPIListenerFixture, r lgtListenerRawRecord) {
			f.writeWord(r.interfaces, maxJavaInterfaces+1)
		}},
		{"missing interface entry", func(f *lgtWIPIListenerFixture, r lgtListenerRawRecord) { f.writeWord(r.interfaces+4, 0) }},
		{"unmapped interface entry", func(f *lgtWIPIListenerFixture, r lgtListenerRawRecord) {
			f.writeWord(r.interfaces+4, 0xfffffff8)
		}},
		{"unnamed interface", func(f *lgtWIPIListenerFixture, r lgtListenerRawRecord) { f.writeWord(r.entry, 0) }},
		{"oversized method run", func(f *lgtWIPIListenerFixture, r lgtListenerRawRecord) { f.writeWord(r.methods, maxJavaMembers+1) }},
		{"different method owner", func(f *lgtWIPIListenerFixture, r lgtListenerRawRecord) { f.writeWord(r.callback, r.handle+4) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, dispatch := range []bool{false, true} {
				t.Run(map[bool]string{false: "setter", true: "dispatch"}[dispatch], func(t *testing.T) {
					fixture := newLGTWIPIListenerFixture(t)
					fixture.setListener(fixture.listeners[0])
					if dispatch {
						fixture.call("play", true)
					}
					test.mutate(fixture, lgtListenerRecord(fixture))
					var err error
					if dispatch {
						pins := fixture.client.javaPinMark()
						err = fixture.client.serviceMediaCallbacks(t.Context())
						if fixture.client.javaPinMark() != pins {
							t.Fatal("rejected callback leaked delivery pins")
						}
					} else {
						err = tryLGTListenerSetter(fixture, fixture.clip, fixture.listeners[1])
					}
					if err == nil {
						t.Fatal("malformed raw callback target was accepted")
					}
					if fixture.client.clips[fixture.clip].listener != fixture.listeners[0] {
						t.Fatal("invalid target changed the installed recipient")
					}
					fixture.wantHistory()
				})
			}
		})
	}
}

func TestLGTWIPIListenerExactRawMethodWithoutNominalInterface(t *testing.T) {
	for _, unrelated := range []bool{false, true} {
		t.Run(map[bool]string{false: "omitted interfaces", true: "unrelated interface"}[unrelated], func(t *testing.T) {
			fixture := newLGTWIPIListenerFixture(t)
			record := lgtListenerRecord(fixture)
			if unrelated {
				fixture.writeWord(record.entry, lgtListenerValidationString(fixture, "java/lang/Runnable"))
			} else {
				setLGTListenerRawInterfaces(fixture, record.handle)
			}
			// AOT compilers can omit interfaces while retaining the exact
			// callback member. The cached summary supplies neither proof.
			class := fixture.client.javaRun.byHandle[record.handle]
			class.Record.Interfaces, class.Record.Methods = nil, nil
			fixture.setListener(fixture.listeners[0])
			fixture.call("play", true)
			fixture.wantHistory()
			restored := fixture.restore(fixture.checkpoint())
			for _, current := range []*lgtWIPIListenerFixture{fixture, restored} {
				current.drain()
				current.wantHistory(current.event(0, 2))
				current.call("stop", true)
				current.wantHistory(current.event(0, 2))
				current.drain()
				current.wantHistory(current.event(0, 2), current.event(0, 3))
			}
		})
	}
}

func TestLGTWIPIListenerDerivedInterfaceAndInheritedMethod(t *testing.T) {
	fixture := newLGTWIPIListenerFixture(t)
	original := lgtListenerRecord(fixture)
	derived := newLGTListenerRawInterface(fixture, "DerivedPlayListener")
	setLGTListenerRawInterfaces(fixture, derived, [2]uint32{fixture.word(original.entry), 0})
	setLGTListenerRawInterfaces(fixture, original.handle, [2]uint32{derived, 0})
	child := cloneLGTListenerRecord(fixture, "InheritedListener")
	fixture.writeWord(child-javaClassHeader+0x10, original.handle)
	fixture.writeWord(child-javaClassHeader+javaClassMethodRun, 0)
	setLGTListenerRawInterfaces(fixture, child)
	listener := newLGTListenerFromRawClass(fixture, child)
	fixture.setListener(listener)
	fixture.call("play", true)
	fixture.drain()
	fixture.wantHistory(lgtWIPIListenerEvent{receiver: listener, clip: fixture.clip, event: 2})
}

func TestLGTWIPIListenerRejectsCyclicGraphsAfterDirectMatch(t *testing.T) {
	for _, superclass := range []bool{false, true} {
		t.Run(map[bool]string{false: "interface", true: "superclass"}[superclass], func(t *testing.T) {
			fixture := newLGTWIPIListenerFixture(t)
			fixture.setListener(fixture.listeners[0])
			record := lgtListenerRecord(fixture)
			if superclass {
				class := fixture.client.javaRun.byHandle[record.handle]
				class.Super = class
				fixture.writeWord(record.header+0x10, record.handle)
			} else {
				loop := newLGTListenerRawInterface(fixture, "LoopingInterface")
				setLGTListenerRawInterfaces(fixture, loop, [2]uint32{loop, 0})
				setLGTListenerRawInterfaces(fixture, record.handle,
					[2]uint32{fixture.word(record.entry), fixture.word(record.entry + 4)}, [2]uint32{loop, 0})
			}
			if err := tryLGTListenerSetter(fixture, fixture.clip, fixture.listeners[1]); err == nil {
				t.Fatal("cyclic listener graph was accepted")
			}
			if fixture.client.clips[fixture.clip].listener != fixture.listeners[0] {
				t.Fatal("rejected cyclic graph replaced the previous listener")
			}
		})
	}
}

func TestLGTWIPIListenerRejectsCachedInheritanceAbsentFromGuestRecord(t *testing.T) {
	fixture := newLGTWIPIListenerFixture(t)
	original := lgtListenerRecord(fixture)
	child := cloneLGTListenerRecord(fixture, "DetachedListener")
	fixture.writeWord(child-javaClassHeader+0x10, original.handle)
	fixture.writeWord(child-javaClassHeader+javaClassMethodRun, 0)
	setLGTListenerRawInterfaces(fixture, child)
	listener := newLGTListenerFromRawClass(fixture, child)
	fixture.setListener(listener)
	fixture.setListener(fixture.listeners[0])
	// The cache still links the parent, but the guest declaration no longer
	// inherits it. That cached edge must not supply a missing interface.
	fixture.writeWord(child-javaClassHeader+0x10, 0)
	if err := tryLGTListenerSetter(fixture, fixture.clip, listener); err == nil {
		t.Fatal("cached superclass granted an interface absent from the guest record")
	}
	if fixture.client.clips[fixture.clip].listener != fixture.listeners[0] {
		t.Fatal("rejected cached inheritance replaced the previous listener")
	}
}

func TestLGTWIPIListenerStrippedDirectInterfaceDispatchForms(t *testing.T) {
	for _, receiverSlot := range []bool{false, true} {
		t.Run(map[bool]string{false: "entry address", true: "receiver vtable slot"}[receiverSlot], func(t *testing.T) {
			fixture := newLGTWIPIListenerFixture(t)
			record := lgtListenerRecord(fixture)
			body := fixture.word(record.callback + 20)
			listener := fixture.listeners[0]
			if receiverSlot {
				child := cloneLGTListenerRecord(fixture, "StrippedInheritedListener")
				fixture.writeWord(child-javaClassHeader+0x10, record.handle)
				fixture.writeWord(child-javaClassHeader+javaClassMethodRun, 0)
				setLGTListenerRawInterfaces(fixture, child)
				listener = newLGTListenerFromRawClass(fixture, child)
				class := fixture.client.javaRun.byHandle[child]
				slot := class.Slots - 1
				fixture.writeWord(record.entry+4, slot)
				fixture.writeWord(class.VTable+4+slot*4, body)
				// Only the receiver's override works; dispatching through the
				// superclass that declares the interface would reach zero.
				parent := fixture.client.javaRun.byHandle[record.handle]
				fixture.writeWord(parent.VTable+4+slot*4, 0)
			}
			fixture.writeWord(record.header+javaClassMethodRun, 0)
			fixture.setListener(listener)
			fixture.call("play", true)
			fixture.drain()
			fixture.wantHistory(lgtWIPIListenerEvent{receiver: listener, clip: fixture.clip, event: 2})
		})
	}
}
