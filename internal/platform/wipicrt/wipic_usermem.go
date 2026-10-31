package wipicrt

import (
	"encoding/binary"
	"fmt"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// A handset carried libraries beyond the WIPI C interface tables, and a game
// reached one by asking the kernel for it by name. The only library the local
// archives ask for is a user-space memory manager: the game hands it a buffer
// out of its own image and then carves that buffer into fixed blocks, so the
// library never owns memory of its own.
//
// That is the whole reason this can be implemented rather than refused. The
// pool is guest memory the caller supplies and the sizes come from the caller
// too, so serving the calls means bookkeeping, not inventing an allocator's
// address space. A name that is not this one is answered with null, which is
// what a handset without that library answers.
const UserMemoryLibrary = "MXUserMemInterf"

// The library's table, established by disassembling the one caller and
// watching which entries it reaches. Only create and alloc are exercised by
// the local archive; the rest are named where the shape is unambiguous and
// reported when reached so the next title says what it needs.
const (
	UserMemoryCreate = 0 // (pool, size) -> 0
	UserMemoryAlloc  = 1 // (pool, size) -> block address, or 0 when the pool is full
	userMemoryFree   = 2 // (pool, block) -> 0
)

// The version reported back through MC_knlGetDLLInterface's out-parameters.
// The one local caller asks for "any version" and ignores the answer, so this
// only has to be a version, not a particular one.
const (
	userMemoryVersionMajor = 1
	userMemoryVersionMinor = 0
)

// UserMemoryPool is the Host's view of one guest buffer handed to the library.
// Blocks are cut from the front and never given back: the caller allocates its
// fixed set once at startup and holds it for the run, so a free list would
// carry cost for a reuse that does not happen. freeCalls records whether that
// assumption ever broke.
type UserMemoryPool struct {
	Base      uint32
	Size      uint32
	Cursor    uint32
	FreeCalls uint32
}

// WIPICGetDLLInterface serves MC_knlGetDLLInterface(name, major, minor,
// rtnMajor, rtnMinor). The version arguments are a request, not a demand — the
// one local caller passes -1 for both, meaning any — and the two out-pointers
// receive what was actually returned when the caller supplied them.
func (runtime *Runtime) WIPICGetDLLInterface(thread *armcore.Thread) (uint32, error) {
	nameAddress, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	name, err := runtime.ReadCString(nameAddress, 128)
	if err != nil {
		return 0, fmt.Errorf("read %s library name: %w", runtime.platformName(), err)
	}
	if name != UserMemoryLibrary {
		// No such library on this handset. Null is the documented answer and
		// the game's own error path reads it; a fabricated table would be a
		// surface whose contract nobody knows.
		runtime.CountDiagnostic(fmt.Sprintf("library %q refused", name))
		return 0, nil
	}
	if runtime.UserMemoryInterface == 0 {
		entries := make([]uint32, WIPICTableFunctions)
		for function := range entries {
			stub, stubErr := runtime.Stub(SVCCategoryWIPIC, wipicTableUserMemory<<16|uint32(function))
			if stubErr != nil {
				return 0, stubErr
			}
			entries[function] = stub
		}
		address, allocErr := runtime.AllocateWords(entries)
		if allocErr != nil {
			return 0, allocErr
		}
		runtime.UserMemoryInterface = address
	}
	// rtnMajor and rtnMinor report the version the caller received. Both are
	// optional; the local caller passes null for each. rtnMajor arrives in r3
	// and rtnMinor is the first stacked argument.
	stackPointer, err := thread.Register(armcore.RegisterSP)
	if err != nil {
		return 0, err
	}
	majorOut, err := thread.Register(3)
	if err != nil {
		return 0, err
	}
	minorOut, err := runtime.readGuestWord(stackPointer)
	if err != nil {
		return 0, fmt.Errorf("read %s library version argument: %w", runtime.platformName(), err)
	}
	for _, out := range [...]struct {
		address uint32
		value   uint32
	}{
		{majorOut, userMemoryVersionMajor},
		{minorOut, userMemoryVersionMinor},
	} {
		if out.address == 0 {
			continue
		}
		if err := runtime.WriteGuestWord(out.address, out.value); err != nil {
			return 0, fmt.Errorf("write %s library version at %#x: %w", runtime.platformName(), out.address, err)
		}
	}
	runtime.CountDiagnostic(fmt.Sprintf("library %q", name))
	return runtime.UserMemoryInterface, nil
}

func (runtime *Runtime) HandleUserMemoryCall(thread *armcore.Thread, function uint32) (uint32, error) {
	pool, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	argument, err := thread.Register(1)
	if err != nil {
		return 0, err
	}
	switch function {
	case UserMemoryCreate:
		if pool == 0 || argument == 0 {
			return WipiErrorCode, nil
		}
		// The buffer must be readable and writable for its whole length before
		// the first block is handed out, so a bad pool fails here rather than
		// somewhere inside the game's data.
		if err := runtime.checkGuestRange(pool, argument); err != nil {
			return 0, fmt.Errorf("%s user memory pool at %#x: %w", runtime.platformName(), pool, err)
		}
		if runtime.UserMemoryPools == nil {
			runtime.UserMemoryPools = make(map[uint32]*UserMemoryPool)
		}
		runtime.UserMemoryPools[pool] = &UserMemoryPool{Base: pool, Size: argument}
		runtime.CountDiagnostic("user memory pool created")
		return 0, nil
	case UserMemoryAlloc:
		record := runtime.UserMemoryPools[pool]
		if record == nil {
			// Allocating from a pool that was never created is the caller's
			// error; answering null is what a full pool answers, and its error
			// path already handles that.
			runtime.CountDiagnostic("user memory alloc without a pool")
			return 0, nil
		}
		// Eight-byte alignment: the caller stores doubles and pointers in these
		// blocks, and the sizes it asks for are not multiples of anything.
		const alignment = 8
		start := (record.Cursor + alignment - 1) &^ (alignment - 1)
		if argument == 0 || uint64(start)+uint64(argument) > uint64(record.Size) {
			runtime.CountDiagnostic("user memory pool exhausted")
			return 0, nil
		}
		record.Cursor = start + argument
		return record.Base + start, nil
	case userMemoryFree:
		record := runtime.UserMemoryPools[pool]
		if record != nil {
			record.FreeCalls++
		}
		// Blocks are not reused; see userMemoryPool.
		runtime.CountDiagnostic("user memory free")
		return 0, nil
	default:
		return 0, fmt.Errorf("%s user memory library function %d is not implemented%s", runtime.platformName(), function, runtime.CallerSite(thread))
	}
}

// checkGuestRange fails unless every byte of a guest range is both readable
// and writable, by rewriting the bytes it read. A pool that is short or
// unmapped is then reported where it was handed over rather than at whichever
// block first crossed the end of it. The size is the guest's own argument, so
// the range is checked against the mappings before a buffer of that size is
// allocated to read it through.
func (runtime *Runtime) checkGuestRange(address, size uint32) error {
	if uint64(address)+uint64(size) > uint64(1)<<32 {
		return fmt.Errorf("range of %d bytes overflows guest memory", size)
	}
	if err := runtime.Client.core.Memory().ValidateRange(address, uint64(size), armcore.PermissionReadWrite); err != nil {
		return err
	}
	data := make([]byte, size)
	if err := runtime.Client.core.Memory().Read(address, data); err != nil {
		return err
	}
	return runtime.Client.core.Memory().Write(address, data)
}

func (runtime *Runtime) readGuestWord(address uint32) (uint32, error) {
	var word [4]byte
	if err := runtime.Client.core.Memory().Read(address, word[:]); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(word[:]), nil
}

func (runtime *Runtime) WriteGuestWord(address, value uint32) error {
	var word [4]byte
	binary.LittleEndian.PutUint32(word[:], value)
	return runtime.Client.core.Memory().Write(address, word[:])
}
