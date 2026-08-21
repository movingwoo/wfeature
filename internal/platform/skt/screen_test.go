package skt

import "testing"

// The rule has to fire on the shape the local small-screen archive has — three
// resources whose names end in the width it was built for, and no variant for
// any other — and stay quiet on everything else, because every other local
// archive runs on the default and a wrong answer here would move it off.
func TestPackagedScreenReadsAWidthOnlyWhenTheNamesAgree(t *testing.T) {
	for _, want := range []struct {
		name    string
		entries []string
		width   int
		height  int
		ok      bool
	}{
		{
			name:    "a title packaged for the smaller handset",
			entries: []string{"title/main_logo_176.png", "title/gamevil_176.png", "title/intro_176.dat", "bg/bg_00.png", "eff/eff_exorcist_1.dat", "n.class"},
			width:   176, height: 220, ok: true,
		},
		{
			name:    "an archive with no width in any name",
			entries: []string{"bg/bg_10_.png", "eff/eff_dem0.dat", "menu/main_frame.png", "d.class"},
		},
		{
			name:    "two widths disagreeing is not a declaration",
			entries: []string{"title/logo_176.png", "title/logo_176_a.png", "title/logo_240.png", "title/splash_240.png"},
		},
		{
			name:    "one name on its own is a coincidence",
			entries: []string{"tiles/tiles_320.png", "bg/bg_00.png"},
		},
		{
			name:    "a width no handset here offers is read and ignored",
			entries: []string{"title/main_logo_120.png", "title/intro_120.dat"},
		},
		{
			name:    "a class file never counts",
			entries: []string{"pack_176.class", "other_176.class"},
		},
	} {
		t.Run(want.name, func(t *testing.T) {
			entries := make(map[string][]byte, len(want.entries))
			for _, name := range want.entries {
				entries[name] = nil
			}
			width, height, ok := packagedScreenFromNames(entries)
			if ok != want.ok || width != want.width || height != want.height {
				t.Fatalf("packagedScreenFromNames() = %d, %d, %v, want %d, %d, %v", width, height, ok, want.width, want.height, want.ok)
			}
		})
	}
}

func TestPackagedScreenAnswersNothingForNoArchive(t *testing.T) {
	if _, _, ok := PackagedScreen(nil); ok {
		t.Fatal("PackagedScreen(nil) claimed a screen")
	}
}
