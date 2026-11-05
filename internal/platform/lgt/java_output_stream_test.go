package lgt

import (
	"context"
	"strings"
	"testing"
)

// A title's own OutputStream overrides write(int) and inherits the rest:
// write(byte[]) and write(byte[], int, int) come to its write(int) one byte at
// a time, in order, each widened with its sign, and flush and close do
// nothing.
func TestJavaOutputStreamBaseWritesThroughTheTitlesWriteInt(t *testing.T) {
	client := fixtureClient(t)
	stream := installGuestSubclass(t, client, "game/Out", javaOutputStreamClass, 64, 0,
		guestRoutine{javaOutputStreamSlotWrite, recordingWrite})
	array, err := client.newJavaByteArray([]byte{1, 0x80, 0xff})
	if err != nil {
		t.Fatal(err)
	}
	mustSlot(t, client, javaOutputStreamClass, 11, stream, array)
	mustSlot(t, client, javaOutputStreamClass, 12, stream, array, 1, 1)
	mustSlot(t, client, javaOutputStreamClass, 12, stream, array, 3, 0)
	if got := recorded(t, client, stream); len(got) != 4 || got[0] != 1 || got[1] != -128 || got[2] != -1 || got[3] != -128 {
		t.Errorf("write(int) was handed %v, want [1 -128 -1 -128]", got)
	}
	for _, probe := range []struct {
		name      string
		arguments []uint32
		want      string
	}{
		{"past the end", []uint32{array, 2, 2}, javaIndexClass},
		{"an offset past the array", []uint32{array, 4, 0}, javaIndexClass},
		{"a negative offset", []uint32{array, 0xffffffff, 1}, javaIndexClass},
		{"a negative count", []uint32{array, 0, 0xffffffff}, javaIndexClass},
		{"a null array", []uint32{0, 0, 0}, javaThrowNullClass},
	} {
		if _, err := slotCall(t, client, javaOutputStreamClass, 12, append([]uint32{stream}, probe.arguments...)...); thrownClass(err) != probe.want {
			t.Errorf("write(byte[], int, int) %s = %v, want %s", probe.name, err, probe.want)
		}
	}
	if _, err := slotCall(t, client, javaOutputStreamClass, 11, stream, 0); thrownClass(err) != javaThrowNullClass {
		t.Errorf("write(null) = %v, want NullPointerException", err)
	}
	mustSlot(t, client, javaOutputStreamClass, 13, stream)
	mustSlot(t, client, javaOutputStreamClass, 14, stream)
	if got := recorded(t, client, stream); len(got) != 4 {
		t.Errorf("a refused write, flush or close reached write(int): %v", got)
	}
	if _, served := javaBakedVirtualSlots[javaOutputStreamClass][javaOutputStreamSlotWrite]; served {
		t.Error("OutputStream's abstract write(int) is served")
	}
}

// PrintStream declares its own write(int), write(byte[], int, int), flush and
// close, none of which this platform serves, so a dispatch on System.out that
// walks up to OutputStream's bodies is refused by name rather than answered
// with the base class's.
func TestJavaOutputStreamBaseRefusesPrintStreamsOwnForms(t *testing.T) {
	client := fixtureClient(t)
	ctx := context.Background()
	out, err := client.javaPlatformObject(javaPrintStreamClass)
	if err != nil {
		t.Fatal(err)
	}
	array, err := client.newJavaByteArray([]byte{'a'})
	if err != nil {
		t.Fatal(err)
	}
	for _, probe := range []struct {
		slot      uint32
		arguments []uint32
		named     string
	}{
		{13, nil, "flush()V"},
		{14, nil, "close()V"},
		{12, []uint32{array, 0, 1}, "write([BII)V"},
		// write(byte[]) is OutputStream's own on a PrintStream too, and it comes
		// to the range write PrintStream declares.
		{11, []uint32{array}, "write([BII)V"},
	} {
		_, err := client.javaCallVirtual(ctx, client.thread, out, probe.slot, probe.arguments...)
		if err == nil || !strings.Contains(err.Error(), "java/io/PrintStream declares its own "+probe.named) {
			t.Errorf("System.out slot %d = %v, want it refused as PrintStream's own %s", probe.slot, err, probe.named)
		}
	}
}
