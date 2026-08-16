package lgt

import (
	"fmt"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/cheat"
)

// cheatTarget adapts the client's guest address space to the cheat engine.
// Scans cover only committed writable pages: game state the guest can mutate
// lives there, and the lazily committed reservations would make every sweep
// orders of magnitude slower.
//
// It is the same adapter shape the other ARM platform uses, because the engine
// asks the same three questions of both — what can be read, what can be
// written, and which regions are worth sweeping.
type cheatTarget struct {
	client *Client
}

func (target cheatTarget) ReadMemory(address uint32, destination []byte) error {
	return target.client.core.Memory().Read(address, destination)
}

// WriteMemory deliberately does not go through the watched write path: the
// engine's own freeze rewrites land every tick, and recording them would make
// the cheat the loudest writer of every address it holds.
func (target cheatTarget) WriteMemory(address uint32, data []byte) error {
	return target.client.core.Memory().WriteUntracked(address, data)
}

func (target cheatTarget) Regions() []cheat.Region {
	committed := target.client.core.Memory().CommittedRegions(armcore.PermissionWrite)
	regions := make([]cheat.Region, 0, len(committed))
	for _, region := range committed {
		regions = append(regions, cheat.Region{
			Base:  region.Base,
			Size:  uint32(min(region.Size, 0xffffffff)),
			Label: target.label(region.Base),
		})
	}
	return regions
}

// label names a region so a hit reads as something. Unlike the other platform
// the module is not at a fixed base — an ELF keeps whatever addresses it names
// — so the module's own span is asked of the loaded module rather than
// compared against a constant.
func (target cheatTarget) label(base uint32) string {
	low, high := target.client.module.Span()
	switch {
	case base >= low && base < high:
		return "module"
	case uint64(base) >= uint64(heapBase) && uint64(base) < uint64(heapBase)+heapSize:
		return "heap"
	case uint64(base) >= uint64(platformCodeBase) && uint64(base) < uint64(platformCodeBase)+platformCodeSize:
		return "stubs"
	case uint64(base) >= uint64(platformDataBase) && uint64(base) < uint64(platformDataBase)+platformDataSize:
		return "platform"
	case uint64(base) >= uint64(stackBase) && uint64(base) < uint64(stackBase)+stackSize:
		return "stack"
	default:
		return "data"
	}
}

// The watch methods forward to the ARM core's store instrumentation, which is
// what makes cheatTarget a cheat.WatchTarget.
func (target cheatTarget) Watch(address uint32)   { target.client.core.Watch(address) }
func (target cheatTarget) Unwatch(address uint32) { target.client.core.Unwatch(address) }
func (target cheatTarget) ClearWatches()          { target.client.core.ClearWatches() }
func (target cheatTarget) Watches() []uint32      { return target.client.core.Watches() }

func (target cheatTarget) WatchHits() []cheat.WatchHit {
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

func (target cheatTarget) WatchHitsOverflowed() bool { return target.client.core.WatchHitsOverflowed() }

// writeOrigin translates the core's answer for the cheat engine, which is kept
// free of the ARM core so a platform without one can still hold a session.
// Translating rather than sharing the type is what keeps that true.
func writeOrigin(origin armcore.WriteOrigin) cheat.WriteOrigin {
	if origin == armcore.OriginHost {
		return cheat.OriginHost
	}
	return cheat.OriginGuest
}

// Cheat returns the session's attached cheat engine, creating it on first use.
// Session.Tick reapplies its frozen values after every round.
func (session *Session) Cheat() *cheat.Session {
	if session == nil || session.client == nil {
		return nil
	}
	if session.cheat == nil {
		session.cheat = cheat.NewSession(cheatTarget{client: session.client})
	}
	return session.cheat
}

// CheatConsole returns the text command console bound to Cheat().
func (session *Session) CheatConsole() *cheat.Console {
	if session == nil || session.client == nil {
		return nil
	}
	if session.cheatConsole == nil {
		session.cheatConsole = cheat.NewConsole(session.Cheat())
		// A saved table names the game it was made against, which is the only
		// thing that lets one be placed months later. This platform gives every
		// title its own PID, so that is the name worth saving.
		if session.archive != nil {
			session.cheatConsole.SetGame(SaveOwner(session.archive.Descriptor))
		}
	}
	return session.cheatConsole
}

// serviceCheat rewrites frozen values after a tick so they win over whatever
// the game just wrote. Freeze failures surface as an error because a cheat
// that silently stops applying is worse than a visible fault.
func (session *Session) serviceCheat() error {
	if session.cheat == nil {
		return nil
	}
	if failed := session.cheat.Tick(); len(failed) > 0 {
		return fmt.Errorf("LGT cheat freeze failed at %#x", failed[0])
	}
	return nil
}
