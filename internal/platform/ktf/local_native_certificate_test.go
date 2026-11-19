package ktf

import (
	"bytes"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
	"github.com/movingwoo/wfeature/internal/testlocal"
)

func TestLocalNativeCertificate(t *testing.T) {
	paths := filepath.SplitList(testlocal.Value(t, "WFEATURE_KTF_BREW_CERTIFICATE_ARCHIVES", "the two local BREW certificate archive paths separated by the OS path-list separator"))
	if len(paths) != 2 {
		t.Fatal("expected a pair of archive paths")
	}
	type sample struct {
		id           uint32
		original     []byte
		files        map[string][]byte
		module, file string
	}
	var samples [2]sample
	for i, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		a, err := OpenNative(data)
		if err != nil {
			t.Fatal(err)
		}
		files, err := readOuterZIP(data)
		if err != nil {
			t.Fatal(err)
		}
		p := sample{id: a.Info.ApplicationID, original: data, files: files, module: a.ModuleName}
		for name := range a.Files {
			if nativeFileKey(name) == nativeCertificatePackages[p.id].file {
				p.file = name
			}
		}
		if len(files[p.file]) != nativeCertificateSize {
			t.Fatalf("application %d lacks its packaged certificate", p.id)
		}
		samples[i] = p
	}
	if samples[0].id == samples[1].id {
		t.Fatal("expected different applications")
	}
	for i, p := range samples {
		t.Run(strconv.FormatUint(uint64(p.id), 10), func(t *testing.T) {
			originalScreen := localNativeCertificateScreen(t, p.original, false, backend.AuthenticationKTFBREWPackagedCertificate)
			renewalScreen := localNativeCertificateScreen(t, p.original, true, backend.AuthenticationOff)
			if renewalScreen == originalScreen {
				t.Fatal("the original and diagnostic launches reached the same screen")
			}
			record := bytes.Clone(p.files[p.file])
			record[len(record)-1] ^= 1
			other := samples[1-i]
			for _, tc := range []struct {
				name, entry string
				change      []byte
				status      backend.AuthenticationStatus
			}{
				{"changed record", p.file, record, backend.AuthenticationKTFBREWPackagedCertificate},
				{"other application record", p.file, other.files[other.file], backend.AuthenticationKTFBREWPackagedCertificate},
				{"unregistered module", p.module, append(bytes.Clone(p.files[p.module]), 0, 0, 0, 0), backend.AuthenticationUnsupported},
			} {
				t.Run(tc.name, func(t *testing.T) {
					files := maps.Clone(p.files)
					files[tc.entry] = tc.change
					if got := localNativeCertificateScreen(t, buildTestZIP(t, files), false, tc.status); got != renewalScreen {
						t.Fatalf("reached screen %#x, want the guest's renewal screen %#x", got, renewalScreen)
					}
				})
			}
		})
	}
}

func localNativeCertificateScreen(t *testing.T, data []byte, disabled bool, want backend.AuthenticationStatus) uint64 {
	t.Helper()
	clock := wipicrt.NewManualClock(time.Time{})
	options := NativeSessionOptions{Clock: clock, SaveStore: newNativeSaveFixtureStore(t), DisableAuthentication: disabled}
	session, err := StartNativeSession(t.Context(), data, options)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if got := session.Authentication(); got != want {
		t.Fatalf("authentication %q, want %q", got, want)
	}
	surface := nativeInterfaceSurface(nativeInterfaceCertificate)
	calls := 0
	for _, slot := range session.Client.SlotSummary() {
		if slot.Surface == surface && slot.Offset == nativeCertificateCheck && slot.Served {
			calls += slot.Count
		}
	}
	if calls != 1 {
		t.Fatalf("expected the guest to call the certificate verifier once at boot, got %d", calls)
	}
	for range 30 {
		clock.Advance(session.Platform().FrameInterval())
		if _, err := session.Tick(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if session.Flushes() == 0 {
		t.Fatal("guest did not present a screen")
	}
	if want != backend.AuthenticationUnsupported {
		saved, err := session.CaptureCheckpoint(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		prepared, err := PrepareNativeSessionCheckpoint(data, saved, options)
		if err != nil {
			t.Fatal(err)
		}
		if got := prepared.session.Authentication(); got != want {
			t.Errorf("restored authentication %q, want %q", got, want)
		}
		prepared.Discard()
		options.DisableAuthentication = !disabled
		if _, err := PrepareNativeSessionCheckpoint(data, saved, options); err == nil {
			t.Fatalf("registered module restored across different verifier settings")
		}
	}
	return session.FrameDigest()
}
