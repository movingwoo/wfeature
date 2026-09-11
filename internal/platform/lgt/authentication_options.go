package lgt

import (
	"bytes"
	"encoding/binary"
	"sync"

	"github.com/movingwoo/wfeature/internal/backend"
)

const authenticationOptionsName = "game_option.txt"
const authenticationOptionsKey = "fs/" + authenticationOptionsName

// These instruction constraints recognize a cached authentication result in a
// 56-byte options record. Zero pairs represent Thumb BL instructions; literal
// loads retain their destination register but resolve their own pool offset.
// Reader, writer, response handler and startup gate must share the same buffer.
var optionReader = []uint16{
	0xb510, 0x480f, 0x2101, 0x2201, 0x4b0e, 0, 0, 0x1e04, 0xdb0a, 0x1c20, 0x490c, 0x2238, 0x4b0c, 0, 0, 0x1c20,
	0x4b0b, 0, 0, 0xe007, 0x4b09, 0x1c20, 0, 0, 0, 0, 0, 0, 0xbc10, 0xbc01, 0x4700,
}
var optionWriter = []uint16{
	0xb510, 0x2108, 0x2201, 0x4b0b, 0x480b, 0, 0, 0x2100, 0x1c04, 0x2200, 0x4b09, 0, 0, 0x4909, 0x2238, 0x1c20,
	0x4b08, 0, 0, 0x1c20, 0x4b07, 0, 0, 0xbc10, 0xbc01, 0x4700,
}
var optionReply = []uint16{
	0xb5f0, 0xb082, 0x4dc2, 0x466c, 0x3406, 0x2700, 0x1d29, 0x2202, 0x4bc0, 0x8027, 0x1c20, 0, 0, 0x2397, 0x2000, 0x5e22,
	0x005b, 0x2606, 0x8821, 0x429a, 0xd124, 0x4bbb, 0x681b, 0x2b01, 0xd10a, 0x79a9, 0x2900, 0xd001, 0x2901, 0xd117, 0, 0,
	0x2002, 0, 0, 0xe20e, 0x2b02, 0xd000, 0xe218, 0x79a9, 0x2900, 0xd002, 0x2901, 0xd005, 0xe008, 0x4ab0, 0x2301, 0x62d6,
	0x6293, 0xe0c2, 0x4bad, 0x62de, 0x6299, 0xe0be, 0x4bab, 0x2208, 0x62da, 0xe205,
}
var optionGate = []uint16{
	0x4d6f, 0x6aeb, 0x2b32, 0xd10b, 0x2401, 0x2008, 0x62ec, 0, 0, 0x4b77, 0x2000, 0, 0, 0x4b76, 0x701c, 0xe0bb,
	0, 0, 0x4b74, 0x2201, 0x601a, 0x6aab, 0x495d, 0x2b01, 0xd00b, 0x6aeb, 0x704a, 0x2b01, 0xd100, 0xe0ad, 0x2b02, 0xd101,
	0x62ea, 0xe0a9, 0x2304, 0x62eb, 0xe0a6, 0x4a6c, 0x2300, 0x6013, 0x2302, 0x704b, 0xe0a0,
}

func authenticationOptions(module *Module) bool {
	readers := findOptionCode(module, optionReader)
	if len(readers) == 0 {
		return false
	}
	replies, gates := findOptionCode(module, optionReply), findOptionCode(module, optionGate)
	var selected uint32
	for _, reader := range readers {
		name, ok := optionLiteral(module, reader+2)
		if !ok || !bytes.Equal(optionBytes(module, name, len(authenticationOptionsName)+1, false), []byte(authenticationOptionsName+"\x00")) {
			continue
		}
		buffer, ok := optionLiteral(module, reader+20)
		if !ok || !optionDataRange(module, buffer, 56) {
			continue
		}
		writer, ok := optionCall(module, reader+52)
		if !ok || !matchOptionCode(module, writer, optionWriter) {
			continue
		}
		writerName, _ := optionLiteral(module, writer+8)
		writerBuffer, _ := optionLiteral(module, writer+26)
		if writerName != name || writerBuffer != buffer {
			continue
		}
		trampoline, _ := optionCall(module, reader+10)
		if !bytes.Equal(optionBytes(module, trampoline, 2, true), []byte{0x18, 0x47}) {
			continue
		}
		fileCalls := true
		for _, call := range []uint32{reader + 26, reader + 34, reader + 44, writer + 10, writer + 22, writer + 34, writer + 42} {
			target, ok := optionCall(module, call)
			fileCalls = fileCalls && ok && target == trampoline
		}
		readOpen, _ := optionLiteral(module, reader+8)
		writeOpen, _ := optionLiteral(module, writer+6)
		readClose, _ := optionLiteral(module, reader+32)
		writeClose, _ := optionLiteral(module, writer+40)
		if !fileCalls || readOpen != writeOpen || readClose != writeClose {
			continue
		}
		matchedReply, matchedGate := false, false
		for _, reply := range replies {
			a, _ := optionLiteral(module, reply+90)
			b, _ := optionLiteral(module, reply+100)
			c, _ := optionLiteral(module, reply+108)
			matchedReply = matchedReply || a == buffer && b == buffer && c == buffer
		}
		for _, gate := range gates {
			value, _ := optionLiteral(module, gate)
			matchedGate = matchedGate || value == buffer
		}
		if matchedReply && matchedGate {
			if selected != 0 && selected != buffer {
				return false
			}
			selected = buffer
		}
	}
	return selected != 0
}

func optionBytes(module *Module, address uint32, size int, executable bool) []byte {
	if module == nil || size < 0 {
		return nil
	}
	for _, section := range module.Sections {
		if executable && !section.Executable || address < section.Address {
			continue
		}
		offset := uint64(address) - uint64(section.Address)
		if offset+uint64(size) <= uint64(len(section.Data)) {
			return section.Data[offset : offset+uint64(size)]
		}
	}
	return nil
}

func optionDataRange(module *Module, address uint32, size uint32) bool {
	for _, section := range module.Sections {
		if !section.Executable && address >= section.Address && uint64(address)+uint64(size) <= uint64(section.Address)+uint64(section.Size) {
			return address%4 == 0
		}
	}
	return false
}

func optionLiteral(module *Module, address uint32) (uint32, bool) {
	code := optionBytes(module, address, 2, true)
	if len(code) != 2 {
		return 0, false
	}
	op := binary.LittleEndian.Uint16(code)
	if op&0xf800 != 0x4800 {
		return 0, false
	}
	pool := ((uint64(address) + 4) &^ 3) + uint64(op&0xff)*4
	if pool > 0xffffffff {
		return 0, false
	}
	data := optionBytes(module, uint32(pool), 4, false)
	if len(data) != 4 {
		return 0, false
	}
	return binary.LittleEndian.Uint32(data), true
}

func optionCall(module *Module, address uint32) (uint32, bool) {
	code := optionBytes(module, address, 4, true)
	if len(code) != 4 {
		return 0, false
	}
	hi, lo := binary.LittleEndian.Uint16(code), binary.LittleEndian.Uint16(code[2:])
	if hi&0xf800 != 0xf000 || lo&0xf800 != 0xf800 {
		return 0, false
	}
	displacement := int32(uint32(hi&0x7ff)<<12 | uint32(lo&0x7ff)<<1)
	displacement = displacement << 9 >> 9
	target := int64(address) + 4 + int64(displacement)
	if target < 0 || target > 0xffffffff || len(optionBytes(module, uint32(target), 2, true)) != 2 {
		return 0, false
	}
	return uint32(target), true
}

func matchOptionCode(module *Module, address uint32, pattern []uint16) bool {
	code := optionBytes(module, address, len(pattern)*2, true)
	if len(code) != len(pattern)*2 || address&1 != 0 {
		return false
	}
	for index := 0; index < len(pattern); index++ {
		want, got := pattern[index], binary.LittleEndian.Uint16(code[index*2:])
		switch {
		case want == 0:
			if index+1 >= len(pattern) || pattern[index+1] != 0 {
				return false
			}
			if _, ok := optionCall(module, address+uint32(index*2)); !ok {
				return false
			}
			index++
		case want&0xf800 == 0x4800:
			if want&0xff00 != got&0xff00 {
				return false
			}
			if _, ok := optionLiteral(module, address+uint32(index*2)); !ok {
				return false
			}
		default:
			if want != got {
				return false
			}
		}
	}
	return true
}

func findOptionCode(module *Module, pattern []uint16) []uint32 {
	var found []uint32
	if module == nil {
		return nil
	}
	for _, section := range module.Sections {
		if !section.Executable {
			continue
		}
		for offset := 0; offset+len(pattern)*2 <= len(section.Data); offset += 2 {
			address := uint64(section.Address) + uint64(offset)
			if address > 0xffffffff || !matchOptionCode(module, uint32(address), pattern) {
				continue
			}
			if len(found) == 64 {
				return nil
			}
			found = append(found, uint32(address))
		}
	}
	return found
}

// Only the cached authentication word is private. Ordinary settings and game
// progress still persist; writing a viewed record restores its original word.
type authenticationOptionStore struct {
	mu       sync.Mutex
	base     backend.SaveStore
	archive  *Archive
	original [4]byte
	volatile []byte
}

func newAuthenticationOptionStore(base backend.SaveStore, archive *Archive) *authenticationOptionStore {
	store := &authenticationOptionStore{base: base, archive: archive}
	if data, ok := store.loadOptions(); ok && len(data) >= 44 {
		copy(store.original[:], data[40:44])
	}
	return store
}

func (store *authenticationOptionStore) loadOptions() ([]byte, bool) {
	if store.base != nil {
		if data, ok := store.base.LoadSave(authenticationOptionsKey); ok {
			return data, true
		}
	}
	if store.volatile != nil {
		return store.volatile, true
	}
	if store.archive != nil {
		return store.archive.Resource(authenticationOptionsName)
	}
	return nil, false
}

func (store *authenticationOptionStore) LoadSave(name string) ([]byte, bool) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if name == authenticationOptionsKey {
		data, ok := store.loadOptions()
		data = bytes.Clone(data)
		if ok && len(data) == 56 {
			binary.LittleEndian.PutUint32(data[40:44], 1)
		}
		return data, ok
	}
	if store.base != nil {
		return store.base.LoadSave(name)
	}
	return nil, false
}

func (store *authenticationOptionStore) StoreSave(name string, data []byte) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if name == authenticationOptionsKey {
		data = bytes.Clone(data)
		if len(data) > 40 {
			copy(data[40:min(44, len(data))], store.original[:])
		}
		if store.base == nil {
			store.volatile = data
		}
	}
	if store.base != nil {
		return store.base.StoreSave(name, data)
	}
	return nil
}
