# SGS external launch boundary

Service `0xc4` at original handler `0x41c750` consumes one resource ID,
copies its NUL-terminated bytes into host action 21, and redirects the current
invocation to its terminator. It does not push a result or directly invoke a
guest completion callback. Unlike native input, it does not stop the three
script timers.

The inspected PC wrapper maps action 21 to its default branch at `0x4201ba`.
That branch does not establish how another handset Host launched an external
application. The browser behavior below is therefore an explicit Host policy,
not a claim about that wrapper. No original runtime code is bundled.

## Observed corpus path

Corpus ID 11 has an alternate menu path that invokes `0xc4` with resource 65,
an absolute HTTP URL of 42 bytes before its terminator. The next guest
instruction is unreachable because the service ends that invocation. No
request was sent to the destination while establishing this evidence.

This alternate URL is not a main-play blocker. A separate local route reaches
play and accepts movement without taking the external path. Displaying or
opening the link does not establish anything about content at the destination.

## Host contract

The script runtime consumes the resource ID and requires a terminator within
2,048 guest bytes. It strictly decodes the SGS EUC-KR string, then accepts only
an absolute `http` or `https` URL with a nonempty host and no embedded
credentials. Relative destinations, malformed encodings, and executable or
local schemes fail before reaching the Host. The server does not fetch the URL.

A browser session supplies a one-slot mailbox. The first request remains there
until the page acknowledges it; repeated guest requests are coalesced while the
slot is occupied. This bounds both memory and outbound events even though guest
timers continue. Each accepted request uses a process-wide monotonic identity,
so an acknowledgement delayed past stop/start cannot clear another game's
request. Parking transfers the mailbox with the game, and the resumed owner
receives one fresh announcement.

The page validates the URL again with the browser parser, displays the exact
normalized anchor destination as text, and creates a normal link limited to
HTTP(S). It never calls `fetch`, changes location, or opens a window on receipt.
Only the person's link click invokes browser navigation. The link opens in a
new tab with `noopener` and `noreferrer`; a separate button dismisses it. Both
actions acknowledge Host bookkeeping, while disconnect or parking detaches the
page notice without acknowledgement so it can be announced after resume.

The notice is nonmodal. A later native text dialog can open over it and remains
the only owner of keyboard input. A matching, stale, or duplicate external
acknowledgement never enters guest code, resumes the ended invocation, changes
timers, or dispatches a guest callback.

CLI diagnostics supply no external-launch capability. A valid `0xc4` request
there returns `SGS external URL launch is unsupported by this host` through the
existing start, key, or tick error path; it does not pretend that a browser was
opened.

## Actual caller verification

On combined revision `274d2be`, the manifest-verified ID 11 alternate menu
route reached `0xc4` once in the first session and once after Host close/reopen.
An observation-only Host callback accepted both 42-byte HTTP destinations;
no network action was supplied. Both sessions completed without execution or
close errors and made no save writes. This proves delivery from the actual
caller, not availability of the destination or downloaded content. Browser
activation and ownership behavior are separately covered by the authored
Chromium fixture.
