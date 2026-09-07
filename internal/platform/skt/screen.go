package skt

import (
	"path"
	"slices"
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
// **A second local title declares the same thing in the same place, as a
// directory.** Its artwork is a single `img_176/` tree and it picks the tree
// from the height of an offscreen image it makes sixteen rows taller than its
// Canvas — which on this vendor is the display, so the ladder it indexes is a
// ladder of displays:
//
//	display > 250  ->  "/img_240/"
//	display > 212  ->  "/img_240/"
//	display > 166  ->  "/img_176/"
//	         else  ->  "/img_120/"
//
// so the tree it ships is the one it picks on a display of 167 to 212 rows.
// Anything taller asks for the `/img_240/` tree it does not carry, catches the
// `IOException`, and paints a screen with nothing on it.
//
// So the rule reads a width suffix on **any** component of an entry's path,
// not only on the name at the end of it, and it is deliberately narrow: the
// suffix is only believed when **every** width-suffixed component in the
// archive agrees on one width, at least two say it, and this project offers a
// handset for that width. That narrowness is what makes it different from the
// KTF case. There the declaration is present in thirteen archives and wrong in
// twelve of them, so honouring it would shrink twelve working titles to fix
// one; across the ninety-one local archives here it answers for three, and one
// of those three is the default it would have run on anyway.
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
//
// **The 176 handset here is 208 rows and the menu's is 220.** An archive
// declares a width and never a height, so the height is this project's answer
// rather than the archive's, and the two archives that declare 176 are the
// only titles it is an answer for. One of them cannot run on 220: its ladder
// above ends at 212 rows, so a 220-row display sends it to artwork it does not
// carry and it paints nothing. Both run on 208, which is a display this
// generation shipped, and the other one draws the same screen on it — the same
// picture, the same lit count, twelve rows shorter. A Host that asks still
// wins: `runskt -screen 176x220` and the browser's 176x220 are unchanged, and
// this map only decides what "no answer" means.
var packagedScreenHandsets = map[int]int{
	128: 160,
	176: 208,
	240: 320,
	320: 480,
}

// packagedScreenMinimumNames is how many width-suffixed components have to
// agree before the suffix is read as a declaration. One is a coincidence
// waiting to happen — a sprite sheet called `tiles_320.png` says nothing about
// a handset — and the two local archives that need this carry three names and
// sixty-two files under one directory.
const packagedScreenMinimumNames = 2

func packagedScreenFromNames(entries map[string][]byte) (width, height int, ok bool) {
	found := make(map[int]int)
	for name := range entries {
		for _, value := range packagedScreenWidths(name) {
			found[value]++
		}
	}
	if len(found) != 1 {
		return 0, 0, false
	}
	widths := make([]int, 0, 1)
	for value := range found {
		widths = append(widths, value)
	}
	slices.Sort(widths)
	if found[widths[0]] < packagedScreenMinimumNames {
		return 0, 0, false
	}
	return widths[0], packagedScreenHandsets[widths[0]], true
}

// packagedScreenWidths answers the widths one entry's path declares, at most
// once each however many of its components say the same one.
//
// A directory counts as much as a name, because a directory is where a title
// that carries a whole artwork tree per handset puts the declaration, and that
// tree is the title's own statement about the handset it shipped for. The name
// at the end of a path drops its extension first; a `.class` never counts,
// because a class named for the pack it belongs to — `pack_176.class` — names
// code rather than artwork.
func packagedScreenWidths(name string) []int {
	var widths []int
	components := strings.Split(name, "/")
	for index, component := range components {
		if index == len(components)-1 {
			if strings.HasSuffix(component, ".class") {
				continue
			}
			if extension := path.Ext(component); extension != "" {
				component = strings.TrimSuffix(component, extension)
			}
		}
		underscore := strings.LastIndexByte(component, '_')
		if underscore < 0 || underscore == len(component)-1 {
			continue
		}
		value, err := strconv.Atoi(component[underscore+1:])
		if err != nil {
			continue
		}
		if _, known := packagedScreenHandsets[value]; !known {
			continue
		}
		if !slices.Contains(widths, value) {
			widths = append(widths, value)
		}
	}
	return widths
}
