package lgt

import (
	"testing"

	"github.com/movingwoo/wfeature/internal/testfixture"
)

func TestLGTWIPIListenerStrippedOverridePrecedesNamedParent(t *testing.T) {
	for _, direct := range []bool{false, true} {
		t.Run(map[bool]string{false: "inherited slot", true: "own entry address"}[direct], func(t *testing.T) {
			f := newLGTWIPIListenerFixture(t)
			raw := lgtListenerRecord(f)
			parent := f.client.javaRun.byHandle[raw.handle]
			originalBody := f.word(testfixture.LGTListenerCallbackAddress)
			noOpBody := f.word(raw.methods + 4 + 20) // authored bx lr constructor
			slot := parent.Slots - 1
			f.writeWord(raw.callback+20, noOpBody)
			f.writeWord(raw.entry+4, slot)
			f.writeWord(parent.VTable+4+slot*4, noOpBody)
			loadedParent, err := f.client.readJavaClass(raw.handle, nil)
			if err != nil {
				t.Fatal(err)
			}
			parent.Record = loadedParent // the metadata a normal loader reads for this form
			child := cloneLGTListenerRecord(f, "OverrideListener")
			f.writeWord(child-javaClassHeader+0x10, raw.handle)
			f.writeWord(child-javaClassHeader+javaClassMethodRun, 0)
			words := make([]uint32, parent.Slots+1)
			words[0] = child
			for index := uint32(0); index < parent.Slots; index++ {
				words[index+1] = f.word(parent.VTable + 4 + index*4)
			}
			words[slot+1] = originalBody
			vtable, err := f.client.allocateWords(words)
			if err != nil {
				t.Fatal(err)
			}
			f.writeWord(child-javaClassHeader+javaClassPool, vtable)
			if direct {
				setLGTListenerRawInterfaces(f, child, [2]uint32{f.word(raw.entry), originalBody})
			} else {
				setLGTListenerRawInterfaces(f, child)
			}
			listener := newLGTListenerFromRawClass(f, child)
			table, err := f.client.javaInterfaceTable(listener, f.word(raw.entry))
			if err != nil {
				t.Fatal(err)
			}
			guestBody := f.word(table + 4)
			if guestBody != originalBody {
				t.Fatalf("guest body %#x, want child %#x", guestBody, originalBody)
			}
			if _, err := f.client.call(t.Context(), guestBody, []uint32{listener, f.clip, 2, 0}); err != nil {
				t.Fatal(err)
			}
			f.wantHistory(lgtWIPIListenerEvent{receiver: listener, clip: f.clip, event: 2})
			f.writeWord(testfixture.LGTListenerHistoryCount, 0)
			f.setListener(listener)
			f.call("play", true)
			restored := f.restore(f.checkpoint())
			for _, current := range []*lgtWIPIListenerFixture{f, restored} {
				current.drain()
				current.wantHistory(lgtWIPIListenerEvent{receiver: listener, clip: f.clip, event: 2})
			}
		})
	}
}
