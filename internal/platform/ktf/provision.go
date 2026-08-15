package ktf

import (
	"fmt"

	"github.com/movingwoo/wfeature/internal/wipic"
)

// Certificate provisioning.
//
// One local title checks a certificate, `cert.c2s`, which is the publisher's
// own file rather than anything WIPI describes. A server issued one per
// handset, and the copy in an archive belongs to whichever handset first
// downloaded it. The check reads, from the title's own code:
//
//	read_file("cert.c2s", &data, &length)
//	key = handset_number()                      /* the 12-byte MIN */
//	if (!decrypt(data, length - 3, key)) fail
//	if (strcmp(plaintext + 0, application_id)) fail   /* 8 bytes  */
//	if (strcmp(plaintext + 8, key))           fail   /* 12 bytes */
//
// So a certificate is only ever valid for one subscriber number, because that
// number is both the key it is sealed with and the thing sealed inside it.
//
// This writes one for the number this platform answers with. It mints an
// authorisation a server used to issue; nothing here pretends otherwise, and
// **nothing in the emulator calls it** — a person runs `wfeature provision`
// deliberately, for an archive they hold. It is kept because the format took
// work to read and may be needed again, not because anything depends on it.

// certificateName is the database entry, and certificateSaveKey is where the
// record database looks for a saved copy before it falls back to the one the
// archive packages. Writing there is what makes the title read this one.
const (
	certificateName    = "cert.c2s"
	certificateSaveKey = "db/" + certificateName
)

const (
	// certificateFields is the plaintext: an eight byte application id
	// followed by the twelve byte subscriber number.
	certificateIDLength     = 8
	certificateNumberLength = 12
	certificatePlainLength  = certificateIDLength + certificateNumberLength
	// certificateTrailer is the three bytes after the ciphertext: the sum of
	// the ciphertext bytes, the salt, and the sum of the plaintext bytes, each
	// modulo 256. The title checks both sums, which is what lets a certificate
	// this platform did not write be recognised before it is replaced.
	certificateTrailer = 3
)

// certificateTable is the 256-byte table the title's cipher mixes into its
// keystream, read out of the image at the address its own code computes.
var certificateTable = [256]byte{
	0x8c, 0x0d, 0xae, 0x4f, 0xe8, 0x81, 0xd2, 0x53, 0x10, 0x35, 0xd6, 0x77, 0x64, 0xa5, 0x96, 0x2b,
	0x34, 0x1d, 0x9e, 0x5f, 0x60, 0x81, 0xbe, 0x73, 0xe4, 0x35, 0xa2, 0x87, 0x28, 0x69, 0x2a, 0x9b,
	0x78, 0xfd, 0x4e, 0xbf, 0x30, 0x51, 0x8e, 0xe3, 0x74, 0xf5, 0x32, 0xa7, 0xf8, 0xc9, 0xb6, 0x9b,
	0x3c, 0x7d, 0x3e, 0x5f, 0x30, 0x01, 0xde, 0x23, 0x04, 0x85, 0x56, 0x57, 0x48, 0x69, 0xaa, 0x4b,
	0x5c, 0xcd, 0xaa, 0x6d, 0xb2, 0x2b, 0x08, 0xe5, 0x82, 0x57, 0xe0, 0xef, 0xee, 0x1b, 0x34, 0x9d,
	0x62, 0x7b, 0x2e, 0x85, 0x5e, 0xbf, 0x34, 0x0d, 0x7a, 0x61, 0x94, 0xb9, 0xa2, 0x9f, 0x7a, 0xcf,
	0x3a, 0x4f, 0x88, 0xa9, 0xe6, 0x8f, 0xb2, 0x79, 0xb6, 0x2f, 0x78, 0xfd, 0x76, 0xbb, 0x48, 0xcb,
	0xca, 0xff, 0x18, 0xb5, 0x80, 0xe7, 0x50, 0x05, 0x6e, 0x5b, 0x68, 0x5d, 0x06, 0xa9, 0x30, 0xc9,
	0xce, 0x93, 0x20, 0x99, 0xd8, 0x6f, 0xa0, 0x91, 0xe6, 0x3b, 0x94, 0xe3, 0x46, 0xf7, 0xdc, 0x2d,
	0x94, 0xfb, 0xac, 0xa1, 0xe6, 0x43, 0x08, 0x23, 0x68, 0xb3, 0x88, 0xc1, 0xde, 0x2b, 0xd0, 0xeb,
	0xd2, 0xab, 0x0c, 0x7d, 0x32, 0x03, 0x3c, 0x8d, 0xfe, 0xeb, 0xf6, 0x65, 0x16, 0xd7, 0x88, 0xed,
	0xea, 0xbd, 0x2c, 0xe1, 0xda, 0xb7, 0xd0, 0xf1, 0x16, 0xab, 0xe4, 0x15, 0x8a, 0x57, 0x16, 0x17,
	0x4a, 0x6b, 0x6c, 0x9d, 0x7e, 0x1f, 0xae, 0xa1, 0x72, 0x37, 0x20, 0xef, 0xee, 0x9b, 0xf4, 0x11,
	0xec, 0x03, 0xec, 0x81, 0x66, 0x03, 0x30, 0x15, 0x6a, 0x65, 0xea, 0x35, 0xc6, 0x07, 0x38, 0x8d,
	0x3e, 0x75, 0x26, 0x6d, 0x9e, 0x53, 0x44, 0x79, 0x12, 0xf1, 0xa4, 0x45, 0xb6, 0x47, 0xf8, 0x29,
	0xae, 0xab, 0x7c, 0x2d, 0x72, 0x2f, 0x12, 0x51, 0x86, 0x9d, 0x3a, 0x03, 0x64, 0xe9, 0xe2, 0xdb,
}

// certificateKeystream is the byte the cipher exclusive-ors with position i.
// The title walks its data with a counter that starts at the salt, indexes the
// table with the low eight bits of that counter and the key with the counter
// modulo the key's length, and adds the two.
func certificateKeystream(index int, salt byte, key []byte) byte {
	counter := index + int(salt)
	return certificateTable[counter&0xff] + key[counter%len(key)]
}

// DecodeCertificate answers the plaintext of a certificate sealed with key, or
// reports which of the title's two checks it failed. It is here because it is
// what recognises a certificate before one is written over it: a file whose
// sums do not agree is not this scheme, and this platform should not replace
// what it does not understand.
func DecodeCertificate(blob, key []byte) ([]byte, error) {
	if len(blob) <= certificateTrailer || len(key) == 0 {
		return nil, fmt.Errorf("KTF certificate is %d bytes, too short to carry its trailer", len(blob))
	}
	body, trailer := blob[:len(blob)-certificateTrailer], blob[len(blob)-certificateTrailer:]
	if sum := checksum(body); sum != trailer[0] {
		return nil, fmt.Errorf("KTF certificate ciphertext sums to %#02x, its trailer says %#02x", sum, trailer[0])
	}
	plain := make([]byte, len(body))
	for index := range body {
		plain[index] = body[index] ^ certificateKeystream(index, trailer[1], key)
	}
	if sum := checksum(plain); sum != trailer[2] {
		return nil, fmt.Errorf("KTF certificate plaintext sums to %#02x, its trailer says %#02x", sum, trailer[2])
	}
	return plain, nil
}

// EncodeCertificate seals a plaintext the way the title's own writer does.
// The salt is a parameter rather than a random draw so that provisioning an
// archive twice produces the same bytes and can be compared.
func EncodeCertificate(plain, key []byte, salt byte) []byte {
	blob := make([]byte, len(plain)+certificateTrailer)
	for index := range plain {
		blob[index] = plain[index] ^ certificateKeystream(index, salt, key)
	}
	blob[len(plain)] = checksum(blob[:len(plain)])
	blob[len(plain)+1] = salt
	blob[len(plain)+2] = checksum(plain)
	return blob
}

func checksum(data []byte) byte {
	total := 0
	for _, value := range data {
		total += int(value)
	}
	return byte(total)
}

// HandsetNumber is the subscriber number this platform answers `PHONENUMBER`
// and `MIN` with, which is the number a certificate has to be sealed with for
// this platform to be the handset it was issued to.
func HandsetNumber() string { return wipic.SystemProperties["PHONENUMBER"] }

// CertificateProvision is one archive's certificate and where it belongs.
type CertificateProvision struct {
	// SaveKey is the record database entry to write, relative to the
	// archive's own save directory.
	SaveKey string
	// Data is the certificate.
	Data []byte
	// Number is the subscriber number it was sealed with.
	Number string
	// Replaced is the certificate the archive packages, which had to be
	// recognised before this one was built.
	Replaced []byte
}

// ProvisionCertificate builds the certificate an archive needs, for the
// handset number this platform answers with.
//
// It refuses an archive it does not recognise. Recognition is not a name
// match: the archive has to package a certificate whose own trailer agrees
// with the format read out of the title, which is the only evidence available
// that this scheme is the one it uses.
func ProvisionCertificate(archive *Archive, number string) (*CertificateProvision, error) {
	if archive == nil {
		return nil, fmt.Errorf("KTF archive is nil")
	}
	if number == "" {
		number = HandsetNumber()
	}
	if len(number) >= certificateNumberLength {
		// The title reads the number into a twelve byte buffer and compares it
		// as a string, so it has to fit with room for the terminator.
		return nil, fmt.Errorf("handset number %q needs fewer than %d digits", number, certificateNumberLength)
	}
	packaged, ok := archive.GuestFiles()[certificateName]
	if !ok {
		return nil, fmt.Errorf("this archive packages no %s, so it does not use a certificate", certificateName)
	}
	// The packaged certificate belongs to another handset and cannot be
	// decoded, but its ciphertext sum is checkable without the key and is what
	// says the format is understood.
	if len(packaged) != certificatePlainLength+certificateTrailer {
		return nil, fmt.Errorf("packaged %s is %d bytes, not the %d this format has",
			certificateName, len(packaged), certificatePlainLength+certificateTrailer)
	}
	body := packaged[:len(packaged)-certificateTrailer]
	if sum := checksum(body); sum != packaged[len(body)] {
		return nil, fmt.Errorf("packaged %s does not carry this format's checksum", certificateName)
	}

	identity := archive.Descriptor.AID
	if len(identity) != certificateIDLength {
		return nil, fmt.Errorf("application id %q is not %d characters", identity, certificateIDLength)
	}
	plain := make([]byte, certificatePlainLength)
	copy(plain, identity)
	copy(plain[certificateIDLength:], number)
	// The salt the archive's own certificate carries is reused, so a
	// provisioned file differs from the packaged one only where the key and
	// the subscriber number differ.
	salt := packaged[len(packaged)-2]
	return &CertificateProvision{
		SaveKey:  certificateSaveKey,
		Data:     EncodeCertificate(plain, []byte(number), salt),
		Number:   number,
		Replaced: packaged,
	}, nil
}

// CertificateRemovalKey is the record database's own list of deleted names. A
// title that rejected a certificate deletes it, and the deletion outlives the
// run, so provisioning has to clear the name from that list or the entry it
// just wrote stays hidden.
const CertificateRemovalKey = databaseRemovedKey
