package lgt

import (
	"context"
	"testing"
)

func TestJavaStackEmptySlotTracksPushAndPop(t *testing.T) {
	client := fixtureClient(t)
	class, err := client.preparePlatformJavaClass(javaStackClass)
	if err != nil {
		t.Fatal(err)
	}
	object, err := client.allocateJavaObject(class)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := javaVectorConstructor(client, nil, nil, []uint32{object}); err != nil {
		t.Fatal(err)
	}
	check := func(want uint32) {
		t.Helper()
		if err := client.thread.SetRegister(0, object); err != nil {
			t.Fatal(err)
		}
		served, err := client.callJavaPlatformVirtual(context.Background(), client.thread, javaVirtualSlot(javaStackClass, 35))
		if !served || err != nil {
			t.Fatalf("empty: served=%v error=%v", served, err)
		}
		if got, _ := client.thread.Register(0); got != want {
			t.Fatalf("empty = %d, want %d", got, want)
		}
	}
	check(1)
	if _, err := javaStackPush(client, nil, nil, []uint32{object, 0}); err != nil {
		t.Fatal(err)
	}
	check(0)
	if _, err := javaStackPop(client, nil, nil, []uint32{object}); err != nil {
		t.Fatal(err)
	}
	check(1)
}
