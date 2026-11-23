package lgt

import (
	"bytes"
	"maps"
	"slices"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/platform/compatibility"
)

// stripMemory maps the code range every recognized site falls in and lays the
// sites' examined code into it. No archive is embedded.
func stripMemory(t *testing.T, sites []screenStripSite) *armcore.Memory {
	t.Helper()
	memory := armcore.NewMemory()
	if err := memory.Map(0x1000, 0x60000, armcore.PermissionReadWriteExecute); err != nil {
		t.Fatal(err)
	}
	for _, site := range sites {
		if err := memory.Load(site.at, site.expect); err != nil {
			t.Fatal(err)
		}
	}
	return memory
}

// Each recognized module's sites lose their 24 and nothing else: the
// instructions around them, and every byte between, stay as they were.
func TestScreenStripConstantClearsOnlyTheStrip(t *testing.T) {
	for digest, sites := range screenStripSites {
		memory := stripMemory(t, sites)
		if err := clearScreenStripConstant(memory, digest); err != nil {
			t.Fatalf("%s: %v", digest[:8], err)
		}
		for _, site := range sites {
			found := make([]byte, len(site.expect))
			if err := memory.Read(site.at, found); err != nil {
				t.Fatal(err)
			}
			want := bytes.Clone(site.expect)
			want[site.immediate] = 0
			if !bytes.Equal(found, want) {
				t.Fatalf("%s at %#x: %x, want %x", digest[:8], site.at, found, want)
			}
		}
	}
}

// The strip is cleared by `movs rN,#24` becoming `movs rN,#0`, which the core
// executes as the zero the module then stores.
func TestScreenStripConstantLoadsZero(t *testing.T) {
	const at = 0x436fe
	for _, cleared := range []bool{false, true} {
		memory := armcore.NewMemory()
		if err := memory.Map(0x43000, 0x1000, armcore.PermissionReadWriteExecute); err != nil {
			t.Fatal(err)
		}
		// movs r3,#24; str r3,[r4,#4]
		if err := memory.Load(at, []byte{0x18, 0x23, 0x63, 0x60}); err != nil {
			t.Fatal(err)
		}
		if cleared {
			if err := memory.Write(at, []byte{0}); err != nil {
				t.Fatal(err)
			}
		}
		state := armcore.NewContext()
		state.Registers[4] = 0x43800
		if err := state.SetPC(at | 1); err != nil {
			t.Fatal(err)
		}
		if _, err := (armcore.Engine{}).Run(&state, memory, 0xffffffff, 2); err != nil {
			t.Fatal(err)
		}
		var stored [4]byte
		if err := memory.Read(0x43804, stored[:]); err != nil {
			t.Fatal(err)
		}
		want := byte(24)
		if cleared {
			want = 0
		}
		if stored != [4]byte{want} {
			t.Fatalf("cleared=%v stored %v, want %d", cleared, stored, want)
		}
	}
}

// A module that is not the one examined is refused with nothing written: one
// changed byte at any site leaves every site as it was, and a digest without
// sites is not guessed at.
func TestScreenStripConstantRejectsUnexpectedCode(t *testing.T) {
	for digest, sites := range screenStripSites {
		for index := range sites {
			memory := stripMemory(t, sites)
			last := sites[index]
			if err := memory.Write(last.at, []byte{last.expect[0] ^ 1}); err != nil {
				t.Fatal(err)
			}
			if err := clearScreenStripConstant(memory, digest); err == nil {
				t.Fatalf("%s: a changed site %d was accepted", digest[:8], index)
			}
			for _, site := range sites {
				var immediate [1]byte
				if err := memory.Read(site.at+uint32(site.immediate), immediate[:]); err != nil {
					t.Fatal(err)
				}
				if immediate[0] != site.expect[site.immediate] {
					t.Fatalf("%s: a refused module was written at %#x", digest[:8], site.at)
				}
			}
		}
	}
	if err := clearScreenStripConstant(stripMemory(t, nil), "00"); err == nil {
		t.Fatal("a module without sites was accepted")
	}
}

// Every module with sites is one the registry selects the fix for, so no
// table entry is dead and no registered module is without its sites.
func TestScreenStripConstantSitesAreRegistered(t *testing.T) {
	registered := compatibility.DigestsForFix(compatibility.LGTScreenStripConstant)
	sites := slices.Sorted(maps.Keys(screenStripSites))
	if !slices.Equal(registered, sites) {
		t.Errorf("screen strip digests: registry %v, Go sites %v", registered, sites)
	}
}

// A site names the immediate of a `movs` holding the strip, and nothing else:
// another instruction, or a movs of another value, is not a site.
func TestScreenStripSitesNameAStripImmediate(t *testing.T) {
	for digest, sites := range screenStripSites {
		if len(sites) == 0 {
			t.Errorf("%s has no screen strip sites", digest)
		}
		for _, site := range sites {
			if !stripImmediate(site) {
				t.Errorf("%s at %#x does not name a strip immediate", digest[:8], site.at)
			}
		}
	}
	for _, site := range []screenStripSite{
		{expect: []byte{0x18, 0x60}, immediate: 0},       // str r0,[r3]
		{expect: []byte{0x19, 0x23}, immediate: 0},       // movs r3,#25
		{expect: []byte{0x18, 0x23}, immediate: 1},       // the opcode byte
		{expect: []byte{0x18, 0x23}, immediate: -1},      // outside the code
		{expect: []byte{0x00, 0x18, 0x23}, immediate: 2}, // no opcode byte after it
	} {
		if stripImmediate(site) {
			t.Errorf("%x at %d was taken for a strip immediate", site.expect, site.immediate)
		}
	}
}
