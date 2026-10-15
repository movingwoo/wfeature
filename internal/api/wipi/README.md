# WIPI Java class library

`definitions.go` declares the `org.kwis.msp` and `org.kwis.msf` surface in Go,
the way `internal/api/midp` declares MIDP's: class metadata with the Go body of
every method this layer implements itself, no Java sources and no JDK in the
build. `Define` (`library.go`) installs it on a VM that already has MIDP,
because every class here names a MIDP class as its superclass. `IsJlet` tells a
Host whether an archive's main class is a WIPI application rather than a MIDlet,
since the two are packaged identically.

It exists because this vendor's container carries two kinds of program: most
archives hold a MIDlet, and a few hold a Jlet, the same packaging with a WIPI
application class inside. Rather than a second runtime, each class here is its
MIDP counterpart plus what the WIPI standard has and MIDP does not. The package
comment in `library.go` has the reasoning, and
[the SKT history](../../../docs/history/skt.md#implementation-three-archives-hold-a-jlet-not-a-midlet),
"Three archives hold a Jlet, not a MIDlet", has what it took to make three real
titles run.

| Package | Classes |
|---|---|
| `org.kwis.msp.lcdui` | `Jlet`, `Card`, `Display`, `Graphics`, `Image`, `Font`, `EventQueue` |
| `org.kwis.msp.handset` | `BackLight` |
| `org.kwis.msp.io` | `File`, `FileSystem` |
| `org.kwis.msp.media` | `Clip`, `Player`, `PlayListener`, `Vibrator` |
| `org.kwis.msf.io` | `Network`, `Socket`, `URL` |

Everything that touches the Host (the card stack, the pixels, the save
directory, the sound) is declared native here and registered by
`internal/platform/skt`, in `wipi.go` and the `wipi_*.go` files beside it.
`Vibrator.on` and `off` record the request on the `backend.Vibrator` passed to
`Define`; a nil one makes them no-ops. The `org.kwis.msf.io` classes connect
nothing: `Network.connect` answers -1, and `Socket` streams and `URL.find` throw
`IOException`, so a title reaches its own error path (see
[`docs/network.md`](../../../docs/network.md)).
