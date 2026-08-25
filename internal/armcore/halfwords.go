package armcore

import (
	"encoding/binary"
	"unsafe"
)

// Bulk halfword transfers.
//
// A platform that keeps its own copy of a guest surface moves that surface
// across this boundary in whole screens rather than in pixels: a 240x320
// framebuffer is 76,800 halfwords, and a drawing title crosses it twice per
// draw call. Read and Write already move a run of bytes in one memmove; what
// cost the time was what each caller then did with those bytes, which was to
// walk them a pixel at a time reassembling a uint16 out of two of them. That
// loop was 16% of a Clet's host time and every byte of its scratch buffer was
// garbage — 8.6 GB of it over a twenty-second run, collected on the machine
// that is also running the guest.
//
// The two functions below are that transfer done once. Guest memory is
// little-endian and so is every host this cross-compiles to, so the halfwords
// a caller holds already *are* the bytes the guest holds: the transfer is the
// same memmove Read and Write do, with no intermediate buffer and no loop.
// The reassembling loop stays for a big-endian host, where the two
// representations really do differ — it is the slow path there rather than the
// wrong answer.
//
// Nothing else about an access changes. Both go through Read and Write, so
// permissions, thread-local words and watchpoints answer exactly as they do
// for a run of bytes.

// nativeLittleEndian is whether this host lays a uint16 out the way the guest
// does. Every platform `make dist` builds for does.
var nativeLittleEndian = binary.NativeEndian.Uint16([]byte{1, 0}) == 1

// halfwordBytes views a halfword slice as the bytes it occupies. It is only
// ever called where nativeLittleEndian says those bytes are in guest order.
func halfwordBytes(values []uint16) []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(&values[0])), len(values)*2)
}

// ReadHalfwords fills destination from the little-endian halfwords at address.
func (memory *Memory) ReadHalfwords(address uint32, destination []uint16) error {
	if len(destination) == 0 {
		return nil
	}
	if nativeLittleEndian {
		return memory.Read(address, halfwordBytes(destination))
	}
	raw := make([]byte, len(destination)*2)
	if err := memory.Read(address, raw); err != nil {
		return err
	}
	for index := range destination {
		destination[index] = binary.LittleEndian.Uint16(raw[index*2:])
	}
	return nil
}

// WriteHalfwords stores source at address as little-endian halfwords.
func (memory *Memory) WriteHalfwords(address uint32, source []uint16) error {
	if len(source) == 0 {
		return nil
	}
	if nativeLittleEndian {
		return memory.Write(address, halfwordBytes(source))
	}
	raw := make([]byte, len(source)*2)
	for index, value := range source {
		binary.LittleEndian.PutUint16(raw[index*2:], value)
	}
	return memory.Write(address, raw)
}
