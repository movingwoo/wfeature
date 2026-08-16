package ktf

import (
	"fmt"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/cheat"
)

// cheatTarget adapts the client's flat guest address space to the cheat
// engine. Scans cover only committed writable pages: game state the guest can
// mutate lives there, and the lazily committed reservations would make every
// sweep orders of magnitude slower.
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

func (target cheatTarget) label(base uint32) string {
	image := target.client.image
	switch {
	case base >= ImageBase && uint64(base) < uint64(ImageBase)+uint64(len(image.Data))+uint64(image.BSSSize):
		return "client"
	case base >= ThreadStackBase && base < platformDataBase:
		return "stack"
	case base >= platformCodeBase && uint64(base) < uint64(platformCodeBase)+platformCodeSize:
		// The 64 MiB data arena overlaps the read/execute stub arena, so the
		// stub pages scan as writable; label them so hits there read as code.
		return "stubs"
	case base >= platformDataBase && uint64(base) < uint64(platformDataBase)+platformDataSize:
		return "platform"
	default:
		return "data"
	}
}

// Cheat returns the session's attached cheat engine, creating it on first
// use. Session.Tick reapplies its frozen values after every service round.
// The watch methods forward to the ARM core's store instrumentation, which is
// what makes cheatTarget a cheat.WatchTarget. A platform without an emulated
// address space implements none of this and its sessions answer
// cheat.ErrWatchUnsupported instead.
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

func (session *Session) Cheat() *cheat.Session {
	if session == nil || session.Client == nil {
		return nil
	}
	if session.cheat == nil {
		session.cheat = cheat.NewSession(cheatTarget{client: session.Client})
	}
	return session.cheat
}

// CheatConsole returns the text command console bound to Cheat().
func (session *Session) CheatConsole() *cheat.Console {
	if session == nil || session.Client == nil {
		return nil
	}
	if session.cheatConsole == nil {
		session.cheatConsole = cheat.NewConsole(session.Cheat())
		// A saved table names the game it was made against, which is the only
		// thing that lets one be placed months later.
		if session.Archive != nil {
			session.cheatConsole.SetGame(session.Archive.Descriptor.MainClass)
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
		return fmt.Errorf("KTF cheat freeze failed at %#x", failed[0])
	}
	return nil
}

// frozenAddresses lists the guest addresses the cheat engine rewrites every
// tick. The collector treats them as roots: a freeze on an object's field has
// to keep that object alive, or the rewrite would land in whatever took its
// place.
func (session *Session) frozenAddresses() []uint32 {
	if session == nil || session.cheat == nil {
		return nil
	}
	entries := session.cheat.Freezes().Entries()
	addresses := make([]uint32, 0, len(entries))
	for _, entry := range entries {
		addresses = append(addresses, entry.Address)
	}
	return addresses
}

// collectGuestObjects reclaims dead guest objects at the end of a service
// round, where no guest code is running on this goroutine.
func (session *Session) collectGuestObjects() error {
	if session == nil || session.Client == nil {
		return nil
	}
	_, err := session.Client.CollectGuestObjects(session.frozenAddresses())
	return err
}
