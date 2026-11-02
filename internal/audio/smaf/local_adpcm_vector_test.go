package smaf

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/movingwoo/wfeature/internal/testlocal"
)

// An encoded MA ADPCM stream and its decoded 16-bit little-endian samples,
// published as a test pair outside this repository, decode exactly under the
// bit-by-bit difference rule. The pair is not committed: like the game
// archives it is opt-in, from a directory holding out.adpcm and out.pcm.
//
//	WFEATURE_ADPCM_VECTOR=/path/to/pair go test ./internal/audio/smaf -run Vector
func TestLocalADPCMMatchesAnExternalVectorPair(t *testing.T) {
	directory := testlocal.Value(t, "WFEATURE_ADPCM_VECTOR", "a directory holding out.adpcm and out.pcm")
	encoded, err := os.ReadFile(filepath.Join(directory, "out.adpcm"))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := os.ReadFile(filepath.Join(directory, "out.pcm"))
	if err != nil {
		t.Fatal(err)
	}
	samples := DecodeADPCM(encoded)
	if len(samples)*2 != len(decoded) {
		t.Fatalf("decoded %d samples, the pair holds %d", len(samples), len(decoded)/2)
	}
	for index, sample := range samples {
		if want := int16(binary.LittleEndian.Uint16(decoded[index*2:])); sample != want {
			t.Fatalf("sample %d = %d, want %d", index, sample, want)
		}
	}
}
