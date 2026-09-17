# SKT Java platform

`internal/platform/skt` loads SKT Java archives and composes the shared JVM,
MIDP, WIPI Java, and SKVM/vendor surfaces. [JVM](jvm.md) describes execution;
[LCDUI](lcdui.md) describes screens and drawing; [SKT history](history/skt.md)
keeps call traces, compatibility investigations, and dated acceptance results.
The separate GNEX/GVM path is documented in [sgs.md](sgs.md); its development
remains paused.

## Loading and lifecycle

The loader validates the descriptor, JAR entries, class names, sizes, and paths.
MIDlet lifecycle work enters through bounded event processing. Guest Java
threads run independently of Host ticks. Their first uncaught asynchronous
error is retained and returned at the next `RunPending` boundary. Terminal
transitions close JVM waits and release audio waits.

Thread state belongs to the Thread object. Only active threads and the main
thread remain roots; completion and failed starts preserve restart prohibition
without retaining inactive roots. Untimed `Thread.join()` supports multiple
waiters, interruption, and VM shutdown. Joining a live cooperative thread is
explicitly unsupported. `VM.Close` does not wait for arbitrary Host native calls.

Pause/resume callbacks and stopped Host ticks do not prove that every guest
thread or clock freezes. Real-phone suspension and active soundtrack recovery
remain separate acceptance requirements.

## Java and device surfaces

The runtime installs its library declarations from Go. CLDC `DataInput` and
`DataOutput` interfaces, assignability, and floating-point stream methods are
covered by class fixtures and a packaged MIDlet. Float/double output uses
canonical NaN encodings.

The SKVM surface includes supported file, graphics, fixed-point math, audio,
and device-property APIs. Missing methods and deliberate stubs remain bounded
by their documented callers; a successful class lookup does not establish
complete semantics. Network, SMS, calls, and phonebook APIs do not provide a
live carrier service. See [network policy](network.md).

## Graphics and input

MIDP Canvas and high-level LCDUI paths provide the supported screen, image,
font, repaint, clipping, and input contracts. Normal clipping is count-based.
An exact Java-class fingerprint can enable the recorded inclusive `setClip`
exception; a legacy profile string alone cannot. The selected correction does
not change WIPI Graphics, `clipRect`, fills, or image dimensions. See
[compatibility registry](platform-compatibility.md).

The runtime has Canvas pointer handlers, but the shared SKT session does not
offer browser pointer input. Host keyboard/IME entry supports known active
editors, including an append-only vendor component whose API cannot return its
existing value. Constraints and UTF-16 limits remain authoritative. See
[native text input](native-text-input.md) for the supported targets and limits.

Display selection follows supported descriptor/resource evidence. Supplying
`-screen 320x240` is a diagnostic override, not proof that a package declares
landscape operation.

## Saves and sound

MIDP RMS and vendor files share the Host save boundary. Error-aware stores
report read failures as guest storage exceptions. Record decoding rejects
truncation and trailing bytes. [RMS](rms.md) describes record identity and
layout; [save integrity](session.md#save-integrity) describes Host contracts.

Audio requests use the guest timeline and shared sink boundary. MIDI messages
and sampled-audio buffers prove software delivery, not audible playback on a
phone. One recorded MIDlet route proves an in-game save, fresh-runtime reload,
and restoration to the same map checkpoint. That result does not cover every
archive or the Jlet path.

## Validation and limits

Run the [standard gates and opt-in probes](testing.md). Newly authored JARs
cover lifecycle, streams, threads, clipping, and supported input. Detailed
fixture regeneration commands remain in [SKT history](history/skt.md).

Known limits include Java SIS image decoding, absent 3D rasterizers, incomplete
vendor APIs, and no general browser pointer path. The available dated corpus
contained MIDlets rather than a real Jlet acceptance route. Jlet gameplay/save
verification, landscape package evidence, physical keyboard composition, and
audible playback still need the corresponding archives or human checks.
