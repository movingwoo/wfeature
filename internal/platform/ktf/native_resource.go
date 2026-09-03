package ktf

import (
	"encoding/binary"
	"fmt"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// A later module of the earlier package does not open its data files. It asks
// the object it was handed for a numbered resource out of a named file, and
// the platform gives it back a block it reads and then hands back:
//
//	ldr  r0, [r7, #0x10]      ; the object the entry was called with
//	movs r3, #6               ; the kind
//	ldr  r1, [r0]             ; its table
//	ldr  r2, [pc, #0x40]      ; 5001, the number
//	ldr  r6, [r1, #0x48]      ; the loader
//	ldr  r1, [pc, #0x40] ; add r1, pc   ; "rpg_org.bar"
//	bl   veneer               ; r0 = the block
//	...                       ; allocate 0x400 and copy 0x400 from r0 + 0x1c
//	ldr  r2, [r1, #0x50]      ; the matching free, called with the block
//
// The `.bar` file beside the module is what the number indexes. Its shape is
// established by the call above rather than by a specification: the item the
// module asks for is 0x41c bytes long, and the module copies 0x400 of it from
// offset 0x1c, which is the whole of it. So the loader hands back the item
// verbatim and what the first 0x1c bytes hold is the title's business.
//
// See docs/ktf.md, "An earlier KTF package".
const (
	// nativeObjectLoadResource takes a file name, a number and a kind, and
	// answers a block holding one item of that file.
	nativeObjectLoadResource = 0x48
	// nativeObjectFreeResource gives that block back.
	nativeObjectFreeResource = 0x50
)

// The resource file's own layout, in bytes from its start:
//
//	0x00  a version halfword, 0x11 in every file of the three archives
//	0x02  two halfwords, 1 and 1
//	0x06  how many groups
//	0x08  where the group table is, and how long it is
//	0x10  where the index table is
//	0x14  how many items there are
//	0x18  where the first item is, repeating the index table's first entry
//	0x1c  a further offset
//
// The index table holds one offset per item and then the end of the last one,
// which is the file's own length in all eight of the local files. A group is
// four halfwords — a kind, the first number it covers, how many further
// numbers it covers, and the index of the item the first number names — so the
// numbers a file answers to are runs rather than a list, and the item for a
// number in a group's run is its distance into the run added to that index.
const (
	nativeResourceHeaderSize  = 0x20
	nativeResourceVersion     = 0x11
	nativeResourceGroupStride = 8
	nativeResourceMaxGroups   = 1 << 12
	nativeResourceMaxItems    = 1 << 16
)

// nativeResourceFile is a parsed resource file.
type nativeResourceFile struct {
	data   []byte
	groups []nativeResourceGroup
	// index holds one offset per item and the end of the last one.
	index []uint32
}

// nativeResourceGroup is one run of numbers the file answers to.
type nativeResourceGroup struct {
	Kind  uint16
	First uint16
	// Span is how many *further* numbers the group covers, so a group with a
	// span of zero still answers one number.
	Span uint16
	// Item is the index of the item the group's first number names.
	Item uint16
}

// parseNativeResourceFile reads a resource file.
//
// Everything it checks is something a file of this shape has and a file of
// another shape does not: the module opens its saves and its own data through
// the file interface too, and a title that hands this loader a name that is
// not a resource file has to be told no rather than handed nine bytes of one.
func parseNativeResourceFile(data []byte) (*nativeResourceFile, error) {
	if len(data) < nativeResourceHeaderSize {
		return nil, fmt.Errorf("resource file is %d bytes, too short for its header", len(data))
	}
	if version := binary.LittleEndian.Uint16(data); version != nativeResourceVersion {
		return nil, fmt.Errorf("resource file names version %#x, want %#x", version, nativeResourceVersion)
	}
	groupCount := uint64(binary.LittleEndian.Uint16(data[0x06:]))
	groupTable := uint64(binary.LittleEndian.Uint32(data[0x08:]))
	groupBytes := uint64(binary.LittleEndian.Uint32(data[0x0c:]))
	indexTable := uint64(binary.LittleEndian.Uint32(data[0x10:]))
	itemCount := uint64(binary.LittleEndian.Uint32(data[0x14:]))
	if groupCount == 0 || groupCount > nativeResourceMaxGroups {
		return nil, fmt.Errorf("resource file declares %d groups", groupCount)
	}
	if itemCount == 0 || itemCount > nativeResourceMaxItems {
		return nil, fmt.Errorf("resource file declares %d items", itemCount)
	}
	if groupBytes != groupCount*nativeResourceGroupStride {
		return nil, fmt.Errorf("resource file declares %d groups in %d bytes", groupCount, groupBytes)
	}
	if groupTable+groupBytes > uint64(len(data)) {
		return nil, fmt.Errorf("resource file group table at %#x runs past its %d bytes", groupTable, len(data))
	}
	// One more offset than there are items: the last item needs an end.
	if indexTable+(itemCount+1)*4 > uint64(len(data)) {
		return nil, fmt.Errorf("resource file index table at %#x runs past its %d bytes", indexTable, len(data))
	}
	file := &nativeResourceFile{data: data}
	for offset := uint64(0); offset < groupCount; offset++ {
		record := data[groupTable+offset*nativeResourceGroupStride:]
		file.groups = append(file.groups, nativeResourceGroup{
			Kind:  binary.LittleEndian.Uint16(record),
			First: binary.LittleEndian.Uint16(record[2:]),
			Span:  binary.LittleEndian.Uint16(record[4:]),
			Item:  binary.LittleEndian.Uint16(record[6:]),
		})
	}
	file.index = make([]uint32, itemCount+1)
	for offset := range file.index {
		file.index[offset] = binary.LittleEndian.Uint32(data[indexTable+uint64(offset)*4:])
	}
	if uint64(file.index[0]) != uint64(binary.LittleEndian.Uint32(data[0x18:])) {
		return nil, fmt.Errorf("resource file names its first item at %#x and its table names %#x",
			binary.LittleEndian.Uint32(data[0x18:]), file.index[0])
	}
	for offset := 1; offset < len(file.index); offset++ {
		if file.index[offset] < file.index[offset-1] {
			return nil, fmt.Errorf("resource file item %d ends at %#x, before it starts at %#x",
				offset-1, file.index[offset], file.index[offset-1])
		}
	}
	if uint64(file.index[len(file.index)-1]) > uint64(len(data)) {
		return nil, fmt.Errorf("resource file last item ends at %#x, past its %d bytes",
			file.index[len(file.index)-1], len(data))
	}
	return file, nil
}

// item finds one numbered item of one kind.
func (file *nativeResourceFile) item(kind, number uint16) ([]byte, bool) {
	if file == nil {
		return nil, false
	}
	for _, group := range file.groups {
		if group.Kind != kind || number < group.First || number > group.First+group.Span {
			continue
		}
		index := uint64(group.Item) + uint64(number-group.First)
		if index+1 >= uint64(len(file.index)) {
			return nil, false
		}
		return file.data[file.index[index]:file.index[index+1]], true
	}
	return nil, false
}

// resourceFile parses one of the title's files, keeping what it parsed: a
// title reads several numbers out of the same file and the tables are the same
// tables every time.
func (platform *NativePlatform) resourceFile(name string) (*nativeResourceFile, bool) {
	if file, ok := platform.resources[name]; ok {
		return file, file != nil
	}
	if platform.resources == nil {
		platform.resources = map[string]*nativeResourceFile{}
	}
	data, ok := platform.contents(name)
	if !ok {
		platform.resources[name] = nil
		return nil, false
	}
	file, err := parseNativeResourceFile(data)
	if err != nil {
		// A name that is not a resource file is the title's own affair — it
		// asks for its saves through the file interface and for these through
		// here — so this is a no rather than the end of the run. What it was
		// is kept for a report to say.
		platform.note(fmt.Sprintf("%s is not a resource file: %v", name, err))
		platform.resources[name] = nil
		return nil, false
	}
	platform.resources[name] = file
	return file, true
}

// loadResource answers the object's resource loader.
func (platform *NativePlatform) loadResource(thread *armcore.Thread) (uint32, error) {
	arguments, err := nativeArguments(thread, 4)
	if err != nil {
		return 0, err
	}
	name, err := platform.readName(arguments[1])
	if err != nil {
		return 0, err
	}
	file, ok := platform.resourceFile(name)
	if !ok {
		return 0, nil
	}
	item, ok := file.item(uint16(arguments[3]), uint16(arguments[2]))
	if !ok {
		platform.note(fmt.Sprintf("%s carries no resource %d of kind %d", name, arguments[2], arguments[3]))
		return 0, nil
	}
	if len(item) == 0 {
		return 0, nil
	}
	block, err := platform.client.Allocate(uint32(len(item)))
	if err != nil {
		return 0, err
	}
	if err := platform.client.core.Memory().Write(block, item); err != nil {
		return 0, fmt.Errorf("write resource %d of %q to %#x: %w", arguments[2], name, block, err)
	}
	return block, nil
}

// freeResource gives one back. The block came out of the arena, so this is the
// platform table's own deallocator reached through another name — and a block
// this platform never handed out is ignored there for the same reason it is
// ignored here.
func (platform *NativePlatform) freeResource(thread *armcore.Thread) (uint32, error) {
	address, err := thread.Register(1)
	if err != nil {
		return 0, err
	}
	platform.client.Free(address)
	return 0, nil
}
