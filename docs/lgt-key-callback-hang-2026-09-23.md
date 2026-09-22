# LGT key callback hang — 2026-09-23

A new gameplay report reopened case 3 after the earlier logo-transition fix.
The debug server used one CPU core while its status endpoint still answered
HTTP 200 in 0.6 ms. Native sampling placed execution inside
`deliverJavaKey -> callJavaCardMethod -> Core.Call -> handleJavaSVC`.
The last delivered event was a key release (type 2, key -5). It was followed
by repeated helper 0x55 calls, expanding the server log beyond 3.7 GB.

The recognized native module's key handler at 0x8e0c waits at 0x8ee6–0x8ef4
for the class static field at offset 0x5c to clear, calling helper 0x55 in the
loop. Event delivery precedes worker service in the session tick, so a worker
needed by this callback cannot run until the callback returns. An authored
ARM key callback waiting on a worker reproduces an instruction-limit failure
in the existing runtime.

Debug report requests also run on the session's command loop, after Tick
returns, explaining why the page cannot save a report during the hang. The
three-billion-instruction session ceiling makes this failure extremely slow
to report, especially when debug logging writes every repeated call.

The live process was suspended with SIGSTOP to stop log growth while preserving
its memory. It was not terminated or restarted. Native samples, the bounded
key-boundary excerpt and input timings are preserved under the ignored
`build/lgt-hang-20260923` directory. Original saves and archives are untouched.

The fix services eligible workers after 64 compiler checkpoints inside key
callbacks, preserving synchronized regions and the original step ceiling.
Worker execution remains serialized. Each worker also retains its own try
regions, jump-buffer pool and native call depth across slices; the suspended
callback's exception state is restored afterwards. Without that separation,
the first scheduling prototype exposed a missing-try-region failure in the
real archive and was not retained as a standalone fix.

## Verification

The authored ARM callback regression fails before the change with the one
million instruction ceiling and passes after it. Additional tests preserve
a platform-held monitor and a worker's exception region across an interleaved
callback, checking distinct jump buffers and cleanup on completion.

The 148 captured key events were replayed in isolated fresh and copied-save
runs. Original timing did not reproduce the live hang in either build. With
the intervals scaled to 90%, the original build hangs at event 78 in the actual
`keyNotify` body at 0x8e0d and reaches a ten-million instruction diagnostic
ceiling. The fixed build completes all 148 events and 100 further ticks:
3,737 ticks, 37.001 guest seconds, 275 flushes, no runtime error, and a visible
gameplay frame. Evidence: `baseline-timing-90.log`, `fixed-timing-90.log` and
`fixed-timing-90.png` under the ignored incident directory.

`make test`, `make test-debug`, `go test -race ./internal/...`, `go vet ./...`
and `git diff --check` pass. The first whole-repository run encountered mixed
packages in diagnostic overlay copies; those local files were renamed to
non-Go inputs and the gates rerun successfully. A replacement debug server
binary has been built separately; the user's live server has not been replaced
or restarted. Restart requires their confirmation because unsaved runtime
progress would be lost.

This fixes the evidenced input/worker wait. It does not add out-of-band report
export for arbitrary blocked guest callbacks or claim all later gameplay has
been exercised.
