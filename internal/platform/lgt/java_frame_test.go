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

// The display holds one card: countCard is one while it is shown, and popCard
// takes it off and answers it, leaving nothing shown and nothing to paint; a
// pop of an empty display answers null.
func TestJavaDisplayCountsAndPopsItsOneCard(t *testing.T) {
	client := fixtureClient(t)
	const (
		countCard = javaDisplayClass + ".countCard()I"
		popCard   = javaDisplayClass + ".popCard()Lorg/kwis/msp/lcdui/Card;"
		card      = 0x1000
	)
	display := mustJavaCall(t, client, javaDisplayClass+".getDefaultDisplay()Lorg/kwis/msp/lcdui/Display;")
	if got := mustJavaCall(t, client, countCard, display); got != 0 {
		t.Errorf("countCard() before a push = %d, want 0", got)
	}
	mustJavaCall(t, client, javaDisplayClass+".pushCard(Lorg/kwis/msp/lcdui/Card;)V", display, card)
	mustJavaCall(t, client, javaDisplayClass+".pushCard(Lorg/kwis/msp/lcdui/Card;)V", display, card)
	if got := mustJavaCall(t, client, countCard, display); got != 1 {
		t.Errorf("countCard() with a card shown = %d, want 1", got)
	}
	if got := mustJavaCall(t, client, popCard, display); got != card {
		t.Errorf("popCard() = %#x, want the card shown %#x", got, uint32(card))
	}
	runtime := client.javaRuntimeState()
	if runtime.card != 0 || runtime.cardDirty {
		t.Error("the popped card is still shown or still asking to be painted")
	}
	if got := mustJavaCall(t, client, countCard, display); got != 0 {
		t.Errorf("countCard() after the pop = %d, want 0", got)
	}
	if got := mustJavaCall(t, client, popCard, display); got != 0 {
		t.Errorf("popCard() of an empty display = %#x, want null", got)
	}
}

// callSerially with a delay queues its Runnable where the form without one
// does, and the delay is not waited out — KTF's answer.
func TestJavaCallSeriallyWithADelayQueuesTheRunnable(t *testing.T) {
	client := fixtureClient(t)
	display := mustJavaCall(t, client, javaDisplayClass+".getDefaultDisplay()Lorg/kwis/msp/lcdui/Display;")
	mustJavaCall(t, client, javaDisplayClass+".callSerially(Ljava/lang/Runnable;)V", display, 0x2000)
	mustJavaCall(t, client, javaDisplayClass+".callSerially(Ljava/lang/Runnable;I)V", display, 0x3000, 5000)
	if got := client.javaRuntimeState().serial; len(got) != 2 || got[0] != 0x2000 || got[1] != 0x3000 {
		t.Errorf("the serial queue holds %#x, want both Runnables in the order they were handed over", got)
	}
}

// A card's override that calls the Card's own notification bodies is answered
// as KTF's and SKT's Card answers it: a key or pointer event not taken, and
// nothing to do on being shown.
func TestJavaCardBaseNotificationsTakeNothing(t *testing.T) {
	client := fixtureClient(t)
	const card = 0x1000
	if got := mustJavaCall(t, client, javaCardClass+".keyNotify(II)Z", card, javaKeyPressed, '5'); got != javaFalse {
		t.Errorf("Card.keyNotify = %d, want false", got)
	}
	if got := mustJavaCall(t, client, javaCardClass+".pointerNotify(III)Z", card, 1, 3, 4); got != javaFalse {
		t.Errorf("Card.pointerNotify = %d, want false", got)
	}
	runtime := client.javaRuntimeState()
	runtime.card, runtime.cardDirty = card, false
	mustJavaCall(t, client, javaCardClass+".showNotify(Z)V", card, 1)
	if runtime.card != card || runtime.cardDirty {
		t.Error("Card.showNotify changed what the display shows")
	}
}
