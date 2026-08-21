package skt

import (
	"path"
	"sort"
	"strconv"
	"strings"
)

// PackagedScreen answers the handset an archive was packaged for, when the
// archive says so.
//
// **An SKT descriptor never says.** Every key of every local `.msd` and every
// manifest inside every local JAR was inventoried, and not one of them carries
// a screen size: the descriptor names the MIDlet, its vendor, its download
// URL, its size and its keys, and stops. So the size cannot come from where
// the other WIPI platform's comes from — KTF's `__adf__` has a `DisplaySize`
// field, and `docs/ktf.md` has why adopting even that one is the wrong move
// there.
//
// **The archive says it another way: in the names of its own resources.** One
// local title branches on the screen width and builds a different title screen
// for each handset it was built for —
//
//	if (width <  176)                     "/title/main_logo_120.png"
//	if (width == 240 && height == 160)    "/title/main_logo_120.png"
//	if (width <  240)                     "/title/main_logo_176.png"
//	else                                  "/title/main_logo_240.png"
//
// — and the copy that reached this library holds only the `_176` variants.
// Started on the 240-wide default it asks for a `_240` image that is not
// there, catches the `IOException` its own way, and then draws the null it was
// left with, which ends the session on a `NullPointerException` rather than on
// anything this runtime got wrong.
//
// So the rule is the resource names, and it is deliberately narrow: a width
// suffix is only believed when **every** width-suffixed name in the archive
// agrees on one width, and only for widths this project offers a handset for.
// That narrowness is what makes it different from the KTF case. There the
// declaration is present in thirteen archives and wrong in twelve of them, so
// honouring it would shrink twelve working titles to fix one; here the signal
// appears in exactly one archive of the fifteen — the one that cannot start
// without it — and in none of the others.
func PackagedScreen(archive *Archive) (width, height int, ok bool) {
	if archive == nil {
		return 0, 0, false
	}
	return packagedScreenFromNames(archive.Entries)
}

// packagedScreenHandsets maps a declared width to the handset this project
// offers for it. A width with no handset here — one local title names a 120
// variant — is not a size a Host can be asked for, so it is read and ignored
// rather than rounded to a neighbour.
var packagedScreenHandsets = map[int]int{
	128: 160,
	176: 220,
	240: 320,
	320: 480,
}

// packagedScreenMinimumNames is how many width-suffixed names have to agree
// before the suffix is read as a declaration. One name is a coincidence
// waiting to happen — a sprite sheet called `tiles_320.png` says nothing about
// a handset — and the local archive that needs this carries three.
const packagedScreenMinimumNames = 2

func packagedScreenFromNames(entries map[string][]byte) (width, height int, ok bool) {
	found := make(map[int]int)
	for name := range entries {
		if strings.HasSuffix(name, ".class") {
			continue
		}
		base := path.Base(name)
		if extension := path.Ext(base); extension != "" {
			base = strings.TrimSuffix(base, extension)
		}
		underscore := strings.LastIndexByte(base, '_')
		if underscore < 0 || underscore == len(base)-1 {
			continue
		}
		value, err := strconv.Atoi(base[underscore+1:])
		if err != nil {
			continue
		}
		if _, known := packagedScreenHandsets[value]; !known {
			continue
		}
		found[value]++
	}
	if len(found) != 1 {
		return 0, 0, false
	}
	widths := make([]int, 0, 1)
	for value := range found {
		widths = append(widths, value)
	}
	sort.Ints(widths)
	if found[widths[0]] < packagedScreenMinimumNames {
		return 0, 0, false
	}
	return widths[0], packagedScreenHandsets[widths[0]], true
}
