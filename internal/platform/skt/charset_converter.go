package skt

import (
	"unicode/utf16"

	"github.com/movingwoo/wfeature/internal/api/skvm"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// com.xce.io.ByteToCharEUC_KR, the vendor's decoder as a class rather than as
// String's charset argument. It is a platform native rather than a body in the
// class library because the table it decodes with is the handset's default
// charset, which the Host installs — the same one String's byte constructors
// and getBytes go through. See "The default charset is the handset's" in
// docs/skvm.md.
//
// The API is the java.io converter of the day: convert fills as much of the
// caller's char array as it can and answers how many characters it wrote.
// The counts are lengths rather than end indexes — a local title computes
// `end - start` for the input and `chars.length - offset` for the output at
// the call site, which is what says which of the two readings this vendor
// took.

// eucKRLeadByte reports whether a byte begins a two-byte character. A range
// that ends on one is a character split across two calls, and the byte waits
// on the converter until the next one.
func eucKRLeadByte(value byte) bool { return value >= 0x81 && value <= 0xfe }

// danglingLeadByte answers the index of a lead byte the chunk ends on without
// its trail, or -1 when the chunk ends on a whole character.
//
// It has to be a forward scan. The same byte value is a lead byte in one
// position and a trail byte in the next, so whether the last byte begins a
// character is only decided by where the characters before it start. The
// parity of the chunk stands in for that state only while the text is entirely
// double-byte, and a title that writes "name:" before a Korean word breaks it
// in both directions: an even chunk ending on a real lead byte decodes that
// byte alone and corrupts its trail at the head of the next chunk, and an odd
// chunk ending on a trail byte in the lead range has that trail torn off a
// character that was whole.
func danglingLeadByte(bytes []byte) int {
	for index := 0; index < len(bytes); {
		if !eucKRLeadByte(bytes[index]) {
			index++
			continue
		}
		if index == len(bytes)-1 {
			return index
		}
		index += 2
	}
	return -1
}

func (runtime *Runtime) byteToCharConvert(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	converter, err := referenceArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	source, err := referenceArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	input, err := jvm.ByteArraySnapshot(source)
	if err != nil {
		return jvm.VoidValue(), err
	}
	inputOffset, err := intArgument(arguments, 2)
	if err != nil {
		return jvm.VoidValue(), err
	}
	inputLength, err := intArgument(arguments, 3)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if inputOffset < 0 || inputLength < 0 || int64(inputOffset)+int64(inputLength) > int64(len(input)) {
		return jvm.VoidValue(), newGuestException("java/lang/ArrayIndexOutOfBoundsException", "converter input range")
	}

	pending, err := converterPending(vm, converter)
	if err != nil {
		return jvm.VoidValue(), err
	}
	bytes := input[inputOffset : inputOffset+inputLength]
	if pending != 0 {
		bytes = append([]byte{byte(pending)}, bytes...)
	}
	// A trailing lead byte is half a character. Decoding it now would answer
	// with a replacement character the caller would then have in its text
	// forever, so it waits for the byte that completes it.
	held := int32(0)
	if index := danglingLeadByte(bytes); index >= 0 {
		held = int32(bytes[index])
		bytes = bytes[:index]
	}
	if err := setConverterPending(vm, converter, held); err != nil {
		return jvm.VoidValue(), err
	}
	return writeConvertedChars(vm, arguments, utf16.Encode([]rune(decodePlatformBytes(bytes))))
}

// byteToCharFlush empties what a split character left behind. There is nothing
// to hand back — a lone lead byte is not a character — so what flushing does
// is drop it, which is what keeps the next conversion from starting inside the
// character before it.
func (runtime *Runtime) byteToCharFlush(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	converter, err := referenceArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if err := setConverterPending(vm, converter, 0); err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.IntValue(0), nil
}

// writeConvertedChars copies the decoded characters into the caller's array.
// The output arguments start at index 4 for convert and at index 1 for flush,
// and only convert produces characters, so this serves the one that does.
func writeConvertedChars(_ *jvm.VM, arguments []jvm.Value, units []uint16) (jvm.Value, error) {
	target, err := referenceArgument(arguments, 4)
	if err != nil {
		return jvm.VoidValue(), err
	}
	outputOffset, err := intArgument(arguments, 5)
	if err != nil {
		return jvm.VoidValue(), err
	}
	outputLength, err := intArgument(arguments, 6)
	if err != nil {
		return jvm.VoidValue(), err
	}
	_, existing, err := jvm.ArraySnapshot(target)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if outputOffset < 0 || outputLength < 0 || int64(outputOffset)+int64(outputLength) > int64(len(existing)) {
		return jvm.VoidValue(), newGuestException("java/lang/ArrayIndexOutOfBoundsException", "converter output range")
	}
	if len(units) > int(outputLength) {
		units = units[:outputLength]
	}
	values := make([]jvm.Value, len(units))
	for index, unit := range units {
		values[index] = jvm.IntValue(int32(unit))
	}
	if err := jvm.SetArrayRange(target, int(outputOffset), values); err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.IntValue(int32(len(values))), nil
}

func converterPending(vm *jvm.VM, converter *jvm.Object) (int32, error) {
	value, err := vm.Field(converter, skvm.ByteToCharEUCKRClass, "pending", "I")
	if err != nil {
		return 0, err
	}
	return value.Int32()
}

func setConverterPending(vm *jvm.VM, converter *jvm.Object, pending int32) error {
	return vm.SetField(converter, skvm.ByteToCharEUCKRClass, "pending", "I", jvm.IntValue(pending))
}
