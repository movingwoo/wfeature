package skt

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/sgsvm"
	"golang.org/x/text/encoding/korean"
)

const (
	wideResourceLimit = 4 << 20
	wideHeapLimit     = 16 << 20
	wideFileLimit     = 4 << 20
)

var errWideHeapFull = errors.New("SGS: media heap exceeds limit")

type wideFileHandle struct {
	name     string
	position int
	readOnly bool
	data     *[]byte
}

func wideResource(vm *sgsvm.WideVM, index int32, write bool) (*sgsvm.Resource, error) {
	if index < 0 || int(index) >= len(vm.Resources) {
		return nil, fmt.Errorf("SGS: invalid media index %d", index)
	}
	r := &vm.Resources[index]
	if write && !r.Mutable {
		return nil, fmt.Errorf("SGS: media %d is constant", index)
	}
	return r, nil
}

func wideResize(vm *sgsvm.WideVM, index int32, size int) (*sgsvm.Resource, error) {
	r, err := wideResource(vm, index, true)
	if err != nil {
		return nil, err
	}
	if size < 0 || size > wideResourceLimit {
		return nil, fmt.Errorf("SGS: media allocation size %d exceeds limit", size)
	}
	if size == len(r.Data) {
		return r, nil
	}
	if err := vm.Charge(1 + len(vm.Resources)/8 + size/64); err != nil {
		return nil, err
	}
	if size > cap(r.Data) {
		total := size - cap(r.Data)
		for _, other := range vm.Resources {
			total += cap(other.Data)
			if total > wideHeapLimit {
				return nil, errWideHeapFull
			}
		}
	}
	data := make([]byte, size)
	copy(data, r.Data)
	r.Data = data
	return r, nil
}

func wideMediaSpan(vm *sgsvm.WideVM, index, offset, count int32, write bool) ([]byte, error) {
	r, err := wideResource(vm, index, write)
	if err != nil {
		return nil, err
	}
	if offset < 0 || count < 0 || int(offset) > len(r.Data) || int(count) > len(r.Data)-int(offset) {
		return nil, fmt.Errorf("SGS: media %d range %d+%d outside %d bytes", index, offset, count, len(r.Data))
	}
	return r.Data[int(offset) : int(offset)+int(count)], nil
}

// Integer pointers may start at any byte of the word-aligned guest array.
// Keep byte access explicit so adjacent bytes survive partial reads.
type wideWordBytes struct {
	words  []int32
	offset int
}

func wideByteAddress(vm *sgsvm.WideVM, addr, offset, count int32) (wideWordBytes, error) {
	if offset < 0 || count < 0 || int64(offset)+int64(count) > wideResourceLimit {
		return wideWordBytes{}, fmt.Errorf("SGS: integer byte span %d+%d exceeds limit", offset, count)
	}
	words, err := vm.Address(addr, (int(offset)+int(count)+3)/4)
	return wideWordBytes{words, int(offset)}, err
}
func (w wideWordBytes) at(i int) byte {
	p := w.offset + i
	return byte(uint32(w.words[p/4]) >> (uint(p%4) * 8))
}
func (w wideWordBytes) put(i int, b byte) {
	p := w.offset + i
	shift := uint(p%4) * 8
	w.words[p/4] = int32(uint32(w.words[p/4])&^(uint32(0xff)<<shift) | uint32(b)<<shift)
}

func (s *ScriptSession) wideStorage(id uint16, vm *sgsvm.WideVM) (bool, error) {
	if id >= 0x63 && id <= 0x6b {
		return true, s.wideFile(id, vm)
	}
	switch id {
	case 0x82, 0x83, 0x8b:
		return true, s.wideSound(id, vm)
	case 0x93, 0x94:
		return true, s.wideNV(id == 0x94, vm)
	case 0x9d, 0x9e:
		n := 1
		if id == 0x9e {
			n = 2
		}
		a, err := vm.Args(n)
		if err != nil {
			return true, err
		}
		r, err := wideResource(vm, a[0], false)
		if err != nil {
			return true, err
		}
		if id == 0x9d {
			return true, vm.Push(int32(len(r.Data)))
		}
		if !r.Mutable || a[1] < 0 || a[1] > wideResourceLimit {
			return true, vm.Push(0)
		}
		size := (int(a[1]) + 1) &^ 1
		if size > len(r.Data) || len(r.Data)-size > 16 {
			if _, err = wideResize(vm, a[0], size); err != nil {
				if errors.Is(err, errWideHeapFull) {
					return true, vm.Push(0)
				}
				return true, err
			}
		}
		return true, vm.Push(1)
	case 0xa9, 0xaa, 0xab, 0xac, 0xbd, 0xbf, 0xc6:
		return true, wideMemory(id, vm)
	default:
		return false, nil
	}
}

func wideMemory(id uint16, vm *sgsvm.WideVM) error {
	n := 4
	switch id {
	case 0xa9:
		n = 3
	case 0xaa:
		n = 2
	case 0xbf:
		n = 5
	}
	a, err := vm.Args(n)
	if err != nil {
		return err
	}
	if id == 0xa9 || id == 0xaa {
		w, err := wideByteAddress(vm, a[0], a[1], 1)
		if err != nil {
			return err
		}
		if id == 0xaa {
			return vm.Push(int32(w.at(0)))
		}
		w.put(0, byte(a[2]))
		return nil
	}
	size := a[n-1]
	if size < 0 || size > wideResourceLimit {
		return fmt.Errorf("SGS: memory transfer size %d exceeds limit", size)
	}
	if err := vm.Charge(1 + int(size)/16); err != nil {
		return err
	}
	if id == 0xbf || id == 0xc6 {
		dst, err := wideMediaSpan(vm, a[0], a[1], size, true)
		if err != nil {
			return err
		}
		if id == 0xc6 {
			for i := range dst {
				dst[i] = byte(a[2])
			}
			return nil
		}
		src, err := wideMediaSpan(vm, a[2], a[3], size, false)
		if err != nil {
			return err
		}
		copy(dst, src)
		return nil
	}
	offset := a[1]
	if id == 0xbd {
		if offset < 0 || offset > wideResourceLimit/2 || size > wideResourceLimit/2 {
			return fmt.Errorf("SGS: invalid word transfer")
		}
		offset *= 2
		size *= 2
	}
	w, err := wideByteAddress(vm, a[0], offset, size)
	if err != nil {
		return err
	}
	if id == 0xac {
		if _, err := wideResize(vm, a[2], int(size)); err != nil {
			return err
		}
	}
	media, err := wideMediaSpan(vm, a[2], 0, size, id != 0xab)
	if err != nil {
		return err
	}
	for i, b := range media {
		if id == 0xab {
			w.put(i, b)
		} else {
			media[i] = w.at(i)
		}
	}
	return nil
}

func (s *ScriptSession) wideSound(id uint16, vm *sgsvm.WideVM) error {
	if id == 0x83 {
		s.audio.Advance(s.clock)
		s.audio.StopAll()
		return nil
	}
	a, err := vm.Args(1)
	if err != nil {
		return err
	}
	if id == 0x8b {
		s.audio.Advance(s.clock)
		s.audio.SetVolume(int(max(0, min(10, a[0]))) * 10)
		return nil
	}
	r, err := wideResource(vm, a[0], false)
	if err != nil {
		return err
	}
	if r.Kind != 2 || len(r.Data) < 10 || string(r.Data[2:6]) != "MMMD" {
		return fmt.Errorf("SGS: invalid sound resource %d", a[0])
	}
	size := 8 + uint64(binary.BigEndian.Uint32(r.Data[6:10]))
	if size > uint64(len(r.Data)-2) {
		return fmt.Errorf("SGS: truncated sound resource %d", a[0])
	}
	if err := vm.Charge(int(size)/16 + 1); err != nil {
		return err
	}
	h, err := s.audio.Load(r.Data[2 : 2+size])
	if err != nil {
		return err
	}
	s.audio.Advance(s.clock)
	if s.sound != 0 {
		_ = s.audio.Close(s.sound)
	}
	s.sound = h
	return s.audio.Play(h, s.clock, binary.LittleEndian.Uint16(r.Data)&0x8000 != 0)
}

func (s *ScriptSession) wideStore() backend.SaveStore {
	if s.options.SaveStore == nil {
		s.options.SaveStore, _ = backend.NewMemorySaveStore(nil)
	}
	return s.options.SaveStore
}

func (s *ScriptSession) wideSeed(name string) ([]byte, bool) {
	if s.archive == nil {
		return nil, false
	}
	data, ok := s.archive.Entries[strings.ToLower(name)]
	return data, ok
}

func (s *ScriptSession) wideNV(write bool, vm *sgsvm.WideVM) error {
	a, err := vm.Args(2)
	if err != nil {
		return err
	}
	if a[1] < 0 || a[1] > 16 {
		return fmt.Errorf("SGS: NV transfer %d exceeds 16 integers", a[1])
	}
	w, err := vm.Address(a[0], int(a[1]))
	if err != nil {
		return err
	}
	data, present, err := backend.ReadSaveLimit(s.wideStore(), "nv/data", 64)
	if err != nil {
		return err
	}
	if !present {
		data, _ = s.wideSeed("UserNV.sys")
	}
	var slot [64]byte
	copy(slot[:], data)
	for i := range w {
		if write {
			binary.LittleEndian.PutUint32(slot[4*i:], uint32(w[i]))
		} else {
			w[i] = int32(binary.LittleEndian.Uint32(slot[4*i:]))
		}
	}
	if write {
		return s.options.SaveStore.StoreSave("nv/data", slot[:])
	}
	return nil
}

func wideFileName(vm *sgsvm.WideVM, index int32) (string, bool) {
	r, err := wideResource(vm, index, false)
	if err != nil {
		return "", false
	}
	raw, _, _ := bytes.Cut(r.Data[:min(len(r.Data), 513)], []byte{0})
	if len(raw) == 0 || len(raw) > 512 {
		return "", false
	}
	name, err := korean.EUCKR.NewDecoder().String(string(raw))
	if err != nil {
		return "", false
	}
	return normalizeWideFileName(name)
}

func normalizeWideFileName(name string) (string, bool) {
	name = strings.TrimLeft(strings.ReplaceAll(strings.ToLower(name), "\\", "/"), "/")
	if strings.ContainsAny(name, ":\r\n") || strings.ContainsRune(name, '\uFFFD') {
		return "", false
	}
	name, err := backend.NormalizeSaveKey(name)
	return name, err == nil && len("files/")+len(name) <= 512
}

// One record distinguishes an empty file from a deletion that hides a bundled seed.
func (s *ScriptSession) wideFileContents(vm *sgsvm.WideVM, name string) ([]byte, bool, error) {
	for _, h := range s.wideFiles {
		if h.data != nil && h.name == name {
			// Restoring a handle on a removed file keeps its cursor and its
			// shared buffer, but the name stays absent until a create or write.
			return *h.data, *h.data != nil, nil
		}
	}
	if err := vm.Charge(32); err != nil {
		return nil, false, err
	}
	raw, present, err := backend.ReadSaveLimit(s.wideStore(), "files/"+name, wideFileLimit+8)
	if err != nil {
		return nil, false, err
	}
	if present {
		if err := vm.Charge(1 + len(raw)/32); err != nil {
			return nil, false, err
		}
	}
	return s.decodeWideFile(name, raw, present)
}

func (s *ScriptSession) decodeWideFile(name string, raw []byte, present bool) ([]byte, bool, error) {
	var data []byte
	if present {
		if len(raw) < 4 || binary.LittleEndian.Uint32(raw) > 1 {
			return nil, false, fmt.Errorf("SGS: invalid saved file record")
		}
		records, err := backend.DecodeSaveRecords(raw)
		if err != nil || len(records) == 0 {
			return nil, false, err
		}
		data = records[0]
	} else {
		var found bool
		if data, found = s.wideSeed(name); !found {
			return nil, false, nil
		}
	}
	if data == nil {
		data = []byte{}
	}
	return data, true, nil
}

func (s *ScriptSession) wideWriteFile(name string, data []byte) error {
	var records [][]byte
	if data != nil {
		records = [][]byte{data}
	}
	return s.wideStore().StoreSave("files/"+name, backend.EncodeSaveRecords(records))
}

func (s *ScriptSession) wideFile(id uint16, vm *sgsvm.WideVM) error {
	n := [...]int{2, 1, 1, 3, 3, 4, 3, 4, 1}[id-0x63]
	a, err := vm.Args(n)
	if err != nil {
		return err
	}
	if id == 0x63 || id == 0x65 || id == 0x6b {
		name, valid := wideFileName(vm, a[0])
		if !valid {
			return vm.Push(-1)
		}
		data, found, err := s.wideFileContents(vm, name)
		if err != nil {
			return err
		}
		if id == 0x6b {
			if found {
				return vm.Push(1)
			}
			return vm.Push(-1)
		}
		if id == 0x65 {
			if !found {
				return vm.Push(-1)
			}
			for _, h := range s.wideFiles {
				if h.data != nil && h.name == name {
					return vm.Push(-1)
				}
			}
			if err := s.wideWriteFile(name, nil); err != nil {
				return err
			}
			return vm.Push(1)
		}
		mode := a[1]
		if mode < 1 || mode > 4 || found == (mode == 2) {
			return vm.Push(-1)
		}
		for i, h := range s.wideFiles {
			if h.data != nil {
				continue
			}
			if mode == 2 {
				keys, listed, err := backend.ListSaves(s.wideStore(), "files")
				if err != nil {
					return err
				}
				if listed && len(keys) >= 256 && !slices.Contains(keys, "files/"+name) {
					return vm.Push(-1)
				}
				data = []byte{}
				if err := s.wideWriteFile(name, data); err != nil {
					return err
				}
			}
			handle := wideFileHandle{name: name, data: &data, readOnly: mode == 3}
			for _, other := range s.wideFiles {
				if other.data != nil && other.name == name {
					handle.data = other.data
					if mode == 2 {
						*handle.data = data
					}
					break
				}
			}
			if mode == 4 {
				handle.position = len(data)
			}
			s.wideFiles[i] = handle
			return vm.Push(int32(i))
		}
		return vm.Push(-1)
	}
	if a[0] < 0 || int(a[0]) >= len(s.wideFiles) || s.wideFiles[a[0]].data == nil {
		return vm.Push(-1)
	}
	h := &s.wideFiles[a[0]]
	switch id {
	case 0x64:
		*h = wideFileHandle{}
		return vm.Push(1)
	case 0x66:
		pos := int64(a[1])
		switch a[2] {
		case 1:
		case 2:
			pos += int64(h.position)
		case 3:
			pos += int64(len(*h.data))
		default:
			return vm.Push(-1)
		}
		if pos < 0 || pos > wideFileLimit {
			return vm.Push(-1)
		}
		h.position = int(pos)
		return vm.Push(1)
	}
	count := a[len(a)-1]
	word := id == 0x67 || id == 0x69
	write := id <= 0x68
	if count < 0 || count > wideFileLimit || word && count > wideFileLimit/4 || write && h.readOnly {
		return vm.Push(-1)
	}
	if word {
		count *= 4
	}
	if err := vm.Charge(1 + int(count)/16); err != nil {
		return err
	}
	var media []byte
	var words wideWordBytes
	if word {
		words, err = wideByteAddress(vm, a[1], 0, count)
	} else {
		media, err = wideMediaSpan(vm, a[1], a[2], count, !write)
	}
	if err != nil {
		return vm.Push(-1)
	}
	data := *h.data
	if count == 0 {
		return vm.Push(0)
	}
	if !write {
		if h.position >= len(data) {
			return vm.Push(0)
		}
		src := data[h.position : h.position+min(int(count), len(data)-h.position)]
		if word {
			for i, b := range src {
				words.put(i, b)
			}
		} else {
			copy(media, src)
		}
		h.position += len(src)
		return vm.Push(int32(len(src)))
	}
	if int(count) > wideFileLimit-h.position {
		return vm.Push(-1)
	}
	end := h.position + int(count)
	if err := vm.Charge(1 + max(len(data), end)/32); err != nil {
		return err
	}
	next := make([]byte, max(len(data), end))
	copy(next, data)
	if word {
		for i := 0; i < int(count); i++ {
			next[h.position+i] = words.at(i)
		}
	} else {
		copy(next[h.position:], media)
	}
	if err := s.wideWriteFile(h.name, next); err != nil {
		return err
	}
	*h.data = next
	h.position = end
	return vm.Push(count)
}
