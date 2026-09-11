package skt

import (
	"bytes"
	_ "embed"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
	"github.com/movingwoo/wfeature/internal/jvm/classfile"
)

//go:embed testdata/license.jar
var licenseJAR []byte

func startLicenseFixture(t *testing.T, archive *Archive, enabled bool, store backend.SaveStore) *Runtime {
	t.Helper()
	frame, err := backend.NewMemoryFramebuffer(120, 160)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := Start(archive, Options{DisableAuthentication: !enabled, Framebuffer: frame, SaveStore: store})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Destroy(true) })
	return runtime
}

func licenseFixture(t *testing.T) *Archive {
	t.Helper()
	archive, err := Open(licenseJAR)
	if err != nil {
		t.Fatal(err)
	}
	return archive
}

func licenseInt(t *testing.T, runtime *Runtime, name, descriptor string) int32 {
	t.Helper()
	value, err := runtime.VM.InvokeStatic("LicenseMIDlet", name, descriptor)
	if err != nil {
		t.Fatal(err)
	}
	result, err := value.Int32()
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestAuthenticationLicenseJARKeepsDigestSideEffectsAndSaves(t *testing.T) {
	archive := licenseFixture(t)
	original := bytes.Clone(archive.Entries["LicenseCheck.class"])
	store := backend.NewDirectorySaveStore(t.TempDir())
	off := startLicenseFixture(t, archive, false, store)
	on := startLicenseFixture(t, archive, true, store)
	if off.Authentication() != backend.AuthenticationOff || on.Authentication() != backend.AuthenticationSKTLicense {
		t.Fatal("incorrect authentication result")
	}
	if licenseInt(t, off, "accepted", "()Z") != 0 || licenseInt(t, on, "accepted", "()Z") != 1 {
		t.Fatal("opposing sessions did not retain their policies")
	}
	for _, runtime := range []*Runtime{off, on} {
		if licenseInt(t, runtime, "digestCalls", "()I") != 1 || licenseInt(t, runtime, "ordinaryEquals", "()Z") != 0 {
			t.Fatal("digest initialization or ordinary equality changed")
		}
	}
	if _, err := on.VM.InvokeStatic("LicenseMIDlet", "advanceAndSave", "()V"); err != nil {
		t.Fatal(err)
	}
	if err := on.Destroy(true); err != nil {
		t.Fatal(err)
	}
	restored := startLicenseFixture(t, archive, true, store)
	if licenseInt(t, restored, "progress", "()I") != 1 {
		t.Fatal("fresh runtime did not restore ordinary progress")
	}
	if !bytes.Equal(archive.Entries["LicenseCheck.class"], original) {
		t.Fatal("adaptation changed archive-owned bytes")
	}
	if licenseInt(t, off, "accepted", "()Z") != 0 {
		t.Fatal("another session changed the off policy")
	}
}

func TestAuthenticationLicensePreservesMalformedPropertyExceptions(t *testing.T) {
	archive := licenseFixture(t)
	runtime := startLicenseFixture(t, archive, true, nil)
	archive.Descriptor.Properties["MIDlet-Jar-URL"] = "short"
	_, err := runtime.VM.InvokeStatic("LicenseCheck", "valid", authenticationCheckDescriptor, jvm.ReferenceValue(runtime.MIDlet))
	if err == nil {
		t.Fatal("authentication swallowed the guest's malformed-URL exception")
	}
}

func TestAuthenticationLicenseRefusesNearMatchesAndTruncation(t *testing.T) {
	archive := licenseFixture(t)
	original := archive.Entries["LicenseCheck.class"]
	class, err := classfile.Parse(original)
	if err != nil {
		t.Fatal(err)
	}
	method := class.FindMethod("valid", authenticationCheckDescriptor)
	if method == nil || !authenticationLicenseShapes[licenseShape(class, method)] {
		t.Fatal("authored compiler layout is not recognized")
	}
	code := method.CodeAttribute().Bytecode
	start := bytes.Index(original, code)
	if start < 0 || bytes.Count(original, code) != 1 {
		t.Fatal("fixture bytecode is not unique")
	}
	// Every ordinary instruction and branch operand participates in the shape.
	// Constant-pool indexes participate through their resolved meaning instead.
	for _, offset := range []int{0, 3, 4, 19, 21, 26, 30, 41, 50, len(code) - 4, len(code) - 1} {
		changed := bytes.Clone(original)
		changed[start+offset] ^= 1
		archive.Entries["LicenseCheck.class"] = changed
		if len(prepareAuthentication(archive)) != 0 {
			t.Fatalf("changed bytecode at %d was recognized", offset)
		}
	}
	for _, text := range []string{"MIDlet-Key", "SERVICE_ID=", "a0a535ef35b", "java/lang/String"} {
		changed := bytes.Clone(original)
		changed[bytes.Index(changed, []byte(text))] ^= 1
		archive.Entries["LicenseCheck.class"] = changed
		if len(prepareAuthentication(archive)) != 0 {
			t.Fatalf("changed contract %q was recognized", text)
		}
	}
	for length := range original {
		archive.Entries["LicenseCheck.class"] = original[:length]
		if len(prepareAuthentication(archive)) != 0 {
			t.Fatalf("truncated class at %d was recognized", length)
		}
	}
}

func TestAuthenticationLicenseObfuscatedClassAndMethodNames(t *testing.T) {
	archive := licenseFixture(t)
	original := archive.Entries["LicenseCheck.class"]
	// Preserve UTF8 lengths while changing every reference consistently.
	changed := bytes.ReplaceAll(original, []byte("LicenseCheck"), []byte("RenamedCheck"))
	changed = bytes.ReplaceAll(changed, []byte("valid"), []byte("probe"))
	delete(archive.Entries, "LicenseCheck.class")
	archive.Entries["RenamedCheck.class"] = changed
	if classes := prepareAuthentication(archive); len(classes) != 1 || classes["RenamedCheck"] == nil {
		t.Fatal("recognition depended on class or method name")
	}
}

func TestAuthenticationSubscriberSnapshotsStayIndependent(t *testing.T) {
	for _, number := range []string{"01024681357", "01976543210"} {
		t.Run(number, func(t *testing.T) {
			t.Parallel()
			runtime := &Runtime{subscriberNumber: number}
			vm := jvm.New(nil, jvm.Options{})
			for key, want := range map[string]string{"MIN": number, "m.MIN": number, "m.CARRIER": subscriberCarrier(number)} {
				value, err := runtime.getSystemProperty(vm, []jvm.Value{jvm.ReferenceValue(vm.NewString(key))})
				if err != nil {
					t.Fatal(err)
				}
				object, err := value.Reference()
				text, _ := jvm.StringText(object)
				if err != nil || text != want {
					t.Fatalf("%s = %q, %v; want %q", key, text, err, want)
				}
			}
		})
	}
}
