# SGS script identity and host role

Static inspection distinguishes three independent values: the device MIN, the
current script record's `UserID`, and the caller/receiver role of a 1:1 session.
The names below describe observed storage and transitions; they are not claims
that these services implement a WIPI-standard identity API. Native addresses
identify evidence in the original runtime. No original code or assets are
included here.

## Resource services

| Opcode | Entry | Input | Result |
|---|---|---|---|
| `0x52` | `0x418a50` | Destination resource ID | Copy device MIN with trailing NUL; pop the ID |
| `0x53` | `0x418ad0` | Destination resource ID | Copy current script `UserID` with trailing NUL; replace the ID with the unsigned role byte |

Both handlers request enough resource storage for the complete string plus NUL.
The second service reads its string at `0x527a2e` and its return byte at
`0x527a01`. It does not return the copied string's length or an allocation-success
boolean. A safe implementation must validate the resource and allocate before
writing; the original unchecked pointer operations are not a host contract.

For `0x52`, initialization at `0x411bd0` calls `0x40c7d0` with destination
`0x527a6c`. That reader obtains the `MIN` key from the `NV_ROM` configuration
section. This is an original PC player's configured mobile identifier, separate
from the per-script field consumed by `0x53`.

## Where UserID comes from

The installed-script metadata reader at `0x40b0e0` reads sections named
`NV_ROM%02d`. Its `UserID` key lookup at `0x40b1c3` uses the key string at
`0x48c218` and stores the result at record offset `0x26`. The record is `0x62`
bytes long; its copy to the caller is at `0x40b400`–`0x40b427`.

Stored-script startup at `0x413560` reads that record into `0x527a08`, making
`0x527a2e` its `UserID` field. Direct PC import also reads `System`/`UserID` at
`0x409e58`–`0x409e9e`, then copies the temporary record into the same current
record at `0x409f60`–`0x409f70`. The next field, the two-byte `ScriptVer`, starts
at offset `0x30`. Thus the inspected record reserves ten bytes for the UserID
string, including its terminator. This is a record-layout bound, not an inferred
universal limit for an unspecified identity protocol.

The network/download setup function at `0x412400` copies its first string
argument into pending metadata at `0x526c28`. Download startup at `0x411310`
validates script identifiers and size, and copies that pending UserID to the
current field at `0x4113c9`–`0x4113ee`. Services `0xc3` (`0x41c690`) and `0xda`
(`0x41d210`) pass the current UserID into this setup path. The packet construction
path at `0x410d90` also prefixes an outgoing supplied string with the current
UserID and a colon. None of these observations identifies the field as a remote
peer address, account authentication result, or device MIN.

## Role byte and host transitions

The role byte is zero for the outgoing side and one for the incoming side.
The original menu's outgoing action enters host state 4 at `0x414ef0`, using
handler `0x415190`; the incoming action enters state 5 at `0x414ecd`, using
handler `0x4153e0`. The UI render path at `0x414f70` distinguishes an outgoing
number prompt, dialing, and waiting for an incoming 1:1 call. These labels and
the corresponding handlers establish the direction; the byte is not a generic
connected/disconnected flag.

The outer host dispatcher at `0x412aa5` handles event 12. On its success branch,
the table at `0x412fdc` maps the preceding host state as follows:

| Host state | Role write | Role | Continuation |
|---|---|---|---|
| 4 | `0x412acf` | 0, outgoing | `0x4154a0(0, 0, 0)` |
| 5 | `0x412aef` | 1, incoming | `0x4154a0(0, 0, 0)` |
| 6 | `0x412b10` | 0, outgoing | `0x4154a0(0, 1, 0)` |
| 7 | `0x412b18` | 1, incoming | `0x4154a0(0, 1, 0)` |

Continuation services `0xd6` (`0x41d0a0`) and `0xd7` (`0x41d120`) select state
6 and host actions 13 and 14. Service `0xd8` (`0x41d170`) selects state 7 and
host action 15. These transitions are conditional on runtime mode 1, stop the
script timers, and yield control. They corroborate the outgoing/incoming role
mapping without establishing every external transport action's full protocol.

The callback at `0x4154a0` sets runtime mode `0x52791c` to 2 before starting the
VM at `0x4154f2`, or before notifying a continuing VM with system event 12 at
`0x4154e0`. The initializer at `0x41df90` exposes that mode through system
variable 0 before running the module's initial entry. Role and runtime mode are
distinct values and must not share storage or interpretation.

## Standalone behavior and remaining limits

Standalone startup explicitly writes role **1** at `0x413a20`–`0x413a28` and
calls `0x4154a0(0, 0, 0)`. Consequently its initial role is one and its initial
runtime mode, including system variable 0, is two, even without a remote peer.
This concrete local startup path is evidence for those defaults; it does not
prove that a local session has accepted a network connection.

The emulator exposes this standalone mode during initialization and through
`0xd2`. A later event replaces system variable 0 with its event parameter, but
does not change the runtime mode returned by `0xd2`. The outgoing/incoming role
remains separate and no communication transition is implied.

The UserID comes from metadata in that path. An archive without this metadata
does not establish a replacement identity. An empty host-provided value must
remain an explicit compatibility choice, not be presented as a value read from
the archive. The device MIN likewise needs a host configuration source if it is
to be nonempty. No phone number, account, or identity should be synthesized from
an archive title or directory name.

The inspected transitions do not establish a complete remote transport,
authentication, or disconnection protocol. Implementing local resource services
and startup defaults alone does not implement the 1:1 host mode transitions.
