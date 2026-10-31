package ktf

import (
	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/cheat"
	"github.com/movingwoo/wfeature/internal/platform/armcheat"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
)

// The earlier package runs on a core of its own, and a core is an address
// space a scan can sweep, so the cheat engine attaches here the same way it
// does to the descriptor package's client. What differs is only the map: this
// title keeps its whole state in what the platform allocated for it, so the
// arena is where a search for a health bar will find one, and the labels say
// so.
func (client *NativeClient) cheatTarget() armcheat.Target {
	return armcheat.Target{Core: func() *armcore.Core { return client.core }, Label: client.cheatLabel}
}

// cheatLabel names a region by where it starts and says whether it holds
// instructions rather than state. Adjacent pages are reported as one region,
// so a label has to be right for the run rather than for the page: the word
// the loader plants sits immediately below the module and comes back merged
// with it, and the entry scratch sits immediately below the arena.
//
// Only the stub arena is code a scan can be kept out of. The module is one
// span of instructions and data with no boundary recorded between them, and it
// stays swept for that reason; it also stays writable, which is what a byte
// patch into a check needs.
func (client *NativeClient) cheatLabel(base uint32) (string, bool) {
	switch {
	case base == nativeHeaderBase || (base >= wipicrt.ImageBase && uint64(base) < uint64(wipicrt.ImageBase)+uint64(client.mapped)):
		return "module", false
	case uint64(base) >= uint64(wipicrt.ThreadStackBase) && uint64(base) < uint64(wipicrt.ThreadStackBase)+wipicrt.ThreadStackSize:
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

// Cheat returns the session's attached cheat engine, creating it on first use.
// ImageHash is the SHA-256 of the module this session loaded, in lower-case
// hex — the same identity the other generation of package answers with, so a
// cheat table is keyed the same way whichever one a title arrived as.
func (session *NativeSession) ImageHash() string {
	if session == nil || session.Client == nil || session.Client.archive == nil {
		return ""
	}
	return armcheat.ImageHash(session.Client.archive.Module)
}

// Tick reapplies its frozen values after every frame.
func (session *NativeSession) Cheat() *cheat.Session {
	if session == nil || session.Client == nil {
		return nil
	}
	if session.cheat == nil {
		session.cheat = cheat.NewSession(session.Client.cheatTarget())
	}
	return session.cheat
}

// CheatConsole returns the text command console bound to Cheat().
func (session *NativeSession) CheatConsole() *cheat.Console {
	if session == nil || session.Client == nil {
		return nil
	}
	if session.cheatConsole == nil {
		// There is no main class here, so the title's own name is what names a
		// saved table; the key beside it is the hash of the module actually
		// loaded, which is what a byte patch is true of whatever the package
		// around it was called.
		session.cheatConsole = armcheat.NewConsole(session.Cheat(), session.Name(), session.ImageHash())
	}
	return session.cheatConsole
}

// serviceCheat rewrites frozen values after a frame so they win over whatever
// the title just wrote.
func (session *NativeSession) serviceCheat() error {
	return armcheat.Reapply(session.cheat, "KTF native")
}
