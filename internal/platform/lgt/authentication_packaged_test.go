package lgt

import (
	"bytes"
	"testing"
)

func TestUnassignedPackagedCertificateAllowsLocalEnrollment(t *testing.T) {
	client := fixtureClient(t)
	cert := bytes.Repeat([]byte{0x71}, 76)
	client.archive.Packaged = map[string][]byte{"CERT.C2S": cert, "settings": {4}}
	client.archive.supplemental = map[string][]byte{"cert.c2s": cert}
	original := client.archive

	for _, digest := range []string{"", moduleDigest([]byte("another record"))} {
		client.omitUnassignedCertificate(digest)
		if got, ok := client.titleFile("cert.c2s"); !ok || !bytes.Equal(got, cert) {
			t.Fatal("a certificate outside the selected record disappeared")
		}
	}

	client.omitUnassignedCertificate(moduleDigest(cert))
	if _, ok := client.titleFile("cert.c2s"); ok {
		t.Fatal("an unassigned packaged record prevented enrollment")
	}
	if got, ok := client.titleFile("settings"); !ok || !bytes.Equal(got, []byte{4}) {
		t.Fatal("another packaged file disappeared")
	}
	if got, ok := original.Resource("cert.c2s"); !ok || !bytes.Equal(got, cert) {
		t.Fatal("the original archive lost the record")
	}
	key, err := fileSaveKey("cert.c2s")
	if err != nil {
		t.Fatal(err)
	}
	client.sessionFiles = map[string][]byte{key: cert}
	if got, ok := client.titleFile("cert.c2s"); !ok || !bytes.Equal(got, cert) {
		t.Fatal("a saved record with the same bytes disappeared")
	}
}
