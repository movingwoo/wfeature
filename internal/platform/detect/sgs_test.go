package detect_test

import (
	"encoding/binary"
	"testing"

	"github.com/movingwoo/wfeature/internal/platform/detect"
)

func scriptMOD() []byte {
	data := make([]byte, 24)
	for _, value := range []string{"application/x-gnex-sgs", "SGS", "1.0.0"} {
		data = binary.LittleEndian.AppendUint32(data, uint32(len(value)))
		data = append(data, value...)
	}
	return data
}

func TestScriptPackageDetection(t *testing.T) {
	inf := append([]byte{1, 2, 3, 3, 9, 9}, []byte("GVM127.0.0.1\x00\x00127.0.0.1\x00\x00")...)
	for _, tc := range []struct {
		name    string
		entries map[string][]byte
		want    detect.Reason
	}{
		{"MOD pair", map[string][]byte{"0001.mod": scriptMOD(), "0001.SGS": {1}}, detect.ReasonClaimed},
		{"wrapped case insensitive pair", map[string][]byte{"./game\\0001.MOD": scriptMOD(), "game/0001.sgs": {2}}, detect.ReasonClaimed},
		{"INF pair", map[string][]byte{"0001.inf": inf, "0001.sgs": {2}}, detect.ReasonClaimed},
		{"payload only", map[string][]byte{"0001.sgs": {2}}, detect.ReasonNoMarker},
		{"unrelated names", map[string][]byte{"0001.mod": scriptMOD(), "0002.sgs": {2}}, detect.ReasonNoMarker},
		{"unrelated folders", map[string][]byte{"a/0001.mod": scriptMOD(), "b/0001.sgs": {2}}, detect.ReasonNoMarker},
		{"extensions alone", map[string][]byte{"0001.mod": []byte("unrelated"), "0001.sgs": {2}}, detect.ReasonNoMarker},
		{"truncated MOD", map[string][]byte{"0001.mod": scriptMOD()[:30], "0001.sgs": {2}}, detect.ReasonNoMarker},
		{"truncated INF", map[string][]byte{"0001.inf": inf[:len(inf)-1], "0001.sgs": {2}}, detect.ReasonNoMarker},
		{"oversized descriptor", map[string][]byte{"0001.mod": append(scriptMOD(), make([]byte, 4096)...), "0001.sgs": {2}}, detect.ReasonNoMarker},
	} {
		t.Run(tc.name, func(t *testing.T) {
			platform, reason, err := detect.Classify(buildZIP(t, tc.entries))
			wantPlatform := detect.Unknown
			if tc.want == detect.ReasonClaimed {
				wantPlatform = detect.SKT
			}
			if err != nil || platform != wantPlatform || reason != tc.want {
				t.Fatalf("Classify = %q, %q, %v; want unknown, %q, nil", platform, reason, err, tc.want)
			}
		})
	}
}

func TestJavaMarkerTakesPrecedenceOverScriptResources(t *testing.T) {
	data := buildZIP(t, map[string][]byte{"app.msd": nil, "0001.mod": scriptMOD(), "0001.sgs": {2}})
	platform, reason, err := detect.Classify(data)
	if err != nil || platform != detect.SKT || reason != detect.ReasonClaimed {
		t.Fatalf("Classify = %q, %q, %v", platform, reason, err)
	}
}
