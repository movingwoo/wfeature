// Package armcheat attaches the cheat engine to a guest that runs on the ARM
// core. The core's flat address space is what a scan sweeps and a freeze
// rewrites, and its store instrumentation is what a watch reports, so every
// platform built on the core answers the engine the same way. What differs
// between them is only how their memory map is named, which each platform
// gives as a Label.
package armcheat

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/cheat"
)

// Target is the cheat engine's view of one guest. Scans cover only committed
// writable pages: game state the guest can mutate lives there, and the lazily
// committed reservations would make every sweep orders of magnitude slower.
//
// Core answers the core the guest runs on at the moment of the call rather
// than at the moment the target was made, because a platform may hand its
// client a different core and the cheat engine has to follow it. Label names
// a committed region by where it starts and says whether it holds
// instructions rather than state. Adjacent pages are reported as one region,
// so a label has to be right for the run that starts there and not only for
// its first page.
type Target struct {
	Core  func() *armcore.Core
	Label func(base uint32) (label string, code bool)
}

func (target Target) ReadMemory(address uint32, destination []byte) error {
	return target.Core().Memory().Read(address, destination)
}

// WriteMemory deliberately does not go through the watched write path: the
// engine's own freeze rewrites land every tick, and recording them would make
// the cheat the loudest writer of every address it holds.
func (target Target) WriteMemory(address uint32, data []byte) error {
	return target.Core().Memory().WriteUntracked(address, data)
}

func (target Target) Regions() []cheat.Region {
	committed := target.Core().Memory().CommittedRegions(armcore.PermissionWrite)
	regions := make([]cheat.Region, 0, len(committed))
	for _, region := range committed {
		label, code := target.Label(region.Base)
		regions = append(regions, cheat.Region{
			Base:  region.Base,
			Size:  uint32(min(region.Size, 0xffffffff)),
			Label: label,
			Code:  code,
		})
	}
	return regions
}

// The watch methods forward to the core's store instrumentation, which is what
// makes a Target a cheat.WatchTarget.
func (target Target) Watch(address uint32)   { target.Core().Watch(address) }
func (target Target) Unwatch(address uint32) { target.Core().Unwatch(address) }
func (target Target) ClearWatches()          { target.Core().ClearWatches() }
func (target Target) Watches() []uint32      { return target.Core().Watches() }

func (target Target) WatchHits() []cheat.WatchHit {
	hits := target.Core().WatchHits()
	converted := make([]cheat.WatchHit, len(hits))
	for index, hit := range hits {
		converted[index] = cheat.WatchHit{
			Address: hit.Address, PC: hit.PC, Origin: writeOrigin(hit.Origin),
			Value: hit.Value, Size: hit.Size, Count: hit.Count,
		}
	}
	return converted
}

func (target Target) WatchHitsOverflowed() bool { return target.Core().WatchHitsOverflowed() }

// writeOrigin translates the core's answer for the cheat engine, which is kept
// free of the ARM core so a platform without one can still hold a session.
// Translating rather than sharing the type is what keeps that true.
func writeOrigin(origin armcore.WriteOrigin) cheat.WriteOrigin {
	if origin == armcore.OriginHost {
		return cheat.OriginHost
	}
	return cheat.OriginGuest
}

// ImageHash is the SHA-256 of the code a session loaded, in lower-case hex. It
// identifies what is running across the archives a title arrives in:
// repackaging changes the file and leaves the image alone. No bytes answer
// nothing rather than the hash of an empty slice, so a caller can tell "no
// image" from "an image of no bytes".
func ImageHash(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// NewConsole is the text command console for engine. A saved table names the
// game it was made against, which is the only thing that lets one be placed
// months later. The name is the label; the key beside it is the hash of the
// image actually loaded, because an address — and a byte patch above all — is
// true of the image and not of what the archive around it was called.
func NewConsole(engine *cheat.Session, game, image string) *cheat.Console {
	console := cheat.NewConsole(engine)
	console.SetGame(game)
	console.SetTableKey(cheat.TableKey{Image: image})
	return console
}

// Reapply rewrites engine's frozen values after a service round, so they win
// over whatever the game just wrote. A freeze that fails is an error naming
// the platform, because a cheat that silently stops applying is worse than a
// visible fault. A session with no engine yet has nothing to rewrite.
func Reapply(engine *cheat.Session, platform string) error {
	if engine == nil {
		return nil
	}
	if failed := engine.Tick(); len(failed) > 0 {
		return fmt.Errorf("%s cheat freeze failed at %#x", platform, failed[0])
	}
	return nil
}
