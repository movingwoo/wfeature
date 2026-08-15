// Package licenses carries the project's licence and its third-party notices
// as files embedded in every binary.
//
// A release here is one executable per operating system: the user downloads it
// and runs it. Notices that live only in the repository never reach that user,
// and some of the bundled components ask to be passed along — the MIT terms
// hqx's translated tables arrive under, and the Open Font License each of the
// embedded fonts carries. So the notices travel inside the binary and are reachable
// without one, through `wfeature licenses` and the server's `/licenses`.
package licenses

import _ "embed"

// Project is this project's own licence.
//
//go:embed LICENSE
var Project string

// ThirdParty reproduces the licence of every bundled third-party component.
//
//go:embed THIRD-PARTY-NOTICES.md
var ThirdParty string
