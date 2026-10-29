# Network

Nothing in this emulator reaches a network. That is one decision applied across
every platform; this document owns the decision and its surface, and each
platform document links here rather than restating it.

Offline authentication compatibility (requested 2026-09-11) adds recognized
certificate, identity and license adapters, and answers three recognized
exchanges in process: an LGT notification/empty-save responder, an LGT
two-stage certificate message and a KTF slot service. External networking stays
disabled. [authentication.md](authentication.md) separates the recognized
schemes and their boundaries;
[authentication case history](history/authentication.md#cases-authentication-case-inventory)
records the corpus inventory, controlled experiments and unresolved cases.

## The decision

Every connection a game asks for is **refused**, through a return value, guest
exception or deferred callback. With authentication compatibility (the default),
the recognized LGT notification and certificate-message contracts use the
bounded in-process responder described below. A KTF title built on the
recognized relay middleware reaches the local slot service regardless of that
setting. All other connections reach the guest's failure path.

An unconditional success response is worse than it looks. A game told a
connection opened waits for bytes inside a screen it leaves only when a read
completes, a state its author never saw: a handset with no coverage, or a
subscriber who declined the data charge, refused the connection outright, so
the refusal path is the one the original developer tested. Faking success
invents a state nobody wrote code for; refusing walks a path that already works.

Refusal is also better than absence. A missing class or an unimplemented slot
stops the session with a host error that no guest `catch` can see; a refusal is
something the game handles.

## The surface

| Platform | Entry point | Answer |
| --- | --- | --- |
| KTF | `org.kwis.msf.io.Network.connect` | `-1`; `0` for a title with the recognized relay middleware |
| KTF | `org.kwis.msf.io.Network.disconnect` | accepted no-op; closes the local slot socket if one is open |
| KTF | `org.kwis.msf.io.URL.find` | the in-process slot socket for the recognized relay endpoint once connected; otherwise `SchemeNotFoundException` |
| KTF | WIPI C net table, `close` | success, and drops pending connect callbacks — closing what never opened is not a failure |
| KTF | WIPI C net table, `connect` | accepts a callback request (at most 16 pending), then reports failure through it; without a callback or with a full queue, refuses immediately |
| KTF | WIPI C net table, everything else | `0xffffffff` |
| KTF | `MC_knlGetAccessLevel` | `0xff`: all API groups are permitted; individual network operations still fail |
| KTF native | BREW network manager, `OpenSocket` | a socket, one object for every socket |
| KTF native | BREW socket, `Connect`, `Read`, `Write` | `AEE_NET_ERROR` at once; `GetLastError` answers `AEE_NET_ENETDOWN` |
| KTF native | BREW network manager, `GetHostByName` | never answered, as out of coverage |
| LGT | `MC_netConnect` | accepted within a 64-request bound, then fails through its callback; recognized local notification dials report local readiness |
| LGT | `MC_netClose` | success, because the call returns void; drops unreported dials |
| LGT | the rest of the net block — socket create (`0x25a` and `0x7d0`), connect/write/read/close, both callback setters | error, except the recognized local notification/empty-save socket |
| LGT | the net block's other 21 calls — bind, packet length, send-to, receive-from, host lookup, accept (`0x25f`–`0x264`) and the HTTP calls (`0x267`–`0x275`) | `M_E_ERROR`, as KTF's table answers them; an error return runs no callback |
| LGT | `org.kwis.msf.io.Network.connect` | `-1`; one title disagrees, see below |
| LGT | `org.kwis.msf.io.URL.find` | `SchemeNotFoundException`, the failure the specification names |
| SKT | `SMS.send`, `Call.call` | `false` |
| SKT | `PhoneBook` reads | empty, null or zero |
| SKT | the WAP browser entry point | accepted and discarded |
| MIDP | `Connector.open` and the four stream helpers | `ConnectionNotFoundException` |
| MIDP | a network locator in `Manager.createPlayer` | `MediaException` |

The following refusal details describe the default path.

**The whole block refuses, not only the connect call.** A title refused a
connection still tears down what it had started. Stopping it at the teardown
with an unimplemented error turns a handled refusal into a crash, so close,
read, write and the callback setters all answer rather than fall through.

**Refusing the callback setters keeps a game moving.** A registered read
callback is how a game waits asynchronously; refusing the registration means it
never enters the wait.

**One call cannot be refused by its return value.** LGT's `MC_netConnect` is

```c
M_Int32 MC_netConnect(NETCONNECTCB cb, void *param)
typedef void (*NETCONNECTCB)(M_Int32 error, void *param)
```

and the specification is explicit that a zero return hands the answer to the
callback, while an error return means the callback is never called. A title
that checks the return sees a refusal by return value, but one that waits on
the callback — the way the asynchronous half of this API is meant to be used —
waits forever, and a local title does exactly that.

The dial is therefore accepted and then fails, as on a handset with no
coverage: the attempt starts, the radio finds nothing, and the callback reports
`M_E_ERROR`. Both kinds of caller reach the same offline path. KTF's C net table
does the same; a caller that passes no callback keeps the synchronous error,
because there is nowhere else to deliver it.

On LGT the failure arrives 200 ms of guest time (four ticks) after the dial
(`netConnectFailureDelay`) rather than immediately. A callback that fires before
the caller has entered its own connecting state reaches a state machine that is
not listening yet, and the game then waits for a second one that never comes.
`MC_netClose` drops a dial that has not reported, because it withdraws the
question.

`internal/platform/lgt/wipic_net.go` and `internal/platform/ktf/wipic_net.go`
hold this. Callbacks run from `Session.Tick`, between guest calls and never
inside one, where timers fire.

**`MC_knlGetAccessLevel` reports API permission, not connectivity.** All API
groups are permitted (`0xff`), so a game may attempt a dial and the network
operation itself supplies the refusal. Withholding the network and serial bits
was tried and ended one title at its own startup error screen: it requires
`& 0xbc` to equal `0xbc` and never reaches a call that could be refused. With
every group granted, every other local title drew the same frames and made no
new network call (`wipicAccessLevel` in `internal/platform/ktf/initialization.go`).

**A BREW socket is created, then refused.** The earlier KTF native package is
BREW ([ktf.md](ktf.md)). One later title opens a socket through BREW's network
manager while it starts and loads its pictures only once it has one, so a
refused socket ended its start-up. Creating a socket reaches nothing: the
manager hands one out, and Connect, Read and Write answer `AEE_NET_ERROR` at
once, with GetLastError naming `AEE_NET_ENETDOWN`, a handset's answer with no
network. Connect refuses by its return value because BREW calls its callback
only for a connection it started. A lookup is never answered, as one out of
coverage is not; the title that asks gives up by itself after 8.7 seconds of
guest time and says its server could not be reached, and answering it would
take a platform call into the guest that nothing else in the package needs.
`internal/platform/ktf/native_network.go` holds this
([history](history/ktf.md#ktf-new-archives-2026-10-21-network)).

## Local LGT protocol compatibility

The automatically selected `lgt-offline-notification` status selects a contract
recognized from connected Thumb code and referenced protocol fields. It handles
only the guest's explicit Yes/No notification, an empty remote-save lookup and
that query's finish. It never contacts the stored endpoint, sends SMS or
transfers a save; ordinary guest file writes are unchanged.
[authentication.md](authentication.md#lgt-local-notification-and-empty-remote-save-service)
records recognition, response bytes, limits and the user-visible restart flow.

The separately recognized `lgt-offline-authentication` contract answers one
exact 60-byte binary request with its eight-byte completion frame. Its packet
builder, command dispatcher, callbacks, endpoint, application token and version
must all agree, and the request must match the session identity and handset
model. The stored endpoint is compared but never contacted, and the guest owns
every preceding and following consent choice. See
[the binary authentication boundary](authentication.md#lgt-local-binary-authentication).

The [WIPI C network specification](https://mirusu400.github.io/wipi-wiki/c-api/network.md)
defines socket callbacks as `(fd, error, param)`, including the fifth stack
argument of `MC_netSocketConnect`. Read/write callbacks are one-shot, and
closing cancels them. The local implementation follows those contracts, invokes
guest callbacks between guest calls, and bounds fragmented requests and queued
work; unknown requests fail instead of waiting for invented remote data.

## The Generic Connection Framework

`javax.microedition.io` is declared in `internal/api/midp/definitions.go` and
installed by `midp.Define`. The Go side is `internal/platform/skt/connector.go`.

`Connector` is native on all seven entry points, and every one raises the same
`ConnectionNotFoundException`, carrying the requested name as its message. It
extends `java.io.IOException`, the catch most titles write: they wrap the whole
attempt in one `catch (IOException)` and never name the specific type.

The connection interfaces — `Connection`, `InputConnection`,
`OutputConnection`, `StreamConnection`, `ContentConnection`,
`StreamConnectionNotifier`, `HttpConnection`, `SocketConnection` — carry no
implementation, and none is ever handed out. They exist so that a game which
declares a field of one of those types, or catches on one, resolves the name
instead of meeting the loader's "class not found". `HttpConnection` keeps its
full constant set, because a game reads those constants while building the
request it is about to be refused.

**The SKT vendor's own two network names are deliberately not declared.** One
local title sets a browser network mode and a socket's PPP preserve time before
connecting — `com.xce.jam.XBrowser.setNetworkMode` and
`com.xce.net.Socket.setPPPPreserveTime`. It never reaches either here: the call
site is behind a check on the `m.SK_VM` property, and this runtime answers the
value that skips it. Declaring the two classes would put a `Socket` in front of
a title that cannot have one, so the gate is documented instead. If that
property's answer changes, both become reachable in the same session;
[the two entries the scan still lists](history/skt.md#implementation-the-two-entries-the-scan-still-lists-and-why-neither-is-work)
records the scan's view of them.

## System properties are not part of the refusal

`internal/wipic/properties.go` answers `RSSILEVEL`, `AIRPLANE_MODE`,
`ROAMING_AREA`, `PHONENUMBER` and the rest with a working handset's values, not
with values chosen to look offline.

This is deliberate. Answering `PHONENUMBER` with an empty string was meant to
push a title onto its offline path; instead a title that takes the last four
digits off the number asked to copy minus four bytes and died during its own
startup. A handset never answers with an empty string. Nothing goes online
either way, so the properties describe a handset and the refusal happens where
the connection is attempted.

### The subscriber number is the one property worth changing

The automatic authentication adapter isolates this choice per KTF session and
uses the recognized accessor's full embedded fallback; the short-number sweep
below did not test that fallback, which has since passed directly. See
[KTF certificates and identity](authentication.md#ktf-certificates-and-identity).
Network refusal is unchanged.

`PHONENUMBER` and `MIN` answer `01000000000` by default. The default is a
compromise, because **two local titles want opposite things from the same
number**:

| Answer | The title that gates on billing | The title that checks a certificate |
|---|---|---|
| `0`–`4` digits | opens its packaged data and plays | "인증오류 (3001)" and stops |
| `5`–`11` digits | offers to download data it already ships | reaches its title screen |
| empty | — | — (and a third title dies in its own startup) |

The first title's predicate is the **length** alone: `911` passes and `01000`
does not, so it is not the value, a prefix or a match against stored data. Five
digits or more is a subscriber it can bill; it then checks a receipt issued to
the handset that paid, which is not this one. The sweep is in
[a second download gate](history/ktf.md#implementation-a-second-download-gate-and-the-answer-that-opened-it).

The second title compares the number against the one sealed into the
certificate `wfeature provision` writes, and refuses a short one outright.

The two ranges do not overlap, so the number is a setting rather than a
constant:

```sh
WFEATURE_PHONE_NUMBER=9999 wfeature runktf var/games/ktf/game.zip
wfeature-server -number 9999          # or WFEATURE_PHONE_NUMBER
```

`wipic.SetSubscriberNumber` sets this Host default. Both hosts call it at
startup, before any session exists (the CLI only when the variable is set), and
it accepts digits only, at most eleven — the width of the shortest buffer a
title reads the number into. An empty number is rejected. Sessions snapshot the
default when they start, and a recognized adapter may change only its own
session's snapshot.

Two consequences:

- **A certificate is sealed with the number in force when it was provisioned.**
  Provisioning under a short number produces a certificate that title refuses,
  so provision with the default and play that title with the default.
- **Every other local title is indifferent.** Booting all 61 KTF and LGT
  archives for 600 ticks under both numbers gave the same frame for every one
  except the title above; the four that differed run to run differ under a
  single number too, which is the noise floor rather than an effect.

## A missing server is not a wrong stub

Some titles gate on an authentication server and have no offline path. They
stop on their own network screen and report their own error. A more graceful
refusal cannot move them: the refusal is correct, and the server they want no
longer exists. They are fewer than they look: of three that looked like this in
one sweep, one was waiting for a key, one wanted a missing file operation, and
only the third wanted a server.

### A certificate is the same gate under another name, and it is not a wall

One LGT title asks for a certificate rather than a login: "[인증서가 없습니다]
최초1회만 서버로부터 인증서를 받아옵니다 (통화료10원미만)", with 예 and 아니요
under it. It was first recorded as a wall — answer it and the title ends
itself — but **the wall was the default answer**. 아니요 starts selected, and
declining a certificate closes the title, as on a handset.

**Pressing left moves the selection, and 예 opens the offline path.** The title
calls `MC_netConnect` and `MC_netClose` once each and shows its own dialog,
"에러: 서버와의 접속이 끊어졌습니다", which is dismissable; past it the title
reaches its main menu and difficulty screen. The whole path takes five keys from
a cold save.

**No certificate file is involved.** Over 1,500 ticks the title opens five
files, none of them a certificate; declining the dialog makes no network call
and calls the platform's exit two ticks later. The gate is entirely the title's
own, and behind it is only the dial this platform refuses, which the title
handles.

There is therefore nothing to provide. KTF's mechanism
(`internal/platform/ktf/provision.go`: a certificate sealed with a subscriber
number, placed in the save tree where the title reads it) has no counterpart
here, because this title never looks for a file, and a title that reports its
own network error and continues is the handled case.

**A dialog's default answer is part of what a screen means.** Reading one button
as "the answer" is how a handled path was filed as a dead end; one more key
would have caught it. Press what moves a selection before pressing what takes
it.

### One title does not report its own error, it parks

The decision above assumes the refusal lands where the title has code for it.
One LGT Java title shows that this is not always so, and the two outcomes are
three instructions apart in its code.

Its save-backup routine shows a notice, calls `Network.connect`, and on `-1`
**returns without taking the notice down**: the branch jumps past its own
teardown call to the epilogue. While the notice is up, its card's `paint` draws
the notice instead of the card, which leaves the flag its Jlet loop waits on set
indefinitely
([what that wait is actually testing](history/lgt.md#implementation-what-that-wait-is-actually-testing)).
Nothing is reported: the screen holds one line of text and the game stops
progressing.

Its other path works. Patching `connect` to report success in a running session
sends it to `URL.find("socket://…")` one call later — which is why `find` is
served now — and the `SchemeNotFoundException` this platform raises is
**caught by the title**, which takes its notice down and continues to its title
menu. Same session, same archive, one branch apart.

So answering `-1`, the specification's failure value (`connect` answers 0 when
access already exists, 1 when it was just established and -1 on failure), is
correct by the specification and wrong for this title. Answering 0 would claim a
network this platform does not have and would change the first answer every
other local title receives, so it is not a change to make on one title's
evidence without measuring the others. It is recorded here rather than made.

**A second LGT title shows the same thing through the callback rather than the
return.** Its authentication screen prints `Connect CB Error [-1]` and holds
인증중 ("authenticating") for as long as the run lasts. With the dial's callback
answered as success — and nothing else changed, so every later socket call is
still refused — it asks a question of its own instead: there is no certificate,
shall it connect once to fetch one at the player's expense? Answering *no*
reaches the title screen and the game. The title has a complete offline path
behind a question the refusal never lets it ask.

**If the policy ever changes, these two titles show its shape**: succeed the
dial and refuse everything after it. A third title with the same screen was not
a network case at all — it was waiting for a key — which is why the change is
measured rather than reasoned from the screen.

### The sweep that measured it, and what it found

The change was built and run: the dial's callback answered success instead of
`M_E_ERROR`, with nothing else touched, over all ninety-four local archives at
1,500 ticks each, against the same binary with the refusal in place. Timing
fields were dropped from the comparison, because the callback's own delay moves
the guest clock slightly and that is not the effect under test.

**Two of the ninety-four rows moved, and one of them is a title that works
now.** The other was noise of the shape just described: forty milliseconds of
guest time and forty-seven instructions apart, with the same flush count and
picture.

The title that moved is the one whose own dialog says the connection failed — a
handled path, reached and dismissable. Told the dial succeeded, it went on to
the socket block as designed and died at tick twenty on **slot `0x7d0`**, which
was above every block base the platform knew and unimplemented, so the run
ended instead of the call failing. Fifteen hundred ticks of a title reporting
its own error became twenty ticks and no session.

**The policy stayed, and the measurement named what would have to come first**:
"refuse everything after the dial" holds only for calls that are served, and the
path a successful connect opens reached at least one that was not. The title
with the offline path behind its question needs a route past its menus, so it
was not in this sweep and would have to be driven by hand to be counted.

Both socket-creation slots, `0x7d0` (`slotNetSocket`) and the standard-order
`0x25a`, now resolve in `internal/platform/lgt/wipic.go` and refuse normally;
`TestSocketIsRefusedLikeTheRestOfTheNetworkBlock` verifies the refusal. The
sweep above is historical evidence of the risk, not the outcome on current
code. The 2026-09-11 authentication census repeated the dial-success comparison
on all 109 LGT archives: unconditional dial success can still turn a working
refusal-to-menu route into a connection wait
([authentication case history](history/authentication.md#cases-failure-permits-play)),
and the one case that reached `0x25a` reaches its menu with either answer
([LGT socket prerequisite](authentication.md#lgt-socket-prerequisite)).

## Not implemented

These are recorded rather than served, because nothing in the local archives
reaches them and a stub would be a contract invented for no caller:

- `HttpsConnection`, `SecureConnection` and `SecurityInfo`
- `CommConnection` (serial port)
- `ServerSocketConnection` and `UDPDatagramConnection`
- `DatagramConnection` and `Datagram`
- `PushRegistry` — inbound push is a different feature from the outbound path
  this document covers

## Changing the classes

The framework's classes are Go declarations in `internal/api/midp`, so a change
to one is a change to that package; there are no class files to rebuild.
`internal/tools/javastub` generates the signatures a fixture is compiled
against; see [the fixture guide](history/testing.md#implementation-compiling-a-java-fixture).

## Tests

`internal/platform/lgt/wipic_net_test.go` builds a two-instruction Thumb
callback in guest memory, dials, and checks that nothing is reported before the
delay, that the callback then runs with `M_E_ERROR` exactly once, and that
`MC_netClose` drops an unreported dial. `internal/platform/ktf/wipic_net_test.go`
covers the KTF C table: failure through the given callback, the synchronous
refusal without one, the queue bound, close dropping pending callbacks, and the
rest of the table refusing.

`internal/platform/skt/connector_test.go` drives a fixture MIDlet through every
`Connector` entry point and checks that each one refuses, that the refusal is
catchable as `IOException`, and that a state machine that dials reaches its
offline state instead of staying in "connecting". It also loads every class of
the framework, so a declaration that stops resolving fails there rather than
inside a game. See `internal/platform/skt/testdata/README.md` for the fixture
recipe.
