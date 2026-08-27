package detect_test

import (
	"errors"
	"testing"

	"github.com/movingwoo/wfeature/internal/platform/detect"
)

// Detection runs before any loader, on bytes nobody has vouched for: a Host
// hands it whatever file a person picked. Every function here has to answer
// rather than panic, and the ones that decompress have to stay inside their
// bounds while doing it.
//
// The seeds are one archive of each shape the switch has a branch for, so the
// fuzzer starts from a zip that reaches the class scan rather than from noise
// that never gets past the zip header.
func FuzzArchiveNeverPanics(f *testing.F) {
	f.Add(buildZIP(f, map[string][]byte{"__adf__": []byte("AID:fixture\n")}))
	f.Add(buildZIP(f, map[string][]byte{"app_info": []byte("AID=0102ABCD\n")}))
	f.Add(buildZIP(f, map[string][]byte{"Fixture.msd": []byte("descriptor")}))
	// The earlier package's shape: an information file beside exactly one
	// module, which is claimed only after every marker and the class scan.
	f.Add(buildZIP(f, map[string][]byte{
		"fixture.mif": []byte("info"),
		"fixture.mod": []byte("module"),
	}))
	// A class file's constant pool is what the SKT scan reads, so a seed
	// carries one of the strings it looks for.
	f.Add(buildZIP(f, map[string][]byte{"Fixture.class": []byte("\xca\xfe\xba\xbecom/skt/m/Device")}))
	// A wrapping folder, which is the path that rewrites every name before the
	// switch sees it.
	f.Add(buildZIP(f, map[string][]byte{
		"Fixture Title/__adf__":     []byte("AID:fixture\n"),
		"Fixture Title/fixture.jar": []byte("jar"),
	}))
	// A zip of zips, which is the one shape that is named and not loaded.
	f.Add(buildZIP(f, map[string][]byte{
		"one.zip": buildZIP(f, map[string][]byte{"__adf__": []byte("AID:one\n")}),
		"two.zip": buildZIP(f, map[string][]byte{"__adf__": []byte("AID:two\n")}),
	}))
	f.Add([]byte("PK\x03\x04 and then nothing"))
	f.Add([]byte("Rar!\x1a\x07\x00"))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		platform, err := detect.Archive(data)
		if err == nil {
			switch platform {
			case detect.KTF, detect.LGT, detect.SKT, detect.Unknown:
			default:
				t.Fatalf("Archive() named %q, which is not a platform", platform)
			}
		} else if platform != "" {
			t.Fatalf("Archive() named %q and failed with %v", platform, err)
		}
		// The three that take the same bytes and never fail. A Host calls
		// them to explain a refusal, which is exactly when the bytes are
		// least trustworthy.
		detect.ContainerFormat(data)
		detect.ArchiveOfArchives(data)
		if wrapped := detect.ContainerError(data, errFixture); wrapped == nil {
			t.Fatal("ContainerError() dropped the error it was given")
		}
	})
}

var errFixture = errors.New("fixture")
