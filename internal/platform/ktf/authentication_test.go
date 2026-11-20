package ktf

import (
	"bytes"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt/wipicrttest"
)

func certificateArchive() *Archive {
	plain := append([]byte("ABCDEF12"), make([]byte, 12)...)
	copy(plain[8:], "01012345678")
	image := append([]byte("cert.c2s\x00PHONENUMBER\x00"), certificateTable[:]...)
	return &Archive{
		Descriptor: Descriptor{AID: "ABCDEF12"},
		Files:      map[string][]byte{"p/cert.c2s": EncodeCertificate(plain, []byte("01012345678"), 17)},
		JAR:        &JAR{Client: ClientImage{Data: image}},
	}
}

func TestAuthenticationIdentityAgreesAcrossGuestAPIsAndSessions(t *testing.T) {
	for _, number := range []string{"01000000000", "01098765432"} {
		t.Run(number, func(t *testing.T) {
			t.Parallel()
			client, runtime := newTestRuntime(t)
			client.SubscriberNumber = number
			check := func(value jvm.Value, err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
				object, err := value.Reference()
				text, _ := jvm.StringText(object)
				if err != nil || text != number {
					t.Fatalf("identity = %q, %v", text, err)
				}
			}
			for _, property := range []string{"PHONENUMBER", "MIN"} {
				arguments := []jvm.Value{jvm.ReferenceValue(client.JVM().NewString(property))}
				check(runtimeGetSystemProperty(runtime, client.JVM(), arguments))
				check(runtimeSystemGetProperty(runtime, client.JVM(), arguments))
				name, err := runtime.AllocateBytes(append([]byte(property), 0))
				if err != nil {
					t.Fatal(err)
				}
				out, err := runtime.Allocate(12)
				if err != nil {
					t.Fatal(err)
				}
				thread := armcore.NewThread(armcore.Context{})
				for index, value := range []uint32{name, out, 12} {
					if err := thread.SetRegister(index, value); err != nil {
						t.Fatal(err)
					}
				}
				if result, err := runtime.WIPICGetSystemProperty(thread); err != nil || result != 0 {
					t.Fatalf("C property: %d, %v", result, err)
				}
				text, err := runtime.ReadCString(out, 12)
				if err != nil || text != number {
					t.Fatalf("C identity = %q, %v", text, err)
				}
			}
			check(runtimeDMInfoHandsetNumber(runtime, client.JVM(), nil))
		})
	}
}

func TestAuthenticationRecognizesCertificateAndExecutableTogether(t *testing.T) {
	for _, number := range []string{"01000000000", "01098765432"} {
		t.Run(number, func(t *testing.T) {
			t.Parallel()
			blob, ok := authenticationCertificate(certificateArchive(), number)
			if !ok {
				t.Fatal("recognized format was refused")
			}
			plain, err := DecodeCertificate(blob, []byte(number))
			if err != nil || string(plain[:8]) != "ABCDEF12" || string(bytes.TrimRight(plain[8:], "\x00")) != number {
				t.Fatalf("certificate does not match session identity: %x, %v", plain, err)
			}
		})
	}
	for _, change := range []func(*Archive){
		func(a *Archive) { a.JAR = nil },
		func(a *Archive) { a.JAR.Client.Data = []byte("cert.c2s\x00PHONENUMBER\x00") },
		func(a *Archive) { a.JAR.Client.Data[30] ^= 1 },
		func(a *Archive) { a.Files["p/cert.c2s"][0] ^= 1 },
		func(a *Archive) { a.Files["p/cert.c2s"] = make([]byte, 52) },
		func(a *Archive) { a.Descriptor.AID = "short" },
	} {
		a := certificateArchive()
		change(a)
		if _, ok := authenticationCertificate(a, "01000000000"); ok {
			t.Fatal("near match was adapted")
		}
	}
	for _, number := range []string{"", "012345678901", "010abc", "010\x00"} {
		if _, ok := authenticationCertificate(certificateArchive(), number); ok {
			t.Fatalf("invalid number %q accepted", number)
		}
	}
}

func TestAuthenticationCertificatePreservesSavedCertificateAndDeletion(t *testing.T) {
	for _, deleted := range []bool{false, true} {
		base := wipicrt.NewDirectorySaveStore(t.TempDir())
		original := []byte("original certificate")
		if err := base.StoreSave(certificateSaveKey, original); err != nil {
			t.Fatal(err)
		}
		removed := []string{"old"}
		if deleted {
			removed = append(removed, certificateName)
		}
		if err := base.StoreSave(wipicrt.DatabaseRemovedKey, wipicrt.JoinRemovalList(removed)); err != nil {
			t.Fatal(err)
		}
		store := newCertificateSaveStore(base, []byte("session certificate"))
		blob, _ := store.LoadSave(certificateSaveKey)
		if string(blob) != "session certificate" {
			t.Fatalf("read %q", blob)
		}
		blob[0] = 0
		if data, _ := store.LoadSave(certificateSaveKey); string(data) != "session certificate" {
			t.Fatal("load aliases session data")
		}
		ledger, _ := store.LoadSave(wipicrt.DatabaseRemovedKey)
		if bytes.Contains(ledger, []byte(certificateName)) {
			t.Fatal("saved deletion hides session certificate")
		}
		if err := store.StoreSave("db/./cert.c2s", nil); err != nil {
			t.Fatal(err)
		}
		if data, _ := base.LoadSave(certificateSaveKey); !bytes.Equal(data, original) {
			t.Fatal("guest write changed original certificate")
		}
		if err := store.StoreSave(wipicrt.DatabaseRemovedKey, wipicrt.JoinRemovalList([]string{certificateName, "new"})); err != nil {
			t.Fatal(err)
		}
		persisted, _ := base.LoadSave(wipicrt.DatabaseRemovedKey)
		if bytes.Contains(persisted, []byte(certificateName)) != deleted || !bytes.Contains(persisted, []byte("new")) || bytes.Contains(persisted, []byte("old")) {
			t.Fatalf("unrelated deletions or original certificate deletion lost: %q", persisted)
		}
		if err := store.StoreSave("db/progress", []byte("saved")); err != nil {
			t.Fatal(err)
		}
		if data, _ := base.LoadSave("db/progress"); string(data) != "saved" {
			t.Fatal("progress did not persist")
		}
		second := newCertificateSaveStore(base, []byte("second handset"))
		if data, _ := second.LoadSave(certificateSaveKey); string(data) != "second handset" {
			t.Fatal("certificate leaked across sessions")
		}
	}
	store := newCertificateSaveStore(nil, []byte("volatile"))
	if err := store.StoreSave("db/progress", []byte("saved")); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.LoadSave("db/progress"); ok {
		t.Fatal("nil backing store gained persistence")
	}
}

func TestCertificateSelectionLeavesOriginalRecordsForTheGuest(t *testing.T) {
	for _, scheme := range []struct {
		size   int
		status backend.AuthenticationStatus
	}{{23, backend.AuthenticationKTFCertificate23}, {52, backend.AuthenticationKTFCertificate52}} {
		t.Run(string(scheme.status), func(t *testing.T) {
			sessionRecord := bytes.Repeat([]byte{1}, scheme.size)
			for _, tc := range []struct {
				name            string
				saved           []byte
				packagedMatches bool
				deleted         bool
				guestManaged    bool
			}{
				{name: "missing"},
				{name: "matching package", packagedMatches: true, guestManaged: true},
				{name: "saved", saved: sessionRecord, guestManaged: true},
				{name: "malformed save", saved: []byte{2}, packagedMatches: true, guestManaged: true},
				{name: "empty overrides package", saved: []byte{}, packagedMatches: true},
				{name: "deleted overrides package and save", saved: sessionRecord, packagedMatches: true, deleted: true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					client, _ := newTestRuntime(t)
					packaged := make([]byte, scheme.size)
					if tc.packagedMatches {
						copy(packaged, sessionRecord)
					}
					client.AttachFilesystem(map[string][]byte{certificateName: packaged})
					entries := map[string][]byte{}
					if tc.saved != nil {
						entries[certificateSaveKey] = tc.saved
					}
					if tc.deleted {
						entries[wipicrt.DatabaseRemovedKey] = wipicrt.JoinRemovalList([]string{certificateName})
					}
					base := testSaveStore(t, entries)
					client.AttachSaveStore(base)
					if err := client.selectCertificate(sessionRecord, scheme.status); err != nil {
						t.Fatal(err)
					}
					want := scheme.status
					if tc.guestManaged {
						want = backend.AuthenticationGuestManaged
					}
					if client.authentication != want || (client.SaveStore == base) != tc.guestManaged {
						t.Fatalf("selected %q with original store %v", client.authentication, client.SaveStore == base)
					}
					if err := client.SaveStore.StoreSave(certificateSaveKey, []byte("guest write")); err != nil {
						t.Fatal(err)
					}
					saved, _ := base.LoadSave(certificateSaveKey)
					if bytes.Equal(saved, []byte("guest write")) != tc.guestManaged {
						t.Fatalf("wrong certificate write destination")
					}
				})
			}
		})
	}
}

func TestCertificateSelectionPropagatesSaveErrors(t *testing.T) {
	for _, key := range []string{certificateSaveKey, wipicrt.DatabaseRemovedKey} {
		t.Run(key, func(t *testing.T) {
			client, _ := newTestRuntime(t)
			store := &wipicrttest.UnreadableSaves{
				DirectorySaveStore: wipicrt.NewDirectorySaveStore(t.TempDir()),
				Keys:               map[string]bool{key: true},
			}
			client.AttachSaveStore(store)
			if err := client.selectCertificate(make([]byte, 23), backend.AuthenticationKTFCertificate23); err == nil {
				t.Fatal("an unreadable save was treated as a fresh installation")
			}
			if client.SaveStore != store {
				t.Fatal("a failed read installed a certificate adapter")
			}
		})
	}
}
