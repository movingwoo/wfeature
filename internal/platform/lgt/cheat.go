package lgt

import (
	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/cheat"
	"github.com/movingwoo/wfeature/internal/platform/armcheat"
)

// cheatTarget adapts the client's guest address space to the cheat engine,
// with the adapter every platform on the ARM core shares: the engine asks the
// same three questions of all of them — what can be read, what can be
// written, and which regions are worth sweeping.
func (client *Client) cheatTarget() armcheat.Target {
	return armcheat.Target{Core: func() *armcore.Core { return client.core }, Label: client.cheatLabel}
}

// cheatLabel names a region so a hit reads as something. Unlike the other
// platform the module is not at a fixed base — an ELF keeps whatever addresses
// it names — so the module's own span is asked of the loaded module rather
// than compared against a constant. Nothing here is marked as code a scan
// keeps out of.
func (client *Client) cheatLabel(base uint32) (string, bool) {
	low, high := client.module.Span()
	switch {
	case base >= low && base < high:
		return "module", false
	case uint64(base) >= uint64(heapBase) && uint64(base) < uint64(heapBase)+heapSize:
		return "heap", false
	case uint64(base) >= uint64(platformCodeBase) && uint64(base) < uint64(platformCodeBase)+platformCodeSize:
		return "stubs", false
	case uint64(base) >= uint64(platformDataBase) && uint64(base) < uint64(platformDataBase)+platformDataSize:
		return "platform", false
	case uint64(base) >= uint64(stackBase) && uint64(base) < uint64(stackBase)+stackSize:
		return "stack", false
	default:
		return "data", false
	}
}

// ImageHash is the SHA-256 of the executable image this session loaded, in
// lower-case hex. It identifies what is running across the archives a title
// arrives in: repackaging changes the file and leaves the image alone, and an
// address — a byte patch above all — is true of the image rather than of what
// the container around it was called.
//
// This platform has exactly one such image, the module inside the JAR, which
// is what makes the answer meaningful here. A platform whose title is a bag of
// class files has no single loaded image, and the file it was read from is
// what identifies it instead; that is not a gap to be filled in with a hash of
// something arbitrary.
func (session *Session) ImageHash() string {
	if session == nil || session.archive == nil {
		return ""
	}
	return armcheat.ImageHash(session.archive.Module)
}

// Cheat returns the session's attached cheat engine, creating it on first use.
// Session.Tick reapplies its frozen values after every round.
func (session *Session) Cheat() *cheat.Session {
	if session == nil || session.client == nil {
		return nil
	}
	if session.cheat == nil {
		session.cheat = cheat.NewSession(session.client.cheatTarget())
	}
	return session.cheat
}

// CheatConsole returns the text command console bound to Cheat().
func (session *Session) CheatConsole() *cheat.Console {
	if session == nil || session.client == nil {
		return nil
	}
	if session.cheatConsole == nil {
		// This platform gives every title its own PID, so that is the name a
		// saved table is worth saving under. The key beside it is the hash of
		// the image actually loaded, so a table made against one packaging of a
		// title is found again by another. The Host fills in the other half of
		// the key, which is the file it read.
		var game string
		if session.archive != nil {
			game = SaveOwner(session.archive.Descriptor)
		}
		session.cheatConsole = armcheat.NewConsole(session.Cheat(), game, session.ImageHash())
	}
	return session.cheatConsole
}

// serviceCheat rewrites frozen values after a tick so they win over whatever
// the game just wrote. Freeze failures surface as an error because a cheat
// that silently stops applying is worse than a visible fault.
func (session *Session) serviceCheat() error {
	return armcheat.Reapply(session.cheat, "LGT")
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

// collectJavaObjects reclaims dead Java objects at the end of a service round,
// where no platform call is running on this goroutine.
func (session *Session) collectJavaObjects() error {
	if session == nil || session.client == nil {
		return nil
	}
	_, err := session.client.CollectJavaObjects(session.frozenAddresses())
	return err
}
