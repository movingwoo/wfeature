package skt

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"testing"
)

func TestNativePairedCertificateSelectsTheSubscriber(t *testing.T) {
	// The first record alone accepts multiple ten-digit strings. Its pair
	// uses the same table in a different order and checks the decoded payload.
	const subscriber = "0101234567"
	var table [256]byte
	for i := range table {
		table[i] = byte(i*37 + 11)
	}
	makeUSIM := func(number string, seed byte) []byte {
		data := make([]byte, 20)
		data[19] = seed
		for i := range data[:18] {
			index := seed + byte(i)
			var plain byte
			if i < len(number) {
				plain = number[i]
			}
			data[i] = plain ^ (table[index] + number[int(index)%len(number)])
			data[18] += data[i]
		}
		return data
	}
	usim := makeUSIM(subscriber, 240)
	cert := make([]byte, 31)
	cert[26] = 240
	plain := []byte("test fields with checksum!")
	// Known signed-shift checksum for the fixture payload.
	binary.LittleEndian.PutUint32(cert[27:], 0xe2f5ea02)
	order := [...]int{12, 14, 4, 2, 11, 9, 7, 0, 6, 1, 8, 5, 3, 13, 10, 15}
	for i, value := range plain {
		index := int(cert[26] + byte(i))
		cert[i] = value ^ (table[16*order[index/16]+index%16] + subscriber[4+index%6])
	}
	data := append(make([]byte, 0x20), table[:]...)
	var executable NativeExecutable
	executable.Data = data
	digest, err := hex.DecodeString("43352886f4e8575cd4d7cc6ee5128e80594cd1d73d6eadfe9a0c2b14fc64ba51")
	if err != nil {
		t.Fatal(err)
	}
	copy(executable.Digest[:], digest)
	archive := NativeArchive{Executable: &executable, Files: map[string][]byte{"usimcert.c2s": usim, "cert.c2s": cert}}

	if got := archive.PairedCertificateSubscriber(); got != subscriber {
		t.Fatal("the paired records did not select the expected subscriber")
	}
	if !bytes.Equal(usim, makeUSIM(subscriber, 240)) {
		t.Fatal("the resolver changed the packaged USIM certificate")
	}
	cert[27] ^= 1
	if got := archive.PairedCertificateSubscriber(); got != "" {
		t.Fatal("the resolver accepted a bad second checksum")
	}
	cert[27] ^= 1
	usim[18] ^= 1
	if got := archive.PairedCertificateSubscriber(); got != "" {
		t.Fatal("the resolver accepted a bad USIM checksum")
	}
	usim[18] ^= 1
	archive.Files["cert.c2s"] = cert[:30]
	if got := archive.PairedCertificateSubscriber(); got != "" {
		t.Fatal("the resolver used a short certificate")
	}
	archive.Files["cert.c2s"] = cert
	executable.Digest[0] ^= 1
	if got := archive.PairedCertificateSubscriber(); got != "" {
		t.Fatal("the resolver accepted an unrecognized executable")
	}
}
