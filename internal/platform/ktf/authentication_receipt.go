package ktf

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"

	"github.com/movingwoo/wfeature/internal/backend"
)

// This compiled revision stores five content lengths in its preference record.
// Older saves can retain zero lengths even though every packaged file is present.
// Its private bit layout is not a WIPI contract, so other executables must not
// inherit this recovery. The subscriber accessor is independently recognized.
const subscriberReceiptImage = "390284cd703b23cd3fddd25991a290fe2a8d9f2104a61db4437fcb39cfec44fc"

var subscriberReceiptFiles = [...]string{"char.dat", "map.dat", "mon.dat", "pattern.dat", "tile.dat"}

type subscriberReceiptStore struct {
	base    SaveStore
	number  string
	table   []byte
	files   map[string][]byte
	lengths [5]uint32
}

func authenticationSubscriberReceipt(base SaveStore, archive *Archive, number string) SaveStore {
	if archive == nil || archive.JAR == nil || fmt.Sprintf("%x", sha256.Sum256(archive.JAR.Client.Data)) != subscriberReceiptImage {
		return base
	}
	// The matched decoder reads this table from its packaged resource container.
	const offset = 0x87938
	resource := archive.JAR.Entries["work.bar"]
	if len(resource) < offset+256 {
		return base
	}
	return newSubscriberReceiptStore(base, number, resource[offset:offset+256], archive.GuestFiles())
}

func newSubscriberReceiptStore(base SaveStore, number string, table []byte, files map[string][]byte) SaveStore {
	if len(number) != 11 || len(table) != 256 {
		return base
	}
	store := &subscriberReceiptStore{base: base, number: number, table: bytes.Clone(table), files: make(map[string][]byte)}
	plain, ok := store.decode(files["prefs"])
	if !ok {
		return base
	}
	for i, name := range subscriberReceiptFiles {
		data, exists := files[name]
		length := receiptBits(plain, 15+19*i, 19)
		if !exists || len(data) < 44 || length == 0 || uint64(length)+44 != uint64(len(data)) {
			return base
		}
		store.lengths[i] = length
		store.files[name] = bytes.Clone(data)
	}
	return store
}

// Reads recover only the content inventory. Settings, slot data, timestamp,
// nonce and unused bits retain their original values. Nothing is written by
// startup; later ordinary guest writes still use the existing save boundary.
func (store *subscriberReceiptStore) ReadSave(name string) ([]byte, bool, error) {
	key, err := NormalizeSaveKey(name)
	if err != nil {
		return nil, false, err
	}
	data, present, err := backend.ReadSave(store.base, key)
	if err != nil || !present || key != "db/prefs" {
		return data, present, err
	}
	plain, ok := store.decode(data)
	if !ok {
		return data, present, nil
	}
	for i := range store.lengths {
		if receiptBits(plain, 15+19*i, 19) != 0 {
			return data, present, nil
		}
	}
	removed, _, err := backend.ReadSave(store.base, databaseRemovedKey)
	if err != nil {
		return nil, false, err
	}
	for _, name := range splitRemovalList(removed) {
		if name == "prefs" {
			return data, present, nil
		}
		if _, ok := store.files[name]; ok {
			return data, present, nil
		}
	}
	for name, packaged := range store.files {
		saved, exists, err := backend.ReadSave(store.base, "db/"+name)
		if err != nil {
			return nil, false, err
		}
		if exists && !bytes.Equal(saved, packaged) {
			return data, present, nil
		}
		saved, exists, err = backend.ReadSave(store.base, "fs/"+name)
		if err != nil {
			return nil, false, err
		}
		if exists && !bytes.Equal(saved, packaged) {
			return data, present, nil
		}
	}
	removed, _, err = backend.ReadSave(store.base, guestFileRemovedKey)
	if err != nil {
		return nil, false, err
	}
	for _, name := range splitRemovalList(removed) {
		if _, ok := store.files[name]; ok {
			return data, present, nil
		}
	}
	for i, length := range store.lengths {
		setReceiptBits(plain, 15+19*i, 19, length)
	}
	encoded := store.crypt(plain)
	var sum byte
	for _, b := range encoded[:62] {
		sum += b
	}
	encoded[62] = sum
	return encoded, true, nil
}
func (store *subscriberReceiptStore) LoadSave(name string) ([]byte, bool) {
	data, present, _ := store.ReadSave(name)
	return data, present
}
func (store *subscriberReceiptStore) StoreSave(name string, data []byte) error {
	return store.StoreSaves(map[string][]byte{name: data})
}
func (store *subscriberReceiptStore) StoreSaves(entries map[string][]byte) error {
	return backend.StoreSaves(store.base, entries)
}

func (store *subscriberReceiptStore) decode(data []byte) ([]byte, bool) {
	if len(data) != 64 {
		return nil, false
	}
	var sum byte
	for _, b := range data[:62] {
		sum += b
	}
	if sum != data[62] {
		return nil, false
	}
	plain := store.crypt(data)
	return plain, receiptBits(plain, 0, 8) == 75
}
func (store *subscriberReceiptStore) crypt(data []byte) []byte {
	result := bytes.Clone(data)
	seed := int(data[63])
	for i := 0; i < 62; i++ {
		result[i] ^= store.table[(seed+i)%256] + store.number[(seed+i)%len(store.number)]
	}
	return result
}

// Fields run from the most significant bit of little-endian 32-bit words.
func receiptBits(data []byte, start, width int) uint32 {
	var result uint32
	for bit := start; bit < start+width; bit++ {
		word := binary.LittleEndian.Uint32(data[(bit/32)*4:])
		result = result<<1 | (word>>uint(31-bit%32))&1
	}
	return result
}
func setReceiptBits(data []byte, start, width int, value uint32) {
	for i := 0; i < width; i++ {
		bit := start + i
		at := (bit / 32) * 4
		word := binary.LittleEndian.Uint32(data[at:])
		mask := uint32(1) << uint(31-bit%32)
		word = word&^mask | ((value>>uint(width-i-1))&1)<<uint(31-bit%32)
		binary.LittleEndian.PutUint32(data[at:], word)
	}
}
