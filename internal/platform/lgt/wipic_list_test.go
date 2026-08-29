package lgt

import (
	"strings"
	"testing"
)

// readPackedNames reads the answer MC_fsList writes: names one after another,
// each NUL-terminated, the list ended by an empty one.
func readPackedNames(t *testing.T, client *Client, address uint32, size int) []string {
	t.Helper()
	block := make([]byte, size)
	if err := client.core.Memory().Read(address, block); err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for len(block) > 0 {
		end := strings.IndexByte(string(block), 0)
		if end <= 0 {
			break
		}
		names = append(names, string(block[:end]))
		block = block[end+1:]
	}
	return names
}

// MC_fsList answers the immediate children of a directory. A title that reads
// a folder of folders to find out what it shipped with depends on the second
// half of that: a file two levels down contributes its own parent's name once,
// not its own.
func TestListAnswersTheChildrenOfADirectory(t *testing.T) {
	client := fixtureClient(t)

	directory, err := client.allocateBytes(append([]byte("data"), 0))
	if err != nil {
		t.Fatal(err)
	}
	const size = 256
	buffer, err := client.allocate(size)
	if err != nil {
		t.Fatal(err)
	}

	if code := int32(callSlot(t, client, slotFsList, directory, buffer, size, 1)); code != wipiSuccess {
		t.Fatalf("MC_fsList answered %d, want success", code)
	}
	names := readPackedNames(t, client, buffer, size)
	if len(names) != 2 || names[0] != "hello.txt" || names[1] != "other.txt" {
		t.Fatalf("the listing is %q, want the two packaged files", names)
	}

	// The root names the directory once rather than each file inside it.
	root, err := client.allocateBytes([]byte{0})
	if err != nil {
		t.Fatal(err)
	}
	if code := int32(callSlot(t, client, slotFsList, root, buffer, size, 1)); code != wipiSuccess {
		t.Fatalf("MC_fsList of the root answered %d, want success", code)
	}
	rootNames := readPackedNames(t, client, buffer, size)
	seen := 0
	for _, name := range rootNames {
		if name == "data" {
			seen++
		}
	}
	if seen != 1 {
		t.Fatalf("the root listing is %q, want \"data\" exactly once", rootNames)
	}
}

// A listing built only from the archive would show a title the files it
// shipped and none of the files it wrote, which for a title that lists a
// directory to find its own save slots is a listing that is always empty. A
// removed file leaves the listing again.
func TestListSeesWhatTheTitleWroteAndNotWhatItRemoved(t *testing.T) {
	client := fixtureClient(t)
	client.saveStore = newMemorySaveStore()

	client.writeFile("saves/slot1.dat", []byte("one"))
	client.writeFile("saves/slot2.dat", []byte("two"))

	directory, err := client.allocateBytes(append([]byte("saves"), 0))
	if err != nil {
		t.Fatal(err)
	}
	const size = 256
	buffer, err := client.allocate(size)
	if err != nil {
		t.Fatal(err)
	}

	callSlot(t, client, slotFsList, directory, buffer, size, 1)
	if names := readPackedNames(t, client, buffer, size); len(names) != 2 {
		t.Fatalf("the listing after two writes is %q, want both saves", names)
	}

	client.removeFile("saves/slot1.dat")
	callSlot(t, client, slotFsList, directory, buffer, size, 1)
	names := readPackedNames(t, client, buffer, size)
	if len(names) != 1 || names[0] != "slot2.dat" {
		t.Fatalf("the listing after a delete is %q, want only the surviving save", names)
	}
}

// A buffer too small for the answer is refused rather than filled to its brim:
// a caller reading a truncated name walks off the end of the last one.
func TestListRefusesABufferItCannotFill(t *testing.T) {
	client := fixtureClient(t)

	directory, err := client.allocateBytes(append([]byte("data"), 0))
	if err != nil {
		t.Fatal(err)
	}
	buffer, err := client.allocate(64)
	if err != nil {
		t.Fatal(err)
	}
	if code := int32(callSlot(t, client, slotFsList, directory, buffer, 4, 1)); code != wipiShortBuffer {
		t.Fatalf("MC_fsList into four bytes answered %d, want the short-buffer code", code)
	}
}

// MC_netSocket is reached at 0x7d0 rather than at the 0x25a the block's order
// predicts, and it answers the way the rest of the block does: there is no
// network, and a title that is refused a socket takes its own offline path.
func TestSocketIsRefusedLikeTheRestOfTheNetworkBlock(t *testing.T) {
	client := fixtureClient(t)

	// MC_AF_INET and MC_SOCKET_STREAM, which is what the caller passes.
	if code := int32(callSlot(t, client, slotNetSocket, 2, 1)); code >= 0 {
		t.Fatalf("MC_netSocket answered %d, want a negative descriptor", code)
	}
	// And the slot resolves, because a module resolves what it might call
	// before it calls anything.
	if _, err := client.importFunction(importTableWIPIC, slotNetSocket); err != nil {
		t.Fatalf("MC_netSocket did not resolve: %v", err)
	}
}
