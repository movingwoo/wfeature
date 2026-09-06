package backend

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"os"
	"path/filepath"
	"testing"
)

func samplePack(t *testing.T) SavePack {
	t.Helper()
	return SavePack{
		Identity: sha256.Sum256([]byte("an archive")),
		Entries: []SaveEntry{
			{Key: "rms/slot0", Data: []byte("progress")},
			{Key: "rms/.index", Data: []byte("slot0\n")},
			{Key: "fs/OptionSave", Data: []byte{0x00, 0xff, 0x10}},
		},
	}
}

func TestSavePackRoundTrip(t *testing.T) {
	pack := samplePack(t)
	container, err := EncodeSavePack(pack)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := DecodeSavePack(container)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.Identity != pack.Identity {
		t.Errorf("identity did not survive the round trip")
	}
	// The entries come back in key order regardless of the order they went in,
	// which is what makes two exports of one directory the same bytes.
	want := []string{"fs/OptionSave", "rms/.index", "rms/slot0"}
	if len(decoded.Entries) != len(want) {
		t.Fatalf("got %d entries, want %d", len(decoded.Entries), len(want))
	}
	for index, key := range want {
		if decoded.Entries[index].Key != key {
			t.Errorf("entry %d is %q, want %q", index, decoded.Entries[index].Key, key)
		}
	}
	if !bytes.Equal(decoded.Entries[2].Data, []byte("progress")) {
		t.Errorf("entry data did not survive the round trip")
	}
}

// The checksum is only useful if one save always encodes to one set of bytes:
// otherwise two exports of an untouched directory look like two different
// saves, and nobody can tell a re-export from a change.
func TestSavePackEncodesTheSameBytesTwice(t *testing.T) {
	pack := samplePack(t)
	first, err := EncodeSavePack(pack)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	shuffled := SavePack{Identity: pack.Identity, Entries: []SaveEntry{
		pack.Entries[2], pack.Entries[0], pack.Entries[1],
	}}
	second, err := EncodeSavePack(shuffled)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Errorf("the same save encoded to two different containers")
	}
}

func TestSavePackHeaderIsTheDocumentedShape(t *testing.T) {
	pack := samplePack(t)
	container, err := EncodeSavePack(pack)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if string(container[:8]) != savePackMagic {
		t.Errorf("magic is %q, want %q", container[:8], savePackMagic)
	}
	if container[8] != savePackVersion || container[9] != 0 {
		t.Errorf("version/reserved are %d/%d, want %d/0", container[8], container[9], savePackVersion)
	}
	if !bytes.Equal(container[10:42], pack.Identity[:]) {
		t.Errorf("the identity is not the 32 bytes at offset 10")
	}
	length := binary.LittleEndian.Uint32(container[42:46])
	if int(length) != len(container)-savePackHeaderSize {
		t.Errorf("length says %d, payload is %d", length, len(container)-savePackHeaderSize)
	}
}

// The two refusals a person can act on have to be different errors. Answering
// "could not import" to both sends someone hunting for another copy of a file
// that was never theirs, or retrying one that will never load.
func TestSavePackTellsTheWrongGameFromDamage(t *testing.T) {
	pack := samplePack(t)
	container, err := EncodeSavePack(pack)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	// A container belonging to another game is intact: it decodes, and only
	// the identity comparison refuses it.
	other := sha256.Sum256([]byte("a different archive"))
	decoded, err := DecodeSavePack(container)
	if err != nil {
		t.Fatalf("an intact container for another game did not decode: %v", err)
	}
	if decoded.Identity == other {
		t.Fatalf("the two identities collided, which makes this test meaningless")
	}

	// Damage is a refusal from the container itself.
	damaged := append([]byte(nil), container...)
	damaged[len(damaged)-1] ^= 0x01
	if _, err := DecodeSavePack(damaged); !errors.Is(err, ErrSavePackDamaged) {
		t.Errorf("a flipped payload bit gave %v, want ErrSavePackDamaged", err)
	}
}

func TestSavePackRefusalsAreDistinguished(t *testing.T) {
	pack := samplePack(t)
	container, err := EncodeSavePack(pack)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	corrupt := func(edit func([]byte) []byte) []byte {
		return edit(append([]byte(nil), container...))
	}

	for _, testCase := range []struct {
		name      string
		container []byte
		want      error
	}{
		{"another file entirely", []byte("this is a photograph"), ErrNotSavePack},
		{"the header alone is too short", container[:savePackHeaderSize-1], ErrNotSavePack},
		{"a newer version", corrupt(func(b []byte) []byte { b[8] = savePackVersion + 1; return b }), ErrSavePackVersion},
		{"a reserved byte with a meaning", corrupt(func(b []byte) []byte { b[9] = 1; return b }), ErrSavePackVersion},
		{"a transfer that stopped", container[:len(container)-3], ErrSavePackDamaged},
		{"bytes appended", append(append([]byte(nil), container...), 0), ErrSavePackDamaged},
		{"a flipped bit", corrupt(func(b []byte) []byte { b[len(b)-1] ^= 0x40; return b }), ErrSavePackDamaged},
		{"a length that disagrees", corrupt(func(b []byte) []byte {
			binary.LittleEndian.PutUint32(b[42:46], 1<<30)
			return b
		}), ErrSavePackDamaged},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := DecodeSavePack(testCase.container)
			if !errors.Is(err, testCase.want) {
				t.Errorf("got %v, want %v", err, testCase.want)
			}
		})
	}
}

// A checksum says the bytes arrived as they were sent, not that whoever sent
// them meant well. A key that climbs out of the save directory is refused even
// though the container checks out.
func TestSavePackRefusesAKeyThatClimbs(t *testing.T) {
	for _, key := range []string{"../elsewhere", "fs/../../elsewhere", "fs//double", "./fs/x"} {
		payload := make([]byte, 0, 16)
		payload = binary.LittleEndian.AppendUint16(payload, uint16(len(key)))
		payload = append(payload, key...)
		payload = binary.LittleEndian.AppendUint32(payload, 0)

		container := handMadeContainer(sha256.Sum256(nil), payload)
		if _, err := DecodeSavePack(container); !errors.Is(err, ErrSavePackDamaged) {
			t.Errorf("key %q gave %v, want ErrSavePackDamaged", key, err)
		}
	}
}

// handMadeContainer builds a valid header around a payload EncodeSavePack
// would never write, which is the only way to reach the payload walk's own
// refusals with a checksum that agrees.
func handMadeContainer(identity [32]byte, payload []byte) []byte {
	container := append([]byte(nil), savePackMagic...)
	container = append(container, savePackVersion, 0)
	container = append(container, identity[:]...)
	container = binary.LittleEndian.AppendUint32(container, uint32(len(payload)))
	container = binary.LittleEndian.AppendUint32(container, crc32.ChecksumIEEE(payload))
	return append(container, payload...)
}

func TestSavePackRefusesKeysOutOfOrderOrRepeated(t *testing.T) {
	entry := func(key string) []byte {
		out := binary.LittleEndian.AppendUint16(nil, uint16(len(key)))
		out = append(out, key...)
		return binary.LittleEndian.AppendUint32(out, 0)
	}
	for _, testCase := range []struct {
		name    string
		payload []byte
	}{
		{"out of order", append(entry("fs/b"), entry("fs/a")...)},
		{"repeated", append(entry("fs/a"), entry("fs/a")...)},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			container := handMadeContainer(sha256.Sum256(nil), testCase.payload)
			if _, err := DecodeSavePack(container); !errors.Is(err, ErrSavePackDamaged) {
				t.Errorf("got %v, want ErrSavePackDamaged", err)
			}
		})
	}
}

func TestSavePackCarriesAnEmptySave(t *testing.T) {
	pack := SavePack{Identity: sha256.Sum256([]byte("x"))}
	container, err := EncodeSavePack(pack)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if len(container) != savePackHeaderSize {
		t.Errorf("an empty save encoded to %d bytes, want %d", len(container), savePackHeaderSize)
	}
	decoded, err := DecodeSavePack(container)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(decoded.Entries) != 0 {
		t.Errorf("got %d entries, want none", len(decoded.Entries))
	}
}

// The index file MIDP's record stores keep is named `.index`, and it is what
// says a store exists at all. A backup that skipped the dotted names would
// restore a save whose stores the game cannot find — which reads to a player
// as an import that worked and lost everything.
func TestReadSaveTreeKeepsTheDottedIndexAndDropsTemporaries(t *testing.T) {
	root := t.TempDir()
	write := func(name string, content string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("rms/.index", "slot0\n")
	write("rms/slot0", "records")
	write("db/.removed", "old\n")
	write("fs/.created", "dir\n")
	// What a killed process leaves behind on the way to a rename.
	write("rms/.slot0.418304559", "half a save")

	entries, err := ReadSaveTree(root)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	got := map[string]bool{}
	for _, entry := range entries {
		got[entry.Key] = true
	}
	for _, key := range []string{"rms/.index", "rms/slot0", "db/.removed", "fs/.created"} {
		if !got[key] {
			t.Errorf("%q is missing from the backup", key)
		}
	}
	if got["rms/.slot0.418304559"] {
		t.Errorf("a leftover temporary file was backed up as a save")
	}
}

func TestReadSaveTreeOnAGameThatHasNeverSaved(t *testing.T) {
	entries, err := ReadSaveTree(filepath.Join(t.TempDir(), "never-played"))
	if err != nil {
		t.Fatalf("a missing save directory should read as no entries: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("got %d entries, want none", len(entries))
	}
}

// A restore is the moment the backup describes, not that moment merged with
// whatever was there. An entry the backup does not name is removed.
func TestWriteSaveTreeReplacesRatherThanMerges(t *testing.T) {
	root := t.TempDir()
	store := NewDirectorySaveStore(root)
	for _, key := range []string{"rms/slot0", "rms/slot1", "fs/deep/older"} {
		if err := store.StoreSave(key, []byte("before")); err != nil {
			t.Fatal(err)
		}
	}

	written, removed, err := WriteSaveTree(root, []SaveEntry{
		{Key: "rms/slot0", Data: []byte("after")},
		{Key: "rms/.index", Data: []byte("slot0\n")},
	})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if written != 2 {
		t.Errorf("wrote %d entries, want 2", written)
	}
	if removed != 2 {
		t.Errorf("removed %d entries, want 2", removed)
	}

	if data, ok := store.LoadSave("rms/slot0"); !ok || string(data) != "after" {
		t.Errorf("the restored entry is %q/%v, want \"after\"", data, ok)
	}
	if _, ok := store.LoadSave("rms/slot1"); ok {
		t.Errorf("an entry the backup does not name survived the restore")
	}
	// The directory an emptied entry left behind goes too, so a later export
	// does not carry a shape the game never wrote.
	if _, err := os.Stat(filepath.Join(root, "fs", "deep")); !os.IsNotExist(err) {
		t.Errorf("an emptied directory was left behind: %v", err)
	}
}

func TestSaveTreeRoundTripThroughAContainer(t *testing.T) {
	source := t.TempDir()
	store := NewDirectorySaveStore(source)
	for key, value := range map[string]string{
		"rms/.index": "slot0\nslot1\n",
		"rms/slot0":  "one",
		"rms/slot1":  "two",
		"fs/a/b/c":   "three",
	} {
		if err := store.StoreSave(key, []byte(value)); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := ReadSaveTree(source)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	identity := sha256.Sum256([]byte("archive bytes"))
	container, err := EncodeSavePack(SavePack{Identity: identity, Entries: entries})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	decoded, err := DecodeSavePack(container)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.Identity != identity {
		t.Fatalf("identity did not survive")
	}
	target := t.TempDir()
	if _, _, err := WriteSaveTree(target, decoded.Entries); err != nil {
		t.Fatalf("write: %v", err)
	}
	restored, err := ReadSaveTree(target)
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if len(restored) != len(entries) {
		t.Fatalf("restored %d entries, want %d", len(restored), len(entries))
	}
	for index := range entries {
		if restored[index].Key != entries[index].Key || !bytes.Equal(restored[index].Data, entries[index].Data) {
			t.Errorf("entry %d differs: %q vs %q", index, restored[index].Key, entries[index].Key)
		}
	}
}
