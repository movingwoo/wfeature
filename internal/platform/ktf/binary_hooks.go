package ktf

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// These games ship their C runtime compiled into the image, and they lean on
// it hard: a title can fill buffers through its own memset, and a screen-wide
// repaint effect then spends most of its guest execution inside that one fill
// loop. Interpreting a byte-at-a-time fill is the worst thing
// this emulator can be asked to do — every byte is several instructions, each
// with a fetch and a decode — while the Host can do the same fill in one Go
// copy.
//
// So the routines are recognized in the image and answered natively. Matching
// is by code shape, not by game: the patterns below are what a particular
// compiler emits for these functions, so any binary built the same way gets
// the same treatment. A pattern that matches nothing costs one scan at load.
//
// The routine's entry is replaced with a supervisor-call stub, so anything
// that reaches it — a direct call, a call through a function pointer — lands
// on the native implementation. The instructions after the stub become
// unreachable rather than wrong, since the stub returns before them.

type binaryHookKind uint32

const (
	hookMemset binaryHookKind = iota + 1
	hookMemcpy
	hookStrlen
	hookStrcpy
)

type binaryHook struct {
	kind    binaryHookKind
	name    string
	pattern string
}

// binaryHooks is the table. The RVCT thunks are the standard library helpers
// reached through a `BX PC` veneer, told apart by the `CMP R2` immediate that
// follows. The Thumb memset is the same function compiled directly into the
// image, which the thunk pattern therefore never sees.
var binaryHooks = []binaryHook{
	{
		kind:    hookMemcpy,
		name:    "RVCT __rt_memcpy thunk",
		pattern: "78 47 00 00 01 40 2d e9 03 00 52 e3",
	},
	{
		kind:    hookMemset,
		name:    "RVCT __rt_memset thunk",
		pattern: "78 47 00 00 01 40 2d e9 04 00 52 e3",
	},
	{
		// PUSH {R4,R5,LR}; MOV R4,R1; MOV R5,R0; MOV R1,R0; CMP R2,#3;
		// BLS byte_tail; TST R0,#3; BNE byte_tail; then a word-at-a-time
		// fill. It returns dst, which it kept in R5.
		kind:    hookMemset,
		name:    "Thumb memset compiled into the image",
		pattern: "30 b5 0c 1c 05 1c 01 1c 03 2a 18 d9 03 23 03 40 00 2b 14 d1 ff 23 1c 40",
	},
	{
		kind:    hookStrlen,
		name:    "RVCT Thumb strlen",
		pattern: "30 b5 03 23 03 40 04 1c 00 2b",
	},
	{
		// The strcpy beside it, told apart by its own prologue: PUSH
		// {R4,R5,LR}; MOV R3,R1; ORRS R3,R0; MOVS R2,#3; ANDS R3,R2; MOV R4,R0.
		// Both use Mycroft's NUL-byte detection over whole words.
		kind:    hookStrcpy,
		name:    "RVCT Thumb strcpy",
		pattern: "70 b5 0b 1c 03 43 03 22 13 40 04 1c",
	},
}

// installImageHooks reads the loaded client image back out of guest memory and
// installs every hook whose pattern it finds. Reading memory rather than the
// archive bytes means the entry's self-relocation has already happened, so a
// pattern that relocation rewrote is not matched and one it left alone is
// matched at the address that will actually run.
func (runtime *initializationRuntime) installImageHooks() (int, error) {
	image := runtime.client.image
	if len(image.Data) == 0 {
		return 0, nil
	}
	loaded := make([]byte, len(image.Data))
	if err := runtime.client.core.Memory().Read(ImageBase, loaded); err != nil {
		return 0, fmt.Errorf("read KTF image for binary hooks: %w", err)
	}
	return runtime.installBinaryHooks(loaded, ImageBase)
}

// installBinaryHooks scans the loaded image for the routines above and
// replaces each match's entry with a stub. It reports how many were installed.
func (runtime *initializationRuntime) installBinaryHooks(image []byte, base uint32) (int, error) {
	installed := 0
	// A stub is wider than some of the patterns, so two matches close together
	// could have one stub land inside another. Patterns this specific do not
	// overlap in practice; refusing is how that stays true rather than being
	// assumed.
	claimed := make([]uint32, 0, len(binaryHooks))
	overlaps := func(address uint32) bool {
		for _, taken := range claimed {
			if address < taken+hookStubSize && taken < address+hookStubSize {
				return true
			}
		}
		return false
	}
	for _, hook := range binaryHooks {
		raw, err := hex.DecodeString(strings.ReplaceAll(hook.pattern, " ", ""))
		if err != nil {
			return installed, fmt.Errorf("KTF binary hook %q pattern: %w", hook.name, err)
		}
		for offset := 0; ; {
			index := bytes.Index(image[offset:], raw)
			if index < 0 {
				break
			}
			address := base + uint32(offset+index)
			offset += index + 1
			if overlaps(address) {
				runtime.client.log("KTF binary hook skipped as overlapping",
					"routine", hook.name, "address", fmt.Sprintf("%#x", address))
				continue
			}
			// A Thumb routine is entered with bit zero set; the stub is Thumb
			// either way, and the ARM thunks branch straight into Thumb code.
			if err := runtime.installHookStub(address, hook.kind); err != nil {
				return installed, err
			}
			claimed = append(claimed, address)
			runtime.countDiagnostic("binary hook " + hook.name)
			runtime.client.log("KTF binary hook installed", "routine", hook.name, "address", fmt.Sprintf("%#x", address))
			installed++
		}
	}
	return installed, nil
}

// installHookStub writes the supervisor-call stub over a routine's entry. It
// is the same shape as the platform callback stubs: stash the identifier in
// r12, trap, and return to the caller's link register. Guest arguments stay in
// r0-r2 where the routine expects them.
// hookStubSize is how many bytes a stub occupies at a routine's entry.
const hookStubSize = 16

func (runtime *initializationRuntime) installHookStub(address uint32, kind binaryHookKind) error {
	stub := []byte{
		0x10, 0xb4, // push {r4}
		0x02, 0x4c, // ldr r4, [pc, #8]
		0xa4, 0x46, // mov r12, r4
		0x10, 0xbc, // pop {r4}
		byte(svcCategoryBinaryHook), 0xdf, // svc #category
		0x70, 0x47, // bx lr
		byte(kind), byte(kind >> 8), byte(kind >> 16), byte(kind >> 24),
	}
	if err := runtime.client.core.Memory().Load(address&^1, stub); err != nil {
		return fmt.Errorf("install KTF binary hook stub at %#x: %w", address, err)
	}
	return nil
}

// handleBinaryHookCall answers one hooked routine. The C signatures are the
// standard ones, and each returns what the original does so a caller using the
// result keeps working.
func (runtime *initializationRuntime) handleBinaryHookCall(thread *armcore.Thread, id uint32) (uint32, error) {
	memory := runtime.client.core.Memory()
	switch binaryHookKind(id) {
	case hookMemset:
		runtime.countDiagnostic("hook memset")
		// void *memset(void *dst, int value, size_t length)
		destination, value, length, err := hookArguments(thread)
		if err != nil {
			return 0, err
		}
		if length == 0 {
			return destination, nil
		}
		fill := bytes.Repeat([]byte{byte(value)}, int(length))
		if err := memory.Write(destination, fill); err != nil {
			return 0, fmt.Errorf("KTF hooked memset(%#x, %d, %d): %w", destination, value, length, err)
		}
		return destination, nil
	case hookMemcpy:
		runtime.countDiagnostic("hook memcpy")
		// void *memcpy(void *dst, const void *src, size_t length)
		destination, source, length, err := hookArguments(thread)
		if err != nil {
			return 0, err
		}
		if length == 0 {
			return destination, nil
		}
		buffer := make([]byte, length)
		if err := memory.Read(source, buffer); err != nil {
			return 0, fmt.Errorf("KTF hooked memcpy(%#x, %#x, %d): %w", destination, source, length, err)
		}
		if err := memory.Write(destination, buffer); err != nil {
			return 0, fmt.Errorf("KTF hooked memcpy(%#x, %#x, %d): %w", destination, source, length, err)
		}
		return destination, nil
	case hookStrcpy:
		runtime.countDiagnostic("hook strcpy")
		// char *strcpy(char *dst, const char *src) — the terminator is copied
		// too, and dst comes back unchanged, which is what r0 already holds.
		destination, source, _, err := hookArguments(thread)
		if err != nil {
			return 0, err
		}
		length, err := guestStringLength(memory, source)
		if err != nil {
			return 0, fmt.Errorf("KTF hooked strcpy(%#x, %#x): %w", destination, source, err)
		}
		buffer := make([]byte, length+1)
		if err := memory.Read(source, buffer); err != nil {
			return 0, fmt.Errorf("KTF hooked strcpy(%#x, %#x): %w", destination, source, err)
		}
		if err := memory.Write(destination, buffer); err != nil {
			return 0, fmt.Errorf("KTF hooked strcpy(%#x, %#x): %w", destination, source, err)
		}
		return destination, nil
	case hookStrlen:
		runtime.countDiagnostic("hook strlen")
		// size_t strlen(const char *text)
		start, err := thread.Register(0)
		if err != nil {
			return 0, err
		}
		length, err := guestStringLength(memory, start)
		if err != nil {
			return 0, fmt.Errorf("KTF hooked strlen(%#x): %w", start, err)
		}
		return length, nil
	default:
		return 0, fmt.Errorf("unknown KTF binary hook %d", id)
	}
}

func hookArguments(thread *armcore.Thread) (uint32, uint32, uint32, error) {
	first, err := thread.Register(0)
	if err != nil {
		return 0, 0, 0, err
	}
	second, err := thread.Register(1)
	if err != nil {
		return 0, 0, 0, err
	}
	third, err := thread.Register(2)
	if err != nil {
		return 0, 0, 0, err
	}
	return first, second, third, nil
}

// hookStringChunk bounds one read while scanning for a terminator, so a string
// that is never terminated fails instead of reading the address space.
const hookStringChunk = 256

func guestStringLength(memory *armcore.Memory, start uint32) (uint32, error) {
	buffer := make([]byte, hookStringChunk)
	for length := uint32(0); length < maxGuestStringLength; {
		if err := memory.Read(start+length, buffer); err != nil {
			// A chunk that runs off the end of a mapping is read byte by byte
			// so a string ending near the boundary still resolves.
			for index := 0; index < hookStringChunk; index++ {
				single := buffer[:1]
				if err := memory.Read(start+length+uint32(index), single); err != nil {
					return 0, err
				}
				if single[0] == 0 {
					return length + uint32(index), nil
				}
			}
			return 0, err
		}
		if index := bytes.IndexByte(buffer, 0); index >= 0 {
			return length + uint32(index), nil
		}
		length += hookStringChunk
	}
	return 0, fmt.Errorf("KTF guest string at %#x is not terminated within %d bytes", start, maxGuestStringLength)
}

const maxGuestStringLength = 1 << 20
