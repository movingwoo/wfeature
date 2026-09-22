package lgt

import "testing"

// A key callback may update state without requesting a frame. Painting anyway
// advances games that update simulation in paint whenever keys are repeated.
func TestJavaKeyDeliveryPreservesExplicitRepaintScheduling(t *testing.T) {
	client := fixtureClient(t)
	writeJavaClassFixture(t, client)
	class, err := client.prepareJavaClass(t.Context(), client.thread, fixtureClassHandle)
	if err != nil {
		t.Fatal(err)
	}
	body := installThumb(t, client, 0x6883, 0x6019, 0x605a, 0x2000, 0x4770)
	class.Record.Methods = append(class.Record.Methods, javaMember{Name: "keyNotify", Descriptor: "(II)Z", Body: body})
	object, err := client.allocateJavaInstance(class.Object)
	if err != nil {
		t.Fatal(err)
	}
	runtime := client.javaRuntimeState()
	runtime.card = object
	for _, pressed := range []bool{true, false, true, false} {
		if err := client.deliverJavaKey(t.Context(), pressed, '5'); err != nil {
			t.Fatal(err)
		}
		if runtime.cardDirty {
			t.Fatal("key delivery requested an unsolicited paint")
		}
		if err := client.PaintJava(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	data, err := client.readWord(object + 8)
	if err != nil {
		t.Fatal(err)
	}
	kind, _ := client.readWord(data)
	key, _ := client.readWord(data + 4)
	if kind != javaKeyReleased || key != '5' {
		t.Fatalf("callback received %d, %d", kind, key)
	}
	if _, err := javaCardRepaint(client, t.Context(), client.thread, []uint32{object}); err != nil {
		t.Fatal(err)
	}
	if err := client.deliverJavaKey(t.Context(), true, '5'); err != nil {
		t.Fatal(err)
	}
	if !runtime.cardDirty {
		t.Fatal("key delivery lost a pending repaint")
	}
}

func TestRemoveCurrentJavaCard(t *testing.T) {
	client := fixtureClient(t)
	method, ok := javaGraphicsMethods[javaDisplayClass+".removeCard(Lorg/kwis/msp/lcdui/Card;)Z"]
	if !ok {
		t.Fatal("Display.removeCard is not implemented")
	}
	runtime := client.javaRuntimeState()
	runtime.card = 0x1000
	runtime.cardDirty = true
	for _, step := range []struct{ card, want uint32 }{{0, 0}, {0x2000, 0}, {0x1000, 1}, {0x1000, 0}} {
		got, err := method.Implementat(client, t.Context(), client.thread, []uint32{0, step.card})
		if err != nil || got != step.want {
			t.Fatalf("removeCard(%#x) = %d, %v", step.card, got, err)
		}
		if step.want == 1 && (runtime.card != 0 || runtime.cardDirty) {
			t.Fatal("removed card remains scheduled")
		}
		if step.card != 0x1000 && (runtime.card != 0x1000 || !runtime.cardDirty) {
			t.Fatal("unrelated removal changed current card")
		}
	}
	if err := client.PaintJava(t.Context()); err != nil {
		t.Fatal(err)
	}
}
