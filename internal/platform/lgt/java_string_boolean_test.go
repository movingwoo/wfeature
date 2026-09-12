package lgt

import (
	"context"
	"testing"
)

func TestJavaStringBufferAppendsBooleanThroughSlot(t *testing.T) {
	client := fixtureClient(t)
	class, err := client.preparePlatformJavaClass(javaStringBufferClass)
	if err != nil {
		t.Fatal(err)
	}
	buffer, err := client.allocateJavaObject(class)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := javaBufferEmpty(client, nil, nil, []uint32{buffer}); err != nil {
		t.Fatal(err)
	}
	for _, value := range []uint32{1, 0} {
		if err := client.thread.SetRegister(0, buffer); err != nil {
			t.Fatal(err)
		}
		if err := client.thread.SetRegister(1, value); err != nil {
			t.Fatal(err)
		}
		served, err := client.callJavaPlatformVirtual(context.Background(), client.thread, javaVirtualSlot(javaStringBufferClass, 21))
		if err != nil || !served {
			t.Fatalf("append(boolean): served=%v error=%v", served, err)
		}
		if got, _ := client.thread.Register(0); got != buffer {
			t.Fatalf("append returned %#x, want %#x", got, buffer)
		}
	}
	value, err := javaBufferToString(client, nil, nil, []uint32{buffer})
	if err != nil {
		t.Fatal(err)
	}
	if text, ok := client.javaText(value); !ok || text != "truefalse" {
		t.Fatalf("text = %q, known=%v", text, ok)
	}
}

func TestJavaStringConstructorCopiesWholeCharArray(t *testing.T) {
	client := fixtureClient(t)
	method, ok := javaPlatformMethods["java/lang/String.<init>([C)V"]
	if !ok {
		t.Fatal("char-array constructor is not served")
	}
	for _, original := range []string{"", "A\uac00Z"} {
		source, err := client.newJavaString(original)
		if err != nil {
			t.Fatal(err)
		}
		array, err := javaStringToCharArray(client, nil, nil, []uint32{source})
		if err != nil {
			t.Fatal(err)
		}
		target, err := client.newJavaString("")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := method.Implementat(client, nil, nil, []uint32{target, array}); err != nil {
			t.Fatal(err)
		}
		if text, _ := client.javaText(target); text != original {
			t.Fatalf("text = %q, want %q", text, original)
		}
	}
}
