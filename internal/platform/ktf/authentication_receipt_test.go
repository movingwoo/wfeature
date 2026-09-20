package ktf

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
)

// Authored records use a deliberately non-permutation table and carry nonzero
// settings and reserved bytes. No archive's substitution table is bundled.
func receiptFixture(t *testing.T) (*DirectorySaveStore, []byte, map[string][]byte, []byte) {
	t.Helper()
	base := NewDirectorySaveStore(t.TempDir())
	table := make([]byte, 256)
	for i := range table {
		table[i] = byte(i/2 + 7)
	}
	files := map[string][]byte{}
	plain := bytes.Repeat([]byte{0x5a}, 64)
	plain[63] = 251
	setReceiptBits(plain, 0, 8, 75)
	for i, name := range subscriberReceiptFiles {
		files[name] = bytes.Repeat([]byte{byte(i + 1)}, 101+i*731)
		setReceiptBits(plain, 15+19*i, 19, uint32(len(files[name])-44))
	}
	files["prefs"] = encodeReceiptFixture(plain, table)
	rejected := bytes.Clone(plain)
	for i := range subscriberReceiptFiles {
		setReceiptBits(rejected, 15+19*i, 19, 0)
	}
	if err := base.StoreSave("db/prefs", encodeReceiptFixture(rejected, table)); err != nil {
		t.Fatal(err)
	}
	if err := base.StoreSave("db/save0.dat", []byte("retained progress")); err != nil {
		t.Fatal(err)
	}
	return base, table, files, rejected
}
func encodeReceiptFixture(plain, table []byte) []byte {
	encoded := bytes.Clone(plain)
	number := "01024681357"
	var sum byte
	for i := 0; i < 62; i++ {
		n := int(plain[63]) + i
		encoded[i] = plain[i] ^ (table[n&255] + number[n%11])
		sum += encoded[i]
	}
	encoded[62] = sum
	return encoded
}
func TestSubscriberReceiptRestoresOnlyContentLengths(t *testing.T) {
	base, table, files, rejected := receiptFixture(t)
	original, _ := base.LoadSave("db/prefs")
	wrapped := newSubscriberReceiptStore(base, "01024681357", table, files)
	store, ok := wrapped.(*subscriberReceiptStore)
	if !ok {
		t.Fatal("fixture rejected")
	}
	got, present, err := backend.ReadSave(store, "db/prefs")
	if err != nil || !present {
		t.Fatalf("read: %t %v", present, err)
	}
	plain, ok := store.decode(got)
	if !ok {
		t.Fatal("recovered checksum or marker invalid")
	}
	for i, name := range subscriberReceiptFiles {
		if receiptBits(plain, 15+19*i, 19) != uint32(len(files[name])-44) {
			t.Fatalf("content length %d not recovered", i)
		}
		setReceiptBits(plain, 15+19*i, 19, 0)
	}
	// Byte 62 is the checksum over the encrypted record, which must change.
	plain[62] = rejected[62]
	if !bytes.Equal(plain, rejected) {
		t.Fatal("settings, nonce or reserved bytes changed")
	}
	unchanged, _ := base.LoadSave("db/prefs")
	if !bytes.Equal(unchanged, original) {
		t.Fatal("read mutated original receipt")
	}
	progress, _ := store.LoadSave("db/save0.dat")
	if string(progress) != "retained progress" {
		t.Fatal("progress changed")
	}
	if err := backend.StoreSaves(store, map[string][]byte{"db/prefs": got, "db/save0.dat": []byte("new progress")}); err != nil {
		t.Fatal(err)
	}
	restarted := newSubscriberReceiptStore(base, "01024681357", table, files)
	again, _, err := backend.ReadSave(restarted, "db/prefs")
	if err != nil || !bytes.Equal(again, got) {
		t.Fatal("ordinary save did not survive restart")
	}
}
func TestSubscriberReceiptRejectsIncompleteOrUnrecognizedState(t *testing.T) {
	for _, name := range []string{"checksum", "marker", "partial", "data", "removed", "removed-file", "missing-package", "wrong-size", "short-table", "wrong-identity"} {
		t.Run(name, func(t *testing.T) {
			base, table, files, rejected := receiptFixture(t)
			number := "01024681357"
			switch name {
			case "checksum":
				b, _ := base.LoadSave("db/prefs")
				b[62]++
				base.StoreSave("db/prefs", b)
			case "marker":
				setReceiptBits(rejected, 0, 8, 74)
				base.StoreSave("db/prefs", encodeReceiptFixture(rejected, table))
			case "partial":
				setReceiptBits(rejected, 15, 19, 1)
				base.StoreSave("db/prefs", encodeReceiptFixture(rejected, table))
			case "data":
				base.StoreSave("db/char.dat", []byte("incomplete"))
			case "removed":
				base.StoreSave(databaseRemovedKey, []byte("char.dat"))
			case "removed-file":
				base.StoreSave(guestFileRemovedKey, []byte("char.dat"))
			case "missing-package":
				delete(files, "char.dat")
			case "wrong-size":
				files["char.dat"] = files["char.dat"][:50]
			case "short-table":
				table = table[:255]
			case "wrong-identity":
				number = "123"
			}
			before, _ := base.LoadSave("db/prefs")
			store := newSubscriberReceiptStore(base, number, table, files)
			got, _, err := backend.ReadSave(store, "db/prefs")
			if err != nil || !bytes.Equal(got, before) {
				t.Fatalf("unsupported state changed: %v", err)
			}
		})
	}
}
func TestSubscriberReceiptBitFieldsCrossWords(t *testing.T) {
	b := make([]byte, 64)
	binary.LittleEndian.PutUint32(b, 0x4b640000)
	binary.LittleEndian.PutUint32(b[4:], 0)
	if receiptBits(b, 0, 8) != 75 || receiptBits(b, 8, 3) != 3 || receiptBits(b, 11, 4) != 2 {
		t.Fatal("bit order differs from word format")
	}
	setReceiptBits(b, 15, 19, 0x12345)
	if binary.LittleEndian.Uint32(b) != 0x4b6448d1 || binary.LittleEndian.Uint32(b[4:]) != 0x40000000 {
		t.Fatalf("cross-word write: %x", b[:8])
	}
}

type receiptReadFailure struct{ SaveStore }

func (store receiptReadFailure) ReadSave(name string) ([]byte, bool, error) {
	if name == databaseRemovedKey {
		return nil, false, errors.New("receipt read failed")
	}
	b, ok := store.LoadSave(name)
	return b, ok, nil
}
func TestSubscriberReceiptPreservesReadErrors(t *testing.T) {
	base, table, files, _ := receiptFixture(t)
	store := newSubscriberReceiptStore(receiptReadFailure{base}, "01024681357", table, files)
	_, _, err := backend.ReadSave(store, "db/prefs")
	if err == nil || !strings.Contains(err.Error(), "receipt read failed") {
		t.Fatalf("error lost: %v", err)
	}
}
func TestSubscriberReceiptRequiresRecognizedExecutable(t *testing.T) {
	base, table, files, _ := receiptFixture(t)
	a := &Archive{Files: files, JAR: &JAR{Client: ClientImage{Data: []byte("unrecognized")}, Entries: map[string][]byte{"work.bar": table}}}
	if got := authenticationSubscriberReceipt(base, a, "01024681357"); got != base {
		t.Fatal("unrecognized executable adapted")
	}
}
func TestLocalSubscriberReceiptRecognition(t *testing.T) {
	path := os.Getenv("WFEATURE_KTF_RECEIPT_ARCHIVE")
	if path == "" {
		t.Skip("set WFEATURE_KTF_RECEIPT_ARCHIVE")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	base := NewDirectorySaveStore(t.TempDir())
	if _, ok := authenticationSubscriberReceipt(base, archive, "01012349876").(*subscriberReceiptStore); !ok {
		t.Fatal("local receipt revision not recognized")
	}
}
