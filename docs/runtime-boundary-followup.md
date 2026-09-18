# Runtime boundary follow-up, 2026-09-17

This implementation follows the fourteen candidates in the local comparison
queue. The evidence below comes from repository-authored fixtures, not a new
real-game or physical-device acceptance run. Two candidates remain incomplete;
their partial changes must not be read as completion of their wider contracts.

## Implemented boundaries

- SKT retains the first uncaught JVM background-thread error and returns it at
  the next `RunPending` boundary. Terminal transitions release guest audio waits
  and close JVM waits. `async_error_test.go` and the authored
  `async-failure.jar` exercise the session boundary.
- JVM thread state belongs to the Thread object. Only active threads and the
  main thread remain roots. Finished threads and failed starts keep restart
  prohibition without remaining roots. Untimed `Thread.join()` handles multiple
  waiters, interruption and `VM.Close`; live cooperative threads report an
  unsupported error. Close does not wait for arbitrary Host native functions.
- LGT preserves descriptor-named JAR precedence. Without a named match, multiple
  candidates require exactly one valid `binary.mod`. Descriptor and archive
  names accept UTF-8 and MS949; installed `P/` and `P/<AID>/` paths are aliases.
  JAR resources retain precedence, case-insensitive lookup is deterministic,
  and canonical duplicates and traversal are rejected. Content-based selection
  shares one expanded-byte budget across candidates and rejects malformed
  auxiliary JARs instead of repeatedly spending a fresh decompression budget.
- LGT substring, lengths, char arrays and StringBuffer mutations use UTF-16
  units, including isolated surrogates. Isolated units use an internal WTF-8
  representation; they are not replaced merely by taking a substring.
- LGT Java Player preserves repeat across pause/resume, rejects duplicate play
  and resume, and distinguishes stop from pause. Resume restarts the score from
  the beginning because the mixer has no saved playback cursor.
- KTF JVM/AOT reentry carries the current JVM Invocation through the ARM
  supervisor-call boundary. `ReentryProbe.java` and `jvm_reentry_test.go` cover
  synchronized Java → ARM → Java calls, reentry during class initialization,
  one shared instruction budget and depth cleanup. AOT depth release uses the
  worker captured at entry instead of mutable current-thread state.
- KTF File input streams share the File cursor and see subsequent writes and
  seeks. Output streams keep their existing shared behavior. Stream close is a
  no-op and does not invalidate the underlying File.
- KTF events at or above `USER_EVENT` (`0x5000`) reach listeners with their
  original type and two arguments. The specification establishes the base
  constant; the supplied compatibility proposal specifies the open-ended range.
  Unknown reserved event kinds below that boundary remain errors.
- KTF NUL padding contributes neither pixels nor advance. Partial LCD flushes
  copy into a retained Host image without modifying their source; pixels outside
  the region survive. An unchanged Card paint retains that partial image, while
  a paint that changes screen bytes presents normally. An explicit flush during
  paint is not overwritten by an extra implicit full flush. Screen snapshots
  are needed only while a partial image is retained. Forced worker slices do
  not request an automatic Host paint; explicit repaint, yield and idle paths
  continue to paint.
- KTF mutable Image surfaces have weak object ownership. Graphics retains its
  Image. Guest collection and repeated guest-address reuse reclaim orphan
  surfaces; explicit framebuffer release removes stale ownership before reuse.
  Clip state also has weak ownership: stopped orphans close, playing orphans
  remain until playback completes. Repeating orphans remain while playing.

The corresponding focused tests are `archive_selection_test.go`,
`java_utf16_test.go`, `java_pause_test.go`, `thread_join_test.go`,
`file_cursor_test.go`, `lcd_flush_test.go`, `worker_paint_test.go` and
`resource_lifetime_test.go` in their platform packages.

## Incomplete candidates

### LGT graphics context ABI

Translated draw rows, overlapping self-blits and nested pixel callbacks have
authored regressions and fixes. The existing revision-scoped origin correction
remains in place. The alternate direct 52-byte context layout is still unknown:
the supplied proposal does not specify field offsets or an unambiguous layout
discriminator, and the WIPI specification treats the context as opaque. Mixed
compact/direct contexts must not be declared supported without an SDK layout,
a reference implementation or an affected caller. The callback error-path regression also confirms that the client lock is
reacquired after an undefined guest instruction.

### KTF native ABI and stack lifetime

Authored fixtures now verify fieldless intermediate payload sizes, independent
ancestor/child field addresses, canonical dispatch-alias ancestry, argument
containers across nested native calls and private helper stacks across guest
collection. These paths already satisfy those fixtures and were not rewritten.
Helper stacks and argument containers still use the existing arena lifetime;
these tests establish no premature reuse, not a new reclamation policy.

The environment return-slot address, width, initialization value and validity
convention remain unspecified. Existing register returns cannot establish slot
precedence. No speculative environment layout has been added. An SDK definition
or affected native caller is needed to complete this part of the candidate.

## Save integrity

`backend.SaveReader` and `backend.ReadSave` distinguish absence from I/O errors
when supported by the Host store. `DirectorySaveStore` implements the boundary;
the legacy `LoadSave` interface remains source compatible and cannot report
errors that a legacy Host already hides. Active KTF, LGT and SKT file/record
read paths use the error-aware boundary. Authentication views preserve it.
Record decoding rejects trailing data as well as truncation. KTF and LGT retain
read failures as terminal native-boundary errors and refuse subsequent writes;
SKT RMS and XFile report guest storage exceptions.

KTF Java files, Java record stores, WIPI-C files and WIPI-C record stores stage
mutations before publishing bytes, cursors, deletion ledgers, names or handles.
Content and ledger changes use the optional `SaveBatchStore` contract. Older
Host stores retain single-key writes and explicitly refuse multi-key operations
unless they implement that contract; they never receive a partial sequence of
those writes. The authentication view transforms a batch before committing it
and publishes its private ledger only after success.

The directory implementation writes all replacements and recovery copies first,
then commits under its store lock. A commit failure restores previous entries
and removes newly created entries. If the filesystem also refuses rollback,
the error identifies the retained recovery directory rather than deleting the
only old copies. This is failure recovery, not a crash-atomic transaction across
multiple renames. Save bytes and canonical paths are unchanged; staging files
are temporary children of the existing save root.

`save_batch_test.go` injects a failed second replacement and checks both existing
and newly created first entries; unreadable-target failures happen before any
replacement. Platform failure tests cover Java file cursors, Java and C record
mutations, C file cursors/handles, deletion, recreation, rename and authentication
ledgers. Existing recreation and persistence tests cover reopening through the
unchanged on-disk layout. The earlier native package's buffered write/flush
policy remains as previously documented; its reads now report I/O errors.
SGS-specific save work remains suspended.

## Intermediate-frame experiment

The authored callback fixture holds the execution lock across two distinct LCD
flushes. The existing `Frame()` path needs that same lock, so it cannot expose
either image before callback return. With the optional `backend.FrameSink`, both
images reach a separate queue while the callback still owns the lock.

KTF samples explicit LCD flushes no more often than once per 1/60 wall-clock
second, independently of virtual game time. The deterministic 1 ms fixture
permits 50–60 changed frames over one second; unchanged samples send nothing.
Both the converter and the receiving Host have separate pixel storage. Filling
the one-frame queue and continuing for another 100 changed flushes never waits
for the consumer. Hosts without a sink keep the original pull-only path.

`session.Options.FrameUpdates` routes these owned frames to the web Host's
existing one-frame PNG encoder queue, including during session startup. Scaling
runs on the encoder goroutine. The wire format remains ordinary PNG binary
messages; the page test receives two frames while its start request is still
pending. Parking detaches the sink before its queue closes; resuming attaches
the new queue, and a scale change updates its presentation factor. Intermediate
frames may be dropped under backpressure, and there is no timer that invents a
flush when guest code does not request one. This is synthetic evidence, not a
new real-game timing measurement or a real-browser acceptance run.

## Fixture regeneration

Compile `internal/platform/ktf/testdata/src/ReentryProbe.java` with
`javac -source 1.8 -target 1.8 -g:none` into a temporary directory and copy only
`ReentryProbe.class` into `internal/platform/ktf/testdata/`. `ReentryBridge` is a
compile-time declaration: the test installs its ARM body itself.

The SKT fixture uses the authored MIDlet stubs and the same Java 8 target as the
other SKT JAR fixtures. Package `AsyncFailureMIDlet.class` and its two anonymous
inner classes with `testdata/ASYNC_FAILURE.MF`; do not include runtime stub
classes in the JAR.

## Validation

On 2026-09-17, `make test`, `make test-debug`,
`go test -race ./internal/...` and `go vet ./...` passed after the final runtime
changes. `git diff --check` and `gofmt` checks passed. The page's intermediate
frame test is included in the Node suite. No new real-game playthrough,
physical-device check or real-browser acceptance run is claimed.
