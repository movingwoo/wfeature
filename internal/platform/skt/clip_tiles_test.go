package skt

import (
	_ "embed"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/api/midp"
	"github.com/movingwoo/wfeature/internal/api/wipi"
	"github.com/movingwoo/wfeature/internal/jvm"
)

//go:embed testdata/clip-tiles.jar
var clipTilesJAR []byte

func TestPackagedLegacyClipTilesHaveNoGaps(t *testing.T) {
	for _, legacy := range []bool{true, false} {
		name := "standard counts"
		if legacy {
			name = "explicit compatibility"
		}
		t.Run(name, func(t *testing.T) {
			archive, err := Open(clipTilesJAR)
			if err != nil {
				t.Fatal(err)
			}

			framebuffer := newTestFramebuffer(t, 32, 48)
			runtime, err := Start(archive, Options{Framebuffer: framebuffer})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = runtime.Destroy(true) })
			if runtime.legacyClip {
				t.Fatal("profile alone enabled clip compatibility")
			}
			if legacy {
				runtime.legacyClip = true
				if err := runtime.repaintNewCurrentCanvas(runtime.currentDisplayable); err != nil {
					t.Fatal(err)
				}
				if err := runtime.RunPending(); err != nil {
					t.Fatal(err)
				}
			}

			frame, _ := framebuffer.Snapshot()
			for y := 0; y < 32; y++ {
				for x := 0; x < 32; x++ {
					want := []byte{0x33, 0xaa, 0x55, 0xff}
					if !legacy && (x%16 == 15 || y%16 == 15) {
						want = []byte{0, 0, 0, 0xff}
					}
					assertRGBAPixel(t, frame, x, y, want)
				}
			}
		})
	}
}

func TestLegacySetClipBoundsAndTranslation(t *testing.T) {
	for _, test := range []struct {
		name                string
		x, y, width, height int32
		want                paintRect
	}{
		{"translated", 1, 2, 3, 4, paintRect{3, 3, 7, 8}},
		{"point", 0, 0, 0, 0, paintRect{2, 1, 3, 2}},
		{"negative", 0, 0, -1, 2, paintRect{}},
		{"large", 0, 0, math.MaxInt32, math.MaxInt32, paintRect{2, 1, 10, 10}},
		{"outside", math.MaxInt32, 0, 1, 1, paintRect{}},
		{"wide span", math.MinInt32, 0, math.MaxInt32, 0, paintRect{0, 1, 2, 2}},
	} {
		t.Run(test.name, func(t *testing.T) {
			context := &graphicsContext{active: true, width: 10, height: 10, translateX: 2, translateY: 1,
				deviceClip: paintRect{maxX: 10, maxY: 10}}
			object := &jvm.Object{ClassName: midp.GraphicsClass, Native: context}
			runtime := &Runtime{legacyClip: true}
			_, err := runtime.setGraphicsClip(nil, []jvm.Value{jvm.ReferenceValue(object), jvm.IntValue(test.x),
				jvm.IntValue(test.y), jvm.IntValue(test.width), jvm.IntValue(test.height)})
			if err != nil {
				t.Fatal(err)
			}
			if got := context.clip; (!got.empty() || !test.want.empty()) && got != test.want {
				t.Fatalf("clip = %+v, want %+v", got, test.want)
			}
		})
	}
	context := &graphicsContext{active: true, width: 10, height: 10, deviceClip: paintRect{maxX: 10, maxY: 10}}
	object := &jvm.Object{ClassName: wipi.GraphicsClass, Native: context}
	_, err := (&Runtime{legacyClip: true}).setGraphicsClip(nil, []jvm.Value{jvm.ReferenceValue(object),
		jvm.IntValue(0), jvm.IntValue(0), jvm.IntValue(1), jvm.IntValue(1)})
	if err != nil || context.clip != (paintRect{maxX: 1, maxY: 1}) {
		t.Fatalf("WIPI clip = %+v, err %v", context.clip, err)
	}
}

func TestClipCompatibilityDoesNotFollowProfile(t *testing.T) {
	for _, profile := range []string{"M_Profile-1.0, SKTP-1.0", "SKTP-1.1", "MIDP-2.0", ""} {
		archive, err := Open(clipTilesJAR)
		if err != nil {
			t.Fatal(err)
		}
		archive.Descriptor.Properties["MicroEdition-Profile"] = profile
		if archive.legacyClipEndpoints() {
			t.Fatalf("profile %q enabled compatibility for unrelated code", profile)
		}
	}
}

// The real archive stays local. This also proves that changing any class
// disables the exception without relying on a title or container filename.
func TestLocalClipCompatibilityArchive(t *testing.T) {
	path := os.Getenv("WFEATURE_SKT_CLIP_ARCHIVE")
	if path == "" {
		t.Skip("set WFEATURE_SKT_CLIP_ARCHIVE to the reported local archive")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	if !archive.legacyClipEndpoints() {
		t.Fatalf("unrecognized code digest: %s", archive.clipCodeDigest())
	}
	archive.Descriptor.Name = "Renamed Archive"
	if !archive.legacyClipEndpoints() {
		t.Fatal("display name changed compatibility")
	}
	for name, data := range archive.Entries {
		if !strings.HasSuffix(name, ".class") {
			continue
		}
		changed := append([]byte(nil), data...)
		changed[len(changed)-1] ^= 1
		archive.Entries[name] = changed
		if archive.legacyClipEndpoints() {
			t.Fatalf("changed class %q retained compatibility", name)
		}
		archive.Entries[name] = data
	}
}
