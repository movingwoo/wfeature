package ktf

import (
	"fmt"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/cheat"
)

// The earlier package runs on a core of its own, and a core is an address
// space a scan can sweep, so the cheat engine attaches here the same way it
// does to the descriptor package's client. What differs is only the map: this
// title keeps its whole state in what the platform allocated for it, so the
// arena is where a search for a health bar will find one, and the labels say
// so.
type nativeCheatTarget struct {
	client *NativeClient
}

func (target nativeCheatTarget) ReadMemory(address uint32, destination []byte) error {
	return target.client.core.Memory().Read(address, destination)
}

// WriteMemory deliberately does not go through the watched write path: the
// engine's own freeze rewrites land every tick, and recording them would make
// the cheat the loudest writer of every address it holds.
func (target nativeCheatTarget) WriteMemory(address uint32, data []byte) error {
	return target.client.core.Memory().WriteUntracked(address, data)
}

func (target nativeCheatTarget) Regions() []cheat.Region {
	committed := target.client.core.Memory().CommittedRegions(armcore.PermissionWrite)
	regions := make([]cheat.Region, 0, len(committed))
	for _, region := range committed {
		label, code := target.label(region.Base)
		regions = append(regions, cheat.Region{
			Base:  region.Base,
			Size:  uint32(min(region.Size, 0xffffffff)),
			Label: label,
			Code:  code,
		})
	}
	return regions
}

// label names a region by where it starts and says whether it holds
// instructions rather than state. Adjacent pages are reported as one region,
// so a label has to be right for the run rather than for the page: the word
// the loader plants sits immediately below the module and comes back merged
// with it, and the entry scratch sits immediately below the arena.
//
// Only the stub arena is code a scan can be kept out of. The module is one
// span of instructions and data with no boundary recorded between them, and it
// stays swept for that reason; it also stays writable, which is what a byte
// patch into a check needs.
func (target nativeCheatTarget) label(base uint32) (string, bool) {
	switch {
	case base == nativeHeaderBase || (base >= ImageBase && uint64(base) < uint64(ImageBase)+uint64(target.client.mapped)):
		return "module", false
	case uint64(base) >= uint64(ThreadStackBase) && uint64(base) < uint64(ThreadStackBase)+ThreadStackSize:
		return "stack", false
	case base >= nativeStubBase && base < nativeStubBase+maxNativeSurfaces*nativePageSize:
		return "stubs", true
	case base >= nativeScratchBase:
		// Everything the title works on is here: it has no writable data of
		// its own, so its whole state was allocated out of the arena that
		// starts just above the entry scratch.
		return "arena", false
	case base >= nativeTableBase:
		return "platform", false
	default:
		return "data", false
	}
}

func (target nativeCheatTarget) Watch(address uint32)   { target.client.core.Watch(address) }
func (target nativeCheatTarget) Unwatch(address uint32) { target.client.core.Unwatch(address) }
func (target nativeCheatTarget) ClearWatches()          { target.client.core.ClearWatches() }
func (target nativeCheatTarget) Watches() []uint32      { return target.client.core.Watches() }

func (target nativeCheatTarget) WatchHits() []cheat.WatchHit {
	hits := target.client.core.WatchHits()
	converted := make([]cheat.WatchHit, len(hits))
	for index, hit := range hits {
		converted[index] = cheat.WatchHit{
			Address: hit.Address, PC: hit.PC, Origin: writeOrigin(hit.Origin),
			Value: hit.Value, Size: hit.Size, Count: hit.Count,
		}
	}
	return converted
}

func (target nativeCheatTarget) WatchHitsOverflowed() bool {
	return target.client.core.WatchHitsOverflowed()
}

// Cheat returns the session's attached cheat engine, creating it on first use.
// ImageHash is the SHA-256 of the module this session loaded, in lower-case
// hex — the same identity the other generation of package answers with, so a
// cheat table is keyed the same way whichever one a title arrived as.
func (session *NativeSession) ImageHash() string {
	if session == nil || session.Client == nil || session.Client.archive == nil {
		return ""
	}
	return imageHash(session.Client.archive.Module)
}

// Tick reapplies its frozen values after every frame.
func (session *NativeSession) Cheat() *cheat.Session {
	if session == nil || session.Client == nil {
		return nil
	}
	if session.cheat == nil {
		session.cheat = cheat.NewSession(nativeCheatTarget{client: session.Client})
	}
	return session.cheat
}

// CheatConsole returns the text command console bound to Cheat().
func (session *NativeSession) CheatConsole() *cheat.Console {
	if session == nil || session.Client == nil {
		return nil
	}
	if session.cheatConsole == nil {
		session.cheatConsole = cheat.NewConsole(session.Cheat())
		// A saved table names the game it was made against, which is the only
		// thing that lets one be placed months later. There is no main class
		// here, so the title's own name is what names it. The key beside the
		// name is the hash of the module actually loaded, which is what a byte
		// patch is true of whatever the package around it was called.
		session.cheatConsole.SetGame(session.Name())
		session.cheatConsole.SetTableKey(cheat.TableKey{Image: session.ImageHash()})
	}
	return session.cheatConsole
}

// serviceCheat rewrites frozen values after a frame so they win over whatever
// the title just wrote.
func (session *NativeSession) serviceCheat() error {
	if session.cheat == nil {
		return nil
	}
	if failed := session.cheat.Tick(); len(failed) > 0 {
		return fmt.Errorf("KTF native cheat freeze failed at %#x", failed[0])
	}
	return nil
}
