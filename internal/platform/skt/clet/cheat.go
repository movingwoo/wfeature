package clet

import (
	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/cheat"
	"github.com/movingwoo/wfeature/internal/platform/armcheat"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
)

// A Clet runs on the ARM core like every other native title, so the cheat
// engine attaches to it with the adapter they all share: its flat address
// space is what a scan sweeps, and the core's store instrumentation is what a
// watch reports.
func (client *Client) cheatTarget() armcheat.Target {
	return armcheat.Target{Core: func() *armcore.Core { return client.Core() }, Label: client.cheatLabel}
}

// cheatLabel names a region and says whether it holds instructions rather
// than state. The program's code (with its read-only data) and its data with
// the BSS are both the title's own and both are swept: the loader maps them
// side by side, and adjacent pages come back as one region, so a region that
// starts in the code may run on into the data a search is looking for. The
// rest of the map is the engine's, named the way a KTF title's is.
func (client *Client) cheatLabel(base uint32) (string, bool) {
	program := client.clet.program
	code := base >= ImageBase && uint64(base) < uint64(ImageBase)+uint64(len(program.Code))
	data := base >= program.DataAddress && uint64(base) < uint64(program.DataAddress)+uint64(len(program.Data))+uint64(program.BSSSize)
	return wipicrt.CheatLabel(base, code || data)
}

// ImageHash is the SHA-256 of the code this session loaded, in lower-case hex:
// the same identity the code a checkpoint is bound to has, and what a saved
// cheat table is keyed by.
func (session *Session) ImageHash() string {
	if session == nil || session.Client == nil {
		return ""
	}
	return armcheat.ImageHash(session.Client.image.Data)
}

// Cheat returns the session's attached cheat engine, creating it on first use.
// Each service round reapplies its frozen values.
func (session *Session) Cheat() *cheat.Session {
	if session == nil || session.Client == nil {
		return nil
	}
	if session.cheat == nil {
		session.cheat = cheat.NewSession(session.Client.cheatTarget())
	}
	return session.cheat
}

// CheatConsole returns the text command console bound to Cheat(). A Clet has
// no main class, so a saved table is named after the program.
func (session *Session) CheatConsole() *cheat.Console {
	if session == nil || session.Client == nil {
		return nil
	}
	if session.cheatConsole == nil {
		session.cheatConsole = armcheat.NewConsole(session.Cheat(), session.Client.clet.program.Name, session.ImageHash())
	}
	return session.cheatConsole
}

// serviceCheat rewrites frozen values after a service round so they win over
// whatever the program just wrote.
func (session *Session) serviceCheat() error {
	return armcheat.Reapply(session.cheat, "SKT Clet")
}
