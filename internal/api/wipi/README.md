# WIPI Java class library

`definitions.go` declares the `org.kwis.msp` and `org.kwis.msf` surface in Go,
the way `internal/api/midp` declares MIDP's: class metadata with the Go body of
every method this layer implements itself, no Java sources and no JDK in the
build. `Define` installs it on a VM that already has MIDP, because every class
here names a MIDP class as its superclass.

It exists because this vendor's container carries two kinds of program. Most
archives hold a MIDlet; a few hold a Jlet, which is the same packaging with a
WIPI application class inside. Rather than a second runtime, each class here is
its MIDP counterpart plus what the WIPI standard has and MIDP does not — the
package comment in `library.go` has the reasoning, and `docs/skvm.md`, "Three
archives hold a Jlet, not a MIDlet", has what it took to make three real
titles run.

Everything that touches the Host — the card stack, the pixels, the save
directory, the sound — is declared native here and registered by
`internal/platform/skt/wipi.go`.
