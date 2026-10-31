package ktf

import (
	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/cheat"
	"github.com/movingwoo/wfeature/internal/platform/armcheat"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
)

// cheatTarget adapts the client's flat guest address space to the cheat
// engine. The core is asked for on every call, so the target follows the
// client to whatever core it runs on.
func (client *Client) cheatTarget() armcheat.Target {
	return armcheat.Target{Core: func() *armcore.Core { return client.Core() }, Label: client.cheatLabel}
}

// cheatLabel names a region and says whether it holds instructions rather
// than state. The two answers come together because the same range decides
// both.
//
// Only the stub arena can be told apart here. The client image is one span of
// code, read-only data and initialized data with no boundary recorded between
// them — the archive carries a length and a BSS size and nothing else — so a
// scan sweeps it whole and a search that lands in its code is narrowed the way
// any other coincidence is. Splitting it would need section information the
// image does not carry.
func (client *Client) cheatLabel(base uint32) (string, bool) {
	image := client.image
	return wipicrt.CheatLabel(base, base >= wipicrt.ImageBase && uint64(base) < uint64(wipicrt.ImageBase)+uint64(len(image.Data))+uint64(image.BSSSize))
}

// ImageHash is the SHA-256 of the client image this session loaded, in
// lower-case hex. It identifies what is running across the archives a title
// arrives in: repackaging changes the file and leaves the image alone.
func (session *Session) ImageHash() string {
	if session == nil || session.Client == nil {
		return ""
	}
	return armcheat.ImageHash(session.Client.image.Data)
}

// Cheat returns the session's attached cheat engine, creating it on first
// use. Session.Tick reapplies its frozen values after every service round. A
// platform without an emulated address space has no watch to forward to, and
// its sessions answer cheat.ErrWatchUnsupported instead.
func (session *Session) Cheat() *cheat.Session {
	if session == nil || session.Client == nil {
		return nil
	}
	if session.cheat == nil {
		session.cheat = cheat.NewSession(session.Client.cheatTarget())
	}
	return session.cheat
}

// CheatConsole returns the text command console bound to Cheat().
func (session *Session) CheatConsole() *cheat.Console {
	if session == nil || session.Client == nil {
		return nil
	}
	if session.cheatConsole == nil {
		var game string
		if session.Archive != nil {
			game = session.Archive.Descriptor.MainClass
		}
		session.cheatConsole = armcheat.NewConsole(session.Cheat(), game, session.ImageHash())
	}
	return session.cheatConsole
}

// serviceCheat rewrites frozen values after a tick so they win over whatever
// the game just wrote. Freeze failures surface as an error because a cheat
// that silently stops applying is worse than a visible fault.
func (session *Session) serviceCheat() error {
	return armcheat.Reapply(session.cheat, "KTF")
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
