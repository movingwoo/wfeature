package skt

import (
	"encoding/binary"
	"encoding/hex"

	"github.com/movingwoo/wfeature/internal/platform/compatibility"
)

const (
	pairedCertificateSubscriberSize = 10
	pairedCertificateTableStart     = 0x20
	pairedCertificateTableEnd       = pairedCertificateTableStart + 256
)

// PairedCertificateSubscriber returns the handset identity confirmed by both
// packaged records of the recognized native certificate format. The caller
// supplies it as a session property and lets the guest verify its own files.
func (archive *NativeArchive) PairedCertificateSubscriber() string {
	if archive == nil || archive.Executable == nil || len(archive.Executable.Data) < pairedCertificateTableEnd {
		return ""
	}
	digest := hex.EncodeToString(archive.Executable.Digest[:])
	if !compatibility.HasFix("skt", compatibility.NativeModule, digest, compatibility.SKTNativePairedCertificate) {
		return ""
	}
	usim, cert := archive.Files["usimcert.c2s"], archive.Files["cert.c2s"]
	if len(usim) != 20 || len(cert) != 31 {
		return ""
	}
	table := archive.Executable.Data[pairedCertificateTableStart:pairedCertificateTableEnd]
	return pairedCertificateSubscriber(usim, cert, table)
}

func pairedCertificateSubscriber(usim, cert, table []byte) string {
	var checksum byte
	for _, value := range usim[:18] {
		checksum += value
	}
	if checksum != usim[18] {
		return ""
	}

	// The 18-byte field is a ten-digit string padded with NULs. The NULs
	// directly determine the table input for their indices. Even when the
	// byte index wraps at 256 they fix at least six of the ten digits.
	var number [pairedCertificateSubscriberSize]byte
	seed := usim[19]
	for i := len(number); i < 18; i++ {
		index := seed + byte(i)
		digit := usim[i] - table[index]
		slot := int(index) % len(number)
		if digit < '0' || digit > '9' || number[slot] != 0 && number[slot] != digit {
			return ""
		}
		number[slot] = digit
	}

	var selected string
	matches := 0
	var complete func(int)
	complete = func(slot int) {
		if matches > 1 {
			return
		}
		if slot == len(number) {
			for i, digit := range number {
				index := seed + byte(i)
				if usim[i]^(table[index]+number[int(index)%len(number)]) != digit {
					return
				}
			}
			if pairedCertificateChecksumMatches(cert, table, number) {
				matches++
				selected = string(number[:])
			}
			return
		}
		if number[slot] != 0 {
			complete(slot + 1)
			return
		}
		for digit := byte('0'); digit <= '9'; digit++ {
			number[slot] = digit
			complete(slot + 1)
		}
		number[slot] = 0
	}
	complete(0)
	if matches == 1 {
		return selected
	}
	return ""
}

func pairedCertificateChecksumMatches(cert, table []byte, number [pairedCertificateSubscriberSize]byte) bool {
	// The second reader assembles the same 16-byte blocks in a different
	// order and uses the last six digits. Its checksum uses arithmetic
	// shifts on signed 32-bit words.
	order := [...]int{12, 14, 4, 2, 11, 9, 7, 0, 6, 1, 8, 5, 3, 13, 10, 15}
	const polynomial int32 = -306674912 // 0xedb88320 as a signed word
	var checksum int32
	for i, value := range cert[:26] {
		index := int(cert[26] + byte(i))
		digit := number[len(number)-6+index%6]
		decoded := value ^ (table[16*order[index/16]+index%16] + digit)
		entry := (checksum ^ int32(decoded)) & 255
		for range 8 {
			if entry&1 != 0 {
				entry = entry>>1 ^ polynomial
			} else {
				entry >>= 1
			}
		}
		checksum = checksum>>8 ^ entry
	}
	return uint32(checksum) == binary.LittleEndian.Uint32(cert[27:])
}
