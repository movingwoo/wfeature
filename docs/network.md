# Network

Nothing here reaches a network. That is one decision applied across four
platforms, and this document is where the decision and its whole surface live;
each platform document links back to it rather than restating it.

## The decision

Every connection a game asks for is **refused**, immediately and with the
failure its own error path was written to expect.

The alternative is worse than it looks. A game told a connection opened waits
for bytes, and it waits inside the screen it only leaves when a read
completes. That is a state its author never saw: a handset with no coverage,
or a subscriber who declined the data charge, refused the connection outright,
so the refusal path is the one the original developer actually tested. Faking
success invents a state nobody wrote code for; refusing walks a path that
already works.

Refusal beats absence too. A missing class or an unimplemented slot stops the
session with a host error, and no guest `catch` can see it. A refusal is
something the game handles.

## The surface

| Platform | Entry point | Answer |
| --- | --- | --- |
| KTF | `org.kwis.msf.io.Network.connect` | `-1` |
| KTF | `org.kwis.msf.io.Network.disconnect` | accepted no-op |
| KTF | WIPI C net table, `close` | success — closing what never opened is not a failure |
| KTF | WIPI C net table, everything else | `0xffffffff` |
| KTF | `MC_knlGetAccessLevel` | the network bit is clear |
| LGT | `MC_netConnect` | accepted, then fails through its callback |
| LGT | `MC_netClose` | success, because the call returns void |
| LGT | the rest of the net block — socket connect/write/read/close, both callback setters | error |
| LGT | `org.kwis.msf.io.Network.connect` | `-1`, and one title disagrees — below |
| LGT | `org.kwis.msf.io.URL.find` | `SchemeNotFoundException`, the failure the specification names |
| SKT | `SMS.send`, `Call.call` | `false` |
| SKT | `PhoneBook` reads | empty, null, or zero |
| SKT | the WAP browser entry point | accepted and discarded |
| MIDP | `Connector.open` and the four stream helpers | `ConnectionNotFoundException` |
| MIDP | a network media locator | `MediaException` |
| MIDP | a network player locator in the high-level UI | refused |

Three details in that table are not obvious.

**The whole block refuses, not only the connect call.** A title refused a
connection still tears down what it had already started. Stopping it at the
teardown with an unimplemented error turns a handled refusal into a crash, so
close, read, write and the callback setters all answer rather than fall
through.

**Refusing the callback setters is what keeps a game moving.** A registered
read callback is how a game waits asynchronously; refuse the registration and
it never enters the wait.

**One call cannot be refused by its return value.** LGT's `MC_netConnect` is

```c
M_Int32 MC_netConnect(NETCONNECTCB cb, void *param)
typedef void (*NETCONNECTCB)(M_Int32 error, void *param)
```

and the specification is explicit that a zero return hands the answer to the
callback, while an error return means the callback is never called. Refusing by
return value alone is therefore only half an answer: a title that checks the
return sees it, but one that waits on the callback — the way the asynchronous
half of this API is meant to be used — waits forever, and a local title does
exactly that.

So the dial is accepted and then fails, which is what a handset with no
coverage does: the attempt starts, the radio finds nothing, and the callback
says so with `M_E_ERROR`. Both kinds of caller reach the same offline path.

The failure is delivered four ticks later rather than immediately. A callback
that fires before the caller has finished entering its own connecting state is
delivered to a state machine that is not listening yet, and the game then waits
for a second one that never comes. `MC_netClose` drops a dial that has not
reported yet, because it withdraws the question.

`internal/platform/lgt/wipic_net.go` holds all of it. The callback runs from
`Session.Tick`, between guest calls and never inside one, the same place the
timers fire.

**`MC_knlGetAccessLevel` is the best refusal of all.** A game that checks the
bit before it dials skips the attempt entirely instead of making one and
handling the answer. Asking is the whole point of the call.

## The Generic Connection Framework

`javax.microedition.io` is declared in `internal/api/midp/definitions.go` and
installed by `midp.Define`. The Go side is
`internal/platform/skt/connector.go`.

`Connector` is native on all seven entry points and every one lands on the
same refusal, carrying the requested name as the exception message.
`ConnectionNotFoundException` extends `java.io.IOException`, which is the
catch most titles actually write: they wrap the whole attempt in one
`catch (IOException)` and never name the specific type.

The connection interfaces — `Connection`, `InputConnection`,
`OutputConnection`, `StreamConnection`, `ContentConnection`,
`StreamConnectionNotifier`, `HttpConnection`, `SocketConnection` — carry no
implementation and none is ever handed out. They exist so that a game which
declares a field of one of those types, or catches on one, resolves the name
instead of meeting the loader's "class not found".

`HttpConnection` keeps its full constant set even though no connection is ever
returned, because a game reads those constants while building the request it
is about to be refused.

## System properties are not part of the refusal

`internal/wipic/properties.go` answers `RSSILEVEL`, `AIRPLANE_MODE`,
`ROAMING_AREA`, `PHONENUMBER` and the rest with a working handset's values,
not with values chosen to look offline.

That is deliberate, and it was learned the hard way. Answering `PHONENUMBER`
with an empty string was meant to push a title onto its offline path; instead
a title that takes the last four digits off the number asked to copy minus
four bytes and died during its own startup. A handset never answers with an
empty string. Nothing goes online either way, so the properties describe a
handset and the refusal happens where the connection is actually attempted.

### The subscriber number is the one property worth changing

`PHONENUMBER` and `MIN` answer `01000000000` by default, and that default is a
compromise rather than a fact, because **two local titles want opposite things
from the same number**:

| Answer | The title that gates on billing | The title that checks a certificate |
|---|---|---|
| `0`–`4` digits | opens its packaged data and plays | "인증오류 (3001)" and stops |
| `5`–`11` digits | offers to download data it already ships | reaches its title screen |
| empty | — | — (and a third title dies in its own startup) |

The first title's predicate is the **length** and nothing else — `911` passes
and `01000` does not, so it is not the value, not a prefix and not a match
against anything it stores. Five digits or more is a subscriber it can bill; it
then checks a receipt issued to the handset that paid, which is not this one.
The sweep is in [`ktf.md`](ktf.md), "A second download gate".

The second title compares the number against the one sealed into the
certificate `wfeature provision` writes, and refuses a short one outright.

There is no overlap, so the number is a setting rather than a constant:

```sh
WFEATURE_PHONE_NUMBER=9999 wfeature runktf var/games/ktf/game.zip
wfeature-server -number 9999          # or WFEATURE_PHONE_NUMBER
```

`wipic.SetSubscriberNumber` is the only way in, both hosts call it before
anything is loaded, and it takes digits only, at most eleven of them — the
width of the shortest buffer a title reads the number into. The default is
unchanged, so nothing moves unless someone asks for it.

Two consequences worth knowing:

- **A certificate is sealed with the number in force when it was provisioned.**
  Provisioning under a short number produces a certificate that title will
  refuse, so provision with the default and play that title with the default.
- **Every other local title is indifferent.** Booting all 61 KTF and LGT
  archives for 600 ticks under both numbers gives the same frame for every one
  of them except the title above; the four that differed run to run differ
  under a single number too, which is the noise floor rather than an effect.

## A missing server is not a wrong stub

Some titles gate on an authentication server and have no offline path at all.
They stop on their own network screen and report their own error. Refusing
more gracefully cannot move them: there is nothing wrong with the refusal, and
the server they want no longer exists. Which titles those are, and which ones
turned out to be handled paths rather than missing servers, is tracked per
title in the support matrix ([`support.md`](support.md)).

### One title does not report its own error, it parks

The decision above assumes the refusal lands somewhere the title has code for.
One LGT Java title says that is not always the same place, and it is worth
writing down because the two answers are only three instructions apart in its
own code.

Its save-backup routine puts a notice up, calls `Network.connect`, and on `-1`
**returns without taking the notice back down** — the branch jumps past its own
teardown call to the epilogue. The notice being up is what makes its card's
`paint` draw the notice instead of the card, which is what leaves the flag its
Jlet loop waits on set for ever ([`lgt.md`](lgt.md), "What that wait is
actually testing"). Nothing reports anything: the screen holds one line of text
and the game is gone.

Its other path works. Patching `connect` to look like a success in a running
session sends it to `URL.find("socket://…")` one call later — which is why
`find` is served now — and the `SchemeNotFoundException` this platform answers
with is **caught by the title**, which takes its notice down and carries on to
its title menu. Same session, same archive, one branch apart.

So the decision "answer `-1`, because that is the specification's failure" is
right by the specification (`connect` answers 0 when access is already
available, 1 when it was just established, and -1 when it failed) and wrong for
this title. Answering 0 would be claiming a network this platform does not
have, and it would change the first thing every other local title is told — so
it is not a change to make on one title's evidence without measuring the other
sixty. It is recorded here rather than made.

## Not implemented

These are recorded rather than served, because nothing in the local archives
reaches them and a stub would be a contract invented for no caller:

- `HttpsConnection`, `SecureConnection` and `SecurityInfo`
- `CommConnection` — the serial bit of `MC_knlGetAccessLevel` is clear for the
  same reason the network bit is
- `ServerSocketConnection` and `UDPDatagramConnection`
- `DatagramConnection` and `Datagram`, which would also need `java.io.DataInput`
  and `java.io.DataOutput` in the core library
- `PushRegistry` — inbound push is a different feature from the outbound path
  this document covers

## Changing the classes

The framework's classes are Go declarations in `internal/api/midp`, so a change
to one is a change to that file and nothing else — no class files to rebuild.
`internal/tools/javastub` is what a fixture is compiled against; see
`docs/testing.md`.

## Tests

`internal/platform/lgt/wipic_net_test.go` builds a two-instruction Thumb
callback in guest memory, dials, and checks that nothing is reported before the
delay, that the callback then runs with `M_E_ERROR`, that it runs once, and that
`MC_netClose` drops an unreported dial.

`internal/platform/skt/connector_test.go` drives a fixture MIDlet through every
`Connector` entry point and checks three things: each one refuses, the refusal
is catchable as `IOException`, and a state machine that dials reaches its
offline state instead of staying in "connecting". It also loads every class of
the framework, so a class file that stops parsing fails there rather than
inside a game. See `internal/platform/skt/testdata/README.md` for the fixture
recipe.
