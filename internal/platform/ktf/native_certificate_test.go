package ktf

import (
	"bytes"
	"testing"
)

func TestNativeCertificateUnknownPackage(t *testing.T) {
	record := bytes.Repeat([]byte{0x62}, 74)
	platform := newTestNativePlatform(t, map[string][]byte{"gbxcerti.dat": record})
	check := platform.client.served[nativeSlotKey{surface: nativeInterfaceSurface(nativeInterfaceCertificate), slot: nativeCertificateCheck / 4}]
	address := nativeString(t, platform, string(record))
	if got := nativeCall(t, check, 0, address, uint32(len(record))); got != 1 {
		t.Errorf("unrecognized package received certificate status %d, want 1", got)
	}
}

func TestNativeCertificatePresentedRecord(t *testing.T) {
	record := bytes.Repeat([]byte{0x42}, nativeCertificateSize)
	platform := newTestNativePlatform(t, nil)
	check := platform.checkCertificate(record)
	address := nativeString(t, platform, string(record))
	if got := nativeCall(t, check, 0, address, nativeCertificateSize); got != nativeCertificateAccepted {
		t.Fatalf("identical record received status %d", got)
	}
	for _, test := range []struct {
		name          string
		address, size uint32
	}{
		{"empty", address, 0},
		{"short", address, nativeCertificateSize - 1},
		{"long", address, nativeCertificateSize + 1},
		{"oversized", address, 0xffffffff},
		{"null", 0, nativeCertificateSize},
		{"unmapped", 0xdeadbeef, nativeCertificateSize},
		{"wraparound", 0xfffffff0, nativeCertificateSize},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := nativeCall(t, check, 0, test.address, test.size); got != nativeCertificateRequired {
				t.Errorf("status %d, want authentication required", got)
			}
		})
	}
	if err := platform.client.core.Memory().Write(address+nativeCertificateSize-1, []byte{0}); err != nil {
		t.Fatal(err)
	}
	if got := nativeCall(t, check, 0, address, nativeCertificateSize); got != nativeCertificateRequired {
		t.Errorf("changed final byte received status %d", got)
	}
}
