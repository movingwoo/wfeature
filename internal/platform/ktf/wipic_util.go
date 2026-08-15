package ktf

import (
	"fmt"
	"math/bits"
	"strconv"
	"strings"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// Table 1 is the utility table: byte-order conversion and the two functions
// that turn an IP address between its text and its integer.
//
// It went unidentified for a while because a table number never appears in the
// guest's own code — the interface is an array of pointers and the game indexes
// it — so the number in `table 1 function 1 is not implemented` could not be
// searched for. Three independent readings now agree on what it is:
//
//   - Position. Every table this platform had already identified sits one past
//     the original runtime's own zero-based numbering: misc, graphics, the
//     database, UI components, media and net all line up, and the entry before
//     misc is the utility table.
//   - The specification's utility section lists exactly six functions in one
//     order — htonl, htons, ntohl, ntohs, inetAddrInt, inetAddrStr — which is
//     the order the original runtime's own table carries them in.
//   - The call site captured from the title that stops here, disassembled in
//     `docs/ktf.md`: function 1 takes a sign-extended 16-bit argument and
//     returns a 16-bit value the caller immediately stores with `strh`. That is
//     `MC_utilHtons` and nothing else in the table has that shape.
//
// The title reaches it from a content-download prompt, which is where a game
// would convert a port number: these are the calls that come before a socket.
//
// Only the six the specification names are answered. The seventh entry the
// original runtime carries is a vendor hash with no published prototype, so it
// keeps failing with its call site rather than being guessed at — the same rule
// that kept this whole table failing until it could be identified.
const (
	wipicUtilHtonl       = 0
	wipicUtilHtons       = 1
	wipicUtilNtohl       = 2
	wipicUtilNtohs       = 3
	wipicUtilInetAddrInt = 4
	wipicUtilInetAddrStr = 5
)

// maxInetAddrText bounds the address string either direction handles.
// "255.255.255.255" is fifteen bytes, and a longer run of digits is not an
// address this can convert however far it is followed.
const maxInetAddrText = 15

// handleWIPICUtilCall services the utility table.
//
// The host is little-endian ARM and network order is big-endian, so all four
// byte-order conversions are one swap; they are kept as separate cases because
// the width differs and a 16-bit swap of a 32-bit register would answer a
// caller of the wrong one with a plausible wrong number.
func (runtime *initializationRuntime) handleWIPICUtilCall(thread *armcore.Thread, function uint32) (uint32, error) {
	switch function {
	case wipicUtilHtonl, wipicUtilNtohl:
		value, err := thread.Register(0)
		if err != nil {
			return 0, err
		}
		return bits.ReverseBytes32(value), nil
	case wipicUtilHtons, wipicUtilNtohs:
		value, err := thread.Register(0)
		if err != nil {
			return 0, err
		}
		// The caller passes a sign-extended halfword and reads the answer back
		// as a halfword, so only the low sixteen bits are the value.
		return uint32(bits.ReverseBytes16(uint16(value))), nil
	case wipicUtilInetAddrInt:
		return runtime.wipicInetAddrInt(thread)
	case wipicUtilInetAddrStr:
		return runtime.wipicInetAddrStr(thread)
	}
	return 0, fmt.Errorf("KTF WIPI C utility function %d is not implemented%s",
		function, runtime.callerSite(thread))
}

// wipicInetAddrInt answers MC_utilInetAddrInt: the dotted-quad string becomes
// the integer a socket call takes, in network byte order. The specification's
// failure answer is -1, which is what an address this cannot read gets — the
// caller has a documented path for it, and inventing an address instead would
// send the game somewhere it never asked for.
func (runtime *initializationRuntime) wipicInetAddrInt(thread *armcore.Thread) (uint32, error) {
	pointer, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	text, err := runtime.readGuestCString(pointer, maxInetAddrText)
	if err != nil {
		return 0, err
	}
	address, ok := parseInetAddress(text)
	if !ok {
		runtime.countDiagnostic("wipic util inet address rejected")
		return wipiErrorCode, nil
	}
	return address, nil
}

// wipicInetAddrStr answers MC_utilInetAddrStr, the same conversion backwards.
func (runtime *initializationRuntime) wipicInetAddrStr(thread *armcore.Thread) (uint32, error) {
	address, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	pointer, err := thread.Register(1)
	if err != nil {
		return 0, err
	}
	if pointer == 0 {
		return 0, nil
	}
	text := fmt.Sprintf("%d.%d.%d.%d",
		address&0xff, (address>>8)&0xff, (address>>16)&0xff, (address>>24)&0xff)
	if err := runtime.client.core.Memory().Write(pointer, append([]byte(text), 0)); err != nil {
		return 0, fmt.Errorf("write KTF utility address string at %#x: %w", pointer, err)
	}
	return 0, nil
}

// parseInetAddress reads a dotted quad into the word a socket wants: the first
// octet in the lowest byte, because the value is in network byte order and this
// is a little-endian machine. Anything else is refused rather than partially
// read, since a half-parsed address is a different host.
func parseInetAddress(text string) (uint32, bool) {
	parts := strings.Split(text, ".")
	if len(parts) != 4 {
		return 0, false
	}
	address := uint32(0)
	for index, part := range parts {
		if part == "" || len(part) > 3 {
			return 0, false
		}
		octet, err := strconv.ParseUint(part, 10, 32)
		if err != nil || octet > 0xff {
			return 0, false
		}
		address |= uint32(octet) << (8 * index)
	}
	return address, true
}

// readGuestCString reads a NUL-terminated byte string, refusing one that runs
// past the bound rather than returning what fitted. The bytes are the guest's,
// so a missing terminator is a value this cannot use rather than a truncation
// to work around.
func (runtime *initializationRuntime) readGuestCString(pointer uint32, limit int) (string, error) {
	if pointer == 0 {
		return "", nil
	}
	memory := runtime.client.core.Memory()
	// One byte at a time: a string that ends near the top of its mapping would
	// make a single read of the whole bound fail, and the string itself is
	// perfectly readable.
	data := make([]byte, 0, limit)
	chunk := make([]byte, 1)
	for offset := 0; offset <= limit; offset++ {
		if err := memory.Read(pointer+uint32(offset), chunk); err != nil {
			return "", fmt.Errorf("read KTF guest string at %#x: %w", pointer, err)
		}
		if chunk[0] == 0 {
			return string(data), nil
		}
		data = append(data, chunk[0])
	}
	return "", nil
}
