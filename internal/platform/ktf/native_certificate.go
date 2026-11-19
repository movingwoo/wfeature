package ktf

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/platform/compatibility"
)

const (
	nativeInterfaceCertificate = 0x103028a
	nativeCertificateCheck     = 0x08
	nativeCertificateSize      = 74
	nativeCertificateAccepted  = 0
	nativeCertificateRequired  = 1
)

// The MIF names the application, the registry recognizes its module, and
// these bindings identify the particular file the module presents.
type nativeCertificatePackage struct {
	moduleSHA256 string
	file         string
	recordSHA256 string
}

var nativeCertificatePackages = map[uint32]nativeCertificatePackage{
	20714: {
		moduleSHA256: "e14a20c0029c38253b9c8e8ac4e5efbd15bd80e60eed5338064c3f1a35c1e76d",
		file:         "c2sgames.bin",
		recordSHA256: "0bee3b3675a7d380dcacbc5add175b6fba4beef82623ba386a17b766a4ed43c8",
	},
	23682: {
		moduleSHA256: "53a4527808c40da6e49b3f6661aed60825ca7232942f6e8add9aa15889ace23c",
		file:         "gbxcerti.dat",
		recordSHA256: "af32bee83288c4082d37c0173e59bd8887be21a79220714fbfc087cb5f098045",
	},
}

func nativeCertificateBinding(archive *NativeArchive) (nativeCertificatePackage, bool) {
	if archive == nil {
		return nativeCertificatePackage{}, false
	}
	binding, ok := nativeCertificatePackages[archive.Info.ApplicationID]
	if !ok {
		return nativeCertificatePackage{}, false
	}
	module := sha256.Sum256(archive.Module)
	digest := hex.EncodeToString(module[:])
	return binding, digest == binding.moduleSHA256 && compatibility.HasFix("ktf", compatibility.NativeModule, digest, compatibility.KTFBREWPackagedCertificate)
}

func (options NativeSessionOptions) disablesPackagedCertificate(archive *NativeArchive) bool {
	if !options.DisableAuthentication {
		return false
	}
	_, recognized := nativeCertificateBinding(archive)
	return recognized
}

func (platform *NativePlatform) selectPackagedCertificate() []byte {
	if platform.authentication == backend.AuthenticationOff {
		return nil
	}
	platform.authentication = backend.AuthenticationUnsupported
	binding, ok := nativeCertificateBinding(platform.archive)
	if !ok {
		return nil
	}
	platform.authentication = backend.AuthenticationKTFBREWPackagedCertificate
	record, found := platform.packagedFile(binding.file)
	if !found || len(record) != nativeCertificateSize {
		return nil
	}
	hash := sha256.Sum256(record)
	if hex.EncodeToString(hash[:]) != binding.recordSHA256 {
		return nil
	}
	return bytes.Clone(record)
}

// Both modules call the interface with (this, record address, byte count).
// Status 1 lets the guest handle renewal through its own interface.
func (platform *NativePlatform) checkCertificate(record []byte) NativeSlotHandler {
	return func(thread *armcore.Thread) (uint32, error) {
		args, err := nativeArguments(thread, 3)
		if err != nil {
			return 0, err
		}
		if len(record) != nativeCertificateSize || args[1] == 0 || args[2] != nativeCertificateSize {
			return nativeCertificateRequired, nil
		}
		if !platform.client.core.Memory().Holds([]armcore.Span{{Address: args[1], Data: record}}) {
			return nativeCertificateRequired, nil
		}
		return nativeCertificateAccepted, nil
	}
}
