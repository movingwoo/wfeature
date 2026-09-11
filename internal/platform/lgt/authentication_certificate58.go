package lgt

import (
	"bytes"
	"sort"
	"strings"
	"sync"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/wipic"
)

const authenticationCertificate58Name = "audio.adt"
const authenticationCertificate58Key = "fs/" + authenticationCertificate58Name

// Require the complete connected 58-byte reader, writer, cipher and subscriber gate.
// BL targets and literal pools may relocate; instruction and field relationships
// remain fixed. A shared filename alone never selects this adapter.
var certificate58Cipher = []uint16{
	0xb530, 0x680c, 0x1c18, 0x2100, 0x1e15, 0xdd0a, 0x4b06, 0x4a07, 0x4343, 0x1898, 0x5c62, 0x1403,
	0x4053, 0x5463, 0x3101, 0x42a9, 0xdbf4, 0xbc30, 0xbc01, 0x4700,
}
var certificate58Writer = []uint16{
	0xb570, 0xb081, 0x1c06, 0x4b2c, 0x203a, 0x1c0c, 0x1c15, 0, 0, 0x223a, 0x4b29, 0x2100,
	0x9000, 0, 0, 0x1c21, 0x9800, 0x4c27, 0, 0, 0x9800, 0x1c29, 0x3028, 0,
	0, 0x9800, 0x4b23, 0x2204, 0x18f1, 0x3034, 0x4b22, 0, 0, 0x1c30, 0x4669, 0x223a,
	0x4b20, 0, 0, 0x2500, 0x481f, 0x2108, 0x2201, 0x4b1f, 0, 0, 0x1c04, 0x1c43,
	0xd102, 0x3501, 0x2d09, 0xddf3, 0x9900, 0x2c00, 0xda08, 0x2900, 0xd003, 0x1c08, 0x4b18, 0,
	0, 0x2300, 0x9300, 0xe013, 0x223a, 0x4b16, 0x1c20, 0, 0, 0x4b15, 0x1c05, 0x1c20,
	0, 0, 0x9800, 0x2800, 0xd002, 0x4b0f, 0, 0, 0x2300, 0x9300, 0x2d00, 0xdc01,
	0x2000, 0xe000, 0x2001, 0xb001, 0xbc70, 0xbc02, 0x4708,
}
var certificate58Reader = []uint16{
	0xb570, 0xb081, 0x1c06, 0x4b25, 0x203a, 0, 0, 0x2500, 0x9000, 0x4823, 0x2108, 0x2201,
	0x4b22, 0, 0, 0x1c04, 0x1c43, 0xd102, 0x3501, 0x2d09, 0xddf3, 0x9800, 0x2c00, 0xdb11,
	0x2100, 0x223a, 0x4b1c, 0, 0, 0x1c20, 0x9900, 0x223a, 0x4b1a, 0, 0, 0x2800,
	0xdc0d, 0x1c20, 0x4b18, 0, 0, 0x9800, 0x2800, 0xd002, 0x4b16, 0, 0, 0x2300,
	0x2000, 0x9300, 0xe015, 0x1c30, 0x4669, 0x223a, 0x4b12, 0, 0, 0x4b12, 0x9900, 0x18f0,
	0x223a, 0x4b11, 0, 0, 0x9800, 0x2800, 0xd002, 0x4b0b, 0, 0, 0x2300, 0x9300,
	0x2001, 0xb001, 0xbc70, 0xbc02, 0x4708,
}
var certificate58Gate = []uint16{
	0xb570, 0xb083, 0x1c05, 0x2100, 0x220c, 0x4b17, 0x4668, 0, 0, 0x1c28, 0, 0,
	0x600, 0x2800, 0xd01f, 0x4b13, 0x2204, 0x18ec, 0x3335, 0x18e9, 0x1c20, 0x4b11, 0, 0,
	0x2300, 0x56e3, 0x2b09, 0xdc0e, 0x4669, 0x220c, 0x4b0d, 0x480e, 0, 0, 0x4b0d, 0x4668,
	0x18e9, 0x220c, 0x4b0c, 0, 0, 0x2800, 0xd101, 0x2001, 0xe001, 0x2001, 0x4240, 0xb003,
	0xbc70, 0xbc02, 0x4708,
}

func authenticationCertificate58(module *Module) bool {
	readers := findOptionCode(module, certificate58Reader)
	if len(readers) == 0 {
		return false
	}
	writers, gates := findOptionCode(module, certificate58Writer), findOptionCode(module, certificate58Gate)
	for _, reader := range readers {
		literal := func(address uint32) uint32 { value, _ := optionLiteral(module, address); return value }
		name := literal(reader + 0x12)
		if !bytes.Equal(optionBytes(module, name, len(authenticationCertificate58Name)+1, false), []byte(authenticationCertificate58Name+"\x00")) {
			continue
		}
		cipher, ok := optionCall(module, reader+0x6e)
		if !ok || !matchOptionCode(module, cipher, certificate58Cipher) || literal(cipher+0xc) != 0x343fd || literal(cipher+0xe) != 0x269ec3 || literal(reader+0x6c) != 0x21c3 {
			continue
		}
		decoded := literal(reader + 0x72)
		if decoded == 0 || decoded > 0x100000 {
			continue
		}
		trampoline, _ := optionCall(module, reader+0xa)
		if !bytes.Equal(optionBytes(module, trampoline, 2, true), []byte{0x18, 0x47}) {
			continue
		}
		if !certificate58Calls(module, reader, certificate58Reader, trampoline, map[uint32]uint32{0x6e: cipher}) {
			continue
		}
		for _, writer := range writers {
			writerCipher, _ := optionCall(module, writer+0x4a)
			stringTrampoline, _ := optionCall(module, writer+0x24)
			if writerCipher != cipher || literal(writer+0x50) != name || literal(writer+0x48) != 0x21c3 || literal(writer+0x34)+1 != decoded ||
				!bytes.Equal(optionBytes(module, stringTrampoline, 2, true), []byte{0x20, 0x47}) ||
				!certificate58Calls(module, writer, certificate58Writer, trampoline, map[uint32]uint32{0x24: stringTrampoline, 0x2e: stringTrampoline, 0x4a: cipher}) {
				continue
			}
			linked := true
			for _, pair := range [][2]uint32{{reader + 6, writer + 6}, {reader + 0x18, writer + 0x56}, {reader + 0x34, writer + 0x14}, {reader + 0x4c, writer + 0x8a}, {reader + 0x58, writer + 0x74}, {reader + 0x58, writer + 0x9a}, {reader + 0x58, reader + 0x86}, {reader + 0x7a, writer + 0x3c}} {
				linked = linked && literal(pair[0]) != 0 && literal(pair[0]) == literal(pair[1])
			}
			if !linked {
				continue
			}
			for _, gate := range gates {
				gateReader, _ := optionCall(module, gate+0x14)
				if gateReader == reader && literal(gate+0x1e)+1 == decoded && literal(gate+0x44) == decoded+40 && literal(gate+0x2a) == literal(reader+0x7a) &&
					bytes.Equal(optionBytes(module, literal(gate+0x3e), 12, false), []byte("PHONENUMBER\x00")) &&
					certificate58Calls(module, gate, certificate58Gate, trampoline, map[uint32]uint32{0x14: reader}) {
					return true
				}
			}
		}
	}
	return false
}

func certificate58Calls(module *Module, address uint32, pattern []uint16, trampoline uint32, special map[uint32]uint32) bool {
	for index := 0; index < len(pattern); index++ {
		if pattern[index] != 0 {
			continue
		}
		offset := uint32(index * 2)
		want := trampoline
		if target, ok := special[offset]; ok {
			want = target
		}
		got, ok := optionCall(module, address+offset)
		if !ok || got != want {
			return false
		}
		index++
	}
	return true
}

func cryptCertificate58(data []byte) {
	state := uint32(0x21c3)
	for index := range data {
		state = state*0x343fd + 0x269ec3
		data[index] ^= byte(state >> 16)
	}
}

// The whole certificate stays private, including guest rewrites and deletion.
// Both filesystem ledgers preserve the original certificate membership while
// ordinary files retain the same persistence path as authentication-off runs.
type authenticationCertificate58Store struct {
	mu                 sync.Mutex
	base               backend.SaveStore
	certificate        []byte
	ledgers            map[string][]byte
	originalMembership map[string]bool
}

func newAuthenticationCertificate58Store(base backend.SaveStore, archive *Archive, number string) (*authenticationCertificate58Store, bool) {
	if wipic.ValidateSubscriberNumber(number) != nil {
		return nil, false
	}
	var data []byte
	var exists bool
	if base != nil {
		data, exists = base.LoadSave(authenticationCertificate58Key)
	}
	if !exists {
		data, exists = archive.Resource(authenticationCertificate58Name)
	}
	// A refused first connection can leave an empty file. The matched guest
	// reader treats it as missing; replace it only in this session.
	if exists && len(data) != 0 && len(data) != 58 {
		return nil, false
	}
	if len(data) == 58 {
		data = bytes.Clone(data)
		cryptCertificate58(data)
	} else {
		data = make([]byte, 58)
	}
	clear(data[40:52])
	copy(data[40:52], number)
	cryptCertificate58(data)
	store := &authenticationCertificate58Store{base: base, certificate: data, ledgers: make(map[string][]byte), originalMembership: make(map[string]bool)}
	for _, key := range []string{fileRemovedKey, fileCreatedKey} {
		var original []byte
		if base != nil {
			original, _ = base.LoadSave(key)
		}
		for _, name := range strings.Split(string(original), "\n") {
			if strings.TrimSpace(name) == authenticationCertificate58Name {
				store.originalMembership[key] = true
			}
		}
		store.ledgers[key] = certificate58Ledger(original, key == fileCreatedKey)
	}
	return store, true
}

func certificate58Ledger(data []byte, include bool) []byte {
	var names []string
	for _, name := range strings.Split(string(data), "\n") {
		name = strings.TrimSpace(name)
		if name != "" && name != authenticationCertificate58Name {
			names = append(names, name)
		}
	}
	if include {
		names = append(names, authenticationCertificate58Name)
	}
	sort.Strings(names)
	return []byte(strings.Join(names, "\n"))
}

func (store *authenticationCertificate58Store) LoadSave(name string) ([]byte, bool) {
	key, err := backend.NormalizeSaveKey(name)
	if err != nil {
		return nil, false
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if strings.EqualFold(key, authenticationCertificate58Key) {
		return bytes.Clone(store.certificate), true
	}
	if data, ok := store.ledgers[key]; ok {
		return bytes.Clone(data), true
	}
	if store.base == nil {
		return nil, false
	}
	return store.base.LoadSave(key)
}

func (store *authenticationCertificate58Store) StoreSave(name string, data []byte) error {
	key, err := backend.NormalizeSaveKey(name)
	if err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if strings.EqualFold(key, authenticationCertificate58Key) {
		store.certificate = bytes.Clone(data)
		return nil
	}
	if _, ok := store.ledgers[key]; ok {
		persisted := certificate58Ledger(data, store.originalMembership[key])
		if store.base != nil {
			original, exists := store.base.LoadSave(key)
			if (exists || len(persisted) != 0) && !bytes.Equal(original, persisted) {
				if err := store.base.StoreSave(key, persisted); err != nil {
					return err
				}
			}
		}
		store.ledgers[key] = bytes.Clone(data)
		return nil
	}
	if store.base == nil {
		return nil
	}
	return store.base.StoreSave(key, data)
}

// Both language boundaries use the identity captured when this client loaded.
func (client *Client) systemProperty(name string) (string, bool) {
	if name == "PHONENUMBER" || name == "MIN" {
		return client.subscriberNumber, true
	}
	value, ok := wipic.SystemProperties[name]
	return value, ok
}
