package lgt

import (
	"strings"
	"testing"
)

// The string slots read what a title built in a buffer of its own — a text
// resource, a line it is assembling — which is not a name and has no name's
// bound. A valid string longer than the bound a name is read with is still a
// string, and every slot here takes it the way strlen already does.
func TestStringSlotsTakeStringsLongerThanAName(t *testing.T) {
	client := fixtureClient(t)
	const run = 5000
	write := func(text string) uint32 {
		t.Helper()
		address, err := client.allocateBytes(append([]byte(text), 0))
		if err != nil {
			t.Fatal(err)
		}
		return address
	}
	long := write(strings.Repeat("x", run) + "/tail")
	other := write(strings.Repeat("x", run) + "/tail!")
	slash, tail, xs := write("/"), write("tail"), write("x")
	number := write("42" + strings.Repeat("x", run))
	readBack := func(address uint32, length int) string {
		t.Helper()
		data := make([]byte, length)
		if err := client.core.Memory().Read(address, data); err != nil {
			t.Fatal(err)
		}
		return string(data)
	}

	t.Run("strchr", func(t *testing.T) {
		if found := callStdlib(t, client, stdlibStrchr, long, '/'); found != long+run {
			t.Fatalf("strchr = %#x, want %#x", found, long+run)
		}
		if found := callStdlib(t, client, stdlibStrrchr, long, 'x'); found != long+run-1 {
			t.Fatalf("strrchr = %#x, want %#x", found, long+run-1)
		}
	})
	t.Run("strspn", func(t *testing.T) {
		if span := callStdlib(t, client, stdlibStrspn, long, xs); span != run {
			t.Fatalf("strspn = %d, want %d", span, run)
		}
		if span := callStdlib(t, client, stdlibStrcspn, long, slash); span != run {
			t.Fatalf("strcspn = %d, want %d", span, run)
		}
		if found := callStdlib(t, client, stdlibStrpbrk, long, slash); found != long+run {
			t.Fatalf("strpbrk = %#x, want %#x", found, long+run)
		}
	})
	t.Run("strcmp", func(t *testing.T) {
		if order := int32(callStdlib(t, client, stdlibStrcmp, long, other)); order >= 0 {
			t.Fatalf("strcmp = %d, want the shorter string first", order)
		}
		if order := callStdlib(t, client, stdlibStrcmp, long, long); order != 0 {
			t.Fatalf("strcmp of a string with itself = %d", order)
		}
	})
	t.Run("strstr", func(t *testing.T) {
		if found := callStdlib(t, client, stdlibStrstr, long, tail); found != long+run+1 {
			t.Fatalf("strstr = %#x, want %#x", found, long+run+1)
		}
	})
	t.Run("atoi", func(t *testing.T) {
		if value := callStdlib(t, client, stdlibAtoi, number); value != 42 {
			t.Fatalf("atoi = %d, want 42", value)
		}
	})
	t.Run("strcpy and strcat", func(t *testing.T) {
		destination, err := client.allocate(3 * run)
		if err != nil {
			t.Fatal(err)
		}
		callStdlib(t, client, stdlibStrcpy, destination, long)
		callStdlib(t, client, stdlibStrcat, destination, tail)
		want := strings.Repeat("x", run) + "/tailtail\x00"
		if got := readBack(destination, len(want)); got != want {
			t.Fatalf("strcpy then strcat wrote %q...", got[run:])
		}
	})
	t.Run("sprintk %s", func(t *testing.T) {
		destination, err := client.allocate(2 * run)
		if err != nil {
			t.Fatal(err)
		}
		format := write("%s!")
		if length := callSlot(t, client, slotSprintk, destination, format, long); length != run+6 {
			t.Fatalf("sprintk wrote %d bytes, want %d", length, run+6)
		}
		if got := readBack(destination+run, 7); got != "/tail!\x00" {
			t.Fatalf("sprintk ended %q", got)
		}
	})
}
