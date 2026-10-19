# Full-source review, 2026-10-16

This review read every non-test source file on `main` at `dcb8c27`, split into
14 areas of about 8,000 to 17,000 lines each. Each area had one reviewer, which
read every listed file in full, and one verifier, which tried to refute each
finding against the code. Tests, `web/acceptance`, `internal/testfixture` and
the mechanically translated hqx tables were out of scope. No game, server or
acceptance run was part of it. Where a verifier reproduced a defect, it used a
throwaway overlay test or scratch program, not a repository fixture.

The local TODO is the execution queue; this document preserves the evidence.
Finding numbers (R001 to R153, D1 to D9) are stable and TODO lines cite them.
Line numbers refer to `dcb8c27`.

CONFIRMED means the verifier traced the failure scenario through the code, and
in some cases reproduced it. PLAUSIBLE means the defect is in the code but
reachability or impact was not fully confirmed. Severity is the verifier's: high
is a crash, hang, data loss, save corruption or security hole reachable from
realistic input; medium is wrong behavior on a real path; low is an edge case or
a latent defect.

Before fixing, add a repository-authored regression fixture. Where a WIPI
contract is involved, check the specification and an affected caller; observed
handset behavior wins over the specification.

## Resolution

Every finding was handled on the branch `fix/source-review-2026-10-16`, in two
rounds on 2026-10-16: 155 fixed, 4 partly fixed (R028, R066, R076, R099) and 3
not fixed (R063, R102, R125). Each finding below ends with its resolution, which
describes the behavior as the second round left it; for a partial or unfixed one
it says what is left and why.

The second round made one save rule common to every platform's file layer ([One
save rule](architecture.md#one-save-rule)): a write is seen at once and stored
by the close or the end of the game, a truncation is stored at the open, an open
for writing creates the file, a removal lasts, names that differ only by case
are different files on every host, and nothing else saves. That fixed R052 and
R118 and the rest of R123. By the user's decision it reverted the iOS background
drain R063 had added, because saving is the player's to do, and passed R125.

Every code fix came with a regression test that was first run against the
unfixed code, and the gates (gofmt, go vet in both profiles and for Windows,
make test, make test-debug, go test -race ./internal/...) pass. The PWA
acceptance from v0.5.2 and the SKT checkpoint acceptance pass in Chromium and
WebKit. The Android app passes its lifecycle check on an emulator and the iOS
app on iOS 27.0 and 26.5 simulators (docs/testing.md), and both checks fail on
the builds before the fixes; the Android check also found that finishing the app
could cut the server's drain short, which is fixed. The case conversion of R118
was checked on a copy of 372 real debug save folders with no difference in any
key or byte.

The review of PR #234 found 15 more defects in that work, all fixed on the same
branch; see [Review of PR #234](#review-of-pr-234-2026-10-17). The limits the
fixes left were taken up on 2026-10-17; see [Follow-up of the remaining
limits](#follow-up-of-the-remaining-limits-2026-10-17). The review of that
work, PR #235, found 22 more, and they and the candidates it did not verify were
taken up on 2026-10-18; see [Follow-up of the review of PR
#235](#follow-up-of-the-review-of-pr-235-2026-10-18).

Real-game acceptance over the local corpus of 532 archives: after the first
round every archive reached the same rung as on `dcb8c27` except three SKT
archives whose interactive rung moved in both directions, which reruns showed to
be the stage's timing noise. After the second round no archive reached a lower
rung than on `dcb8c27`; the one that moved went further, and it is one of those
three. CPU per tick after the first round, measured with `runktf -play`, was
unchanged on a CPU-bound KTF title and about 7% higher on a light one (0.84 to
0.90 ms per tick), spread over the cpu, KTF runtime, KTF graphics and JVM
changes at 1 to 3% each.

## Documentation review of PR #233

A diff review of `HEAD~1..HEAD` (PR #233, the first Markdown refinement pass)
before the full-source review. Links, anchors, cited history sections, make
targets, environment variables and about 40 numeric limits were checked and
matched; these are what did not.

- **D1** `cmd/cli/native.go:26`: The comment says options that mean something
  only to the descriptor loop are refused by name rather than ignored. `runKTF`
  (`cmd/cli/main.go:1071-1105`) refuses only `-diag`, `-gdb`, `-profile` and
  `-profile-folded`, and silently drops `-touch`, `-park` and the `-speed`
  multiplier for the earlier KTF package. `docs/cli.md:247-248` now documents
  the silent drop. `wfeature runktf native.zip -park 40:5000 -ticks 200` never
  parks and exits 0. Related: R140. **Fixed:** The earlier KTF package refuses
  -touch, -park, a -speed other than 1 and -profile-from by name with exit 2, as
  the comment promised, and docs/cli.md no longer describes a silent drop.
  `-speed` is now passed to that package's session, which takes one (P12 of
  the PR #234 review).
- **D2** `README.md:335`: The CLI paragraph lists four LGT authentication
  results. `internal/backend/authentication.go:15-21` also reports
  `lgt-certificate-100`, `lgt-save-subscriber` and `lgt-certificate-message`;
  `docs/cli.md:1089` lists all seven. **Fixed:** README lists all seven LGT
  authentication statuses and names the two offline ones explicitly.
- **D3** `docs/architecture.md:132`: The detection table says detection asks for
  `__adf__`, then `app_info`, then `.msd`.
  `internal/platform/detect/detect.go:165-185` tests all three markers per entry
  in central-directory order and returns on the first entry that matches any of
  them, so there is no KTF, LGT, SKT priority. See also R138. **Fixed:**
  docs/architecture.md says the three named markers have no priority: the first
  entry in central-directory order that wears any of them decides, and only then
  are the shape checks asked in table order.
- **D4** comment pointers: Bare `See docs/ktf.md`, `docs/lgt.md` or `armcore.md`
  citations still point at overviews for content that now exists only in
  `docs/history/`: `internal/platform/ktf/native_client.go:33` and `:848`,
  `internal/platform/ktf/native_platform.go:426`,
  `internal/platform/ktf/runtime_java.go:1610`,
  `internal/platform/lgt/java_api.go:750`,
  `internal/platform/lgt/java_runtime.go:607`, `internal/armcore/memory.go:287`.
  Other citations in the same files already point at `docs/history/`. **Fixed:**
  The seven bare `See docs/ktf.md` / `docs/lgt.md` / `armcore.md` citations now
  name the docs/history file and section that hold the content, in the citation
  form the same files already use.
- **D5** `docs/running.md:520`: The verification table says `go build ./...`
  passes in CI on Windows. `.github/workflows/checks.yml` builds only
  `./cmd/server` on the Windows smoke runner (line 155); `./...` is compiled
  only on Ubuntu. **Fixed:** docs/running.md says CI builds only `./cmd/server`
  on the Windows runner and that `go build ./...` for Windows is cross-compiled
  (checked: `GOOS=windows go build ./...` and `go vet ./...` pass).
- **D6** `docs/skvm.md:119`: The PackagedScreen description says the width is
  selected when every such component agrees. `packagedScreenWidths`
  (`internal/platform/skt/screen.go:146`) drops suffixes that are not in the
  handset map, such as `_120`, before counting, so disagreeing unknown widths
  never block a selection. **Fixed:** docs/skvm.md says PackagedScreen counts
  only the widths it has a handset for, ignoring a suffix such as `_120` rather
  than counting it against the others.
- **D7** `.gitignore:88`: The `CLAUDE.md` pattern is unanchored and ignores that
  name at every depth. `/CLAUDE.md` would ignore only the root symlink.
  **Fixed:** `.gitignore` anchors `/CLAUDE.md`, and `/AGENTS.md` which had the
  same unanchored pattern, to the repository root.
- **D8** `CHANGELOG.md:12`: The unreleased section, which ships verbatim as
  release notes, mixes noun endings (`…차단함`, `…사용 가능`, line 82 `…Go 1.26 이상 필요`)
  with `…올려야 합니다` (line 34) and `…새로 퀵세이브해야 합니다` (line 50). **Fixed:** The three
  '…합니다' sentences in the unreleased CHANGELOG section now use the noun endings
  of the rest of the section.
- **D9** `internal/platform/skt/skvm.go:901`: The rewritten comment was not
  reflowed; one line is 128 columns in the middle of an 80-column block.
  **Fixed:** The one overlong comment line in internal/platform/skt/skvm.go is
  reflowed to the surrounding width without changing a word.

## High (11)

### R001 objectText/readUTF re-enter guest code on a fresh execution and deadlock

`internal/jvm/builtins.go:2029` · jvm · concurrency · CONFIRMED

- **Defect:** StringBuffer/StringBuilder.append(Object) and
  String.valueOf(Object) call vm.objectText, which runs toString via
  vm.InvokeVirtual. That creates a new execution with a new ID, no thread and
  empty state.initializing. DataInputStream.readUTF does the same for read() and
  readUnsignedShort(). The callee therefore cannot re-enter monitors its own
  caller holds, and it waits for a class initialization its own caller is
  performing. Every other core-library callback uses the caller's Invocation.
- **Failure:** (1) `synchronized String describe(){ return "x" + this; }` with a
  synchronized toString(): the nested execution blocks in enterMonitor on a
  monitor its own caller owns. A probe hung until VM.Close. (2) class Item {
  static String[] NAMES = {...}; static Item DEFAULT = new Item(0); static
  String LABEL = "item:" + DEFAULT; public String toString(){ return NAMES[id];
  } }: during Item.&lt;clinit> the nested toString reaches getstatic Item.NAMES.
  Because vm.initializing[Item] is set and the new state.initializing is empty,
  it waits on initCond for ever. A probe hung, and VM.Close never broadcasts
  initCond, so the goroutine leaks. Thread.currentThread() inside such a
  toString also reports the main thread rather than the caller.
- **Evidence:** builtins.go:2029 vm.InvokeVirtual(object, "toString", ...);
  vm.go:435 InvokeVirtual uses vm.newExecution(); builtins.go:288 and 298
  readUTF use vm.InvokeVirtual; vm.go:1192-1199 ensureInitialized waits while
  vm.initializing[className] is set, unless state.initializing has it;
  monitor.go:40 ownership is per execution id; contrast corelib_io.go:1482
  printReferenceText, which uses call.InvokeVirtual.
- **Verification:** objectText (builtins.go:2029) and readUTF (builtins.go:288, 298) call vm.InvokeVirtual, which creates vm.newExecution() at vm.go:435. That
  execution has a new id, thread == nil and an empty initializing map. Monitor
  ownership is per execution id (monitor.go:40), and ensureInitialized
  (vm.go:1191-1199) waits on initCond while vm.initializing[cls] is set unless
  state.initializing has the class. VM.Close (thread_join.go:9-13) only closes
  vm.closed and never broadcasts initCond. An overlay probe compiled with javac
  8 confirmed both hangs: Sync.run(), a synchronized describe() doing ("x"+this)
  with a synchronized toString, hung for more than 2 s, and so did Item.label(),
  where &lt;clinit> builds "item:"+DEFAULT and toString reads Item.NAMES.
  Elsewhere the code uses call.InvokeVirtual with the caller's state
  (corelib_io.go:1482). Both trigger patterns are ordinary Java but uncommon in
  game code.
- **Fixed:** objectText (String.valueOf(Object),
  StringBuffer/StringBuilder.append(Object)) and readUTF run their guest
  callbacks on the caller's execution, so a monitor the caller holds and a class
  initializer it is running no longer deadlock the callback.

### R002 getstatic/putstatic initialize the referenced class, never a superinterface

`internal/jvm/vm.go:833` · jvm · correctness · CONFIRMED

- **Defect:** staticValue/setStaticValue call ensureInitialized on
  reference.Class and then read the slot of the resolved declaring class.
  initializeClass walks only SuperName and never initializes superinterfaces, so
  an interface field reached through an implementing class is read before the
  interface's &lt;clinit> has ever run. The same root cause runs a subclass's
  &lt;clinit> too early when an inherited static is reached through the subclass
  name.
- **Failure:** interface T { int[] TABLE = {7,8,9}; } class Game implements T {
  ... TABLE[0] ... }. javac emits `getstatic Game.TABLE` (the qualifying type).
  ensureInitialized(Game) runs; resolveField finds T.TABLE through
  declaringInterfaceField; statics[T.TABLE] was never set because T.&lt;clinit>
  never ran, so the read returns null and iaload throws NullPointerException. A
  scratchpad probe compiled with javac 1.8 confirmed this: `execute
  Probe.interfaceTable()I at pc 4 (opcode 0x2e):
  java/lang/NullPointerException`. Non-inlined interface constants (arrays,
  objects, strings built at init) all read as null or zero.
- **Evidence:** vm.go:833 ensureInitialized(state, reference.Class);
  vm.go:840-848 resolveFieldReference then statics lookup, falling back to
  zeroValue; vm.go:852 same for putstatic; vm.go:1237-1241 initializeClass
  recurses only into class.SuperName; vm.go:1025/1042-1059 field resolution does
  search interfaces, so the slot is the interface's, but nothing initializes it.
- **Verification:** staticValue (vm.go:833) and setStaticValue (vm.go:852) call
  ensureInitialized on reference.Class. resolveField (vm.go:1025, 1042-1059)
  then resolves the slot to the declaring interface. initializeClass
  (vm.go:1237-1241) recurses only into SuperName, so the interface's
  initializeStaticFields and &lt;clinit> never run, and the statics lookup falls
  back to zeroValue (null). I reproduced this with a go test -overlay probe kept
  in the scratchpad: for classes compiled with -target 1.8 and with -target 1.3,
  javap shows `getstatic Probe.TABLE`, and Probe.interfaceTable() fails with
  `NullPointerException: array operation` at pc 4. The same source compiled with
  -target 1.2 emits `getstatic PT.TABLE` and returns 7. So any class file at
  target 1.3 or later that reads a non-constant interface field (an array or
  object) through an implementing class hits this. That covers JDK 1.4 with
  -source 1.4 and newer compilers. The bytecode interpreter is the class-file
  path (SKT and runtime classes). The secondary claim also holds: JVMS getstatic
  initializes only the declaring class, but here the class named in the
  reference has its &lt;clinit> run as well.
- **Fixed:** getstatic/putstatic and the Host StaticField/SetStaticField resolve
  the field first and initialize only its declaring class or interface (JVMS
  5.5); superinterfaces with default methods are still not initialized, which no
  CLDC-era class file has.

### R003 Image framebuffers made lazily or by copy are never reclaimed

`internal/platform/ktf/runtime_graphics.go:633` · ktf2 · resource-leak · CONFIRMED

- **Defect:** imageFramebufferHandle (decoded image rasterized on first
  masked/XOR draw, Image.drawImage, createImage(Image)) and copyImageRegion
  (createImage(Image), createSubImage) allocate a WIPI framebuffer record plus
  pixel buffer but never register it in runtime.imageSurfaces, which is the only
  path that frees an Image's framebuffer when the Java object dies. Those
  allocations live in wipicAllocations forever.
- **Failure:** A title that cuts a sprite frame with Image.createSubImage each
  frame (or reloads a stage's transparent PNGs on every stage change and draws
  them) leaks w\*h\*2+record bytes per call/per image. The 64 MiB platform arena
  fills, collectForAllocation cannot reclaim WIPI allocations, and allocate
  fails with 'KTF platform initialization data space exhausted', ending the
  session after minutes to hours of play.
- **Evidence:** runtime_graphics.go:633 newWIPICFramebufferRecord in
  imageFramebufferHandle, :652 only sets Fields[guestFramebuffer:I];
  runtime_graphics.go:1457 newWIPICFramebufferRecord in copyImageRegion, result
  object at :1482 not registered. Only runtime_java.go:2122-2125
  (createImage(II)) registers runtime.imageSurfaces[weak.Make(image)].
  collect.go:505-511 collectHostResources destroys framebuffers only for
  imageSurfaces owners; initialization.go:2145-2171 freeWIPIC is the only other
  release (guest MC_knlFree).
- **Verification:** imageFramebufferHandle (runtime_graphics.go:633) and
  copyImageRegion (:1457) call newWIPICFramebufferRecord
  (wipic_graphics.go:864-893), which takes two allocateWIPIC blocks (pixels plus
  a 20-byte record) tracked in wipicAllocations. Neither path adds the owning
  Image to runtime.imageSurfaces. Only runtimeImageCreateSized does that
  (runtime_java.go:2122-2125). collectHostResources (collect.go:505-511) frees
  framebuffers only for imageSurfaces owners. The collector's own arena.release
  (collect.go:466) covers only tracked objects, and the only other release is
  freeWIPIC, the guest's MC_knlFree (initialization.go:2145-2170). The only
  test, resource_lifetime_test.go, covers createImage(II) alone. Every Image
  built by createImage(Image) or createSubImage (runtime_java.go:1139-1140)
  therefore leaks. So does every decoded image that is rasterized for a masked,
  XOR or Image.drawImage draw, whose framebuffer is never freed when the image
  dies. A title that reloads transparent sprites per stage or cuts sub-images
  loses arena it cannot get back until allocate fails
  (initialization.go:2619-2636).
- **Fixed:** A `trackImageSurface` helper registers in `runtime.imageSurfaces`
  the framebuffers that `imageFramebufferHandle` rasterizes and that
  `copyImageRegion` (createImage(Image), createSubImage) creates, so
  `collectHostResources` frees them when their Image dies, as
  createImage(int,int) already did.

### R004 MC_grpDrawLine loops forever when an endpoint is ~2^30 away

`internal/platform/ktf/wipic_graphics.go:677` · ktf3 · untrusted-input · CONFIRMED

- **Defect:** wipicDrawLine runs Bresenham entirely in int32. Two things
  overflow: x1-x0 and y1-y0 when the endpoints straddle ±2^31, and `doubled := 2 * difference` once difference passes 2^30. After that the steps go wrong, and
  the only exit is an exact `x0 == x1 && y0 == y1`, so the Go loop never ends.
- **Failure:** A title calls MC_grpDrawLine(fb, 0, 0, 1500000000, 7, gc), for
  example with an unshifted 16.16 fixed-point value or an off-screen projected
  point. The Host goroutine spins forever inside the call while holding
  client.run. The ARM step limit does not apply because this is a Go loop,
  nothing checks ctx, and Session.Close/DestroyApp block forever on client.run.
- **Evidence:** wipic_graphics.go:651-686: deltaX/deltaY/difference/doubled are
  int32, and the loop ends only at 674 `if x0 == x1 && y0 == y1`. I copied the
  loop body exactly into a scratch Go program. (0,0)->(1500000000,7) and
  (INT_MIN,0)->(100,10) were still running after 3e10 iterations. For
  (INT_MIN,0)->(INT_MAX,1), y advances on every step, so y0 is always -1 when
  x0==x1 and the exit never happens. plot() (641-650) only rejects pixels and
  cannot stop the loop.
- **Verification:** wipic_graphics.go:651-686: deltaX, deltaY, difference and
  doubled are all int32. The only exit is `if x0 == x1 && y0 == y1` at 674, and
  plot() (641-650) only skips pixels. I ran the loop body exactly as written in
  a scratch program with a 1e10-iteration cap. (0,0)->(1500000000,7),
  (0,0)->(1500000000,0), (0,0)->(1100000000,7), (0,0)->(1000000000,7),
  (INT_MIN,0)->(100,10) and (INT_MIN,0)->(INT_MAX,1) all hit the cap without
  finishing. Small spans such as (0,0)->(15728640,458752) finish. The real
  threshold is lower than the reviewer said: after `difference += deltaX`,
  difference can reach about 1.5\*deltaX, so `2*difference` overflows once
  deltaX is above roughly 2^31/3 (about 7.2e8), not 2^30. Nothing bounds this Go
  loop. No ctx is checked, no step budget applies, and the call runs under
  client.run (client.go:375 etc.), so the session cannot be ended. A real
  handset clips the line and survives such coordinates.
- **Fixed:** MC_grpDrawLine walks the same Bresenham line in 64-bit arithmetic
  (`clippedLine`) over only the part of the major axis on the surface, entering
  mid-line with an exact position and error term, so far endpoints finish within
  one step per surface column or row and in-surface pixels are unchanged.

### R005 Collector does not trace a guest-backed stream's Source object or buffer

`internal/platform/lgt/collect.go:480` · lgt1 · correctness · CONFIRMED

- **Defect:** walkJavaPayload and markJavaPlatformRoots never mark
  javaStream.Source.Object (the title's own InputStream subclass instance) or
  Source.Buffer (the tracked byte[] the platform hands to read([BII)). Both are
  held only in Go, so the collector condemns and then frees them while the
  wrapper stream still uses them, which is a use-after-free in the guest heap.
- **Failure:** A title builds `new DataInputStream(new MyStream(...))` and keeps
  only the wrapper. javaWrapStream (java_stream.go:566-573) stores the shared
  \*javaStream under both objects but writes no guest field linking them. The
  first pull allocates Source.Buffer with newJavaByteArray
  (java_stream_guest.go:237-242). That array is tracked and pinned only for that
  platform call. Over the next two collection cycles (from arena growth or
  collectForAllocation) both MyStream and the buffer are unreachable, so they
  are condemned and then freed: zeroed and returned to the arena
  (releaseJavaObjectMemory). The next readInt/readFully through the wrapper
  calls the title's read at Source.ReadBlock with the freed receiver and the
  freed buffer (java_stream_guest.go:245). The guest then faults on a zero
  vtable or block pointer and the session stops, or, if the arena has reused the
  blocks, it writes up to 4096 bytes into another live object and reads that
  object's fields as stream data. Data being loaded (a save or table) is
  silently corrupted.
- **Evidence:** collect.go:480-509 walkJavaPayload marks clip listeners,
  vectors, widgets, wrapped, sinkFiles, streamFiles and thread runnables, but
  nothing from runtime.streams[object].Source. collect.go:382-440
  markJavaPlatformRoots has no stream roots. java_array.go:158 trackJavaObject
  makes the buffer collectible. java_stream_guest.go:237-261 reuses
  source.Buffer and source.Object on every pull. collect.go:599-601 deletes
  runtime.streams[inner] for the freed inner object, while the wrapper keeps the
  same \*javaStream pointer.
- **Verification:** Nothing marks javaStream.Source.Object or Source.Buffer.
  walkJavaPayload (collect.go:480-509) follows only clips, vectors, widgets,
  wrapped, sinkFiles, streamFiles and thread runnables. markJavaPlatformRoots
  (collect.go:382-440) has no stream roots. javaWrapStream
  (java_stream.go:561-575) writes the shared \*javaStream into the map under
  both objects and writes no guest field, because the DataInputStream
  constructor is native. Source.Buffer is allocated once through
  newJavaByteArray -> allocateJavaArray -> trackJavaObject (java_array.go:158).
  Its only protection is a pin, and the pin ends when the platform call returns
  (client.go:772-773) or at the round safepoint (collect.go:323). callOn
  (client.go:712-732) pins nothing. After that the only things naming the buffer
  are leftover register or stack words, so two cycles condemn it and then free
  it. releaseJavaObjectMemory zeroes the object and returns it to the arena.
  readJavaGuestStream (java_stream_guest.go:237-261) still passes the same
  Source.Buffer and Source.Object to the title's read and reads back through
  javaArrayBlock. The result is a fault, or a write of up to 4096 bytes into
  whatever reused that block. The buffer half happens even when the title keeps
  its own stream object. Only a guest stream that lives across two cycles (a
  loading thread that spans ticks, or 8-round follow-ups) is exposed, but
  nothing prevents that.
- **Fixed:** `walkJavaPayload` marks a guest-backed stream's Source.Object and
  Source.Buffer from any object holding that stream, so they live as long as a
  reachable wrapper and are collected with it.

### R006 MC_fsWrite/MC_fsSeek let a guest allocate gigabytes of host memory

`internal/platform/lgt/wipic_file.go:546` · lgt2 · untrusted-input · CONFIRMED

- **Defect:** transferFile allocates `make([]byte, length)` for any guest uint32
  length, and seekFile accepts any non-negative position (and keeps adding to it
  with SEEK_CUR), after which a write grows file.data to cursor+length with a
  zero-filled append. Nothing bounds either size, unlike memcpy/memset (16 MiB)
  or clips (1 MiB).
- **Failure:** A title (buggy or hostile) calls MC_fsSeek(fd, 0x7fffffff,
  SEEK_SET) two or three times with SEEK_CUR and then MC_fsWrite(fd, buf, 1).
  end-len(file.data) is several GiB, so `append(file.data, make(...)...)`
  allocates and copies (touches) that many bytes. The Go runtime aborts with an
  unrecoverable out-of-memory error that takes down the whole server (every
  session) or the mobile app. A garbage length in MC_fsWrite (e.g. 0xffffffff)
  also allocates 4 GiB before Memory().Read rejects the range. If the allocation
  does succeed, closing the file writes a multi-GiB save.
- **Evidence:** wipic_file.go:540 `chunk := make([]byte, length)` (length is r2,
  uint32); :544-546 `end := file.cursor + int(length)` then `append(file.data,
  make([]byte, end-len(file.data))...)`; seekFile :570-575 sets `file.cursor =
  position` with no upper bound, and SEEK_CUR (:564) builds on the previous
  cursor; :551 returns int32(length), which goes negative for large lengths.
  Compare stdlib.go:125 maxStdlibLength and wipic_media.go:459 maxClipBytes.
- **Verification:** seekFile (wipic_file.go:568-575) only rejects position&lt;0;
  SEEK_SET with int32 0x7fffffff, plus repeated SEEK_CUR (base=file.cursor,
  :564), puts the cursor anywhere on a 64-bit int. transferFile then computes
  end=file.cursor+int(length) (:544) and runs `append(file.data, make([]byte,
  end-len(file.data))...)` (:546). The compiler's extendslice clears the new
  region, which touches GiBs of memory. Nothing bounds file size on the write
  side: DirectorySaveStore.StoreSave (backend/save.go:171) has no limit, so a
  close writes the multi-GiB file. The 4 GiB `make([]byte, length)` at :540
  happens before Memory().Read validates the range (armcore/memory.go:453). It
  returns wipiError, not a session error, so a title can repeat it. That
  allocation stays untouched virtual memory, so it is the weaker half; the
  seek+write path is the real OOM. All dist targets are 64-bit (Makefile:18), so
  int does not overflow first. No memory limit (SetMemoryLimit) exists anywhere.
  Contrast maxStdlibLength (stdlib.go:125) and maxClipBytes
  (wipic_media.go:459).
- **Fixed:** Seeks and writes may not take a file past a 16 MiB cap
  (`maxFileBytes`, checked before any guest-sized allocation, 16 times the 1 MiB
  quota the space slots report); `MC_fsSeek`/`MC_fsWrite` answer `wipiError`
  past it, and a packaged file already larger is still read and written within
  its own length.

### R007 WIPI File modes read as XFile bits: READ_WRITE unusable, no truncate

`internal/platform/skt/skvm_file.go:107` · skt2 · save-integrity · CONFIRMED

- **Defect:** org.kwis.msp.io.File(String,int[,int]) is routed to initXFileName,
  which reads the mode as XFile bit flags (1=read, 2=write). The WIPI File enum
  is 1 READ_ONLY, 2 WRITE (append), 3 WRITE_TRUNC, 4 READ_WRITE. KTF
  (runtime_file.go:280-300) and LGT (java_file.go:41-50) both implement it that
  way, and the spec lists WRITE and WRITE_TRUNC. On SKT, mode 4 opens a file
  that can be neither read nor written, and mode 3 never truncates.
- **Failure:** A Jlet opens its save with File("save", 4) (READ_WRITE). writable
  is 4&2==0, so every read returns -1 (mode&1==0) and every write returns -1:
  the save never loads and never persists. If the file is missing, the open
  throws IOException('no such file'). A title that rewrites its save with
  File("save", 3) (WRITE_TRUNC) gets the old bytes kept and the cursor at 0, so
  a shorter save leaves the previous save's tail behind. A reader that goes to
  sizeOf()/EOF then parses garbage records.
- **Evidence:** wipi.go:117-118 registers both File &lt;init> overloads to
  runtime.initXFileName. skvm_file.go:107 sets writable: mode&xFileWrite != 0
  (xFileWrite=2). Line 143 refuses a missing file unless mode&2. Line 233
  (xFileRead) returns -1 when mode&xFileRead==0. Lines 123-126 keep existing
  data with no truncation for any mode. api/wipi/definitions.go:485-487 declares
  READ_ONLY=1, WRITE_ONLY=2, READ_WRITE=3 and has no WRITE or WRITE_TRUNC
  fields. ktf/runtime_file.go:297-300 treats mode 3 as truncate, and
  lgt/java_file.go:41-50 maps 1 RO, 2 append, 3 truncate, 4 RW. The wiki File.md
  lists WRITE (append) and WRITE_TRUNC (size 0 on open).
- **Verification:** wipi.go:117-118 sends both File &lt;init> overloads to
  initXFileName, and the mode at arguments[2] is right for both.
  skvm_file.go:107 then reads it as XFile bits: writable = mode&2. In xFileRead
  (skvm_file.go:233), a mode without bit 1 returns -1. Line 143 refuses a
  missing file unless mode&2 is set. No mode truncates: lines 123-126 always
  keep the stored bytes. The local constants (api/wipi/definitions.go:485-487:
  READ_ONLY=1, WRITE_ONLY=2, READ_WRITE=3) are invented. Titles compile in the
  real constants. The spec page lists READ_ONLY, WRITE (append), WRITE_TRUNC
  (size 0 on open) and READ_WRITE but gives no values. KTF
  (runtime_file.go:280-300 accepts 1..4 and truncates on 3) and LGT
  (java_file.go:41-50: 1 RO, 2 append, 3 truncate, 4 RW) both use the same 1..4
  numbering. Results: File(name,4) reads -1, writes -1, and throws IOException
  when the file is missing. File(name,3) keeps the old tail. The SKT tests
  (wipi_file_stream_test.go) pass xFileRead|xFileWrite, so they never exercise
  the real constants. Whether any SKT Jlet in the corpus uses File is
  unverified, but the code path is direct.
- **Fixed:** `org.kwis.msp.io.File` opens through `initWIPIFileName`, which
  reads the specification's enumeration (1 READ_ONLY, 2 WRITE appends, 3
  WRITE_TRUNC empties on open, 4 READ_WRITE) and maps it onto the XFile bits;
  other modes throw IOException, and the published constants are READ_ONLY=1,
  WRITE=2, WRITE_TRUNC=3, READ_WRITE=4.

### R008 Rejected save owner runs the game with no save store, losing saves

`internal/webhost/session.go:1844` · host · save-integrity · CONFIRMED

- **Defect:** saveDirectory returns "" whenever ownerRejected(owner) is true,
  and startGame then starts the session with SaveStore nil. On SKT that means
  every RMS/XFile save lives only in memory and is gone when the game closes.
  Nothing is logged and the page is not told.
- **Failure:** An SKT MIDlet whose MIDlet-Name contains ':' '?' '"' '\*' '&lt;'
  '>' '|' or a bidi mark (for example "Title: Part 2"), or whose name is longer
  than 64 bytes (about 22 Hangul syllables), or whose DD-ProgName contains '/'.
  skt.jarSaveOwner keeps those characters (it only replaces / \ NUL and control
  characters), so ownerRejected refuses the owner and saveDirectory answers "".
  startGame passes saveStoreIn("") = nil to session.Start. The title saves
  normally and the player sees no error, but closing or restarting the game
  loses all progress. The CLI saves the same title to savedata/skt/&lt;name>, so
  the two Hosts disagree, and export/import only says the game keeps no saves.
- **Evidence:** session.go:1843-1852 saveDirectory `if owner == "" ||
  ownerRejected(owner) { return "" }`; session.go:794 directory :=
  saveDirectory(...); session.go:823 SaveStore: r.server.saveStoreIn(directory)
  → nil for "" (1855-1859); saves.go:26-50 ownerRejected (64-byte cap,
  `/\:*?"<>|`, bidi); platform/skt/rms.go:91-112 jarSaveOwner sanitizes only / \
  NUL &lt;0x20; platform/skt/skt.go:253-258 DD-ProgName returned raw;
  platform/skt/runtime.go:269-271 'Without one they live only as long as the
  session'; cmd/cli/main.go:375 CLI joins skt.SaveOwner unfiltered.
- **Verification:** Traced end to end. session.go:131 sets summary.SaveOwner =
  skt.SaveOwner(descriptor). SaveOwner (skt.go:253-258) returns DD-ProgName
  trimmed but otherwise raw. jarSaveOwner (rms.go:91-112) replaces only '/',
  '\\', NUL and characters below 0x20, so ':', '?', '\*', '"', '&lt;', '>', '|',
  bidi marks and names over 64 bytes all pass through. ownerRejected
  (saves.go:26-50) refuses each of those, so saveDirectory returns ""
  (session.go:1843-1846). saveStoreIn("") returns nil (1855-1859) and startGame
  passes it as session.Options.SaveStore (session.go:823). skt.Options documents
  that without a store, RMS and XFile data 'live only as long as the session'
  (runtime.go:269-271). Nothing in the start path logs this or tells the page.
  Neither docs nor tests say the behaviour is intended. The CLI (main.go:375)
  joins the same owner unfiltered and persists to disk.
- **Fixed:** A game whose save owner `saveDirectory` refuses is no longer
  started without a save store: the start is refused through the existing error
  path with a Korean sentence naming the owner, a Warn log names platform and
  owner, and save export/import say the owner cannot be a folder name. KTF and
  LGT owners are now mapped like SKT's, so no platform reaches that refusal
  (P07 of the PR #234 review).

### R009 Handset channel overflow yields MIDI channel >=16, then index panic

`internal/audio/smaf/tonemap.go:192` · media · correctness · CONFIRMED

- **Defect:** When every melodic MIDI channel is taken, toneMap.realChannel
  falls back to `real := index` (line 201). For handset-format tracks the pseudo
  channel goes up to 63, so a channel at or above 16 gets a MIDI channel of 16
  or more. The next allocation runs `used[tones.realMap[other]] = true` on a
  `[16]bool` (line 192; the same loop is in atmosphereSetup at line 438), which
  panics with index out of range.
- **Failure:** A SMAF file with five or more handset-format (HPS) MTR score
  tracks, each playing notes on its 4 melodic channels. handyChannelOffset
  accumulates 0,4,8,12,16 and the shared handyToneMap never resets realMapSet.
  Logical channel 16 (track 5) finds no free candidate, gets realMap[16]=16, and
  is emitted as NoteOn on MIDI channel 16. Logical channel 17 then panics in
  realChannel. I reproduced this with a throwaway overlay test: smaf.Decode
  panicked with 'runtime error: index out of range [16] with length 16'.
  Play/Decode promise that a badly packaged sound stays silent. Instead the
  panic comes out of backend Audio.Load, session.guarded turns it into a
  permanent session failure, and the running game ends. Without the panic, the
  channel-16 events still make loadEvents refuse the whole sound.
- **Evidence:** tonemap.go:188-209: `var used [16]bool` ...
  `used[tones.realMap[other]] = true` (192) ... `real := index` (201), where
  index = pseudoChannel(channel), which is &lt;=63 for HandyPhoneStandard
  (162-168). player.go:90-99 shares handyToneMap across all score tracks, and
  scoreTrackEvents adds 4 to the offset per HPS track (173). tonemap.go:436-439
  repeats the unchecked loop. internal/session/guard.go:45-55 recovers the panic
  and records s.failed, which ends the session.
- **Verification:** Reproduced with a scratchpad overlay test (no repo change).
  Five HPS MTR chunks, each with notes on channels 0-3, made smaf.Decode panic
  with 'index out of range [16] with length 16'. Four tracks decode cleanly (36
  events). The trace: player.go:90-99 shares handyToneMap across every MTR
  chunk, and parseWithBudget accepts up to 4096 chunks, so any number of MTR
  tracks is allowed. player.go:173 adds 4 to the offset per HPS track.
  pseudoChannel (tonemap.go:162-168) passes HPS indexes up to 63. Logical
  channels 0-14 use up the 15 entries of melodyAllocationOrder. Channel 15 falls
  back to real=15, and channel 16 falls back to real=16 (tonemap.go:201). The
  next new allocation then runs used[tones.realMap[16]] on a [16]bool at line
  192, which panics; atmosphereSetup at 436-439 has the same loop. Neither
  Decode (player.go:76) nor Audio.Load (backend/audio.go:163) recovers, and
  SKT's newPlayer calls smaf.Play directly. session.guarded (guard.go:45-55)
  turns the panic into s.failed, which ends the session. Real HPS files carry at
  most 4 tracks (16 channels), which collapses onto channel 15 without
  panicking, so only a malformed or crafted file triggers this. It is still an
  unbounded index on untrusted game data, which the project rules forbid.
  loadEvents (audio_resources.go:90-91) would also refuse channel-16 notes if
  the panic did not happen first.
- **Fixed:** When every MIDI channel is taken, a logical channel past the
  sixteenth maps to a no-channel marker whose events `sequenceEvents` drops and
  both allocation loops ignore, so excess handset channels are dropped instead
  of producing channel >= 16 or panicking; files with up to four handset tracks
  decode identically.

### R010 Image decode allocates header-declared size with no pixel bound

`internal/wipic/png.go:179` · media · untrusted-input · CONFIRMED

- **Defect:** DecodeStandard and DecodeBitmap (bitmap.go:46) pass game image
  bytes straight to image.Decode / x/image/bmp. Those decoders allocate the full
  width\*height buffer from the header before they read any pixel data, with no
  upper bound: Go png only checks for int overflow, and x/image/bmp only checks
  Mul3(w,h,4) for overflow. The KTF and LGT routers call these with no dimension
  pre-check. SKT checks DecodeConfig + backend.RGBAByteLength first.
- **Failure:** In a throwaway overlay test, a 75-byte PNG whose IHDR declares
  8000x8000 RGBA with a 5-byte IDAT made DecodeStandard allocate 244 MB before
  failing with 'not enough pixel data'. The allocation scales with the header:
  an IHDR of 40000x40000, or a 54-byte BMP header declaring 40000x40000 32-bit,
  asks for about 6.4 GB. A 16-bit PNG asks for 8 bytes per pixel. On a Linux or
  Android host with default overcommit, an allocation larger than RAM+swap
  fails, and Go aborts the whole server process with 'fatal error: runtime: out
  of memory', which recover cannot catch. That kills every session. When the
  first decode fails, DecodeStandard may decode again on a repaired copy, which
  repeats the allocation. Reachable from KTF MC_grpCreateImage /
  Image.createImage (decodeGuestImage) and LGT decodeImage / newJavaImage.
- **Evidence:** png.go:178-193 calls stdimage.Decode(bytes.NewReader(data)) up
  to three times with no DecodeConfig gate. bitmap.go:46 calls bmp.Decode
  directly. In x/image/bmp@v0.46.0 reader.go, decodeConfig only checks
  safemath.Mul3 for overflow, and decodePaletted/decodeRGB call
  image.NewPaletted/NewRGBA(c.Width,c.Height) before reading rows. Callers with
  no bound: ktf/wipic_graphics.go:758,773 and lgt/wipic_image.go:196,204.
  Contrast skt/image.go:333-339, which checks DecodeConfig then RGBAByteLength.
- **Verification:** Reproduced with an overlay test: a 62-byte PNG whose IHDR
  says 8000x8000 RGBA, with a 5-byte IDAT, made DecodeStandard allocate 244 MB
  before failing with 'not enough pixel data'. Go 1.27's png parseIHDR
  (reader.go:161-170) only rejects non-positive sizes and int overflow.
  readImagePass calls image.NewRGBA/NewNRGBA64(width,height) (reader.go:457-492)
  before it reads any rows. x/image@v0.46.0 bmp only checks safemath.Mul3 for
  overflow (reader.go:202) and allocates at lines 37/76/105. KTF
  decodeGuestImage (wipic_graphics.go:753-778, called from 1040 and
  runtime_java.go:2132) and LGT decodeImage (wipic_image.go:192-206, called from
  createImage and newJavaImage) have no DecodeConfig or size gate. SKT does have
  one (skt/image.go:333-339, RGBAByteLength). The allocation scales with the
  header up to Go's maxAlloc. On a Linux host with heuristic overcommit, or on
  iOS, a mapping larger than RAM+swap fails and the runtime throws an
  unrecoverable 'out of memory', which aborts the whole server. A corrupted IHDR
  also gets here: the IHDR CRC mismatch fails the first decode, then
  RepairPNGChecksums rewrites the CRC and decodes again (png.go:183-184).
  Whether the process actually aborts depends on host overcommit; the unbounded
  header-sized allocation is certain.
- **Fixed:** DecodeStandard (on every attempt, including checksum-repair
  retries) and DecodeBitmap read the header with DecodeConfig and refuse more
  than `backend.MaxFramebufferPixels` (the rule SKT already applied) as an
  ordinary decode error before the standard decoders allocate.

### R011 Cheat console save/load reach the server filesystem from WebSocket

`internal/cheat/console.go:309` · tools · security · CONFIRMED

- **Defect:** The text console's `save <file>` and `load <file>` take a path
  from the command line and call os.WriteFile/os.ReadFile on it. The console is
  the same object the browser host drives over the session socket, so any client
  that can start a game can write JSON over any file the server can write, and
  can make it read any path it names.
- **Failure:** A device on the LAN connects to the default server (it binds
  every interface, and a request with no Origin header passes SameOrigin). It
  starts any KTF, LGT or SKT game and sends
  {"kind":"cheat","op":"console","command":"save
  var/savedata/release/ktf/&lt;owner>/&lt;key>"}. The player's save is replaced
  with cheat-table JSON, or the command overwrites any other writable file.
  Sending `load /dev/zero` makes os.ReadFile allocate without bound on the
  emulator goroutine until the process is OOM-killed, taking every session and
  parked game with it.
- **Evidence:** console.go:300-320: save calls os.WriteFile(args[0], data,
  0o644). console.go:322-330: load calls os.ReadFile(args[0]).
  webhost/session.go:1324-1331 passes message.Command into console.Execute with
  no command filtering. cmd/server/main.go:347-351: the default address has an
  empty host, so it binds every interface. wsproto/handshake.go:134-147 accepts
  a request that has no Origin header. The page already has
  PanelSaveTable/PanelLoadTable (panel.go:241-266), which exchange table text
  and need no server path.
- **Verification:** I traced this through the code. Console.execute
  (console.go:104-107) sends `save` and `load` straight to os.WriteFile(args[0],
  …, 0o644) at console.go:309 and os.ReadFile(args[0]) at :326, with no root and
  no path check. webhost/session.go:698 routes every clientCheat message to
  runCheat, and runCheat calls console.Execute(message.Command) at :1326 with no
  filtering. CheatConsole is not gated by build profile
  (session/session.go:466-480). serveSession (session.go:107-139) has no
  authentication. The default address binds every interface
  (cmd/server/main.go:347-351), and SameOrigin accepts a request with no Origin
  header (wsproto/handshake.go:134-147). The page itself never sends console
  commands (web/session.js:340 cheat(command) has no caller), so the console is
  only an attack surface. `load /dev/zero` makes os.ReadFile grow without bound
  and OOM the whole process. This breaks the stated rule that the page may only
  touch var/ext.
- **Fixed:** The cheat console's file access is opt-in: `save`/`load` refuse
  unless `Console.AllowFiles()` was called, only the CLI's `-cheat` attach calls
  it, `load` reads at most 4 MiB, and a test fails if any non-CLI source calls
  AllowFiles.

## Medium (54)

### R012 DataInputStream fixed-width reads raise IOException, not EOFException

`internal/jvm/corelib_io.go:918` · jvm · correctness · CONFIRMED

- **Defect:** Every fixed-width DataInputStream read (readByte,
  readUnsignedByte, readShort, readUnsignedShort, readChar, readInt, readLong,
  readFloat, readDouble and readBoolean) goes through dataInputRequired, which
  raises java/io/IOException at end of stream. The DataInput contract is
  java/io/EOFException. The readUTF builtin does the same on a truncated string.
  readFully, by contrast, already raises EOFException.
- **Failure:** A title reads records until the end with `try { while(true) v =
  in.readInt(); } catch (EOFException e) { done }`. Here the exception is a
  plain IOException, so the EOFException handler never matches. Control goes to
  an outer IOException handler (treated as a corrupt save or resource) or the
  exception escapes and ends the session. A probe that read past the end of a
  1-byte stream returned 2 (IOException branch) instead of 1 (EOFException
  branch).
- **Evidence:** corelib_io.go:917-919 `if value < 0 { return 0,
  guestException(IOExceptionClass, "unexpected end of stream") }`;
  corelib_io.go:824 readFully uses java/io/EOFException; builtins.go:303-304
  readUTF uses IOExceptionClass; exception.go:101 declares EOFException under
  IOException.
- **Verification:** dataInputRequired raises IOExceptionClass at end of stream
  (corelib_io.go:917-919). It backs readByte, readUnsignedByte, readShort,
  readChar, readInt and the others (corelib_io.go:947, 963, 991, 995, 1009). The
  readUTF builtin also raises IOException on truncation (builtins.go:304).
  CLDC's DataInput contract specifies EOFException, and readFully here already
  raises it (corelib_io.go:824). EOFException is declared as a subclass of
  IOException (exception.go:101), so a `catch (EOFException)` handler never
  matches this exception. No doc records this as intentional: docs/jvm.md:129
  only lists that EOFException exists.
- **Fixed:** Every fixed-width DataInputStream read and a truncated readUTF
  raise java/io/EOFException at end of input instead of a plain IOException.

### R013 Cyclic superclass chain from a class file hangs method resolution

`internal/jvm/vm.go:729` · jvm · untrusted-input · CONFIRMED

- **Defect:** The loader never checks the superclass chain for a cycle. The
  unbounded walks in resolveInstanceMethod, resolveStaticMethod,
  resolveNativeEntry and catches() therefore follow it for ever without
  consuming instruction steps, checking VM.closed or consulting RenewSteps. The
  comment in resolveField claims the loader refuses a cycle, but it does not.
- **Failure:** A crafted or corrupted JAR has class C whose super_class names C
  itself (or A→B→A). `new C().hashCode()` runs resolveInstanceMethod(C,
  hashCode): C does not declare it, className becomes C.SuperName == C, and this
  repeats for ever. A probe with a patched class file spun past 3 s, and
  VM.Close cannot stop it. catches() detects only the self-cycle, so an
  exception class in a 2-cycle hangs handler search the same way. The session
  goroutine burns a CPU core indefinitely.
- **Evidence:** vm.go:718-730 `for className != "" { ... className =
  class.SuperName }` with no depth or visited set; vm.go:685-697 is the same for
  static methods; vm.go:779-801 resolveNativeEntry; exception.go:60-76 catches()
  breaks only when SuperName == current; loader.go:111-159 Load does no
  hierarchy validation; vm.go:1012 comment 'The chain is bounded by the loader,
  which refuses a cycle'. By contrast, IsSubclassOf (vm.go:516-527) and layout
  (heap.go:66-70) do bound it.
- **Verification:** Loader.Load (loader.go:111-159) and classfile.Parse
  (class.go:116-123) never validate the superclass chain. resolveInstanceMethod
  (vm.go:718-730), resolveStaticMethod (vm.go:685-697) and resolveNativeEntry
  (vm.go:779-801) loop on SuperName with no depth limit or visited set.
  catches() (exception.go:60-76) breaks only on a self-cycle. ensureInitialized
  does not hang, because state.initializing short-circuits re-entry, so
  execution reaches the method walk. An overlay probe built a class whose
  super_class names itself and called InvokeVirtual(hashCode). It spun for more
  than 2 s, and nothing in the loop checks vm.closed. The comment at vm.go:1012
  ('bounded by the loader, which refuses a cycle') is false. Only crafted or
  corrupted JARs can trigger this, but the repository rules treat class files as
  untrusted.
- **Fixed:** resolveStaticMethod, resolveInstanceMethod, resolveNativeEntry and
  catches() stop after 64 superclasses (`maxSuperclassDepth`), so a cyclic chain
  fails resolution instead of spinning.

### R014 multianewarray total allocation is unbounded

`internal/jvm/vm.go:1106` · jvm · untrusted-input · CONFIRMED

- **Defect:** MaxArrayLength bounds each array individually. newMultiArray then
  multiplies the dimensions with no total budget, and every element is a 24-byte
  Value, so a single bytecode instruction can request many gigabytes. That runs
  the whole server process out of memory, not only the guest.
- **Failure:** Guest bytecode executes `new int[16384][16384]`. Each dimension
  passes the per-array check (16384 &lt; 16M), and newMultiArray allocates 16384
  child arrays of 16384 Values: 268M elements × 24 bytes ≈ 6.4 GB. The Go
  runtime aborts with out-of-memory and every session on the server dies. Even a
  single legal `new byte[16_000_000]` costs about 384 MB.
- **Evidence:** vm.go:1076 per-array check only; vm.go:1079 make([]Value,
  length); vm.go:1106-1114 loop allocating one child per element with no running
  total; value.go:23-27 Value{kind uint8; bits uint64; ref \*Object} is 24 bytes
  per element, even for byte arrays.
- **Verification:** newArray checks only the per-array length against
  MaxArrayLength (vm.go:1076, default 16M at vm.go:280-281), then calls
  make([]Value, length) and fills each slot with zeroValue, which commits the
  memory. newMultiArray (vm.go:1106-1114) recurses for each element with no
  running total. Value is {uint8 kind; uint64 bits; \*Object ref}, which is 24
  bytes (value.go:23-27). So `new int[16384][16384]` commits about 6.4 GB in one
  instruction. No platform overrides MaxArrayLength. Only crafted bytecode
  reaches this. The VM also has no global heap budget, so an allocation loop can
  exhaust memory as well; fixing multianewarray alone would not close that.
- **Fixed:** multianewarray checks every count for NegativeArraySizeException
  first, then caps one instruction's total at MaxArrayLength (each array
  counting as 16 elements), failing with the limit error newArray uses; there is
  still no global heap budget (documented).

### R015 ARM misc encodings (CLZ, BLX Rm, QADD, SMULxy) run as flag-setting TST/CMP

`internal/armcore/arm.go:85` · vm · correctness · CONFIRMED (reviewer said high)

- **Defect:** classifyARM sends every encoding with bits 27:26 == 00 that
  matched nothing earlier to armDataProcessing. That includes the
  opcode-8..11/S=0 miscellaneous space (CLZ, BLX register, QADD/QSUB,
  SMULxy/SMLAxy, BKPT, and v6T2 MOVW/MOVT). executeARMDataProcessing then forces
  setFlags for TST/TEQ/CMP/CMN, so these instructions overwrite NZC(V), write no
  result and do not branch. They should raise ErrUndefinedInstruction, as the
  docs promise for anything unsupported.
- **Failure:** An ARM-state module built for ARMv5TE runs `clz r0, r1`
  (0xe16f0f11) in a division helper: r0 keeps its old value, the flags are
  overwritten by a CMN of pc+8 and a shifted r1, and execution continues
  silently. `blx r2` (0xe12fff32) falls through to the next instruction without
  calling and without setting LR. Probed via a go test -overlay harness:
  clz/blx/smulbb/qadd all returned err=nil with r0 unchanged, and qadd set Z.
- **Evidence:** arm.go:85 `case instruction&0x0c000000 == 0: return
  armDataProcessing` comes after the BX (0x012fff10 exact), MRS/MSR, multiply
  and halfword tests, none of which match 0x012fff3x, 0x016f0f1x, 0x0160xxx8 or
  0x0100xx5x. arm.go:283-299: opcodes 0x8-0xb set `writeResult = false; setFlags
  = true` without checking bit 20. Overlay probe output: `clz r0, r1: err=<nil>
  r0=0x55`, `blx r2: err=<nil> pc=0x1004 lr=0x0`, `qadd ...: cpsr=0x40000010`.
- **Verification:** I traced each encoding through classifyARM (arm.go:62-90).
  clz 0xe16f0f11, blx r2 0xe12fff32, qadd and smulbb match none of the earlier
  masks: BX needs exactly 0x012fff10, MRS/MSR register needs bits 7:4 == 0, and
  the multiply/halfword tests need bits 7:4 == 1001 or 1xx1 with the right top
  bits. All of them reach `instruction&0x0c000000 == 0` and become
  armDataProcessing. executeARMDataProcessing (arm.go:283-299) then forces
  setFlags=true and writeResult=false for opcodes 8-b without looking at bit 20.
  armOperand2 refuses only a register shift with bit 7 set (arm.go:348), so none
  of these encodings fault. They run as TST/TEQ/CMP/CMN: the flags are
  clobbered, nothing is written and nothing branches. docs/armcore.md:69-72
  describes the core as ARMv4T plus the observed BLX forms. These encodings are
  undefined there, so silently running them is wrong under either reading. I
  lowered the severity from high: no corpus title is shown to contain ARM-state
  v5TE encodings. LGT modules are built for v5T (thumb.go:399-406) and contain
  some ARM code, and a v5TE division helper or an ARM `blx rN` would misbehave
  silently instead of faulting.
- **Fixed:** The opcode-8..11/S=0 space is decoded as ARMv5TE does it: CLZ, BLX
  register, QADD/QSUB/QDADD/QDSUB and SMUL/SMLA/SMULW/SMLAW/SMLAL&lt;x>&lt;y>
  are implemented with the Q flag (MSR can write Q), and the rest of that space
  (BKPT, BXJ, MOVW/MOVT) faults with ErrUndefinedInstruction instead of running
  as a flag-setting compare.

### R016 Fill-loop stand-in ignores store/advance order and mismatched base

`internal/armcore/fill_loop.go:402` · vm · correctness · CONFIRMED

- **Defect:** analyseStoreLoop never checks that the halfword store comes before
  the pointer's `adds #2`, and it never checks that the store's base register is
  the register being advanced: the ADD case simply overwrites loop.pointer,
  which the store had already set. runStoreLoop then fills [ptr, ptr+span) from
  the register value at the loop head. Pre-increment bodies and bodies whose
  store base differs from the advanced register are therefore emulated wrongly,
  and the write-permission check covers the wrong span.
- **Failure:** Body `adds r0,#2; strh r1,[r0]; subs r2,#1; bhs head`: the guest
  writes [r0+2 .. r0+2n]. The stand-in writes [r0 .. r0+2n-2], so the last
  halfword of every fill is never written, and a fault at the real end of the
  span is not checked. Body `strh r1,[r3]; adds r0,#2; subs r2,#1; bhs head`:
  the guest writes only [r3] repeatedly, but the stand-in writes 2n bytes
  starting at r0. Overlay A/B against standInsRefused: 1 halfword differs in the
  first case and 40 in the second.
- **Evidence:** fill_loop.go:385 (store: `haveStore, loop.pointer, loop.width =
  true, base, 2`) and fill_loop.go:398-404 (ADD: `loop.pointer = register`, no
  check against the store base and no ordering check). The final check at :461
  only tests written[loop.pointer]==1, which the ADD register satisfies.
  runStoreLoop:227-258 uses `start := context.Registers[loop.pointer]` as the
  first destination.
- **Verification:** For `adds r0,#2; strh r1,[r0]; subs r2,#1; bhs`, the walk at
  fill_loop.go:376-404 sets pointer=r0 from the ADD and again from the store.
  Nothing records whether the store comes before or after the advance. The
  checks at :461-490 pass: written[r0]==1, written[r2]==1, and the value r1 is
  never written. runStoreLoop:227 starts at the head-time r0, so it writes [r0,
  r0+2n-2], while the guest writes [r0+2, r0+2n]. That writes one halfword the
  guest never touches, skips the last one, and validates the wrong span. In the
  second case the store base r3 is overwritten by `loop.pointer = register` at
  :402 and never compared, so a body storing through r3 while advancing r0 is
  also accepted. The pre-increment shape is a plausible compiler output, but no
  corpus title is shown to contain it. The mismatched-base shape is unrealistic.
- **Fixed:** The fill recogniser accepts the pointer advance only after the
  store and only on the register the store went through, so pre-increment and
  mismatched-base bodies are left to the interpreter.

### R017 gdb detach/disconnect leaves the guest parked and breakpoints armed

`internal/gdbstub/session.go:154` · vm · protocol · CONFIRMED

- **Defect:** The 'D' packet returns "OK", done=true. Serve returns, and Listen
  goes back to Accept. Neither releases a guest parked in CoreTarget.hook or
  detaches the debugger, so the game stays frozen until another client connects
  and sends 'c'. The same happens when the connection drops. Continue also
  blocks Serve in `<-target.stops`, so a Ctrl-C (0x03) or a disconnect while the
  target runs is never seen and no other client can be accepted. A non-EOF read
  error (for example a connection reset) makes Listen return and stops the stub
  for good. This contradicts qAttached's comment and Detach's doc, which both
  say a detaching client leaves the game running.
- **Failure:** The client stops at a breakpoint, gdb sends `z0,1008,4` then `D`,
  and the guest stays blocked on `<-wakeup` indefinitely. Probed with an overlay
  test on the existing core fixture: replies ["OK" "OK"], then "guest still
  parked 2s after the client detached".
- **Evidence:** session.go:154-155 `case 'D': return "OK", true, nil`;
  listen.go:28-35 only closes the connection and loops; core.go:52-55 Detach
  (which releases) is called only by the CLI's exit defer
  (cmd/cli/main.go:1179-1182); core.go:181 `stop, ok := <-target.stops` blocks
  Serve during 'c'; listen.go:33-34 returns on any error other than
  ErrDetached/EOF.
- **Verification:** 'D' returns ("OK", done=true) (session.go:154-155). Serve
  returns nil, and Listen (listen.go:28-35) only closes the connection and calls
  Accept again. Nothing calls target.release() or Detach(). Detach is called
  only from the CLI's exit defer (cmd/cli/main.go:1178-1182). A guest parked in
  hook (core.go:88, `<-wakeup`) stays parked, and the debugger stays attached at
  one instruction per quantum. 'c' blocks Serve in `<-target.stops`
  (core.go:181), so a 0x03 or a disconnect during a run is not read until the
  next stop. readPacket maps only io.EOF to ErrDetached, so a connection reset
  makes Listen return at :33-34 and the stub stops for good. This contradicts
  Detach's doc (core.go:50-51) and the qAttached comment (session.go:174-176).
  The gdb stub is a dev-only CLI feature, which bounds the impact.
- **Fixed:** However a gdb connection ends (D, hang-up, reset), Serve removes
  the client's breakpoints and releases the parked guest; the connection is read
  on its own goroutine so Ctrl-C and a hang-up are heard while the target runs,
  and Listen keeps accepting after any per-connection error.

### R018 Duplicate ZIP entries bypass the expanded-size budget (decompression DoS)

`internal/platform/ktf/archive.go:407` · ktf1 · untrusted-input · CONFIRMED

- **Defect:** In readZIP, the duplicate-name branch fully decompresses each
  later entry with readEntry (up to 128 MiB each) to compare it with the first.
  This happens before the per-entry size check and without charging the
  maxExpandedSize budget. An archive of up to 8192 entries with the same name
  can therefore force terabytes of inflate work. The entries can point at the
  same compressed data.
- **Failure:** A crafted archive is uploaded through the page (var/ext) or
  placed in var/games. It holds 8191 central-directory entries named `x`, all
  pointing at one local header whose deflate stream expands to 128 MiB.
  Open/ReadDescriptor call readZIP. The first `x` is charged once, and each
  duplicate runs readEntry to inflate another 128 MiB. That is about 1 TiB of
  decompression, tens of minutes of CPU per load, while the 512 MiB
  maxExpandedSize budget never trips. Even without overlapping entries, 128 MiB
  of input at about 1000:1 gives over 100 GiB.
- **Evidence:** archive.go:401-415: the duplicate branch calls readEntry(file)
  and `continue`s before the maxEntrySize check at line 417 and before the
  expanded-budget accounting at 420-423. maxEntryCount = 8192 (line 29), and
  readEntry reads up to maxEntrySize+1 (line 451). Go's archive/zip does not
  reject overlapping local headers.
- **Verification:** archive.go:401-415: for a case-folded duplicate name,
  readZIP calls readEntry(file) before the maxEntrySize check (line 417) and
  before the maxExpandedSize accounting (lines 420-423), then `continue`s
  whenever the bytes equal the first entry. readEntry inflates up to
  maxEntrySize+1 = 128 MiB (line 451). Go's checksumReader stops only at the
  declared UncompressedSize64, which the attacker sets (go1.27
  archive/zip/reader.go:301), and the Go 1.27 reader has no check for
  overlapping local headers. So up to maxEntryCount-1 = 8191 identical entries,
  which can share one local header, each cost a full 128 MiB inflate plus an
  io.ReadAll buffer, and the 512 MiB budget never trips. Untrusted archives
  reach this through ktf.Open/ReadDescriptor (archive.go:102/118 -> readOuterZIP
  -> readZIP), from var/games and from page uploads. webhost/upload.go stores
  the bytes unexamined, up to 32 MiB, which is still enough for thousands of
  central-directory entries. The CLAUDE.md untrusted-input rule makes this a
  real bounds gap. It is a CPU and memory DoS, not corruption, so medium.
- **Fixed:** readZIP applies the per-entry size check and the 512 MiB expansion
  budget to a duplicate entry before inflating it, so identical copies of one
  name can no longer force unbounded decompression.

### R019 Hooked memset/memcpy allocate a guest-chosen length up to 4 GiB

`internal/platform/ktf/binary_hooks.go:197` · ktf1 · untrusted-input · CONFIRMED

- **Defect:** handleBinaryHookCall takes the length from guest r2 and calls
  hookBuffer(length), which runs make([]byte, length) for anything over 256 KiB.
  The destination or source range is only checked by the later Memory.Write or
  Memory.Read. memset then fills every byte of that buffer in Go before the
  write fails.
- **Failure:** A title's memset receives a negative computed size (r2 =
  0xFFFFFFFF). That is a fault on a handset, but here it allocates about 4 GiB
  and the fill loop touches all of it before memory.Write rejects the unmapped
  range. On the 64-bit server this pushes RSS to about 4 GiB and the OS kills
  it, taking every session down. On mobile the app is killed. On a 32-bit
  target, make panics with 'len out of range' in the session goroutine. memcpy
  with the same length reserves the 4 GiB before Read fails.
- **Evidence:** binary_hooks.go:195-198 `if length > hookWindowLimit { return
  make([]byte, length) }`. binary_hooks.go:211-224: memset takes the length from
  hookArguments (r2), fills with `for index := range fill`, and only then calls
  memory.Write. binary_hooks.go:229-238: memcpy allocates before memory.Read.
  Only strcpy is bounded, by maxGuestStringLength (1 MiB).
- **Verification:** binary_hooks.go:195-198 returns `make([]byte, length)` for
  any length above 256 KiB, and the length comes straight from guest r2
  (hookArguments, lines 296-310) with no upper bound. For memset, lines 218-221
  fill every byte in Go before memory.Write validates the destination range
  (armcore/memory.go:434). r2=0xFFFFFFFF therefore allocates and touches about 4
  GiB. Hooks are installed by code-pattern match on any image
  (installBinaryHooks), so a crafted or buggy title reaches this, and the
  CLAUDE.md rule requires guest lengths to be bounded. Two parts of the claimed
  impact are overstated. memcpy allocates the buffer without touching it, and
  Memory.Read validates the range first, so memcpy is mostly a virtual
  reservation. And `make dist` builds only 64-bit targets (Makefile:18), so the
  32-bit panic does not apply to shipped builds. The memset RSS spike, which can
  get a mobile app or a constrained host killed, does hold.
- **Fixed:** Hooked memset/memcpy validate the guest destination and source with
  Memory.ValidateRange before sizing a host buffer from the r2 length, so a
  garbage length is refused without a multi-GiB allocation.

### R020 Unset/re-arm of another C timer during a timer round is lost or duplicated

`internal/platform/ktf/client.go:678` · ktf1 · correctness · CONFIRMED

- **Defect:** During a timer round, every queued timer lives in the local
  `pending` slice and runtime.pendingTimers starts empty. MC_knlUnsetTimer and
  MC_knlSetTimer only search runtime.pendingTimers. When callback A cancels or
  re-arms a timer B that comes later in the batch, the cancel does nothing, or
  the re-arm adds a second entry. The loop then still runs B if it is due, or
  re-appends the stale B if it is not.
- **Failure:** Timer A (index 0) and timeout timer B (index 1) are both queued.
  A's callback calls MC_knlUnsetTimer(&B) to cancel the timeout. wipicUnsetTimer
  filters runtime.pendingTimers, which does not hold B. The loop then reaches B:
  if B is due, its callback runs although it was cancelled; if not, line 678
  re-queues it and it fires later. If A instead calls MC_knlSetTimer(&B, 1000)
  to push the timeout back, wipicSetTimer appends a new B because the stale one
  is not in the queue. Line 678 then re-appends the stale B as well, so B fires
  twice, and every later re-arm replaces only the first of the two entries.
- **Evidence:** client.go:664-665 empties the queue for the round.
  client.go:677-679 re-appends not-yet-due stale entries unconditionally.
  initialization.go:2054-2063 (wipicSetTimer) and 2072-2078 (wipicUnsetTimer)
  scan only runtime.pendingTimers. The comment at initialization.go:2047-2053
  covers only a callback re-arming itself, not other timers in the same batch.
- **Verification:** During a round, entries not yet processed exist only in the
  local `pending` (client.go:664-665). wipicUnsetTimer
  (initialization.go:2072-2078) filters only runtime.pendingTimers, and
  wipicSetTimer (2054-2063) searches only runtime.pendingTimers before
  appending. The loop never checks whether an entry was cancelled: line 674
  skips only callback==0 && task==nil, then the entry runs if due or is
  re-appended at line 678. So when callback A unsets timer B, which sits later
  in the same batch, the cancel is lost. When A re-arms B, B gets a new entry
  and the stale one is also re-queued, which makes a duplicate. The comment at
  initialization.go:2047-2053 and docs/history/ktf.md:6664-6666 ('A running
  callback is not in the queue') cover only a callback re-arming itself, not
  other records in the batch. The trace is complete. How often it happens
  depends on titles that keep two C timers where one cancels or reschedules the
  other.
- **Fixed:** A timer round exposes its not-yet-reached entries as
  `runtime.roundTimers`; MC_knlUnsetTimer, MC_knlSetTimer and Java
  Timer/TimerTask cancel matching entries there, and zeroed entries are skipped
  and never re-queued, so a cancel from a callback is honoured and a re-arm
  stays one timer.

### R021 Uncaught exception in one C timer drops every other pending timer

`internal/platform/ktf/client.go:744` · ktf1 · correctness · CONFIRMED

- **Defect:** serviceTimersLocked moves the whole pendingTimers queue into a
  local `pending` slice and sets the queue to nil. A timer callback that ends in
  an error returns at once, and pending[index+1:] is never put back.
  Session.Tick then absorbs an uncaught guest exception from a timer and keeps
  the session running, so every timer queued after the failing one is lost,
  including timers that were not yet due.
- **Failure:** A title arms a frame-loop timer A and a second timer B, in that
  queue order. A's callback throws a Java exception nobody catches, so
  throwAOTException returns UncaughtAOTException, which armcore wraps with %w.
  ServiceTimers returns the error at line 741/744 before reaching B. Tick passes
  it to absorbUncaughtCallback('timer'), which returns nil because it unwraps to
  \*jvm.GuestException. The session continues, but B has silently vanished from
  runtime.pendingTimers. If B was the game loop or a pending transition, the
  game freezes with no error shown.
- **Evidence:** client.go:664-665 `pending := client.runtime.pendingTimers;
  client.runtime.pendingTimers = nil`. client.go:732-745 returns on err with no
  re-append of pending[index+1:] (the limit branch at 669-672 does re-append).
  session.go:386-391 `timerErr = client.absorbUncaughtCallback("timer",
  timerErr)`. uncaught.go:41-66 absorbs any error that unwraps to
  \*jvm.GuestException. aot.go:86-91: UncaughtAOTException.Unwrap returns the
  GuestException. armcore/core.go:454 wraps handler errors with %w.
- **Verification:** client.go:664-665 moves the whole queue into the local
  `pending` and sets runtime.pendingTimers to nil. On a callback error, lines
  741/744 return without putting pending[index+1:] back; only the limit branch
  at 669-672 re-appends the rest. UncaughtAOTException.Unwrap returns the
  \*jvm.GuestException (aot.go:86-91), and armcore wraps handler errors with %w
  (armcore/core.go:452). session.go:386-390 runs the timer error through
  absorbUncaughtCallback("timer"), which returns nil for anything that unwraps
  to \*jvm.GuestException (uncaught.go:41-46). The session therefore continues
  with the later C timers, and any java/util/Timer task entries that share this
  slice, silently gone. This contradicts uncaught.go's stated design that 'a
  callback that threw did not happen, and the next one still does'. C timers are
  used by KTF titles that also run AOT Java code (docs/history/ktf.md:1985,
  2752), so a Java throw inside a timer callback is a real path. The anchor
  should be line 744 (the absorbed error), not 741, which is the guest-fault
  branch.
- **Fixed:** When a timer callback ends in an error, `serviceTimersLocked`
  re-queues the rest of the round (minus cancelled entries) before returning, so
  an absorbed uncaught exception no longer drops every later timer.

### R022 Allocation-triggered GC skips the active worker's registers

`internal/platform/ktf/collect.go:383` · ktf1 · correctness · CONFIRMED

- **Defect:** collectionThreads only scans client.thread and client.workers.
  ServiceThreads takes the running worker out of client.workers for the length
  of its slice. If an allocation fails inside a guest worker's slice,
  collectForAllocation runs without the live registers or thread-local words of
  the thread that is actually running, so objects held only in its registers are
  treated as unreachable.
- **Failure:** The arena is close to its 64 MiB limit. A guest Thread.run does
  `A a = new A(); int[] b = new int[n]; a.f = b;`, so after the first `new` the
  reference to `a` sits only in callee-saved r4. The newarray SVC finds the
  arena full and calls collectForAllocation. `a` is unmarked, so
  ReleaseAOTObject drops its strong binding. If Go GC then collects the
  jvm.Object, the next end-of-round cycle marks it from the parked worker's
  registers and counts it as Lost. Later platform calls on `a` then fail with
  'unbound guest address'. If a second allocation fails in the same slice, `a`
  is freed instead: releaseObjectMemory zeroes it and the arena hands its bytes
  to a new object while the guest still writes through r4, which corrupts guest
  memory.
- **Evidence:** collect.go:383-394 builds the thread list from client.thread and
  client.workers only. client.go:475-476 pops the worker (`client.workers =
  client.workers[1:]`) before `worker.grant <-` and re-appends it only after the
  slice (lines 492/495). initialization.go:2623-2630: runtime.allocate calls
  collectForAllocation on every arena refusal, from any SVC including a
  worker's. collect.go:327-333 enters collectGuestObjects directly, with no
  guard for client.activeWorker != nil. collect.go:199-208: the root registers
  come only from collectionThreads().
- **Verification:** client.go:474-475 removes the running worker from
  client.workers (`client.workers = client.workers[1:]`) before `worker.grant
  <-`, and only puts it back after `<-worker.events` (lines 492/495).
  collectionThreads (collect.go:383-394) lists only client.thread and
  client.workers. Worker ARM threads are independent: workers.go:140 creates
  them with `armcore.NewThread(initial)` and never makes them derived calls of
  client.thread, so client.thread.LiveContexts() (armcore/core.go:184) does not
  reach them either. runtime.allocate (initialization.go:2618-2630) calls
  collectForAllocation on any arena refusal, including from an SVC on the
  worker's goroutine, and collectForAllocation (collect.go:327-333) has no
  activeWorker guard. Worker stacks are RW memory and get scanned, but values
  held only in r4-r11 at the SVC (svcStub at client.go:40-49 restores r4 before
  `svc`) are not roots. With nothing marking it, the object is unpinned by
  ReleaseAOTObject. The next cycle then counts it as Lost (collect.go:263-274)
  if Go has collected its Object, or frees it (collect.go:288-293) if a second
  allocation in the same slice fails. The collectForAllocation comment and
  docs/history/ktf.md:235 argue safety only for Go frames and the Host thread;
  they do not cover the running worker's registers. It needs an arena close to
  full during a worker slice, so medium.
- **Fixed:** `collectionThreads` includes `client.activeWorker`'s ARM thread, so
  a collection triggered by an allocation during a worker slice treats that
  worker's registers and thread-local words as roots.

### R023 Arc and round-rect bindings pass unclipped guest sizes to O(w\*h) loops

`internal/platform/ktf/runtime_curves.go:54` · ktf2 · untrusted-input · CONFIRMED (reviewer said low)

- **Defect:** graphicsFillArc/DrawArc/FillRoundRect/DrawRoundRect hand raw guest
  width/height to internal/curve, whose loops iterate every row and column of
  the bounding box (or 2\*pi\*radius steps) before the per-span clip. FillArc's
  inner loop uses `column <= width`, so width == Integer.MAX_VALUE never
  terminates.
- **Failure:** Guest calls g.fillArc(0, 0, Integer.MAX_VALUE, 1, 0, 360): the
  column loop wraps and never exits; fillArc(0,0,100000,100000,0,360) runs 1e10
  float iterations. The session worker hangs with the run lock held.
- **Evidence:** runtime_curves.go:54-61 return curve.FillArc/DrawArc(x, y,
  width, height, ...) with no intersection against state.clip/target; :30-51
  likewise for round rects; internal/curve/curve.go:180-213 for row &lt; height
  { for column := int32(0); column &lt;= width; column++ }, :217-224 steps :=
  2\*pi\*max(radius).
- **Verification:** graphicsFillArc and graphicsDrawArc
  (runtime_curves.go:54-61) pass raw guest sizes to internal/curve. FillArc
  loops `for column := int32(0); column <= width; column++`
  (curve/curve.go:193), so width == MaxInt32 wraps and never ends. A
  100000x100000 arc costs about 1e10 float iterations before the clipped span
  emit. DrawArc's step count grows with the radius (:217-224), and
  FillRoundRect/DrawRoundRect iterate every row up to height. Nothing intersects
  these with the clip first. This is the same unbounded-host-loop class as [3]
  and [4], so the severity is raised to match.
- **Fixed:** The KTF Java Graphics fillArc/drawArc/fillRoundRect/drawRoundRect
  bindings pass the clip met with the target, moved back by the translation, to
  bounded curve walks, so a huge guest box costs only its visible part and
  on-screen pixels (XOR included) stay identical.

### R024 Java File stream writes persist the whole file once per byte

`internal/platform/ktf/runtime_file.go:408` · ktf2 · untrusted-input · CONFIRMED

- **Defect:** runtimeFileWrite stores the entire file through
  saveChanges/StoreSaves on every call, and FileOutputStream.write(I) plus
  DataOutputStream.writeInt/writeShort/etc. reach it one byte at a time. Each
  byte therefore costs a locked, fsynced, renamed whole-file write: O(n) durable
  writes and O(n^2) bytes for an n-byte save, and the save is only atomic per
  byte.
- **Failure:** A title saving 2 KB through
  File.openDataOutputStream().writeInt(...) triggers ~2048
  DirectorySaveStore.StoreSave calls, each taking the save-tree lock, writing a
  temp file, Sync (F_FULLFSYNC on darwin), rename and directory sync; the
  session worker stalls for seconds to tens of seconds while holding the run
  lock, and a session that ends mid-save leaves a store copy that is the new
  prefix over the old tail. The LGT platform buffers the same stream and drains
  on flush/close.
- **Evidence:** runtime_file.go:405-415 staged whole file,
  storeGuestFile(state.name, staged) per call; :577-587 runtimeFileWriteByte
  wraps a single byte into runtimeFileWrite; :506 FileOutputStream.write(I)V ->
  runtimeFileWriteByte; jvm/corelib_io.go dataOutputByte/dataOutputWriteInt
  invoke out.write(I)V per byte; save.go:25-66 saveChanges ->
  backend.StoreSaves; backend/save.go:171-236 StoreSave
  lock+CreateTemp+Sync+Rename+syncDirectory.
- **Verification:** FileOutputStream.write(I) is bound to runtimeFileWriteByte
  (runtime_file.go:506). That wraps a one-byte array into runtimeFileWrite
  (:577-587), which writes the whole staged file through storeGuestFile on every
  call (:405-408). The ledger is unchanged for an existing name, so saveChanges
  (save.go:25-66) passes StoreSaves a single entry, which goes straight to
  DirectorySaveStore.StoreSave (backend/save_batch.go:22-25). Each StoreSave
  takes the save-tree lock, writes a temp file, Syncs, renames and syncs the
  directory (backend/save.go:171-236). openDataOutputStream wraps that stream in
  the jvm DataOutputStream (runtime_file.go:536-541), and its
  writeShort/writeInt/writeLong call out.write(I)V once per byte
  (jvm/corelib_io.go:1146-1226). A KTF title needed openDataOutputStream, which
  is why it was added (docs/history/ktf.md:2713). The comment at
  runtime_file.go:506-508 documents write-through as intended, but not the cost
  of one durable whole-file write per byte.
- **Fixed:** File writes made inside one guest DataOutputStream call (writeInt,
  writeUTF and the like) are held and stored once as that call returns; a
  refused store throws IOException from that call and restores the file, any
  other platform call from the thread stores held bytes first, and a guest loop
  calling write(I) itself still stores once per call by the documented rule.

### R025 fillPolygon scanline loop wraps forever at Integer.MAX_VALUE

`internal/platform/ktf/runtime_graphics.go:1297` · ktf2 · untrusted-input · CONFIRMED

- **Defect:** The fill loop iterates y from the polygon's min to max y with `y
  <= bottom` in int32 and never clips to the clip/target rows. A vertex at
  Integer.MAX_VALUE makes the condition always true (y wraps), and a span from
  MIN to MAX iterates 2^32 rows each scanning every edge.
- **Failure:** Guest calls fillPolygon(new int[]{0,10,5}, new
  int[]{0,0,Integer.MAX_VALUE}); bottom=MaxInt32, y++ wraps to MinInt32 and the
  host loop never ends, hanging the session worker with the run lock held.
  Confirmed the wrap with a standalone loop.
- **Evidence:** runtime_graphics.go:1288-1296 top/bottom from guest ys; :1297
  for y := top; y &lt;= bottom; y++ { ... } with no intersection against
  state.clip/target height; :1306 crossings use int32 products that also
  overflow.
- **Verification:** runtime_graphics.go:1288-1297 takes top and bottom from raw
  guest ys and runs `for y := top; y <= bottom; y++` in int32, with no
  intersection against the clip or the target. With bottom == MaxInt32 the
  condition is always true and y++ wraps, so the loop never ends. The crossings
  arithmetic at :1306 wraps silently but cannot panic: y1 != y0 is guaranteed,
  and int32 MIN/-1 does not panic in Go. Nothing stops the loop. As with [3],
  the input is extreme, but the result is a host hang.
- **Fixed:** fillPolygon scans only the rows that can reach the target, computed
  in int64 against the translation, so a vertex at Integer.MAX_VALUE no longer
  wraps the row counter.

### R026 drawLine/drawPolygon Bresenham never terminates on Integer.MIN_VALUE

`internal/platform/ktf/runtime_graphics.go:1343` · ktf2 · untrusted-input · CONFIRMED

- **Defect:** graphicsDrawSegment negates deltaX/deltaY in int32 and doubles the
  error term in int32 without clipping the segment first. With an endpoint at
  Integer.MIN_VALUE the negation overflows, 2\*difference wraps to 0, neither
  coordinate ever advances, and the loop spins forever; large but finite spans
  iterate up to 2^32 times. No instruction budget or context check applies to
  this host loop.
- **Failure:** Guest calls g.drawLine(0, 0, Integer.MIN_VALUE, 0) (or
  drawPolygon with such a vertex): deltaX=-2^31 stays negative,
  difference=-2^31, doubled=0, both branch tests fail, x0 never changes; the
  session goroutine hangs holding client.run and cannot be stopped. Reproduced
  the arithmetic standalone (non-terminating with a 1e8 iteration cap).
- **Evidence:** runtime_graphics.go:1344-1350 deltaX, deltaY := x1-x0, y1-y0;
  deltaX = -deltaX; :1358 difference := deltaX - deltaY; :1359-1375 unbounded
  for loop calling graphicsFillRect per point; callers
  runtime_graphics.go:303-313 (drawLine) and :1280-1285 (drawPolygon) pass raw
  guest ints.
- **Verification:** graphicsDrawSegment (runtime_graphics.go:1343-1376) uses
  unclipped int32 arithmetic. For drawLine(0,0,Integer.MIN_VALUE,0): deltaX =
  MIN, and -MIN wraps back to MIN; deltaY = 0, stepX = -1, difference = MIN,
  doubled = 2\*MIN, which wraps to 0. Neither `0 > -0` nor `0 < MIN` holds, so
  x0 never moves and the loop never ends. graphicsFillRect only clips (:122-126)
  and returns nil, so no error breaks the loop. There is no step bound or ctx
  check. The arguments are raw guest ints from drawLine (:303-313) and
  drawPolygon (:1280-1285). Large finite spans also loop up to about 2^32 times.
  Only extreme guest values trigger this, but the result is an unstoppable host
  hang.
- **Fixed:** Graphics.drawLine and drawPolygon segments use the same clipped
  64-bit walk, bounded by the target in translated coordinates, so
  Integer.MIN_VALUE endpoints and large spans finish.

### R027 Module native invoke allocates an argument container per call, never freed

`internal/platform/ktf/module_link.go:731` · ktf2 · resource-leak · PLAUSIBLE

- **Defect:** invokeModuleMethod allocates a fresh arena block for the argument
  container on every native invocation and never releases it after core.Call
  returns. Every runtime-provided class method is flagged ACC_NATIVE, so every
  platform API call an older-module title makes through jump slot 1 leaks 4..4n
  bytes of the 64 MiB arena; the collector does not track these blocks.
- **Failure:** An older relocatable-module title calling a few hundred platform
  methods per frame (Graphics, String, StringBuffer, Vector) at 15-20 fps leaks
  tens of KB per second; after roughly 15-60 minutes the arena is exhausted and
  the session dies with 'KTF platform initialization data space exhausted'.
  docs/ktf.md:58 says containers keep arena lifetime, but this path allocates
  one per call from the host side, so lifetime equals leak.
- **Evidence:** module_link.go:731 container, allocationErr :=
  runtime.allocateWords(arguments); :745 core.Call; :749 return
  completeModuleMethod(...) with no arena.release. runtime_java.go:3411
  PutUint16(methodData[22:], method.accessFlags|0x0100) marks every runtime
  class method native, so the guest helper picks moduleJumpInvokeNative
  (module_link.go:653-654). initialization.go:2619-2636 allocate has no per-call
  release; collector releases only runtime.objects/wipicAllocations.
- **Verification:** The leak is real. Every runtime class method record has
  ACC_NATIVE forced on (runtime_java.go:3411). The module's invoke helper picks
  jump slot 1 on that bit (module_link.go:612-621), and invokeModuleMethod then
  calls allocateWords(arguments) once per call (module_link.go:731). Nothing
  releases that block after core.Call (:745-749), and arena.release runs only at
  collect.go:466 (tracked objects) and initialization.go:2170
  (wipicAllocations). docs/ktf.md:58 and docs/history/testing.md:2059
  acknowledge that containers keep their arena lifetime. That is a known
  limitation, not a guard. Not confirmed: how many runtime calls per frame the
  three local older-module titles make, so the claimed 15-60 minute time to
  exhaustion is unverified.
- **Fixed:** `invokeModuleMethod` zeroes the native argument container and
  returns it to the arena when the call returns.

### R028 C file names skip storableName; '/' or '.' writes the 'db' scope key

`internal/platform/ktf/wipic_filesystem.go:505` · ktf3 · untrusted-input · CONFIRMED

- **Defect:** wipicFileOpen, wipicFileRename and wipicMakeDirectory check only
  reservedStorageName. They never call the storableName guard that the
  record-database table uses (wipic_record_database.go:166). A name that
  normalizes to the bare scope, such as '', '/', '.' or './', is persisted under
  key 'db/' or 'db/.', and NormalizeSaveKey collapses that to 'db'. The
  storableName comment describes this exact failure for jdb.
- **Failure:** MC_fsOpen("/", create). readFileName trims it to "". The store is
  new, so persist writes key "db/" and DirectorySaveStore renames a regular file
  onto &lt;save>/db. With no db/ directory yet, every later db/&lt;name> write
  fails at MkdirAll ('not a directory') and every read answers ENOTDIR as
  absent, so the title's saves are lost from then on, across sessions. With an
  existing db/ directory the rename fails and the open returns a Go error that
  ends the session. Two related cases: a name containing '..' or '\\' makes
  NormalizeSaveKey fail and ends the session instead of returning an error code,
  and deleting "a\nb" writes a removal list that hides databases 'a' and 'b'.
  The directory list is also read back with TrimSpace (443), so 'dir ' and 'dir'
  collide.
- **Evidence:** wipic_filesystem.go:500-507 (open: readFileName, then only
  reservedStorageName), 534 staged.persist → 42 key "db/"+name. Rename at 313
  and mkdir at 416 check the same way, and 216 deletes 'db/'+name.
  backend/save.go:63-84 NormalizeSaveKey drops empty and '.' parts.
  wipic_record_database.go:824-848 storableName exists to refuse exactly these
  names and is used at 166/293, but never in this file.
- **Verification:** readFileName (253-259) only trims leading '/', so '/'
  becomes "", and '.' and './' are kept. wipicFileOpen checks only
  reservedStorageName (505), which returns false for these names because
  NormalizeSaveKey("db/") gives "db" and the CutPrefix finds nothing.
  databaseSeed("") and guestFile("") report absent, so any mode other than 1
  creates a store and persist (42) calls StoreSaves({"db/": ...}). The
  single-entry path goes to DirectorySaveStore.StoreSave, then savePath gives
  root/db, and os.Rename puts a regular file there. After that every
  db/&lt;name> write fails at MkdirAll and every read answers ENOTDIR as absent
  (backend/save.go:124-127). This is exactly the jdb failure that storableName's
  own comment (wipic_record_database.go:833-839) describes. storableName is
  never called in wipic_filesystem.go. If db/ already exists, the rename fails
  and the open returns a Go error. A name containing '..' is worse than the
  reviewer said: databaseSeed's loadSave error sets the sticky
  runtime.saveReadError (save.go:84-86), so every later save in the session
  fails. Rename rejects an empty name (292) but not '.', and mkdir only builds a
  list and does no direct damage. Severity stays medium because a title has to
  pass such a name.
- **Partly fixed:** Every name-taking WIPI C file slot refuses names that are
  not a save key of their own ('/', '.', './', '..' components) before any
  lookup, and remove, rename and mkdir also apply `storableName`; the 'dir ' /
  'dir' collision in the db/.dirs reader is left because changing the parse
  would change how existing lists are read.

### R029 File/record DB handles never recycled; 4095 opens per session

`internal/platform/ktf/wipic_filesystem.go:558` · ktf3 · save-integrity · CONFIRMED

- **Defect:** MC_fsOpen and the record-database open take handles from a counter
  that only goes up, and both refuse once it reaches 0x0fff. Close deletes the
  handle but never returns its number. The counter is also saved in checkpoints.
  After 4095 opens in one session, every later open fails, including the open a
  title makes to write its save.
- **Failure:** Example: a title that opens and closes its save or data files
  with MC_fsOpen on every load, save and stage change is played for a long
  session, possibly across quick save/load, which keeps the counter. Its 4096th
  MC_fsOpen returns wipicErrorInvalid. The title's save routine cannot open the
  file, and the player's progress is silently not written (or the title shows
  its own I/O error). The record-database table fails the same way.
- **Evidence:** wipic_filesystem.go:530/555 `if runtime.nextCDatabaseHandle >=
  maxCFileHandles { return wipicErrorInvalid }`, 558
  `runtime.nextCDatabaseHandle++`. Close at 108-113 only deletes the map entry.
  wipic_record_database.go:169/209-213 has the same pattern, close at 81. No
  other code writes the counter except storage_state.go:52/301, which carry it
  through checkpoints. maxCFileHandles = maxRecordDatabaseHandles = 0x0fff
  (wipic_filesystem.go:62, wipic_record_database.go:50).
- **Verification:** nextCDatabaseHandle and nextRecordDatabaseHandle are only
  ever incremented (wipic_filesystem.go:558, wipic_record_database.go:212). The
  only other writes are the checkpoint save and restore in
  storage_state.go:52/301. Close only deletes the map entry
  (wipic_filesystem.go:108-113, wipic_record_database.go:81). Once the counter
  reaches 0x0fff, both open paths return wipicErrorInvalid (530/555; 169/209),
  whatever handles have been closed. No doc or test calls this limit
  intentional. The mechanism is deterministic: open number 4096 in one session
  fails, including a save write. A new session starts at 0, and a quick load
  restores the checkpoint's counter. How often real titles open files was not
  measured, so severity stays medium.
- **Fixed:** File and record-database handles still come from the counter
  (unchanged for the first 4095 opens); after that the lowest closed number is
  reused, and an open is refused only while 4095 handles are held at once.

### R030 C blit/copy loops iterate the guest's unclipped width×height

`internal/platform/ktf/wipic_graphics.go:1187` · ktf3 · untrusted-input · CONFIRMED

- **Defect:** wipicBlitClipped (MC_grpDrawImage, MC_grpCopyArea) and
  wipicCopyFramebuffer (MC_grpCopyFrameBuffer) take the guest's int32 width and
  height without clipping. They test each pixel against the surface bounds
  inside a nested Go loop, so the work grows with the requested size, not the
  surface size.
- **Failure:** MC_grpDrawImage(dst, 0,0, 0x7fffffff, 320, src, 0,0, gc). The
  width might come from a destroyed image's record or an uninitialized variable.
  Each of up to 320 rows with an in-range fromY/toY runs about 2^31 inner
  iterations that only `continue`, roughly 7e11 iterations in all. The Host
  goroutine is stuck under client.run for minutes to hours, nothing checks ctx,
  and the session cannot be closed.
- **Evidence:** wipic_graphics.go:1149-1157 passes int32(registers[3]),
  int32(registers[4]) unchanged. 1181-1211 and 720-744 loop `for column :=
  int32(0); column < width` with a per-pixel bounds `continue`. clipRect
  (215-251), which fill and rect use, is never applied here. The Java callers
  clip first (runtime_graphics.go:946).
- **Verification:** wipicDrawImage (1149-1157) and wipicCopyArea (1282-1287)
  pass the guest's int32 width and height unchanged. wipicBlitClipped loops `for
  line < height` / `for column := int32(0); column < width` (1181-1187) and
  checks bounds per pixel with `continue`. wipicCopyFramebuffer does the same at
  718-726. clipRect (215-251) is never applied on these paths. With
  width=0x7fffffff, every row whose fromY and toY are in range runs about 2^31
  no-op iterations, so a screen-height surface costs on the order of 1e11 or
  more iterations, minutes of work on the Host goroutine under client.run with
  no ctx check. The Java Graphics.copyArea clips first
  (runtime_graphics.go:946). Image.drawImage (runtime_graphics.go:1530-1540)
  does not clip either, so the exposure is slightly wider than the reviewer
  stated. Severity stays medium because a real title has to pass a garbage size
  to reach it.
- **Fixed:** `wipicBlitClipped` (MC_grpDrawImage, MC_grpCopyArea and the Java
  callers of `wipicBlit`) and MC_grpCopyFrameBuffer first narrow the copy in
  int64 to the rows and columns both surfaces hold (`clipCopy`), so loop work is
  bounded by surface size.

### R031 MC_grpCopyArea snapshot allocates guest width\*height\*2 unclipped

`internal/platform/ktf/wipic_graphics.go:1233` · ktf3 · untrusted-input · PLAUSIBLE

- **Defect:** blitSourceSnapshot allocates `int(width)*int(height)*2` bytes
  straight from the guest's unclipped MC_grpCopyArea width and height, before
  anything is clipped to the surface. A large rectangle that overlaps itself
  makes a multi-gigabyte host allocation. If that allocation fails, Go raises a
  fatal out-of-memory error that recover() cannot catch.
- **Failure:** A title calls MC_grpCopyArea(fb, 0,0, 0x10000,0x10000, 1,0, gc),
  for example with an uninitialized size. Source and destination are the same
  surface, and none of the overlap tests at 1228-1229 rejects the rectangle.
  make([]byte, 8 GiB) runs, and on a phone or a server with limited memory the
  whole process (every session) aborts with 'runtime: out of memory'. Where it
  succeeds, the copy loop then walks 65536x65536 pixels.
- **Evidence:** wipic_graphics.go:1260-1287: wipicCopyArea passes registers[3],
  registers[4] unchanged into wipicBlitClipped. 1177 calls blitSourceSnapshot,
  and 1222-1233 checks only same-pixels, w/h>0 and overlap before the `make`.
  The Java copyArea clips first (runtime_graphics.go:946 state.clip) and only
  then calls wipicBlitClipped, so only the C path is exposed.
  internal/session/guard.go only recovers panics, not fatal OOM.
- **Verification:** Traced: wipicCopyArea (1260-1287) passes registers[3]/[4]
  unclipped into wipicBlitClipped. blitSourceSnapshot (1222-1233) checks only
  same-pixels, width/height>0, equal origin and rectangle overlap before
  `make([]byte, int(width)*int(height)*2)`, so (0,0,0x10000,0x10000) copied from
  (1,0) asks for 8 GiB. Whether that kills the process depends on the host. All
  dist targets are 64-bit (Makefile:18, mobile arm64). On darwin and
  overcommitting Linux the 8 GiB is reserved virtually. The snapshot fills only
  rows that fall inside the surface, so few pages get touched, and the result is
  a multi-second stall: about 4.3e9 iterations of the copy loop. On Windows,
  which commits at allocation, or on a low-memory Linux or phone, the commit can
  fail with an unrecoverable runtime out-of-memory. Very large sizes (0x7fffffff
  squared) exceed maxAlloc and become a recoverable makeslice panic instead. One
  correction: not every Java caller clips. runtimeImageDrawImage
  (runtime_graphics.go:1530-1540) passes min(width) and min(height) of guest
  geometry unclipped to wipicBlit, but source and destination are different
  images there, so the snapshot is rarely taken on that path.
- **Fixed:** Clipping runs before `blitSourceSnapshot`, so the overlap snapshot
  is at most the clipped rectangle (at most 8 MiB) instead of the guest's
  width\*height\*2.

### R032 ELF accepts 65535 aliased sections, making load and code scans quadratic

`internal/platform/lgt/elf.go:108` · lgt1 · untrusted-input · CONFIRMED

- **Defect:** ParseModule accepts up to 65535 section headers, and any number of
  them may point at the same file bytes and the same address. Load copies every
  section, and each recognition scan (findOptionCode, findJavaClassRecord) walks
  every executable section and calls optionBytes, which is itself linear in the
  section count. Session start becomes O(sections × section size) on archive
  input, before any instruction budget applies.
- **Failure:** A crafted binary.mod of about 3.6 MB has one 1 MB code blob and
  65535 SHT_PROGBITS executable headers, all with offset 0x34 and address
  0x8000. ParseModule accepts it, since each header only has to lie inside the
  file. Load then performs 65535 memory.Load copies of 1 MB (64 GB of memcpy,
  client.go:407-415). authenticationOptions, authenticationCertificate58,
  authenticationNotification, authenticationHandshake and
  authenticationCertificate100 each run findOptionCode over 65535 × 512K offsets
  (about 3.4e10 matchOptionCode calls per pattern, and roughly 20 patterns).
  NewSession, and also every checkpoint restore through checkAdapters, holds a
  server worker for hours with no ctx check.
- **Evidence:** elf.go:77 count is a uint16 with no cap. elf.go:108-142 has no
  overlap or aliasing check, and section.Data = data[offset:offset+size] may
  repeat. client.go:407-415 loads each section.
  authentication_options.go:195-216 loops over every executable section and
  offset. authentication_options.go:100-114 optionBytes walks module.Sections on
  every call. java_class.go:346-362 scans every section in 4-byte steps.
- **Verification:** ParseModule (elf.go:77-142) accepts up to 65535 headers. It
  checks only that each section lies inside the file and does not wrap, so many
  headers can alias the same offset and address. Load copies every section
  (client.go:407-415). At session start, StartSession (session.go:150-176) and
  recognizedNetwork (225-244) run findOptionCode at least about 10 times on a
  module with no matches: options, certificate58, notification, all six
  handshake patterns unconditionally
  (authentication_handshake_contract.go:63-68), and certificate100. Each run
  walks every executable section × every 2-byte offset
  (authentication_options.go:195-216). optionBytes returns at the first aliased
  section, so each probe is O(1), but the outer loops are sections × section
  bytes. With 65535 × ~1 MB that is about 3.4e10 probes per pattern. With the 64
  MB entry limit (archive.go:39-44) it can be about 100× more. Nothing checks
  ctx, so a crafted archive pins a worker for a very long time.
- **Fixed:** ParseModule refuses more than 64 loaded sections and any two
  non-empty loaded sections that overlap in memory or share file bytes, so
  loaded and scanned bytes are bounded by the file.

### R033 Draw primitives loop over unclipped guest extents with no bound

`internal/platform/lgt/graphics.go:542` · lgt1 · untrusted-input · CONFIRMED

- **Defect:** graphicsContext.fill iterates every (column,row) of the requested
  rectangle and rejects pixels one by one in put. line() steps over the full
  coordinate distance, and polygon() fills every row from top to bottom. None of
  them intersects the request with the clip or the surface first, and none
  checks ctx, so one guest draw call can occupy the host for hours outside any
  instruction budget.
- **Failure:** A Clet calls MC_grpFillRect(surface, 0, 0, 0x7fffffff,
  0x7fffffff, ctx), or a Java title calls g.fillRect(0, 0, Integer.MAX_VALUE,
  Integer.MAX_VALUE). wipic_draw.go:75 and java_screen.go:584 pass the int32
  values straight to fill, which runs about 4.6e18 put calls. The session worker
  never returns and Host cancellation cannot interrupt it. MC_grpFillPolygon
  with y values of -2^31 and 2^31-1 loops about 4e9 rows. A line between
  opposite corners of the int32 range is about 8.6e9 steps. Arc spans reach fill
  the same way through curve.FillArc.
- **Evidence:** graphics.go:542-548 `for row := y; row < y+height` / `for column
  := x; column < x+width` with no clamp. graphics.go:561 `for step := 0; step <=
  deltaX-deltaY+1`. graphics.go:609 `for y := top; y <= bottom`. Callers:
  wipic_draw.go:71-77, 110 and 118 pass int(values[i]) read from guest
  registers. java_screen.go:584-626 pass int(int32(arguments[i])). The clip only
  filters individual pixels inside put (graphics.go:503-518).
- **Verification:** graphicsContext.fill (graphics.go:542-548) loops over the
  full requested width and height. Clipping happens one pixel at a time inside
  put/clipped (503-518). javaFillRect (java_screen.go:579-587) passes
  int(int32(arguments)) straight in. The WIPI-C path (wipic_draw.go:73-77)
  passes int(values[i]) from readArguments, and readArguments ([]int32,
  wipic_draw.go:267) does no clamping. javaDrawContext and javaDraw
  (java_screen.go:134-205) intersect nothing with the surface either.
  fillRect(0,0,MaxInt32,MaxInt32) is therefore about 4.6e18 put calls. Nothing
  checks ctx, and the instruction budget does not apply to Go code, so the
  session worker never returns. line() (561) and polygon() (609) are unbounded
  the same way. The arc path is also unbounded inside internal/curve FillArc.
  This needs a crafted or buggy title, which the untrusted-input rule covers.
- **Fixed:** fill, line and polygon intersect the request with clip and surface
  in int64 before iterating, and a line computes its minor coordinate in closed
  form only for drawable steps; pixels match the old loops exactly.

### R034 Stale DataBase object overwrites another object's stored records

`internal/platform/lgt/java_database.go:283` · lgt1 · save-integrity · PLAUSIBLE

- **Defect:** Every openDataBase builds an independent javaDatabase copy of the
  container (line 134). Every mutation, and closeDataBase even when nothing
  changed (`!database.rebuilt || database.changed`), re-encodes and stores that
  whole copy. Two live objects for the same database therefore overwrite each
  other's stored records.
- **Failure:** A title opens DataBase "save" as A at startup to read settings
  and keeps it. Later its save routine opens "save" again as B, calls
  insertRecord/updateRecord (stored as R+x) and closes B. When the title later
  calls A.closeDataBase() (or A.updateRecord), storeDatabase(A) writes A's stale
  copy R back to fs/save.db, and the progress x written through B is lost. A
  handset's DataBase objects share one persistent store, so the first close
  would not undo the second object's writes. Closing an unchanged object also
  brings back a container that deleteDataBase removed while the object was open.
- **Evidence:** java_database.go:134 `database := &javaDatabase{...}` with
  records from a fresh readFile per open. java_database.go:283-285 closes with
  storeDatabase whenever !rebuilt, even if unchanged. java_database.go:191-197
  storeDatabase writes database.encode() of this object's records only.
  java_database.go:455-456 updateRecord also stores the object's whole copy.
  checkpoint_storage.go:166-175 already acknowledges that two objects of one
  database can coexist.
- **Verification:** The code works as described. Each openDataBase decodes a
  fresh, independent javaDatabase (java_database.go:134). insertRecord,
  updateRecord and deleteRecord each call storeDatabase, which writes this
  object's whole encode() (java_database.go:191-197, 416, 457). closeDataBase
  stores whenever !rebuilt, even when nothing changed (283-285). So a second
  live object for the same name overwrites the first one's writes, and closing
  an unchanged object brings back a container that javaDeleteDataBase (565-580)
  removed. checkpoint_storage.go:166-175 and its test acknowledge that two
  objects for one database can coexist. The spec does not cover opening a
  database twice, and nothing shows a real title holding two open objects for
  one database at once. Reachability is therefore unconfirmed. If it happens,
  saves are lost.
- **Fixed:** `DataBase` objects on one database share one record set, so a
  change, including one the store refused, is what every other object and a
  fresh open read, and `closeDataBase` stores only a change the store refused; a
  stale object can no longer overwrite newer records or bring a deleted database
  back.

### R035 Failed StartSession leaks Java worker goroutines and the whole client

`internal/platform/lgt/session.go:195` · lgt2 · resource-leak · CONFIRMED

- **Defect:** A Java title's startApp runs inside client.Start and can call
  Thread.start, which launches a goroutine blocked on worker.grant that holds
  the client. If Start or StartClet then fails, StartSession returns a
  StartFailure without calling StopJavaThreads. The goroutine waits forever and
  pins the client with all its guest memory.
- **Failure:** A Java title starts its game thread in startApp and then reaches
  an unimplemented platform method, which is common for this platform
  (ErrJavaAppUnsupported), or exceeds the step budget. StartSession returns the
  error and the Host retries or moves on, but each failed start leaves a parked
  goroutine referencing tens of MiB of guest memory (heap, 16 MiB data, 16 MiB
  surfaces, stacks), so a server accumulates leaked clients.
- **Evidence:** session.go:195-200 return &StartFailure{...} with no cleanup;
  java_thread.go:355 `go client.runJavaWorker(thread, worker)`, :361-364 blocks
  on `<-worker.grant`; java_launcher.go:67-76 runs startApp from the module
  entry; the only StopJavaThreads callers are session.go:687,
  session_checkpoint.go:302/377, checkpoint_state.go:1089.
- **Verification:** A Java title's startApp runs inside client.Start through the
  module entry (java_launcher.go:67-76). Thread.start calls startJavaWorker,
  which launches `go client.runJavaWorker` (java_thread.go:355), and that
  goroutine blocks on `<-worker.grant` (:361-364). Only StopJavaThreads closes
  grant (java_thread.go:681). Its callers are session.go:687,
  session_checkpoint.go:302/377 and checkpoint_state.go:1089, and none runs on
  the StartFailure returns (session.go:195-200). StartFailure carries only Err
  and Trace, so the Host cannot clean up. The goroutine pins the client. Many
  Java titles fail at start with ErrJavaAppUnsupported, so on a long-running
  server each such failure after a thread start leaks one client.
- **Fixed:** Both StartFailure returns go through `startFailure`, which takes
  the trace and then calls `StopJavaThreads`, so a failed start no longer leaves
  parked worker goroutines pinning the client.

### R036 copyArea/copyFramebuffer/drawImage loop w\*h with no bound, holding mu

`internal/platform/lgt/wipic_draw.go:289` · lgt2 · untrusted-input · CONFIRMED

- **Defect:** copyArea, copyFramebuffer and (in wipic_image.go) drawImage
  iterate `for row < height { for column < width }` over guest-supplied int32
  sizes. The per-pixel bounds check only `continue`s, so the iteration count is
  never clipped to the surface. transferRGBPixels has a bound
  (maxPixelTransfer); these three do not.
- **Failure:** MC_grpCopyArea(dst, 0, 0, 0x7fffffff, 0x7fffffff, 0, 0, pgc), or
  Java Graphics.copyArea with large w/h, runs about 4.6e18 iterations. It runs
  in host Go code, so it never checks ctx and the guest step budget does not
  apply. handleWIPICSVC holds client.mu throughout, so the Host's
  Frame/Flushes/SendKey calls also block. The session hangs forever with one
  core at 100% and cannot be closed. Sizes around 65535 already stall for
  seconds per call.
- **Evidence:** wipic_draw.go:286-299 (copyArea loops height\*width; the only
  check is the source range at :294), :325-347 (copyFramebuffer the same);
  wipic_image.go:281-315 (drawImage the same); wipic.go:304-305 holds client.mu
  across handleDraw; java_screen.go:798-802 sends Java copyArea w/h straight
  into copyArea; contrast wipic_draw.go:365 maxPixelTransfer.
- **Verification:** copyArea (wipic_draw.go:286-299), copyFramebuffer (:325-347)
  and drawImage (wipic_image.go:281-315) iterate the full guest width\*height
  and only `continue` on out-of-surface pixels. Nothing clips the loop bounds
  first. drawBand/syncRowsFromGuest clamp only the row read-back
  (graphics.go:418), not the loop. handleWIPICSVC holds client.mu for the whole
  slot (wipic.go:304-305). javaCopyArea passes Java w/h through unchanged
  (java_screen.go:798-802). Host Go code does not check ctx or the step budget,
  so 0x7fffffff x 0x7fffffff spins effectively forever with the lock held.
  transferRGBPixels is the only one with a cap (maxPixelTransfer, :365). Medium:
  this needs absurd guest sizes, but it hangs the session uninterruptibly.
- **Fixed:** copyArea, copyFramebuffer and drawImage iterate only the rows and
  columns whose source pixel is on the source surface and whose destination is
  drawable (`blitSteps`), with identical output.

### R037 Opening a file for truncate and closing it never truncates it

`internal/platform/lgt/wipic_file.go:449` · lgt2 · save-integrity · CONFIRMED

- **Defect:** openFile with MC_FILE_OPEN_WRTRUNC (or Java File mode 3) sets the
  handle's data to nil but leaves dirty false. closeFileHandle, flushOpenFiles
  and the quick-save path persist only dirty handles, so a truncate with no
  following write never reaches the store.
- **Failure:** A title clears a save slot with `fd = MC_fsOpen("save.dat",
  WRTRUNC); MC_fsClose(fd);` (or `new File(name, WRITE_TRUNC).close()`). The
  store still holds the old save, so the next
  MC_fsIsExist/MC_fsFileAttribute/read returns the old size and data, and the
  title loads a save the player just reset. While the handle is open, readFile
  also reports the old length instead of 0.
- **Evidence:** wipic_file.go:447-461 (`case flag == fileOpenWriteTruncate: data
  = nil`; openFile struct built with dirty unset); :514-516 closeFileHandle
  writes only `if file.dirty`; :609 flushOpenFiles skips !dirty;
  java_file.go:47-54 maps Java mode 3 to fileOpenWriteTruncate through the same
  openFile.
- **Verification:** For an existing file, openFile with
  flag==fileOpenWriteTruncate sets data=nil (wipic_file.go:449-450) and builds
  the openFile with dirty unset (:459-460). The create-at-open writeFile only
  runs when the file was absent (:426-437). closeFileHandle writes only if dirty
  (:514). flushOpenFiles skips !dirty (:609). The Java close (java_file.go:401)
  and the checkpoint paths (checkpoint_state.go:613,
  checkpoint_storage.go:132/293) also persist only dirty handles. Java mode 3
  maps to the same flag (java_file.go:47-48). An open-truncate followed by a
  close with no write leaves the old store bytes, and readFile keeps serving
  them. docs/architecture.md:955-957 says the KTF path empties the file in the
  store at open, so LGT silently diverges. The scenario needs a truncate with
  zero writes after it, which keeps this at medium.
- **Fixed:** A truncating open (C flag 4, Java `File` mode 3) stores the empty
  file at the open, as the one save rule has it, so a title that opens to
  rewrite and closes without writing has an empty file and every read sees it at
  once.

### R038 Failed save store on close/flush is silently dropped

`internal/platform/lgt/wipic_file.go:620` · lgt2 · save-integrity · CONFIRMED

- **Defect:** writeFile discards storeFile's error. closeFileHandle then deletes
  the handle and MC_fsClose answers success, and flushOpenFiles clears dirty, so
  a write the store refused is lost with nothing kept to retry and only a debug
  log line, which release builds drop. writeFile's comment says 'the in-memory
  copy stays authoritative', but after the close no in-memory copy exists.
- **Failure:** The save store refuses StoreSave on close, for example because
  the disk is full, a permission or lock error occurs, or the browser store
  fails. The title is told its save succeeded and readFile later serves the
  previous store contents or the archive file. The player's progress is lost
  with no error reaching the Host, and a later quick save cannot retry because
  the handle is gone and dirty is false.
- **Evidence:** wipic_file.go:619-621 `_ = client.storeFile(name, data)`;
  :509-519 closeFileHandle deletes the handle and returns true regardless;
  :305-308 MC_fsClose answers wipiSuccess; :612-613 flushOpenFiles sets
  `file.dirty = false` after an ignored failure; :640-645 storeFile only logs at
  Debug level.
- **Verification:** writeFile discards storeFile's error
  (wipic_file.go:619-621). storeFile logs a StoreSave failure only at Debug
  (:640-645). closeFileHandle deletes the handle and returns true either way
  (:509-519), so MC_fsClose answers wipiSuccess (:305-308). flushOpenFiles
  clears dirty after the ignored failure (:612-613). Once the handle is gone,
  readFile reads only the store or archive (:580-590), so the comment's
  'in-memory copy stays authoritative' is false after a close. The new bytes are
  lost, the title is told it succeeded, and nothing reaches the Host. The
  trigger is an environmental store failure (disk full, permission, lock), not
  guest input, so this is medium rather than high.
- **Fixed:** A store refusal no longer drops the write: the handle stays open
  and dirty, `MC_fsClose` answers `wipiError`, Java `File.close` keeps the File
  open without throwing, `flushOpenFiles` keeps it dirty, and a refused rename
  keeps the old name.

### R039 Close flushes open files before destroyClet, losing writes made there

`internal/platform/lgt/session.go:691` · lgt2 · save-integrity · PLAUSIBLE

- **Defect:** Session.Close calls flushOpenFiles and then DestroyClet. Anything
  the title writes into a still-open file inside its destroy entry point is left
  in the openFile buffer, and nothing flushes after destroyClet returns.
- **Failure:** A C title keeps its save open for the whole session (the openFile
  comment describes one) and writes its final state with MC_fsWrite in
  destroyClet without calling MC_fsClose. When the Host closes the session,
  flushOpenFiles runs first and finds nothing dirty, then destroyClet marks the
  handle dirty, and Close returns. The client is discarded and the save keeps
  its old contents. MC_knlExit inside destroy would flush, but a plain return
  does not.
- **Evidence:** session.go:687-695: StopJavaThreads(); flushOpenFiles();
  DestroyClet(ctx); return. client.go:605-609 DestroyClet only calls callClet;
  the other flushOpenFiles call sites are wipic.go:419 (MC_knlExit) and
  java_widget.go:685.
- **Verification:** The order is as described: Close calls StopJavaThreads, then
  flushOpenFiles (session.go:691), then DestroyClet (:692), and nothing flushes
  afterwards. DestroyClet only calls callClet (client.go:605-609). A write made
  in destroyClet to a handle that is still open and never closed stays in
  openFile.data and is discarded with the client. MC_fsClose (closeFileHandle)
  and MC_knlExit (wipic.go:419) inside destroy would persist it. Only a plain
  write-and-return is lost. The code comments describe titles that keep a save
  open all session, and the documented case of a title that writes and exits
  without closing (docs/history/lgt.md 'A file written and never closed') shows
  the pattern exists. No title is known to write in destroyClet without closing,
  so reachability is not confirmed.
- **Fixed:** `Session.Close` flushes open files before `destroyClet` and again
  after it, so what a title writes during destroy to a still-open file reaches
  the store.

### R040 Ending flush ignores Java File output streams, losing buffered saves

`internal/platform/lgt/wipic_file.go:607` · lgt2 · save-integrity · PLAUSIBLE

- **Defect:** Bytes a Java title writes through File.openOutputStream stay in a
  Go-side sink until the stream's flush/close or File.close drains them.
  flushOpenFiles is the 'the ending is a second durable moment' hook used by
  MC_knlExit, Jlet.notifyDestroyed and Session.Close. It only persists
  client.files and never drains bound sinks.
- **Failure:** A Java title writes its save via
  `DataOutputStream(f.openOutputStream()).writeInt(...)` and then calls
  notifyDestroyed(), or the Host closes the session, before flushing or closing
  the stream or File. javaNotifyDestroyed calls flushOpenFiles, which sees the
  File's openFile as not dirty because the sink was never drained. The written
  bytes are discarded with the client. This is the Java case of the C 'write
  then exit' scenario that flushOpenFiles exists to cover.
- **Evidence:** wipic_file.go:607-615 iterates client.files only;
  java_widget.go:683-686 javaNotifyDestroyed -> flushOpenFiles; session.go:691;
  java_stream.go:729-737 sink writes only append to runtime.sinks;
  drainJavaSinkToFile is called only from java_stream.go:789 (sink flush/close)
  and java_file.go:387 (File.close).
- **Verification:** flushOpenFiles (wipic_file.go:607-615) walks client.files
  only. Bytes written through a File-bound sink stay in runtime.sinks
  (java_stream.go:736/756/772) until drainJavaSinkToFile runs, which happens
  only on sink flush/close (java_stream.go:789) or File.close
  (java_file.go:383-390). That is what sets open.dirty (java_file.go:364).
  javaNotifyDestroyed (java_widget.go:683-686) and Session.Close
  (session.go:691) therefore persist nothing for a sink that was written but not
  drained. The handset's stream is not buffered: DataOutputStream writes
  straight through in CLDC. A title that writes and calls notifyDestroyed
  without close loses data here and would not on a handset. No title is shown
  doing this, so reachability is unconfirmed.
- **Fixed:** A write through a `File`'s output stream goes straight into the
  `File`'s buffer, so a flush, the `File`'s close, the ending or a quick save
  stores it like any write; a checkpoint from an earlier build that holds
  unflushed stream bytes puts them into the `File` at restore.

### R041 Descriptor continuation lines are concatenated quadratically

`internal/platform/skt/descriptor.go:43` · skt1 · untrusted-input · CONFIRMED

- **Defect:** ParseDescriptor appends each continuation line to logicalLine with
  string +=, copying the whole logical line every time. The scanner's 1 MB token
  cap bounds one physical line but not the logical line. Work is therefore
  O(n^2) in the manifest or .msd size, and archive limits allow up to 128 MB per
  entry.
- **Failure:** A JAR (or SKT container .msd) whose MANIFEST.MF is 'MIDlet-1:
  a,b,c' followed by hundreds of thousands of ' xxxx' continuation lines with no
  blank line. A copy of the loop measured here: 1.6 MB → 2.0 s, 3.2 MB → 6.3 s,
  6.4 MB → 23 s. A 128 MB entry, which compresses to a few hundred KB, takes
  hours. The parse runs in openJAR, in unpackArchiveWithin (skt.go:100) and in
  ReadDescriptor (saveowner.go:45, which the save-owner collision scan runs over
  every archive). One crafted archive in the game folders can therefore hang
  loading or the library scan.
- **Evidence:** descriptor.go:19 sets the scanner token limit to 1 MiB per
  physical line only. descriptor.go:39-44 does logicalLine += line[1:] for every
  line that starts with a space. archive.go:30-35 allows a 128 MiB entry.
  Timings came from a standalone copy of the loop in the scratchpad.
- **Verification:** descriptor.go:43 runs `logicalLine += line[1:]` for every
  continuation line. Go string concatenation allocates and copies the whole
  string each time, so the total cost is O(n^2) in the logical-line length. The
  scanner Buffer cap at line 19 bounds one physical line only, and the archive
  limits allow a 128 MiB entry (archive.go:30-35). The parse is reached from
  openJAR (archive.go:107), from unpackArchive for the .msd (skt.go:100), from
  session.Inspect via skt.Open (session.go:121, which webhost calls at session
  start, checkpoint and savepack) and from the CLI SaveOwnerCollisions scan
  (saveowner.go:45, cmd/cli/main.go:2152). A crafted archive therefore hangs the
  load. It is not high because the input has to be a deliberately crafted
  archive that the user adds.
- **Fixed:** Continuation lines are joined in a `strings.Builder`, so descriptor
  parsing is linear in manifest size.

### R042 IMPLICIT List soft-key commands do not select the focused row

`internal/platform/skt/lcdui_input.go:46` · skt1 · correctness · CONFIRMED

- **Defect:** MIDP defines Choice.IMPLICIT as a choice in which the currently
  focused element is selected when a Command is initiated. Here UP/DOWN move
  only screenData.selection (the highlight), and soft-key commands, including
  those fired from the command menu, call fireCommand without selecting the
  focused element. Only FIRE runs selectExclusive.
- **Failure:** A title builds List("Menu", IMPLICIT) with an "OK" Command and
  reads list.getSelectedIndex() in commandAction. The player presses DOWN twice;
  the highlight is on row 2 but element 0 is still the selected one. The player
  presses the soft key → commandAction(OK) → getSelectedIndex() returns 0, so
  the title acts on the wrong menu entry. The same happens with 3+ commands
  through the soft-key menu (handleMenuKey → fireCommand).
- **Evidence:** lcdui_input.go:43-61: the Soft1/Soft2 branches call
  fireCommand(screen, commands[i]) directly. lcdui_input.go:130-155: the menu
  FIRE path also calls fireCommand only. lcdui_input.go:172-177: UP/DOWN change
  content.selection only. selectExclusive is called only on FIRE
  (lcdui_input.go:184).
- **Verification:** The MIDP Choice.IMPLICIT contract says the currently focused
  element is selected when a Command is initiated. In the code, UP/DOWN change
  only content.selection (lcdui_input.go:172-177). Soft1, Soft2 (lines 44-60)
  and the menu FIRE (lines 143-154) call fireCommand with no
  selectExclusive(content.selection), and choiceSelectedIndex returns
  choice.selectedIndex() (lcdui.go:1498-1510), which is the stale element.
  docs/lcdui.md:66 documents only FIRE→SELECT_COMMAND and says nothing about
  soft-key commands, so this is not a documented intentional divergence. The
  repo's own UI fixture builds exactly this shape (an IMPLICIT List with OK and
  BACK commands, lcdui_test.go:14-43).
- **Fixed:** Soft-key and menu commands on an IMPLICIT List select the focused
  row first, and a row the title selects with setSelectedIndex/setSelectedFlags
  becomes the focused row.

### R043 List focus index never re-clamped after delete; FIRE panics or deselects

`internal/platform/skt/lcdui_input.go:181` · skt1 · correctness · CONFIRMED

- **Defect:** choiceDelete/choiceDeleteAll shrink choice.elements but never
  adjust screenData.selection (the List focus). handleListKey then indexes
  choice.elements[content.selection] for MULTIPLE lists and calls
  selectExclusive(content.selection) for the others, using a stale index.
- **Failure:** A MULTIPLE List has 5 rows and the player is on row 4. The game
  deletes the checked rows (list.delete) and stays on the List, leaving 3 rows.
  The player presses FIRE, or UP and then FIRE (max(4-1,0)=3 is still out of
  range). choice.elements[4] panics, and Session.guarded turns the panic into a
  session-ending failure. On an IMPLICIT or EXCLUSIVE List the same press runs
  selectExclusive(4), which clears every element, then fires SELECT_COMMAND, so
  getSelectedIndex() returns -1. Also, a quicksave taken while the focus is
  stale is captured without error, but restore rejects it: the screenList check
  in checkpoint_heap_restore.go:282 fails because selection >= len(elements).
  The player ends up holding a quicksave that cannot be loaded.
- **Evidence:** lcdui.go:1416-1448: choiceDelete/choiceDeleteAll change
  choice.elements only. lcdui_input.go:168-181: the len==0 guard is followed by
  unclamped choice.elements[content.selection] and
  selectExclusive(content.selection). formDelete (lcdui.go:1725) clamps
  form.selection, but nothing does the same for a List.
  capturePlatformState/captureNative have no matching check, while
  restoreNative's heap.checks (checkpoint_heap_restore.go:274-284) reject it. No
  recover() in the skt package; session/guard.go:50 ends the session on panic.
- **Verification:** choiceDelete (lcdui.go:1416-1436) and choiceDeleteAll
  (lcdui.go:1438-1447) change only choice.elements. They call
  refreshChoiceOwner, which only queues a paint (lcdui_render.go:34-45). No List
  path ever writes screenData.selection apart from handleListKey; formDelete
  clamps form.selection (lcdui.go:1725), but nothing does the same for a List.
  handleListKey only guards len==0 (lcdui_input.go:168). UP gives max(sel-1,0),
  which can still be out of range. FIRE on a MULTIPLE List indexes
  choice.elements[content.selection] (line 181) and panics. That runs under
  dispatchLocked on the Host path, and the only recover is session/guard.go:50,
  so the session ends. On IMPLICIT/EXCLUSIVE lists, selectExclusive(stale)
  clears every element and SELECT_COMMAND still fires (lines 184-191), so
  getSelectedIndex returns -1. Capture writes n.selection unchecked
  (checkpoint_heap.go:275), while restore rejects selection >= len(elements)
  (checkpoint_heap_restore.go:282). Severity stays medium because only the
  MULTIPLE case panics, and that needs an LCDUI List that shrinks while focused
  past its new end.
- **Fixed:** Every List row edit keeps the focus inside the rows and the key
  handler clamps it before indexing, so FIRE never panics or clears the
  selection and a quicksave never holds a focus its restore rejects.

### R044 Choice/Form/Item mutators run on guest threads without any lock

`internal/platform/skt/lcdui.go:1374` · skt1 · concurrency · PLAUSIBLE

- **Defect:** choiceAppend, choiceInsert, choiceDelete, choiceDeleteAll,
  choiceSet, choiceSetSelected\*, formAppend, formInsert, formDelete,
  formDeleteAll, formSet and the Item setters change choice.elements, form.items
  and itemData fields with no lock. SKT guest threads are free-running
  goroutines (no GuestThreadStarter). The Host thread reads the same slices in
  paintScreen (under textMu+renderMu), in
  deliverScreenKey/handleListKey/handleFormKey (no lock) and in
  textInputSnapshot (under lcdui state.mu). None of those locks is taken by the
  writers.
- **Failure:** A title's own thread refreshes a List (deleteAll then append)
  while the player presses FIRE. handleListKey passes the
  len(choice.elements)!=0 guard with the old slice and then indexes the new nil
  or shorter slice → index out of range panic → the session ends. Concurrent
  append/delete against paintScreen's range over choice.elements or form.items
  is a data race on slice headers, which can produce torn len/ptr reads. Only
  the text natives were moved under textMu; the choice and form natives were
  not.
- **Evidence:** lcdui.go:1360-1475 and 1650-1775 have no lock in the mutators.
  lcdui_render.go:84-92 paintScreen holds textMu+renderMu and reads
  content.items/choice.elements (lcdui_render.go:217,304).
  host_text_input.go:68-98 reads screen.items under state.mu only.
  runtime.go:132-135 says textMu exists because guest threads call setters
  outside dispatch. runtime.go:401 creates the VM without GuestThreadStarter, so
  threads are goroutine-backed.
- **Verification:** The race exists in the code.
  choiceAppend/Insert/Delete/DeleteAll/Set (lcdui.go:1360-1475) and
  formAppend/Insert/Delete/DeleteAll/Set (lcdui.go:1650-1760) take no lock. SKT
  guest threads are free-running goroutines: runtime.go has no
  GuestThreadStarter or ThreadYield, jvm/builtins.go:192 runs `go
  vm.runGuestThread`, and parkThread gates only checkpoints. The Host reads the
  same slices in paintScreen (under textMu and renderMu, which the writers never
  take, lcdui_render.go:84-151) and in handleListKey/handleFormKey (under
  dispatchMu only). The textMu comment at runtime.go:132-135 confirms that guest
  threads do call LCDUI setters outside dispatch. Whether a real title mutates a
  List or Form from its own thread at the same moment as a key press or paint
  was not confirmed, so this stays PLAUSIBLE.
- **Fixed:** The Choice, Form, Item, StringItem, ImageItem, Alert and
  List.setSelectCommand natives run under `textMu`, the lock the painter uses;
  the List and Form key handlers take it and release it before calling guest
  code.

### R045 SKT save owner not sanitized to the Host's owner rule; saves silently dropped

`internal/platform/skt/rms.go:91` · skt2 · save-integrity · CONFIRMED

- **Defect:** jarSaveOwner replaces only '/', '\\', NUL and control characters,
  and SaveOwner returns DD-ProgName trimmed but otherwise unchecked. The web
  host refuses any owner longer than 64 bytes or containing :\*?"&lt;>|, DEL or
  bidi controls, and then starts the game with no SaveStore at all. RMS and
  XFile then live only in memory, and nothing tells the player. The CLI instead
  joins an unchecked DD-ProgName into a filesystem path (cmd/cli/main.go:375),
  so '../..' escapes the save root.
- **Failure:** A bare MIDlet JAR named 'Puzzle: Deluxe', or one whose Korean
  MIDlet-Name is 22 or more syllables (over 64 UTF-8 bytes), starts in the
  browser. saveDirectory returns "", saveStoreIn returns nil, the title saves
  normally from its point of view, and every save is gone when the session
  closes. In the CLI, an archive whose .msd sets DD-ProgName to ../../x writes
  saves outside var/savedata/skt.
- **Evidence:** rms.go:91-112 jarSaveOwner only maps '/', '\\', 0 and &lt;0x20.
  skt.go:253-257 returns strings.TrimSpace(DD-ProgName). webhost/saves.go:26-46
  ownerRejected (len>64, `/\:*?"<>|`, 0x7f, bidi). webhost/session.go:1843-1846
  returns "" for a rejected owner, and session.go:823 passes saveStoreIn("")
  (nil) as the SaveStore. session/session.go:131 uses skt.SaveOwner.
- **Verification:** jarSaveOwner (rms.go:91-112) maps only '/', '\\', NUL and
  &lt;0x20. SaveOwner (skt.go:253-257) returns the trimmed DD-ProgName with no
  check. The web host's ownerRejected (webhost/saves.go:26-46) refuses owners
  over 64 bytes, owners containing :\*?"&lt;>|, DEL and bidi characters.
  saveDirectory then returns "" (session.go:1843-1846), saveStoreIn("") returns
  nil, and the session starts with no SaveStore and no notice (session.go:794,
  823; admitStart skips claims when the directory is empty). A bare JAR named
  with ':' or '?', or with 22 or more Hangul syllables, gets in-memory saves
  only. In the CLI (cmd/cli/main.go:375), filepath.Join(platformSaveRoot,
  DD-ProgName) with '../..' escapes the save root, because NormalizeSaveKey
  validates keys, not the root. DD-ProgName is normally numeric
  (skt_test.go:228), so the realistic trigger is the MIDlet-Name fallback for
  bare JARs.
- **Fixed:** `SaveOwner` keeps every owner the web host accepts and turns a
  refused one into an accepted name (refused characters replaced, cut to 64
  bytes, `~<8 hex>` digest); `MigrateSaveOwner` moves the old CLI directory, its
  quick-save slots and the set-aside generation to the new owner without
  merging, and the CLI (`runskt`, quick-save claim) and the web host (start,
  save pack, quick-load check, via `session.MigrateSaveOwner`) call it before
  claiming the directory. The rule, the mapping and the move now live in
  `internal/backend/saveowner.go` for every platform, the move claims both
  directories and never follows `.` or `..`, and the Hosts take the earlier
  owner from `session.Inspect` without reading the archive again (P07, P08, P09
  of the PR #234 review).

### R046 Unclamped blit loops spin for width\*height while holding renderMu

`internal/platform/skt/skvm.go:587` · skt2 · untrusted-input · CONFIRMED

- **Defect:** graphics2DDrawImage, xDisplayDrawImageEx (skvm.go:1577) and
  setGraphicsRGBPixels (wipi.go:523) loop over the full guest-supplied
  width\*height. Out-of-range pixels are skipped with continue instead of the
  loops being clamped to the source image, array or clip. Guest step limits do
  not apply inside a native, and the loops run inside withDestinationWrite,
  which holds runtime.renderMu for the screen Graphics.
- **Failure:** A title passes width=height=0x7fffffff (or a large value from a
  corrupted table) to Graphics2D.drawImage on the screen Graphics. The native
  iterates about 4.6e18 times holding renderMu, so the Host pass blocks on
  renderMu while holding dispatchMu, Destroy cannot get dispatchMu, and the
  session hangs forever with one CPU core pinned.
- **Evidence:** skvm.go:577-616: rectArguments has no bounds (render.go:848-857)
  and the loops run `row < height`, `column < width` with continue.
  skvm.go:1576-1588 has the same pattern. wipi.go:522-543 has the same with a
  per-pixel `source >= len(values)` continue. render.go:794-803
  withDestinationWrite locks context.screen (=&runtime.renderMu).
- **Verification:** rectArguments has no bounds (render.go:848-857). In
  graphics2DDrawImage (skvm.go:586-616), xDisplayDrawImageEx (skvm.go:1576-1588)
  and setGraphicsRGBPixels (wipi.go:522-543), the loops run row&lt;height and
  column&lt;width and skip out-of-range pixels with continue instead of clamping
  to the source or clip. All of them run inside withDestinationWrite, which
  locks context.screen for the screen Graphics (render.go:794-803). Guest step
  limits do not reach inside a native. width=height=0x7fffffff means about
  4.6e18 iterations holding renderMu, so the session's present and paint paths
  block for good. Reachable only from a hostile title or corrupted data.
- **Fixed:** Graphics2D.drawImage, drawImageEx and setRGBPixels loop only over
  the rows and columns inside the source, the array and the clip (`blitSpan`,
  64-bit arithmetic), with identical pixels for ordinary coordinates.

### R047 WIPI File.read returns 0 at EOF instead of -1; read loops spin

`internal/platform/skt/skvm_file.go:238` · skt2 · correctness · CONFIRMED

- **Defect:** WIPI File.read([B) and read([BII) are routed to xFileRead, which
  returns 0 when the cursor is at or past EOF. The WIPI contract (and KTF's
  implementation) returns -1 when EOF is hit before any byte. The stream wrapper
  converts 0 to -1, but the direct File.read path does not.
- **Failure:** A Jlet loads with `while ((n = file.read(buf)) != -1) {...}`. At
  EOF every call returns 0, so the loop never ends: an unbounded guest spin, and
  the step window is renewed while the MIDlet is alive. A title that checks
  `file.read(buf) == -1` to detect an empty or new save never sees it.
- **Evidence:** wipi.go:119-120 register read([B) to wipiFileReadWhole (which
  calls xFileRead) and read([BII) directly to xFileRead. skvm_file.go:237-240:
  `count := min(...); if count <= 0 { return IntValue(0) }`.
  wipi_file_stream.go:170-173 shows the 0-to-(-1) conversion exists only for
  streams. ktf/runtime_file.go:348-350 returns -1 when remaining&lt;=0. Wiki
  File.md: 'number of bytes read, or -1 if EOF is reached before any byte is
  read'.
- **Verification:** WIPI File.read([BII) goes directly to xFileRead
  (wipi.go:120). File.read([B) goes through wipiFileReadWhole, which passes
  length=len(buf) (wipi.go:600-606). In xFileRead (skvm_file.go:237-240),
  count=min(length, len-cursor) is &lt;=0 at EOF and the call returns 0. The
  spec says read returns -1 if EOF is reached before any byte. KTF returns -1
  when remaining&lt;=0 (runtime_file.go). The stream wrapper converts 0 to -1
  only for streams (wipi_file_stream.go:170-173). A '!= -1' read loop over a
  File therefore never ends.
- **Fixed:** `xFileRead` returns -1 at end of file when the receiver is a WIPI
  File and the length is positive; XFile still returns 0 and a zero-length read
  returns 0.

### R048 XFile seek past EOF then write allocates up to 2 GiB, no capacity cap

`internal/platform/skt/skvm_file.go:271` · skt2 · untrusted-input · CONFIRMED

- **Defect:** xFileSeek accepts any position up to 2^31-1, and xFileWrite
  zero-fills from len(data) to the cursor with no bound. xFileCapacity (512
  KiB), which fsavail reports, is never enforced, so file growth is unbounded
  and every flush/close copies the whole buffer and writes it to the save tree.
- **Failure:** A buggy or hostile title calls xfile.seek(0x7fffffff, SEEK_SET)
  and then write(buf,0,1). make([]byte, ~2^31) plus append's copy allocates
  about 4 GiB in the shared server process (fatal OOM risk for every session).
  close() then copies 2 GiB again and writes a 2 GiB file into var/savedata.
- **Evidence:** skvm_file.go:310-315 bounds the position only to 1&lt;&lt;31-1.
  Lines 270-276 call `append(file.data, make([]byte,
  file.cursor-len(file.data))...)` with no limit. Line 26 defines xFileCapacity,
  which only xFileAvail (line 413) reads. Lines 346 and 358 copy and store the
  whole buffer. By contrast, wipi.go:648-650 clamps WIPI seek to len(data).
- **Verification:** xFileSeek bounds the position only to [0, 1&lt;&lt;31-1]
  (skvm_file.go:310-314), and docs/skt-checkpoints.md confirms that bound is all
  there is. xFileWrite then zero-fills up to the cursor with no limit (270-276).
  xFileCapacity is read only by xFileAvail (412-414) and never enforced.
  persistXFile clones and stores the whole buffer (346, 358). seek(0x7fffffff,0)
  followed by a 1-byte write allocates about 2 GiB plus the append copy in the
  shared process. WIPI File.seek clamps to len(data) (wipi.go:646-651), but SKVM
  XFile.seek does not. Reachable only from a hostile or broken title, but
  CLAUDE.md requires such sizes to be bounded.
- **Fixed:** A write that would grow an XFile past 16 MiB (`xFileMaxBytes`)
  returns -1 without allocating; files already larger can still be rewritten in
  place.

### R049 Card.move/resize write the field map unlocked under keys getfield never reads

`internal/platform/skt/wipi.go:314` · skt2 · concurrency · CONFIRMED

- **Defect:** setCardFields writes card.Fields["x"/"y"/"w"/"h"] directly. It
  takes no lock, while the interpreter guards every getfield/putfield on that
  same map with object.fieldMu, and SKT guest threads are real goroutines. The
  bare keys also never match the resolved key the interpreter uses
  ('org/kwis/msp/lcdui/Card.w:I'), so resize() has no observable effect and a
  Card subclass reading this.x/this.w never sees move()/resize().
- **Failure:** A Jlet's game thread calls card.move(x, y) while the Host pass is
  running that Card's paint, which reads its own fields via getfield (map read
  under fieldMu.RLock). The Go runtime detects a concurrent map read and write
  and aborts the whole server process (fatal error, not recoverable), taking
  every session down. Separately, a title that calls resize(w, h) and then reads
  this.w gets 0.
- **Evidence:** wipi.go:311-315 assigns card.Fields[first]/[second] with no
  lock. jvm/vm.go:924-926 and 945-950 access object.Fields only under
  object.fieldMu. jvm/vm.go:1031-1033 builds the key as
  declaring+"."+name+":"+descriptor. api/wipi/definitions.go declares Card
  fields x,y,w,h as protected instance fields. jvm/builtins.go:193 runs SKT
  guest threads as goroutines (no GuestThreadStarter installed for SKT).
- **Verification:** setCardFields (wipi.go:311-315) writes card.Fields with no
  fieldMu. getfield/putfield go through object.fieldMu (jvm/vm.go:924-926,
  945-950) under the resolved key declaring+'.'+name+':'+descriptor
  (vm.go:1031-1033). Card declares x,y,w,h (api/wipi/definitions.go:206-210), so
  this.w resolves to 'org/kwis/msp/lcdui/Card.w:I' and never sees the bare 'w'.
  Only getX/getY (cardField, definitions.go:248-249,260-270) read the bare
  'x'/'y' keys, so resize() is unobservable and this.x/this.w never reflect
  move/resize. The constructor's setCardBounds (definitions.go:341-347) has the
  same bare-key problem, so this.w is 0 even without resize. SKT installs no
  GuestThreadStarter, so guest threads are goroutines (jvm/builtins.go:177-193).
  A move() on a game thread concurrent with getfield on the same Card during
  Host paint is an unsynchronized Go map write, which is fatal. The fatal race
  needs a title that moves a Card off the paint thread, which is uncommon.
- **Fixed:** Card.move/resize write through `VM.SetField`, the key and lock
  getfield uses.

### R050 getRGBPixels/captureLCD allocate width\*height before any bound

`internal/platform/skt/wipi.go:492` · skt2 · untrusted-input · CONFIRMED

- **Defect:** getGraphicsRGBPixels allocates a []jvm.Value of capacity
  width\*height and walks every pixel before it checks the destination array.
  graphics2DCaptureLCD allocates width\*height\*4 bytes before newMIDPImage
  applies MaxFramebufferPixels. Both take guest ints with only a sign check.
- **Failure:** A title calls getRGBPixels(0,0,50000,50000,buf,0,0) with a small
  buf, or Graphics2D.captureLCD(0,0,50000,50000). Go tries to allocate tens to
  hundreds of GB, and the runtime aborts the process with 'out of memory'
  (unrecoverable), killing every session on the server. Values near 2^31
  overflow the product, and make panics instead.
- **Evidence:** wipi.go:567-569 rejects only negative sizes. wipi.go:492
  `make([]jvm.Value, 0, int(width)*int(height))`, and the array range check is
  only at 505-509. skvm.go:536-540 checks only positivity, then `make([]byte,
  int(width)*int(height)*4)`, and the size limit applies later in newMIDPImage
  (image.go:294 to backend.RGBAByteLength).
- **Verification:** graphicsRGBArguments rejects only negative sizes
  (wipi.go:567-569). getGraphicsRGBPixels runs make([]jvm.Value, 0,
  width\*height) and fills every pixel (492-503) before the destination-array
  check at 505-509, so a small buf does not stop it. graphics2DCaptureLCD
  (skvm.go:536-540) checks only positivity, allocates width\*height\*4 bytes and
  fills them under renderMu. The MaxFramebufferPixels limit applies later, in
  newMIDPImage. 50000x50000 means about 60 GB of Values or 10 GB of bytes, all
  touched by the loops: an unrecoverable OOM. Native panic recovery does not
  help.
- **Fixed:** getRGBPixels checks the array range before allocating and holds one
  row at a time; captureLCD applies `backend.RGBAByteLength` (the 16M-pixel
  limit) before allocating.

### R051 Graphics.getRGBPixels writes to argument 4 (height), always NPEs

`internal/platform/skt/wipi.go:510` · skt2 · correctness · CONFIRMED (reviewer said high)

- **Defect:** getGraphicsRGBPixels copies each row into arrayObjectOf(arguments,
  4). For (IIII[III)V, argument 4 is the int height, not the int[] buffer at
  index 5. Reference() on an int fails, arrayObjectOf returns nil, and
  SetArrayRange(nil) throws NullPointerException into the guest. No call with
  height>0 ever fills the caller's array.
- **Failure:** A WIPI Card calls g.getRGBPixels(0,0,16,16,buf,0,16) with a valid
  256-int buffer. The bounds check at line 507 passes, then
  jvm.SetArrayRange(nil, ...) returns guestException NullPointerException('array
  operation'), and the title's paint or logic thread dies or takes its catch
  path.
- **Evidence:** wipi.go:510 `jvm.SetArrayRange(arrayObjectOf(arguments, 4),
  start, ...)`. graphicsRGBArguments (wipi.go:563,570) takes the rect from index
  1..4 and the array from index 5. jvm/value.go:353 Reference() errors on a
  non-reference, so arrayObjectOf returns nil. jvm/interpreter.go:1346
  objectArray(nil) returns guestException NPE. KTF's implementation reads the
  buffer from arguments[5] (ktf/runtime_graphics.go).
- **Verification:** For (IIII[III)V the native gets receiver=0, x..h=1..4,
  int[]=5, offset=6, scan=7. graphicsRGBArguments (wipi.go:567-575) reads the
  rectangle at 1-4 and the array at 5. Line 510 instead passes
  arrayObjectOf(arguments, 4), the int height. Value.Reference()
  (jvm/value.go:353-357) errors on a non-reference, so arrayObjectOf returns
  nil. SetArrayRange(nil) reaches objectArray (jvm/interpreter.go:1347-1348),
  which returns a guest NullPointerException. Every call with height>0 whose row
  bounds pass therefore throws NPE. No test covers getRGBPixels. I lowered the
  severity: the effect is a guest exception in a rarely used API on the
  platform's few Jlets, not a process crash or hang.
- **Fixed:** getRGBPixels writes into the int[] at argument 5 instead of the
  height at argument 4.

### R052 XFile.unlink / FileSystem.remove leaves exists()==true

`internal/platform/skt/skvm_file.go:435` · skt2 · correctness · PLAUSIBLE

- **Defect:** Unlink stores an empty entry instead of removing the file.
  xFileExists, xFileSize and FileSystem.exists/isFile/sizeOf then find that
  empty entry and report that the file exists with size 0, and a read-only open
  of it succeeds with no data instead of failing.
- **Failure:** A Jlet's 'new game' menu calls FileSystem.remove("save"). On the
  next launch it runs `if (FileSystem.exists("save")) load(); else newGame();`.
  exists answers true, load() opens a 0-byte file, and DataInputStream.readInt
  throws EOFException. The title crashes on start or shows a corrupt slot, where
  a handset would report no file and start a new game.
- **Evidence:** skvm_file.go:435 `runtime.storeSave(key, nil)`. Lines 371-377
  (xFileContents) return present=true for any stored key including an empty one.
  Line 391 xFileExists answers 1. Line 409 xFileSize answers 0. Lines 118-126
  (initXFileName) set found=true for the empty entry. wipi.go:132-137 maps
  FileSystem exists/isFile/remove/sizeOf to these. The wiki's MC_fsIsExist
  returns M_E_NOENT for a removed file.
- **Verification:** Traced: xFileUnlink stores an empty entry
  (skvm_file.go:435). xFileContents then reports present=true (371-377),
  xFileExists answers 1 (391), xFileSize answers 0 (409), and
  FileSystem.exists/isFile/sizeOf map to these (wipi.go:132-137). Unlinking a
  file that never existed also creates an empty one. This is a documented,
  intentional limitation: the code comment at 420-421 and docs/skvm.md:129-130
  ('unlink empties a file rather than removing it'). The docs do not cover the
  exists()==true consequence or record a handset divergence, and KTF solves the
  same problem with a fs/.removed ledger. Real divergence; whether a title
  depends on it is unverified.
- **Fixed:** `XFile.unlink` and `FileSystem.remove` empty the shared file and
  put its name on `fs/.removed` in one batch, as KTF and LGT do, so `exists()`
  answers false, a read-only open fails as for a missing file, a packaged file
  stays hidden in later sessions, and writing the path again brings it back;
  removing a missing name stores nothing.

### R053 keepUpdated enumeration rewinds on every change, so an update loop never ends

`internal/api/midp/rms.go:531` · host · correctness · CONFIRMED

- **Defect:** With keepUpdated(true), every recordAdded/Changed/Deleted
  notification runs rebuild(), and setRecordIDs resets position to 0. A title
  that modifies records while iterating restarts from the first record after
  every write.
- **Failure:** The title calls enumerateRecords(null,null,true) and runs
  `while(e.hasNextElement()){ int id=e.nextRecordId(); rs.setRecord(id,buf,0,n);
  }`. setRecord synchronously calls RecordSet.recordChanged → rebuild →
  position=0, so hasNextElement stays true and nextRecordId returns the first id
  forever. The guest hangs and rewrites the same save record in a loop.
- **Evidence:** rms.go:526-533 recordSetChanged → InvokeVirtual rebuild;
  rms.go:387 setRecordIDs(...) → line 154 position=0; platform/skt/rms.go:750
  notifyRecordListeners(store,"recordChanged",id) invoked synchronously
  (931-950).
- **Verification:** With keepUpdated(true), the set is a registered listener.
  SKT setRecord calls notifyRecordListeners synchronously (skt/rms.go:750,
  931-950), which invokes recordChanged, then rebuild (rms.go:526-533), then
  setRecordIDs, which resets position to 0 (rms.go:154). In a loop of
  hasNextElement/nextRecordId/setRecord, position goes 1, then 0, then
  nextRecordId returns ids[0] again, and the loop never ends. The fix point is
  setRecordIDs resetting the cursor on every rebuild.
- **Fixed:** A kept-updated enumeration rebuilds on each change but keeps its
  place (on its current record, or just before the next surviving one), so
  rewrite and delete loops end; an explicit rebuild/reset still goes back to the
  start. A cursor whose record was deleted sits between records, so stepping
  back answers the record before the deleted one (P02 of the PR #234 review).

### R054 V3/A3 fields written under bare keys; guest getfield/putfield miss them

`internal/api/skvm/micro3d.go:162` · host · correctness · CONFIRMED (reviewer said high)

- **Defect:** setVector3D, writeAffine3D and intField read and write
  object.Fields["x"] / ["m00"]. The JVM stores instance fields under
  "DeclaringClass.name:descriptor" (for example "m/V3.x:I"). Guest bytecode
  therefore never sees what these bodies write, and these bodies never see what
  the guest writes. The writes also bypass object.fieldMu.
- **Failure:** A title builds `new V3(7,8,9)` and reads `v.x` with getfield: it
  gets 0 (reproduced with a throwaway overlay test: getfield V3.x = 0, Fields
  map holds key "x"). A title writes `p.x = 100` and calls `a.trans(dst, p)`:
  trans reads the stale "x" key (7, or 0 for a fresh object). The result written
  to dst is again invisible to the guest's getfield. Every camera and projection
  value the title reads back is 0, and the comment says direct field access is
  the only way the title uses these classes.
- **Evidence:** micro3d.go:158-165 object.Fields["x"]=...; 178-191 intField
  reads object.Fields[name]; 307-314 writeAffine3D object.Fields[name];
  jvm/vm.go:1031-1033 key = declaring+"."+name+":"+descriptor; vm.go:921-951
  instanceValue/setInstanceValue use that key under object.fieldMu;
  micro3d_test.go only checks through vector3DOf (the same bare key), so the
  tests pass.
- **Verification:** Reproduced with a scratch overlay test, using go test
  -overlay so the repo was not changed. After new V3(7,8,9), machine.Field(v,
  "m/V3", "x", "I") returned 0 and the Fields map held the bare key "x". After
  machine.SetField(...x=100), vector3DOf still read 7. Getfield and putfield
  resolve the key as declaring+"."+name+":"+descriptor (vm.go:1031-1033), and
  m/V3 declares x, so the key is "m/V3.x:I". setVector3D (micro3d.go:158-165),
  intField (178-191) and writeAffine3D (307-314) use bare keys and bypass
  fieldMu. Severity lowered to medium: docs/history/skt.md:1915-1940 says
  XO_World draws nothing, so the visible impact is wrong values in the title's
  own read-back projection. No crash has been shown.
- **Fixed:** V3/A3 fields are read and written under the declared-class key and
  the field lock, and reads fall back to the bare key so older quicksaves still
  read.

### R055 Card x/y/w/h stored under bare keys; a Card subclass reads 0

`internal/api/wipi/definitions.go:344` · host · correctness · CONFIRMED

- **Defect:** setCardBounds fills the protected Card fields as
  object.Fields["x"|"y"|"w"|"h"], but guest getfield resolves
  "org/kwis/msp/lcdui/Card.w:I". The comment says these are 'the four protected
  fields a title reads directly', yet a direct read always returns 0. Only
  getX/getY, which read the same bare key, agree with each other.
- **Failure:** A Jlet whose Card subclass lays out with `this.w`/`this.h` after
  `super(display)` or `super(x,y,w,h)` reads 0. Reproduced with an overlay test:
  new Card(1,2,30,40) then getfield Card.w = 0, while the Fields map holds w=30.
  The title draws its layout into a zero-sized area, centres against 0, or
  divides by w.
- **Evidence:** definitions.go:207-211 fields declared; 341-347
  object.Fields["x"]... bare keys; 260-271 cardField reads the bare key;
  platform/skt/wipi.go:288-312 setCardFields also writes bare keys;
  jvm/vm.go:1031 field key is "Class.name:desc".
- **Verification:** Reproduced with an overlay test. After setCardBounds(obj, 1,
  2, 30, 40), machine.Field(obj, CardClass, "w", "I") returned 0, while Fields
  held w=30 under the bare key. Card declares x/y/w/h as protected
  (definitions.go:207-211), so a subclass getfield resolves to
  "org/kwis/msp/lcdui/Card.w:I" (vm.go:1031). setCardBounds (341-347), cardField
  (260-271) and the SKT move/resize (skt/wipi.go:288-312) all use bare keys.
  wipi.Define is installed for SKT at skt/runtime.go:412.
- **Fixed:** Card's constructor stores x/y/w/h under the getfield key; getX/getY
  read that key and fall back to the bare key so older quicksaves still answer.

### R056 State-changing HTTP routes accept cross-site simple POSTs (no Origin check)

`internal/webhost/webhost.go:269` · host · security · CONFIRMED

- **Defect:** Only the WebSocket upgrade checks Origin. POST /api/shutdown
  trusts any loopback peer, and POST /api/games and POST /api/savepack accept
  any body. A browser sends a no-cors POST with no custom headers and no
  preflight, so any website open in a browser on the server machine can trigger
  them. The claim that loopback callers 'gain nothing they did not already have'
  does not hold for a browser.
- **Failure:** A web page runs
  fetch('http://127.0.0.1:11541/api/shutdown',{method:'POST',mode:'no-cors'}).
  The peer is loopback with no forwarding header, so the server drains and exits
  and every attached game is closed, losing unsaved in-game progress. A fetch to
  /api/games?name=&lt;known>.zip with a junk body replaces the user's added
  archive (writeFileAtomically renames over it). A crafted .wfs for a publicly
  available archive, POSTed to /api/savepack?game=games/&lt;grp>/&lt;name>.zip,
  makes WriteSaveTree replace and delete the player's saves and closes the
  parked game. Browsers with local-network-access prompts mitigate this; others
  do not.
- **Evidence:** webhost.go:186-214 ServeHTTP has no Origin/Sec-Fetch check;
  260-281 serveShutdown; 293-311 isLoopbackRequest; upload.go:63-109 no origin
  check, overwrite via rename; savepack.go:165-239 import;
  wsproto/handshake.go:127-148 SameOrigin applied only to /api/session; CORP
  header (webhost.go:329) does not stop the request being sent.
- **Verification:** ServeHTTP (webhost.go:186-214) has no Origin or Sec-Fetch
  check, and cmd/server/main.go:439 installs the handler without middleware.
  serveShutdown accepts POST from any loopback peer that sends no forwarding
  header (260-311). readBody (saves.go:175-184) does not check Content-Type, so
  a no-cors simple POST, which is sent without a preflight, reaches the
  shutdown, upload and savepack handlers. architecture.md:400-405 applies an
  Origin check only to the WebSocket, on the assumption that CORS covers
  everything else, but CORS does not stop a simple POST from being sent.
  Practical limits: Local Network Access prompts in some browsers. A savepack
  import needs a .wfs whose identity matches the archive (savepack.go:198-205).
  An upload overwrite needs the archive's name.
- **Fixed:** Every request other than GET/HEAD is refused with 403 (and logged)
  when it carries an Origin whose host differs from the request Host, the
  SameOrigin rule the WebSocket already used; requests with no Origin (stop
  command, curl) and the page's same-origin requests work as before.

### R057 SMAF emits MIDI data bytes >=128, permanently breaking quicksave

`internal/audio/smaf/player.go:277` · media · save-integrity · CONFIRMED

- **Defect:** The score-track translation passes raw file bytes through as 7-bit
  MIDI fields. Mobile control changes keep their unmasked Control and Value
  (player.go:275-277), and handset pan (0x3a, line 302) and modulation (0x33,
  line 319) keep their unmasked Value. backend audioOutput.MIDIControlChange
  sets the sticky `invalid` flag for control>=128 or value>=128. Nothing resets
  that flag, and every later checkpoint capture then fails. The PCM-track path
  already drops Value>=128 (player.go:358).
- **Failure:** An overlay test showed Decode emitting an EventControlChange with
  Control:135 Value:144 for a mobile sequence `00 B0 87 90`, and Control:10
  Value:144 / Control:1 Value:200 for handset `00 3A 90` / `00 33 C8`.
  Audio.Load accepts these, because only notes and waves are validated. When the
  sound plays, audioOutput.invalid becomes true. From then until the session
  ends, every quicksave fails in captureStateAt with 'audio output cannot be
  represented within checkpoint limits'.
- **Evidence:** player.go:272-278 `Control: event.Control, Value: event.Value`.
  player.go:300-302 SeqPan `Value: event.Value`. player.go:317-319 SeqModulation
  `Value: event.Value`. score.go:296-301 and 438-460 read these bytes unmasked.
  backend/audio_output.go:211-214 sets `output.invalid = true` and 320-322 makes
  capture return an error. backend/audio_resources.go:90-100 validates only note
  and wave events.
- **Verification:** Reproduced with an overlay test. The mobile sequence `00 B0
  87 90` decodes to EventControlChange Control:135 Value:144. HPS `00 3A 90` and
  `00 33 C8` decode to Control:10 Value:144 and Control:1 Value:200. Those come
  from player.go:275-277, 302 and 319, which pass event.Control/event.Value
  unmasked; score.go:296-301 and 438-460 read them raw. Audio.Load's validation
  (audio_resources.go:71-104) checks only notes, waves and PCM controls.
  audio.emit (audio.go:367) feeds audioOutput.MIDIControlChange, which sets
  output.invalid on any value >= 128 (audio_output.go:211-214). Nothing clears
  the flag, and capture() refuses with 'cannot be represented within checkpoint
  limits' (320-322), so every later quicksave fails. Two caveats lower the
  claim. A restore replaces the output (audio_state.go:221), so 'until the
  session ends' really means until the next restore. Nothing is corrupted, only
  refused. Well-formed SMAF uses 7-bit data bytes, so a malformed sound is
  needed.
- **Fixed:** SMAF never emits a MIDI data byte >= 128 (8-bit score controls and
  handset pan/modulation >= 128 are ignored without losing their time,
  volume/expression clamped), and Audio loading (Load, LoadEvents,
  PlayTransient) refuses ProgramChange, ControlChange or PitchBend with channel >= 16, a 7-bit field >= 128 or a bend >= 16384, so no event can poison the
  recorder and every later quicksave; quicksaves from the previous build still
  restore.

### R058 Arc/round-rect walk the guest's full unclipped box (hang)

`internal/curve/curve.go:193` · media · untrusted-input · CONFIRMED

- **Defect:** FillArc tests every pixel of the guest-supplied width\*height
  bounding box. DrawArc takes 2\*pi\*max(radius) steps, and Fill/DrawRoundRect
  emit one span per row of height. None of them is told the clip or bounded, and
  every platform passes raw guest int32 arguments straight in.
- **Failure:** I measured FillArc at about 11 ns per pixel (4000x4000 took 175
  ms in an overlay test). A KTF/LGT/SKT title, or a garbage register, calling
  fillArc(x,y,100000,100000,0,360) blocks the session worker for about 110 s.
  fillArc(0,0,0x7fffffff,0x7fffffff,...) runs about 4.6e18 iterations and never
  returns. drawArc with width 0x7fffffff takes about 6.7e9 steps, and
  fillRoundRect with height 0x7fffffff makes 2^31 emit calls. A host call has no
  instruction budget or interrupt, so the session hangs, burning a CPU core,
  until the process exits.
- **Evidence:** curve.go:187-209 nested loops over row&lt;height and
  column&lt;=width. curve.go:226 `steps := int(math.Max(8,
  2*math.Pi*math.Max(radiusX, radiusY)))`. curve.go:95 loops over every row of
  height. Callers pass unbounded guest values: ktf/wipic_graphics.go:413,424
  (bounds from registers), ktf/runtime_curves.go:39-60,
  lgt/java_screen.go:614-616, lgt/wipic_draw.go:124, skt/runtime.go:803-806.
- **Verification:** FillArc (curve.go:187-209) loops over every row &lt; height
  and every column &lt;= width of the guest box, with no clip. When width is
  0x7fffffff, `column <= width` stays true after the int32 increment wraps, so
  the loop never ends. DrawArc's step count is 2\*pi\*max(radius) (line 226),
  and Fill/DrawRoundRect emit once per row (lines 95, 115). Every caller passes
  raw guest int32 values and only clips inside the emit callback: KTF
  wipicArcCall (wipic_graphics.go:401-424) and runtime_curves.go:39-60, LGT
  javaCurve (java_screen.go:607-617) and wipic_draw.go:113-126, SKT
  graphicsCurve (render.go:610-640). The KTF round-rect wrappers only check
  width/height > 0. The session has no watchdog that could interrupt a running
  Go host call. Realistic titles pass sizes near the screen, so only garbage or
  extreme arguments hang the worker; medium stands.
- **Fixed:** internal/curve gained `Bounds` and `...Within` walks that visit
  only the rows, columns and curve stretches that can land in the caller's
  bounds, with identical in-bounds pixels; the KTF Java and WIPI-C, LGT Java and
  WIPI-C, and SKT MIDP curve calls pass the clip met with the surface, so a
  guest box as large as an int returns at once.

### R059 RepairPNGPixelChecksum inflates IDAT with no output bound

`internal/wipic/png.go:146` · media · untrusted-input · CONFIRMED

- **Defect:** After any failed decode of a PNG, DecodeStandard calls
  RepairPNGPixelChecksum, which concatenates every IDAT and runs io.ReadAll over
  flate.NewReader. Nothing limits the inflated size, which can be about 1000
  times the compressed input. Nothing checks it against what IHDR implies,
  height\*(1+rowBytes).
- **Failure:** A 1x1 PNG whose IDAT is a deflate stream of 64 MB of zeros (131
  KB on disk) fails Go's decoder with 'too much pixel data'.
  RepairPNGPixelChecksum then inflates all of it: an overlay test measured 157
  MB allocated, and that was with nothing to repair. KTF accepts up to
  maxPlatformAllocation and LGT up to 8 MB of encoded image (maxImageBytes). An
  8 MB PNG of this shape makes io.ReadAll grow its buffer to roughly 8-16 GB,
  and the server process aborts out of memory.
- **Evidence:** png.go:146 `inflated, err :=
  io.ReadAll(flate.NewReader(bytes.NewReader(stream[2 : len(stream)-4])))`, with
  no io.LimitReader and no comparison to IHDR dimensions. It is reached from
  DecodeStandard at png.go:189 on every decode error, including errors unrelated
  to checksums.
- **Verification:** Reproduced with an overlay test: a 1x1 grayscale PNG whose
  65 KB IDAT inflates to 64 MB failed with 'too much pixel data', and
  DecodeStandard then allocated 157 MB. Go's decoder stops after the expected
  rows and reports the extra data. The CRCs are intact, so RepairPNGChecksums
  mends nothing. RepairPNGPixelChecksum (png.go:111-146) then concatenates every
  IDAT and runs io.ReadAll(flate.NewReader(...)), with no LimitReader and no
  comparison against the IHDR-implied size. Its only bound is maxPNGChunk on
  each chunk length. Encoded input caps: KTF 4 MB (maxPlatformAllocation,
  initialization.go:32), LGT 8 MB (maxImageBytes). SKT allows 16 MB and is also
  reachable, because a 1x1 header passes its DecodeConfig/RGBAByteLength gate
  before DecodeStandard (skt/image.go:333-342). At deflate's ~1000:1 ratio that
  is roughly 4-16 GB of memory that actually gets written, enough to kill the
  process on any typical host. Only crafted input produces a stream like this,
  so medium stands.
- **Fixed:** RepairPNGPixelChecksum hashes the inflated stream through an
  Adler-32 writer limited to the size the IHDR implies (every Adam7 pass
  included) and refuses a longer stream, so a deflate bomb no longer allocates.

### R060 runskt never destroys the MIDlet: destroyApp saves and dirty XFiles lost

`cmd/cli/main.go:377` · tools · save-integrity · CONFIRMED

- **Defect:** runSKT starts the runtime and exits without runtime.Destroy(true).
  The shared session calls Destroy on close. A MIDlet that writes its record
  store in destroyApp, or that leaves an XFile dirty and expects it to be closed
  then, loses that data on every CLI run.
- **Failure:** `runskt game.jar -cheat` played and then interrupted with Ctrl-C,
  or a `-ticks`/`-route` run, against the default tree
  var/savedata/&lt;profile>/skt/&lt;owner>. The MIDlet saves its RMS progress in
  destroyApp. runSKT returns after the loop, so destroyApp never runs and the
  save is not written. Dirty XFile data (persisted only by flush, close or
  checkpoint) is lost as well.
- **Evidence:** main.go:377 skt.Start, and runSKT returns at main.go:657 with no
  Destroy or close. skt/runtime.go:488-524: Destroy invokes destroyApp.
  skt/skvm_file.go:339-348: XFile data is persisted only from flush or close.
  session/session.go:1355-1357 calls runtime.Destroy(true) on close.
- **Verification:** runSKT starts the runtime at main.go:377 with a
  DirectorySaveStore on the default tree (:374-381). It returns through
  exitForRun at :657, and nothing in :156-663 calls Destroy or a close. The
  shared session calls runtime.Destroy(true) on close (session.go:1355-1357),
  and that is the only path that invokes destroyApp (skt/runtime.go:488-524). A
  MIDlet that saves in destroyApp therefore loses that save in every CLI run.
  The XFile half is overstated. persistXFile is called only from flush/close
  (skvm_file.go:323, :332) and from checkpoints (checkpoint_storage.go:82),
  never from Destroy itself. A dirty XFile the MIDlet does not close is lost in
  the server too, so that part is not specific to the CLI.
- **Fixed:** runskt calls `runtime.Destroy(true)` when the run ends, so a
  MIDlet's destroyApp save lands as it does when the server closes a session.

### R061 runlgt never closes the LGT session: buffered writes and destroy lost

`cmd/cli/main.go:2551` · tools · save-integrity · CONFIRMED

- **Defect:** runLGT starts an lgt.Session and returns without calling
  session.Close. Close is what flushes files the title wrote and left open
  (flushOpenFiles) and what runs DestroyClet. Every runlgt run that ends by tick
  budget, route end or Ctrl-C therefore drops those writes and the title's
  destroy-time save. The shared session and the server always call
  lgtSession.Close.
- **Failure:** `runlgt game.zip -cheat` (an interactive play session) or a
  `-ticks N` / `-route` run against the default save tree,
  var/savedata/&lt;profile>/lgt/&lt;owner>. The title keeps its save file open
  and writes to it, or it saves in its destroy callback. Ctrl-C or the end of
  the run returns from runLGT. The dirty file data is never stored and
  DestroyClet never runs, so the save on disk stays stale or zero-length. A CLI
  verification run also persists different data from what the browser host
  would.
- **Evidence:** main.go:2551 lgt.StartSession, and no Close anywhere in runLGT
  (grep `Close(` in cmd/cli finds only KTF, native, script and checkpoint).
  lgt/session.go:680-695: Close calls StopJavaThreads, flushOpenFiles and
  DestroyClet. lgt/wipic_file.go:592-614 says writes stay buffered until close.
  session/session.go:1352-1354 closes LGT with lgtSession.Close.
- **Verification:** runLGT (main.go:2286-2906) has only `defer stop()` and never
  calls session.Close. runKTF does `defer session.Close()`, and the shared
  session closes LGT at session/session.go:1352-1354. lgt Session.Close
  (lgt/session.go:680-695) is what runs StopJavaThreads, flushOpenFiles and
  DestroyClet. Its comment names 'a run that reached its tick count' as a case
  that needs the flush. flushOpenFiles has only two other callers, MC_knlExit
  (wipic.go:419) and Jlet.notifyDestroyed (java_widget.go:685). So a run that
  ends on its tick count, on a route end or on Ctrl-C drops buffered writes to
  the default save tree, var/savedata/&lt;profile>/lgt/&lt;owner>
  (main.go:2523-2525).
- **Fixed:** runlgt closes its LGT session with a fresh context when the run
  ends, so files the title left open are flushed and its destroy callback runs,
  as on the server.

### R062 Activity recreation kills the server and the running game

`mobile/android/src/com/movingwoo/wfeature/MainActivity.java:461` · tools · protocol · CONFIRMED

- **Defect:** onDestroy destroys the server process unconditionally, without
  checking isChangingConfigurations(). The manifest handles only
  keyboardHidden|orientation|screenSize|screenLayout|uiMode, so other
  configuration changes recreate the Activity. That ends the in-progress game
  and starts a second server on the same port. The `server` field is also
  assigned on a background thread and read in onDestroy without synchronization.
- **Failure:** Mid-game the player enters split-screen or pop-up view (a
  smallestScreenSize change) or connects a Bluetooth keyboard (a `keyboard`
  change). Android destroys the Activity, onDestroy SIGTERMs the server, the
  game is closed and progress since the last save is lost. The new onCreate
  relaunches on 11541 while the old process is still draining. If onDestroy runs
  before startServerAndShow has assigned `server`, the process is never
  destroyed: an orphan server keeps the port and the save locks, and
  waitUntilServing can accept that orphan's /api/status as the new server.
- **Evidence:** MainActivity.java:455-464: onDestroy calls server.destroy() with
  no isChangingConfigurations() check. AndroidManifest.xml:
  configChanges="keyboardHidden|orientation|screenSize|screenLayout|uiMode" (no
  smallestScreenSize, keyboard, navigation or density). MainActivity.java:62 and
  :90: `private Process server` is assigned on the thread started at :82, not
  volatile. :177-190: waitUntilServing accepts any 200 from 127.0.0.1:11541.
- **Verification:** onDestroy (MainActivity.java:456-464) calls server.destroy()
  and does not check isChangingConfigurations(). The manifest
  (AndroidManifest.xml:40) handles only
  keyboardHidden|orientation|screenSize|screenLayout|uiMode. Target SDK is 35
  (build.sh:82) and resizeableActivity is not declared, so split-screen (a
  smallestScreenSize change) and attaching a hardware keyboard (a keyboard
  change; a D-pad controller can also change navigation) recreate the Activity.
  The server catches SIGTERM (cmd/server/main.go:463) and closes the running
  game. onCreate then starts a second server on the fixed port 11541 (:59, :82).
  waitUntilServing (:177-190) accepts any 200 from that port. `server` is a
  plain field written on the background thread (:90), so the null-check race is
  real but narrow. The gamepad support makes controller attach a realistic
  trigger.
- **Fixed:** `configChanges` also lists keyboard, navigation, smallestScreenSize
  and density, the server Process is a static field a recreated activity reuses,
  and onDestroy ends it only when `isChangingConfigurations()` is false
  (compile-checked; no device run).

### R063 iOS drains the server only in applicationWillTerminate

`mobile/ios/src/App.swift:40` · tools · save-integrity · CONFIRMED

- **Defect:** WfeatureStop, the only call that closes the games (CloseSessions:
  destroyApp, the LGT open-file flush, the SKT destroy), is made from
  applicationWillTerminate. iOS does not call that for an app it terminates
  while suspended, which is how an iOS app normally ends. Nothing reacts to the
  scene entering the background.
- **Failure:** The player switches away mid-game. The page parks the game and
  the app is suspended. Later iOS reclaims memory, or the player swipes the app
  away from the switcher. The process is killed without
  applicationWillTerminate, so the parked game never gets destroyApp and any LGT
  file it wrote but did not close is never persisted. The save on disk is older
  than the player's progress.
- **Evidence:** App.swift:39-41 is the only WfeatureStop call. PlaySceneDelegate
  (App.swift:44-58) implements only willConnectTo: no sceneDidEnterBackground
  and no beginBackgroundTask. ios/lib/lib.go:72-84: WfeatureStop runs
  running.Close(), which is appserver.Close → CloseSessions
  (appserver.go:145-161).
- **Verification:** WfeatureStop is called only from applicationWillTerminate
  (App.swift:39-41). PlaySceneDelegate (App.swift:44-58) implements only
  willConnectTo. WfeatureStop → running.Close() (ios/lib/lib.go:72-84) is the
  only path that closes games. iOS does not call applicationWillTerminate for a
  suspended app it kills or that the user swipes away. Parking does not flush
  either: webhost park (session.go:935-975) only calls game.Pause. LGT
  flushOpenFiles runs only on exit, notifyDestroyed or Close (wipic.go:419,
  java_widget.go:685, lgt/session.go:691). So destroyApp saves and unclosed LGT
  file writes are lost on the normal iOS termination path. What is lost depends
  on whether the title writes that way.
- **Not fixed:** By the user's decision on 2026-10-16 the app saves nothing
  because it left the screen: saving is the player's to do in the game, so the
  background drain this branch had added was reverted and the iOS app drains the
  server only in `applicationWillTerminate`, as before (docs/mobile.md, "When
  the app leaves the screen"; checked on the iOS simulator).

### R064 Unknown-value scans keep up to 17 full candidate copies, unbounded

`internal/cheat/scanner.go:221` · tools · resource-leak · PLAUSIBLE

- **Defect:** A first `unknown` scan records one 16-byte Candidate per aligned
  address of every committed writable region. pushHistory then copies the whole
  candidate set on every later scan, keeping up to 16 snapshots. Nothing caps
  the candidate count, and the panel offers u8 (align 1) with "값 모름" (unknown).
- **Failure:** On the phone app the player chooses u8 and 값 모름, then presses
  "unchanged" a few times on a static screen. With N committed bytes, memory
  reaches about 17×16×N bytes: a few MB of guest memory becomes about 1 GB of Go
  heap. The in-process server is OOM-killed, and the game and any unpersisted
  writes go with it.
- **Evidence:** scanner.go:234-287: sweep appends a Candidate for every aligned
  address that the filter accepts, and FilterUnknown accepts everything.
  scanner.go:221-228: pushHistory copies all candidates and keeps
  maxScanHistory=16. panel.go:77-98: PanelScan sets the value type from the
  page. web/index.html:310-315 offers u8. No limit on len(result) anywhere.
- **Verification:** Growth is unbounded. sweep appends one 16-byte Candidate
  (Address uint32 plus Value int64) for every aligned address the filter accepts
  (scanner.go:234-287), and the unknown filter accepts everything. pushHistory
  (:221-228) deep-copies the whole set on every scan and keeps up to
  maxScanHistory=16 copies. Nothing caps candidates: PanelCandidateList
  (panel.go:132-140) caps only the list sent to the page. The panel offers u8
  and 값 모름 (web/index.html:310-324), and PanelScan sets the type
  (panel.go:77-98). CommittedRegions counts only touched pages
  (armcore/memory.go:521-527), so the per-title size is unknown. Whether
  repeated 안 변함 actually OOMs a phone depends on the title's committed memory,
  which I could not measure without running games.
- **Fixed:** A first unknown-value sweep is refused past 4,194,304 candidates
  (ErrTooManyCandidates, with a Korean panel message), and Undo history is
  capped at the same total, dropping the oldest steps first.

### R065 Save import uses window.confirm, which the iOS app answers no

`web/save-backup.js:213` · web · save-integrity · CONFIRMED

- **Defect:** Before restoring a .wfs backup, the import asks through
  `globalThis.confirm`. The iOS app's WKWebView has no WKUIDelegate, so
  `confirm()` returns false right away and shows nothing. On the iOS app, '세이브
  가져오기' therefore never restores anything and gives no status. Every other
  destructive prompt on the page uses `askToConfirm` (confirm.js) because of
  exactly this WebView behaviour.
- **Failure:** In the iOS app, a player taps 세이브 가져오기 and picks a backup in the
  Files sheet. The change handler calls `ask(...)`, which is window.confirm and
  returns false with no dialog shown. The handler returns at line 213 before
  `busy`/`importSave` runs, and no status is shown. The player cannot restore
  their progress on the platform this feature was built for, and nothing says
  why. The input's `accept=".wfs"` has no matching UTI declared in
  mobile/ios/Info.plist, which may also grey the file out in the picker.
- **Evidence:** save-backup.js:146 `confirm: ask =
  globalThis.confirm?.bind(globalThis)`; save-backup.js:213 `if (ask && !ask("이
  게임의 저장 데이터를...")) return;`; app.js:881-885 calls initSaveBackup without a
  `confirm` override; confirm.js:1-22 and add-game.js:167-170 both say
  window.confirm answers 'no' with nothing on screen in the app WebView;
  mobile/ios/src/App.swift sets no `uiDelegate` / runJavaScriptConfirmPanel
  handler (grep finds none); the Android MainActivity WebChromeClient keeps the
  default onJsConfirm, which does show a dialog, so only iOS is affected.
- **Verification:** save-backup.js:146 defaults `ask` to globalThis.confirm, and
  app.js:881-885 calls initSaveBackup without a `confirm` override. In
  mobile/ios/src/App.swift:109-139, show(url:) builds the WKWebView and never
  sets `uiDelegate`, and the file has no WKUIDelegate conformance at all.
  Without runJavaScriptConfirmPanel, WKWebView acts as if Cancel was pressed:
  confirm() returns false and nothing appears on screen. So save-backup.js:213
  returns before busy()/importSave and sets no status, and 세이브 가져오기 does nothing
  in the iOS app. The page's other destructive prompts already avoid
  window.confirm for this exact reason: confirm.js:1-17, add-game.js:167-170,
  app.js:957-959 and the index.html:126 comment. The only end-to-end import
  check is web/acceptance/pwa.mjs:82, which auto-accepts dialogs in a desktop
  browser and so never exercises WKWebView. Android's WebChromeClient
  (MainActivity.java:234) does not override onJsConfirm, so only iOS is
  affected. The point about `accept=".wfs"` and the picker is speculative and
  not needed for the defect. The feature is broken on one platform but nothing
  is lost, so medium.
- **Fixed:** Save import now asks through the page's own confirmation dialog
  (`askToConfirm`, accept button "가져오기") and goes on only after an explicit yes,
  so a page that cannot ask restores nothing and the iOS web view no longer
  silently answers "no".

## Low (88)

### R066 java/lang/Class objects are not canonical

`internal/jvm/builtins.go:60` · jvm · correctness · CONFIRMED

- **Defect:** getClass(), ldc of a Class constant and Class.forName each
  allocate a new Class object. The same class therefore never compares == to
  itself, and synchronizing on a class literal locks a different object on every
  call.
- **Failure:** A key class implements `equals(Object o){ if (o == null ||
  getClass() != o.getClass()) return false; ...}`. Here the comparison is always
  unequal, so equals is always false, and Hashtable.get, Vector.indexOf and
  contains never find such keys. `synchronized (Foo.class)` in a class file of
  version 49 or later (ldc) provides no mutual exclusion between threads.
- **Evidence:** builtins.go:60 `classObject := &Object{ClassName:
  "java/lang/Class", Native: object.ClassName}`; interpreter.go:739 ldc builds a
  new &Object each time; builtins_lang.go:665 forName returns a new object.
- **Verification:** getClass allocates a new Object each call (builtins.go:60),
  and so do ldc of a Class constant (interpreter.go:739), forName
  (builtins_lang.go:665) and aot.go:222. if_acmp compares pointers
  (interpreter.go:383), and Object.equals is identity (builtins.go:41-53). So
  `getClass() != o.getClass()` is always true for the same class. Pre-1.5 class
  files cache `Foo.class` in a synthetic static per using class, which limits
  the synchronized(Foo.class) impact, but getClass equality is broken
  everywhere.
- **Partly fixed:** getClass, ldc of a class constant and Class.forName return
  one canonical Class object per class name, rebuilt on heap restore without a
  format change; `VM.NewClassObject` (KTF's AOT bridge, bound to KTF
  class-record addresses) and `static synchronized` locking `vm.classMonitors`
  (part of the checkpoint ClassMonitors record) are left as they are. Locking
  any Class object now takes its class monitor, so `synchronized (Foo.class)`
  and Foo's static synchronized methods exclude each other (P03 of the PR #234
  review); only the separate `NewClassObject` identity is left.

### R067 Object.wait(long, int) ignores nanos, so wait(0, n) waits for ever

`internal/jvm/builtins.go:117` · jvm · correctness · CONFIRMED

- **Defect:** The (JI)V wait reads only the millisecond argument. Java rounds
  wait(0, nanos>0) up to a 1 ms timed wait, but here a zero timeout becomes an
  untimed wait.
- **Failure:** A title calls lock.wait(0, 500000) as a short poll. Instead of
  returning after about 1 ms, the thread waits until notified, which may be
  never, and the loop stalls.
- **Evidence:** builtins.go:109-127: for descriptor != "()V" only
  arguments[1].Int64() is used; a timeout of 0 makes waitMonitor set timed=false
  (monitor.go:107-108).
- **Verification:** For both (J)V and (JI)V, only arguments[1].Int64() is read
  (builtins.go:116-126), and the nanos argument is ignored. A zero timeout makes
  waitMonitor set timed=false (monitor.go:107-108), so wait(0, n>0) waits until
  notified. CLDC 1.1 rounds this case up to wait(1). It is a rare call.
- **Fixed:** Object.wait(long,int) checks nanos is in 0..999999
  (IllegalArgumentException otherwise) and rounds like the specification, so
  wait(0, n>0) is a 1 ms timed wait.

### R068 Large sleep/wait milliseconds overflow time.Duration

`internal/jvm/builtins.go:233` · jvm · correctness · CONFIRMED

- **Defect:** Thread.sleep and Object.wait compute time.Duration(milliseconds)
  \* time.Millisecond without clamping. Any value above about 9.22e12 ms wraps
  in int64.
- **Failure:** A guest thread parks itself with Thread.sleep(Long.MAX_VALUE).
  The product wraps to -1 ms, the timer fires immediately and the thread returns
  at once. The loop meant to sleep 'for ever' then spins and burns CPU.
  sleep(10^13) wraps to a large negative value and also returns immediately.
- **Evidence:** builtins.go:233 `vm.guestDelay(time.Duration(milliseconds) *
  time.Millisecond)`; builtins.go:125 is the same for wait; guestDelay
  (builtins.go:385-394) returns non-positive values unchanged;
  thread_checkpoint.go:185 time.NewTimer(negative) fires immediately.
- **Verification:** time.Duration(milliseconds) \* time.Millisecond
  (builtins.go:233) wraps for values above about 9.22e12. Long.MAX_VALUE becomes
  -1 ms. guestDelay returns non-positive values unchanged (builtins.go:385-387),
  and time.Sleep or time.NewTimer with a negative duration returns immediately,
  so sleep(Long.MAX_VALUE) does not block. For wait (builtins.go:125), a wrapped
  negative timeout becomes an untimed wait (monitor.go:107), which is harmless
  in effect. Only sleep misbehaves, and only with unusually large arguments.
- **Fixed:** Large sleep/wait times are capped at the longest time.Duration
  (`guestMilliseconds`, a slowed speed clamps too), and a Host-pass sleep is
  released by VM.Close with ErrClosed.

### R069 StringBuffer.setLength allocates whatever length the guest passes

`internal/jvm/builtins.go:1651` · jvm · untrusted-input · CONFIRMED (reviewer said medium)

- **Defect:** setLength accepts any non-negative int and immediately allocates
  make([]uint16, length) without checking MaxArrayLength or any other budget.
- **Failure:** Guest code calls sb.setLength(Integer.MAX_VALUE), for example
  after a corrupted length is read from a save record. The native attempts a 4
  GB allocation and the Go runtime aborts the server process. Java/CLDC would
  throw OutOfMemoryError, which a title can catch.
- **Evidence:** builtins.go:1640-1654: `if length < 0 {...}` then `grown :=
  make([]uint16, length)` with no upper bound.
- **Verification:** setLength (builtins.go:1640-1654) rejects only negative
  values and then runs make([]uint16, length) with no upper bound. StringBuffer
  is not declared synchronized and has no other check (corelib.go:171-208). On
  64-bit hosts a fresh 4 GB span may stay virtual until it is touched. The
  immediate abort the reviewer describes therefore depends on the host, but a
  later toString or String(StringBuffer) runs utf16.Decode, which writes about 8
  GB and does exhaust memory. Reaching this needs an absurd length argument,
  either crafted or corrupted. No realistic title path was shown.
- **Fixed:** StringBuffer.setLength refuses a length above MaxArrayLength before
  allocating, with the limit error arrays use.

### R070 Null String arguments end the session instead of throwing NPE

`internal/jvm/builtins.go:1851` · jvm · error-handling · CONFIRMED

- **Defect:** nativeString returns a plain Go error for a null reference.
  String.concat, compareTo, startsWith, endsWith, indexOf(String),
  equalsIgnoreCase, Class.forName and String(String) therefore fail as host
  errors rather than raising java/lang/NullPointerException, which a title can
  catch.
- **Failure:** A title calls `name.compareTo(other)` with other == null inside
  try { ... } catch (Exception e) { fallback }. Java throws NullPointerException
  and the fallback runs. Here the native returns `native argument 1 is not a
  string`, which is not a GuestException, so it is never handled and the guest
  thread or session ends.
- **Evidence:** builtins.go:1851-1859 nativeString; builtins.go:1165-1175
  concat, 1310-1318 compareTo, 1266-1276 startsWith, 1338-1347 endsWith, 1249
  indexOf; builtins_lang.go:657 forName; contrast parsedText
  (builtins.go:810-817), which does raise a guest exception for null.
- **Verification:** For a null argument, nativeString returns a plain fmt error
  (builtins.go:1851-1859). The interpreter handles only \*GuestException
  (interpreter.go:90-99). Any other error becomes an ExecutionError that ends
  the thread or session, so a title's catch handler never sees it. A null
  receiver is caught earlier as an NPE, so only null arguments are affected:
  concat, compareTo, startsWith, endsWith, indexOf(String) and forName.
  equalsIgnoreCase (builtins.go:1327-1336) is worse than claimed, because Java
  returns false for null rather than throwing.
- **Fixed:** `nativeString` raises a catchable NullPointerException for a null
  String argument, and equalsIgnoreCase(null) returns false.

### R071 StringBuffer charAt/setCharAt/String(StringBuffer) skip the buffer lock

`internal/jvm/builtins_lang.go:941` · jvm · concurrency · CONFIRMED

- **Defect:** Every other StringBuffer native takes data.mu, but setCharAt,
  charAt and the String(StringBuffer) constructor read and write data.units
  without it. On SKT, guest threads are goroutines (no GuestThreadStarter), so
  these race with append, delete or setLength on the same buffer.
- **Failure:** Thread A appends to a shared StringBuffer, which reassigns
  data.units under data.mu, while thread B calls sb.charAt(i) without the lock.
  B can read a torn slice header (new length, old pointer) and index past the
  backing array, or read stale data. Guest code relies on StringBuffer being
  synchronized. The race detector flags it, and a torn read can panic, which
  ends that guest thread.
- **Evidence:** builtins_lang.go:938-941 setCharAt and 953-956 charAt access
  data.units unlocked; builtins_lang.go:922 `utf16.Decode(data.units)` unlocked;
  builtins.go:1524-1533, 1663-1665 and 1690-1692 show the locked pattern;
  skt/runtime.go sets no GuestThreadStarter.
- **Verification:** setCharAt (builtins_lang.go:938-941), charAt (953-956) and
  String(StringBuffer) (922) read and write data.units without data.mu, while
  append, setLength and toString take it (builtins.go:1647, 1663, 1690). The
  StringBuffer methods are not declared ACC_SYNCHRONIZED (corelib.go:171-208),
  so invokeInstanceReceived never takes the receiver monitor and data.mu is the
  only guard. SKT installs no GuestThreadStarter (only ktf/client.go:1225 does),
  so guest threads there are goroutines. The race is real but needs a buffer
  shared across threads.
- **Fixed:** StringBuffer.charAt, setCharAt and the String(StringBuffer)
  constructor take the buffer lock like every other StringBuffer native.

### R072 InputStreamReader swallows valid characters after a bad byte

`internal/jvm/corelib_io.go:1807` · jvm · correctness · CONFIRMED

- **Defect:** inputStreamReaderRead appends bytes until the decoder yields
  exactly one non-U+FFFD unit, up to 4 bytes. After an undecodable byte the
  pending prefix always decodes to two or more units, so the following valid
  bytes are consumed into the same replacement character.
- **Failure:** A text resource contains a stray 0xFF followed by "ABC". The read
  accumulates [FF], then [FF,'A'] ("�A", 2 units), then [FF,'A','B'], then
  [FF,'A','B','C'], and returns one U+FFFD. 'A', 'B' and 'C' are lost from the
  dialogue or word list. A UTF-8 4-byte character, which is 2 UTF-16 units, is
  likewise always replaced.
- **Evidence:** corelib_io.go:1787-1811: `units :=
  utf16.Encode(...decodePlatformBytes(pending))`; returns only `if len(units) ==
  1 && units[0] != 0xfffd`, otherwise keeps appending until maxDecodedSequence
  and returns 0xfffd.
- **Verification:** inputStreamReaderRead (corelib_io.go:1787-1811) keeps
  reading until the decoded pending bytes form exactly one non-U+FFFD unit, for
  up to 4 bytes. The KTF decoder (x/text EUC-KR, ktf/client.go:1240) decodes an
  invalid lead byte followed by 'A' as "� A", which is 2 units. The loop
  therefore consumes up to three following valid bytes and returns one U+FFFD.
  With the default UTF-8 decoder, a 4-byte sequence always yields 2 units, so it
  is also replaced. Only text with stray bytes is affected.
- **Fixed:** After an unreadable sequence InputStreamReader.read returns one
  U+FFFD and keeps the following byte for the next read; a character outside the
  basic plane comes back as two surrogate units over two reads.

### R073 Throwable.getMessage returns null for an empty message

`internal/jvm/corelib_lang.go:71` · jvm · correctness · CONFIRMED

- **Defect:** throwableGetMessage treats an empty message string as absent, so
  an exception constructed with "" reports null rather than "".
- **Failure:** A title throws `new Exception("")` and its handler calls
  `e.getMessage().length()`. Java returns 0, but here getMessage() is null and
  the handler itself throws NullPointerException.
- **Evidence:** corelib_lang.go:70-73 `message, _ := object.Native.(string); if
  message == "" { return ReferenceValue(nil), nil }`; throwableInitMessage
  (corelib_lang.go:61) stores "" as is.
- **Verification:** throwableInitMessage stores "" as is
  (corelib_lang.go:56-61), and throwableGetMessage returns null for an empty
  string (corelib_lang.go:70-73), so new Exception("").getMessage() is null
  where Java returns "". throwableToString (corelib_lang.go:84) likewise omits
  the colon and space. This is an edge case.
- **Fixed:** getMessage returns "" for an exception built with "" (null only
  when built without a message), and toString keeps the ": " separator.

### R074 Interrupt consumed by sleep leaves a stale wake token

`internal/jvm/thread_checkpoint.go:159` · jvm · correctness · CONFIRMED

- **Defect:** Thread.interrupt sets the interrupted flag and posts a token to
  the buffered wake channel. sleepCheckpointThread clears the flag but does not
  drain the token. The next sleep, wait or join on that thread then sees the
  stale token and throws a second, spurious InterruptedException.
  consumeThreadInterrupt drains correctly; this path does not.
- **Failure:** A guest thread interrupts itself, then calls Thread.sleep(10)
  twice, catching InterruptedException each time. Java throws once. Here both
  sleeps throw: a probe counted first=1, second=1. A worker that treats
  InterruptedException as a stop request, or any later wait(), is falsely
  interrupted.
- **Evidence:** thread_checkpoint.go:155-165 clears state.interrupted without
  draining state.wake; thread_checkpoint.go:192-197 any token on state.wake
  raises InterruptedException; builtins.go:203-207 interrupt posts the token;
  thread_join.go:40-53 consumeThreadInterrupt drains it, and monitor.go:147-149
  likewise fires on a stale token.
- **Verification:** Thread.interrupt sets interrupted and posts a token to the
  buffered wake channel (cap 1, builtins.go:203-207, 838). sleepCheckpointThread
  clears the flag and throws without draining state.wake
  (thread_checkpoint.go:157-161). On the next sleep the flag is false, but the
  select at thread_checkpoint.go:192 takes the stale token immediately and
  throws a second InterruptedException. A later wait() does the same:
  consumeThreadInterrupt drains only when the flag is set
  (thread_join.go:40-53), so continueMonitorWait (monitor.go:147) fires on the
  stale token. This applies to guest threads only; the main thread uses
  time.Sleep.
- **Fixed:** A sleep that throws for an earlier interrupt also drains the
  leftover wake signal, so the next sleep/wait/join does not throw a second
  InterruptedException.

### R075 Number parsing accepts input CLDC rejects

`internal/jvm/builtins_lang.go:207` · jvm · correctness · PLAUSIBLE

- **Defect:** Integer.parseInt and Integer.valueOf use strconv.ParseInt, which
  accepts a leading '+'. CLDC's parseInt throws NumberFormatException for it.
  Long.parseLong, Byte.parseByte, Short.parseShort and the radix forms TrimSpace
  the text first, so padded strings parse where Java throws.
- **Failure:** A title validates typed or stored numeric text with `try {
  Integer.parseInt(s) } catch (NumberFormatException e) { reject }`. "+5" and
  (for parseLong) " 12 " are accepted here, so input the handset rejected is
  taken as valid, for example in a save or name record.
- **Evidence:** builtins.go:798 and 444 strconv.ParseInt(text, radix, 32) with
  no '+' check; builtins_lang.go:207, 262 and 307 strings.TrimSpace before
  parsing; builtins_lang.go:566 parseRadix also trims.
- **Verification:** The divergence is real. strconv.ParseInt accepts a leading
  '+' (builtins.go:798, 444), which CLDC's parseInt rejects. parseLong,
  parseByte and parseShort and the radix forms call TrimSpace first
  (builtins_lang.go:207, 262, 307, 566). docs/jvm.md:562-569 says no local title
  reaches the radix forms. A title that relies on a NumberFormatException for
  '+5' or for padded text is speculative, so the impact is smaller than claimed.
- **Fixed:** All integer parses reject a leading '+' and surrounding spaces as
  CLDC does (parseInt already did not trim), and Integer.parseInt(String,int)
  checks the radix range.

### R076 Legal class files with an unpaired surrogate in a constant are rejected

`internal/jvm/classfile/mutf8.go:44` · jvm · correctness · PLAUSIBLE (reviewer said medium)

- **Defect:** The JVM specification lets CONSTANT_Utf8 encode any sequence of
  UTF-16 code units, each 3-byte encoded, including lone surrogates.
  decodeModifiedUTF8 rejects unpaired surrogates, so Parse fails and the whole
  class cannot load. Separately, strings are held as Go UTF-8 at run time, so
  String(char[]), valueOf(char) and append(char) of a lone surrogate silently
  become U+FFFD.
- **Failure:** A title whose obfuscator encrypts string literals (char values
  XORed with a key, which routinely produce 0xD800–0xDFFF) or which stores table
  data as "\uD8xx" literals fails to load the containing class (`unpaired high
  surrogate at code unit N`), so the title cannot start. When such a class does
  load, a decrypt loop over toCharArray()/new String(char[]) gets 0xFFFD in
  place of the original unit and produces wrong text or data.
- **Evidence:** classfile/mutf8.go:41-53 returns errors for unpaired high/low
  surrogates; classfile/constant_pool.go:84-87 turns this into a class parse
  failure; builtins.go:1114, 1371 and 1436 use utf16.Decode, which maps lone
  surrogates to U+FFFD; classfile/class_test.go:82 asserts the rejection.
- **Verification:** decodeModifiedUTF8 rejects lone surrogates (mutf8.go:41-53),
  and the rejection is asserted in classfile/class_test.go:82. JVMS allows them
  in CONSTANT_Utf8, so legal class files can fail Parse
  (constant_pool.go:84-87). At run time, strings are Go UTF-8, and utf16.Decode
  maps lone surrogates to U+FFFD. No title in evidence has such a constant. The
  obfuscation scenario is speculative: XOR with keys below 0x100 cannot move
  Hangul or ASCII text into D800-DFFF, so only 16-bit keys or binary-in-string
  data would hit this. The defect is real; reachability is unconfirmed.
- **Partly fixed:** A CONSTANT_Utf8 with half a surrogate pair no longer rejects
  the class: it loads and the lone half reads as U+FFFD, because run-time
  strings are Go text (documented limitation).

### R077 notify can be absorbed by a waiter that already timed out

`internal/jvm/monitor.go:163` · jvm · concurrency · PLAUSIBLE

- **Defect:** A timed or interrupted waiter leaves continueMonitorWait's select
  and only afterwards takes m.mu to remove itself from m.waiters. A notify() in
  that window pops this departing waiter as the notified thread, so another
  waiter in the wait set is never woken.
- **Failure:** Thread A is in wait(100) and thread C is in wait() on the same
  monitor. A's timer fires, and before A re-locks m.mu, thread B calls notify(),
  which removes A (waiters[0]) and closes its notification channel. A then
  returns as a timeout. C stays in the wait set with no notification and blocks
  for ever: a lost wakeup that hangs the producer/consumer path.
- **Evidence:** monitor.go:142-149 the select picks deadline or wake;
  monitor.go:162-164 removes the waiter only afterwards; monitor.go:66-74 notify
  pops waiters[0] unconditionally.
- **Verification:** The timeout scenario the reviewer describes is not a defect.
  wait(long) cannot tell timeout from notify. A waiter whose timer fired but
  that is still in the wait set when notify runs (monitor.go:66-74, 162-164) is
  legitimately notified under JLS 17.2. HotSpot has the same window between
  unpark and dequeue, so C staying blocked is normal Java behaviour, and the
  reason code uses notifyAll. The interrupt variant is a real defect. If the
  waiter.wake branch fires (monitor.go:147-149) and notify then pops that
  waiter, the waiter still throws InterruptedException (monitor.go:171-172),
  ignoring wait.notified, while other waiters stay parked. That violates JLS
  17.2.4: notifications cannot be lost due to interrupts. The window is narrow.
- **Fixed:** A waiter that a notify chose after an interrupt woke it returns
  normally with the interrupt still pending, so the notification is not lost
  (JLS 17.2.4); the timeout variant is legitimate per the verifier and
  unchanged.

### R078 Blended clipped stand-in skips the flag-word overlap check

`internal/armcore/blended_blit.go:1312` · vm · correctness · CONFIRMED

- **Defect:** runBlendedClipped treats the blend flag as invariant for the whole
  run but never checks the flag word (flagRecord + flagOffset) against the
  destination span. It checks only the frame slot holding the record pointer.
  The plain path (clipped_blit.go:183) and runBlendedSpilled (:1477) both check
  the word. The writer's invariants cover it only if the writer happens to read
  the same word.
- **Failure:** A blended clipped blit whose destination span covers the flag
  word, with a writer that does not read that word: the guest's first stored
  pixel changes the flag, so later iterations take the plain fall-through. The
  stand-in keeps blending every pixel of the run.
- **Evidence:** blended_blit.go:1309-1313 checks slotAddress, stack+record,
  record+colours, stack+lowSlot/highSlot/flagSlot only; clipped_blit.go:183
  `overlaps(flagRecord+loop.flagOffset)`; blended_blit.go:1477 checks the
  spilled guard word.
- **Verification:** runBlendedClipped checks slotAddress, the record and colours
  pointers, the low/high/flag slots and draw.program.invariants
  (blended_blit.go:1309-1320). It never checks flagRecord+loop.flagOffset. It
  does not even receive flagRecord, which runClippedBlit reads at
  clipped_blit.go:117-124. The plain path checks that word
  (clipped_blit.go:183), and runBlendedSpilled checks its guard word
  (blended_blit.go:1477). A destination span that covers the flag word, with a
  writer that does not read the same word, would keep blending after the guest's
  first store cleared the flag. A blit that draws over its own graphics record
  is unrealistic, so the bug is latent.
- **Fixed:** `runBlendedClipped` receives the flag record and declines when the
  destination span covers the flag word, as the plain path already did.

### R079 Memory.Read mutates shared caches under a read lock

`internal/armcore/memory.go:409` · vm · concurrency · CONFIRMED (reviewer said medium)

- **Defect:** Read takes mu.RLock and calls readLocked. readLocked goes through
  validateLocked, which writes memory.lastMapping (memory.go:507), and through
  pageAt, which writes dataIndex/dataPage and page.permission via
  rememberPermission. Concurrent host readers therefore race. ValidateRange's
  own comment says validateLocked mutates and takes Lock for that reason; Read
  does not.
- **Failure:** The gdb stub serves an `m` packet on its connection goroutine
  while a session goroutine reads guest memory through
  Memory.Read/ReadHalfwords. Both hold RLock and both write lastMapping, a
  24-byte struct. A torn value (start from one mapping, end/permission from
  another) makes the lastMapping fast path in validateLocked accept accesses
  that span unmapped or read-only memory. A torn dataIndex/dataPage pair returns
  another page's bytes. Confirmed with `go test -race` on two goroutines calling
  Read over two mappings: DATA RACE between memory.go:507 (write) and
  memory.go:479 (read).
- **Evidence:** memory.go:408-411 `memory.mu.RLock(); ... return
  memory.readLocked(...)`; memory.go:507 `memory.lastMapping = mapping`;
  memory.go:1268 `memory.dataIndex[way], memory.dataPage[way] = index, page`;
  memory.go:922 `page.permission |= mapping.permission`; state.go:65 comment
  `validateLocked may refresh the last-mapping cache` then takes mu.Lock.
- **Verification:** Memory.Read holds only mu.RLock (memory.go:408-411).
  readLocked then calls validateLocked, which writes memory.lastMapping (:507),
  and pageAt, which writes dataIndex/dataPage (:1268) and page.permission
  through rememberPermission (:922). Two concurrent readers therefore race, and
  ValidateRange (state.go:65-66) takes Lock for exactly this reason. In normal
  sessions host access is serialized: KTF and LGT workers run on grants, and the
  session goroutine is serialized with keys and cheats. The race is reachable
  through the gdb stub. docs/cli.md:227 says the run does not wait for a client,
  and armcore/debug.go stops only at breakpoints or steps, so the guest runs
  freely while the connection goroutine serves `m` packets.
  CoreTarget.ReadMemory then calls Memory.Read alongside the session goroutine's
  SVC handlers. The stub is a dev-only CLI feature and a harmful torn write
  needs a narrow interleaving, so I rate it low.
- **Fixed:** `Memory.Read` takes the exclusive lock, as ValidateRange does,
  because validation and the page lookup refresh shared caches. Historical
  since 2026-10-19 (PR #242 and its review follow-up): the exclusive lock cost
  a light KTF title most of the 12% more instructions a tick it retired than on
  0.5.2, so `Memory.Read`, `Memory.Holds` and `ValidateRange` share the lock
  again and refresh no cache instead. They read the last-mapping cache without
  writing it and find pages through the page directory rather than the page
  cache. `TestConcurrentHostReadsDoNotRace` reads, under `-race`, two mappings
  whose pages share page-cache ways, which is where a read through the page
  cache would race. See [ARM core](armcore.md).

### R080 Table-blit and byte-blend analysers accept pre-incremented pointers

`internal/armcore/table_blit.go:272` · vm · correctness · CONFIRMED

- **Defect:** Like the fill loop, analyseTableBlit and analyseByteBlend never
  require a pointer's load or store to precede its advance. In table_blit the
  advance check compares against loop.source/loop.destination before they are
  bound, and their zero value is r0, so an `adds r0,#1` (or `adds r0,#2` for the
  destination) ahead of the access is accepted. byte_blend binds the pointers
  from the first `adds #1` regardless of where the loads and store sit. Both
  stand-ins then read and write from the head-time pointer instead of
  pointer+step.
- **Failure:** Palette blit body `adds r0,#1; ldrb r3,[r0]; lsls r3,r3,#1; ldrh
  r3,[r2,r3]; strh r3,[r1]; adds r1,#2; subs r3,r4,#1; lsls r3,r3,#16; lsrs
  r4,r3,#16; ldr r3,[pc,#4]; cmp r4,r3; bne`: every stood-in pixel is shifted
  one source byte. Overlay A/B: 39 of 40 pixels differ, while the registers
  match, so nothing else flags it.
- **Evidence:** table_blit.go:272-281 `case step == 1 && !haveSource: if
  register != loop.source` with loop.source still zero before the ldrb at :187;
  byte_blend.go:241-246 bind source/destination from the first two `adds #1`
  with no order relation to the ldrb/strb at :183-194; runTableBlit:57-58 and
  runByteBlend:59-60 start at the head register value.
- **Verification:** table_blit.go:272-281 compares the advanced register with
  loop.source and loop.destination before they are bound. Their zero value is
  r0, because `*loop = tableBlit{...}`. I traced the reviewer's body: `adds
  r0,#1` passes because r0 == 0, then ldrb binds source=r0, and the remaining
  checks (seen set, limit==scratch allowed at :337) pass. runTableBlit:57 then
  starts at the head-time r0, which is off by one byte. This only affects
  pointers in r0. byte_blend.go:241-246 binds source and destination from the
  first two `adds #1` with no order relation to the loads or the store, so any
  pre-increment body is accepted, and runByteBlend:59-60 uses head-time
  pointers. The defect is real but latent, since the observed titles use
  post-increment.
- **Fixed:** The table-blit and byte-blend recognisers require each pointer to
  advance after every access through it (the byte blend also requires each
  loaded byte to come before its use), so pre-incremented bodies are refused.

### R081 Modulate stand-in reads masks from registers the body may reuse

`internal/armcore/word_modulate.go:272` · vm · correctness · CONFIRMED (reviewer said medium)

- **Defect:** runWordModulate takes both channel masks from the
  maskFirst/maskSecond registers as they are at the loop head. The guest reloads
  them from literals at body[1] and body[3] on every iteration. The analysis
  never proves those registers survive to the head: the exclusion list compares
  them only with first/second/counter, so maskFirst/maskSecond may be the same
  register as `negative`, `advance` or `test`, which are written after the
  masks' last use. With 12 low-register roles and 8 low registers, some reuse is
  unavoidable, and a different allocation from another compiler run picks a mask
  register.
- **Failure:** The tutorial body with the minus-one built in r5 (`movs r5,#1;
  rsbs r5,r5,#0; add r12,r5`, where r5 is maskFirst) is still recognised. At the
  head r5 holds 0xffffffff, so every stood-in word is blended with mask -1.
  Overlay A/B: 102 of 108 stream words differ from the interpreted run. The loop
  already records loop.literals, so the masks could have been read from there.
- **Evidence:** word_modulate.go:85-86 `maskFirst :=
  context.Registers[loop.maskFirst]`; :178-191 maskFirst/maskSecond are bound
  from literal loads at body[1]/body[3]; :217-237 negative/advance/test are
  bound with no relation to the masks; :272 exclusion loop checks only `seen` =
  {first, second, counter}; `negative` is not in any check.
- **Verification:** The masks are bound at body[1] and body[3]
  (word_modulate.go:178-191). The second half reuses them without reloading, so
  they must survive only until body[32]. `negative` (body[33]), `advance`
  (body[36]) and `test` (body[39]) are written later, and the exclusion check at
  :265-276 tests them only against {first, second, counter}, never against
  maskFirst or maskSecond. A body with negative==r5==maskFirst is therefore
  accepted. At the loop head, runWordModulate:85-86 reads r5, which then holds
  0xffffffff instead of the literal. The fix is easy because loop.literals is
  already recorded. The impact is latent: this is a 42-instruction sequence
  match for one title. That title's fixture (word_modulate_test.go:14-62) keeps
  the masks in r4/r5 and uses r3 and r2 for negative, advance and test, so the
  real title is unaffected.
- **Fixed:** The modulate stand-in reads the masks from the literals the guest
  reloads every step instead of head-time registers, and the recogniser refuses
  bodies whose mask registers alias each other or anything written while the
  masks are in use.

### R082 gdb PC writes go through interworking SetPC and flip Thumb threads

`internal/gdbstub/core.go:136` · vm · protocol · CONFIRMED

- **Defect:** CoreTarget.SetRegister(15, v) reaches Context.SetPC, which picks
  the state from bit 0. gdb writes PC without the Thumb bit and sets the state
  through CPSR, which SetCPSR silently drops. Writing PC on a Thumb thread
  therefore either switches it to ARM state (word-aligned address) or fails with
  E01 (halfword-aligned). A 'G' write fails only after r0-r14 have already been
  written.
- **Failure:** Stopped in Thumb code, the user runs `jump *0x1000` or `set $pc =
  0x1000`. The stub clears CPSR.T and the guest resumes decoding Thumb code as
  ARM. At 0x1002 the write is refused with E01 instead.
- **Evidence:** gdbstub/core.go:131-137 forwards to thread.SetRegister;
  armcore/core.go:225 -> context.go:51-53 `if index == RegisterPC { return
  context.SetPC(value) }`; context.go:68-79 bit 0 selects state and `address&3
  != 0` errors; gdbstub/core.go:143 `func (target *CoreTarget) SetCPSR(uint32)
  {}`; session.go:191-199 writes registers in order and returns E01 midway.
- **Verification:** CoreTarget.SetRegister forwards to Thread.SetRegister
  (gdbstub/core.go:131-137), which calls Context.SetRegister and, for r15, SetPC
  (context.go:51-53, 68-79). SetPC chooses the state from bit 0: an aligned
  value clears CPSR.T, and a halfword-aligned value returns an error, which
  becomes E01. gdb sends the PC without the Thumb bit and the state through
  CPSR, and SetCPSR is a no-op (gdbstub/core.go:143). `set $pc` or `jump` on a
  stopped Thumb thread therefore switches it to ARM state or fails.
  writeAllRegisters (session.go:191-199) writes r0-r14 before PC fails. Even a
  'G' round trip of the PC that 'g' reported, which is stored without bit 0,
  flips a Thumb thread to ARM. This affects the dev-only gdb stub.
- **Fixed:** A gdb PC write keeps the thread's instruction set (only an address
  only Thumb can run switches an ARM thread), SetCPSR applies NZCVQ and T, and
  'G' parses all registers before writing any.

### R083 Stale buffered stop makes 'c' report a stop while the guest runs

`internal/gdbstub/core.go:181` · vm · concurrency · CONFIRMED

- **Defect:** hook publishes into the 1-slot stops buffer before parking, and
  resume calls release() before reading stops. If the guest parks while no
  resume is waiting, its stop stays buffered. This happens when it hits a
  breakpoint after Z0 but before 'c' arrives, since the guest runs freely after
  attach. The next 'c' wakes the guest past that breakpoint and immediately
  consumes the stale stop, replying S05 while the guest is running. From then on
  every 'c' releases the newest park and reports the previous one.
- **Failure:** gdb sends Z0 on a hot loop, and the guest hits it before 'c'
  arrives. 'c' answers S05 at once while the thread is running again. A
  following 'g' returns the last saved context, not the current state, and 'P'
  fails with E01 (thread not suspended). The debugger stays one stop out of sync
  for the rest of the session.
- **Evidence:** core.go:25,44 `stops: make(chan armcore.DebugStop, 1)`;
  core.go:82-88 publish-then-park; core.go:180-181 `target.release()` then
  `stop, ok := <-target.stops` with no draining of an earlier stop;
  core.go:131-137 SetRegister fails unless the thread is suspended.
- **Verification:** stops is buffered with size 1 (core.go:44). hook stores the
  stop with a non-blocking send and then parks (core.go:82-88), so a stop raised
  while no resume is waiting stays buffered. The guest runs freely after attach
  (docs/cli.md:227; debugCheck stops only on breakpoints or steps). If a
  breakpoint is hit between Z0 and 'c', resume calls release() and then
  immediately receives the stale stop (core.go:180-181). It replies S05 while
  the guest runs past the breakpoint. The next stop is buffered again with
  nobody waiting, so every later 'c' is one stop behind. During that window the
  thread is Running, so 'P' fails with E01 through Thread.SetRegister
  (armcore/core.go:219-223).
- **Fixed:** The stop is published under the same lock as the park, so resume
  tells a stop still parked from one already released: a parked stop is reported
  without releasing the guest, and a stale one is dropped.

### R084 gdb packet body read has no length bound

`internal/gdbstub/packet.go:60` · vm · untrusted-input · CONFIRMED (reviewer said medium)

- **Defect:** readPacket reads the body with bufio.Reader.ReadString('#'), which
  keeps growing its buffer until a '#' arrives. The stub listens on a
  user-supplied TCP address, possibly all interfaces, and advertises
  PacketSize=1000, but never enforces any bound, so a peer can force unbounded
  allocation.
- **Failure:** A peer connects to the -gdb port, sends '$' and then streams
  bytes with no '#'. ReadString keeps appending until the process runs out of
  memory, which takes down the emulator session. writeMemory then decodes
  whatever arrives with no cap of its own.
- **Evidence:** packet.go:60 `body, err := reader.ReadString('#')`;
  session.go:172 advertises `PacketSize=1000` but nothing enforces it;
  listen.go:41-42 `net.Listen("tcp", address)` with the CLI-provided address
  (cmd/cli/main.go:1174).
- **Verification:** readPacket calls reader.ReadString('#') on an unbounded
  bufio.Reader (packet.go:60). ReadString keeps collecting buffer fragments
  until it finds the delimiter, so a peer that sends '$' and then streams bytes
  without '#' grows memory without limit. maxTransfer caps only the `m` reply
  length (session.go:236-252), and PacketSize=1000 is only advertised. I lowered
  the severity because the stub is an opt-in development CLI feature on a
  user-chosen address, and any peer that can connect already has unauthenticated
  control of the guest (M writes, k kill). Memory exhaustion is a minor addition
  to that exposure.
- **Fixed:** gdb packet bodies are read with a 64 KiB bound (16 times the
  advertised PacketSize); a longer body ends the connection with
  errPacketTooLong.

### R085 ARM LDM/LDR to PC never interworks while Thumb POP {pc} does

`internal/armcore/arm.go:726` · vm · correctness · PLAUSIBLE (reviewer said medium)

- **Defect:** Thumb POP {..., pc} uses branchExchange (ARMv5T interworking on
  bit 0), and the core accepts ARMv5T BLX in both states. ARM-state LDM with PC
  in the list and LDR pc call setARMPC instead, which clears bits 1:0 and forces
  ARM state. In ARMv5T, both of those loads switch to Thumb when bit 0 is set.
- **Failure:** ARMv5T interworking code: a Thumb caller reaches an ARM function
  with BLX, which the core supports. The function returns with `ldmfd sp!, {r4,
  pc}`, and the popped LR has bit 0 set. The core continues in ARM state at the
  Thumb return address & ~3 and decodes Thumb halfword pairs as ARM words,
  giving an undefined fault or garbage execution. Overlay probe: `ldmia r0,{pc}`
  and `ldr pc,[r0]` loading 0x1101 gave pc=0x1100 thumb=false.
- **Evidence:** arm.go:725-726 `if register == RegisterPC {
  context.setARMPC(value) }`; arm.go:656-657 same for LDR; thumb.go:548-553 POP
  uses `context.branchExchange(value)`; context.go:87-90 setARMPC clears the T
  bit; arm.go:57-58 and thumb.go:399-408 accept v5T BLX.
- **Verification:** The code does what the finding says. ARM LDM loading PC
  (arm.go:725-726) and LDR pc (arm.go:656-657) call setARMPC, which clears T and
  bits 1:0 (context.go:87-90). Thumb POP {pc} uses branchExchange
  (thumb.go:548-553), and BLX is accepted in both states (arm.go:57-58,
  thumb.go:399-408). The core is therefore inconsistent: Thumb POP follows v5T,
  ARM LDM and LDR follow v4T. docs/armcore.md:69 documents an ARMv4T core, under
  which non-interworking LDM/LDR pc is correct. The finding is valid only if the
  v5T semantics implied by accepting BLX apply. I found no module returning from
  ARM to Thumb through LDM {pc}. Such a return would fault quickly, and the
  documented LGT and KTF boot sweeps do not show it, so I rate it latent.
- **Fixed:** ARM LDR pc and LDM with pc in the list interwork on bit 0 per
  ARMv5T, matching Thumb POP {pc}.

### R086 Limit hook resets the window and drops stand-in overrun

`internal/armcore/core.go:393` · vm · correctness · PLAUSIBLE

- **Defect:** When window >= budget, Run calls the limit hook once and sets
  window = 0, discarding window-budget. Stand-ins legitimately push a quantum's
  steps far past its count (up to about 2^22 iterations × steps), so one hook
  call can stand for many budgets' worth of retired instructions. Hosts that
  pace by renewals then under-count, which is the same overrun the engine fix in
  docs/history/armcore.md "The charge a quantum could not hold" addressed one
  level down. Examples: KTF continueHostService subtracts one MaxSteps per
  renewal, and worker slices park once per exhaustion.
- **Failure:** A worker thread's slice ends inside a stand-in that charges 30×
  the slice. The interpreter would have parked about 30 times or consumed 30
  service windows. The core parks once, and the KTF service allowance is debited
  one window, so stand-in and interpreted runs schedule threads differently and
  ErrServiceStepLimit fires later than intended.
- **Evidence:** core.go:382-394 `if window >= budget { ... thread.limitHook(ctx)
  ...; window = 0 }`; engine.go:85-92,164 stand-ins add `stood` to steps;
  internal/platform/ktf/service_budget.go:134-140 `client.serviceRemaining -=
  window` per renewal.
- **Verification:** The code matches the finding: when `window >= budget` the
  hook is called once and `window = 0` (core.go:382-394). The amount by which a
  stand-in pushed window past the budget (engine.go adds `stood` to steps) is
  dropped, where the interpreter would have carried it into the next windows and
  possibly further renewals. That is inconsistent with the engine-level fix in
  docs/history/armcore.md:1962-1980. The claimed magnitude is not realistic,
  though. Budgets are 2M (KTF workers.go:30), 4M (LGT javaThreadSliceSteps) or
  100M-3B (MaxSteps). A stand-in's charge is bounded by one loop run: about 300k
  steps for a full-screen fill, about 32k for a blended row. A charge of 30
  times the slice would need fills of tens of megabytes. The drift is at most
  one stand-in's charge per renewal, which is a small scheduling difference from
  the oracle.
- **Fixed:** The limit hook is called once for each budget window a stand-in
  used up and the overrun is carried into the next window; ResumeCall carries
  the same overrun for a limit stop, and Validate refuses a limit frame whose
  overrun is larger than one quantum can report.

### R087 New string's char array is unrooted across a collecting allocation

`internal/platform/ktf/aot.go:405` · ktf1 · correctness · PLAUSIBLE

- **Defect:** For java/lang/String, allocateAOTObject first allocates the [C
  content array, and holds its address only as a Go uint32 inside the payload
  byte slice. It then calls runtime.allocate for the String itself. If that
  allocation fails and triggers collectForAllocation, the brand-new array is
  unreachable from every root (guest memory, registers, Go-retained objects) and
  is released. Its jvm.Object can then be collected by Go before the next cycle
  finds it through the string's payload.
- **Failure:** With the arena near full, a guest registers a string literal or
  the runtime allocates a String. allocateGuestCharArray succeeds, and
  runtime.allocate(totalSize) for the String fails, which runs a collection. The
  [C array is unmarked and ReleaseAOTObject drops its strong binding. Other
  objects are freed, so the String allocation succeeds and its payload now names
  the array. Go GC collects the array's jvm.Object. At the next cycle the array
  is marked through the String, but AOTObject fails, so it is counted Lost and
  dropped from tracking. Any later platform call that takes the string's value
  array (e.g. drawChars or arraycopy) fails with 'unbound guest address'.
  publishGuestTextComponent (field_sync.go:699 then the ensureResultBound calls
  at 723/730) has the same pattern.
- **Evidence:** aot.go:402-413 calls allocateGuestCharArray and keeps
  arrayAddress only in `payload`. aot.go:419 `runtime.allocate(totalSize)` →
  initialization.go:2628 collectForAllocation. collect.go:288-293 releases
  unmarked objects. collect.go:263-274 counts Lost and drops tracking when the
  binding is gone.
- **Verification:** aot.go:402-413 allocates the [C array, and its Go Object is
  then held only by the pinned binding: the local in allocateGuestCharArray goes
  out of scope, and the String's Go Object does not reference it. The array's
  address lives only in the Go `payload` slice. runtime.allocate(totalSize) at
  line 419 can call collectForAllocation (initialization.go:2628), where nothing
  marks the array: there is no guest word, no register, and extraRoots is nil.
  ReleaseAOTObject unpins it (collect.go:288-293). If Go then collects that
  Object before a later cycle re-mirrors or re-pins it through AOTObject
  (jvm/aot.go:318-341), the next cycle reaches it through the String and counts
  it Lost (collect.go:263-274). It needs the arena to refuse exactly the String
  allocation after the array fit, then a Go GC inside that window, then a
  platform call that needs the array's Object. The guest's own reads of the
  bytes still work, so the defect is real but narrow and low impact.
- **Fixed:** A new String's char array, and a text component's new character
  array, are held as collection roots (`holdAllocationRoot`) until the object
  naming them is written.

### R088 Native-form AOT call container allocated per call and never freed

`internal/platform/ktf/lifecycle.go:358` · ktf1 · resource-leak · PLAUSIBLE

- **Defect:** runAOTMethod allocates an argument container in the platform arena
  for every call to a method with ACC_NATIVE (0x0100) and never releases it. All
  runtime Java class methods are written with 0x0100. A Host→guest dispatch that
  resolves to a platform-owned body therefore leaks 4–N bytes each time. The
  container is untracked arena memory, so the root scan keeps the receiver and
  every object argument alive for the rest of the session.
- **Failure:** A title whose card or Jlet does not override a callback
  (pointerNotify, pauseApp/resumeApp, Thread.run on a Runnable-wrapping Thread,
  and so on) has it dispatched through invokeAOTFromJVM → runAOTMethod. Each
  dispatch allocates a container through allocateWords that is never released.
  Over a long session the arena grows without bound. Every receiver and argument
  named in those containers stays rooted by scanRootMemory, so objects such as
  Graphics instances passed in are never collected, which hastens arena
  exhaustion.
- **Evidence:** lifecycle.go:353-362 `container, allocationErr :=
  runtime.allocateWords(rawArguments)` has no matching release anywhere (the
  only arena.release callers are collect.go:466 and initialization.go:2170).
  runtime_java.go:3411 writes every runtime method's flags as
  `method.accessFlags|0x0100`. collect.go:398-429 scans all committed RW memory
  outside tracked objects as roots, which includes these containers.
- **Verification:** The leak exists: lifecycle.go:358 allocates a container for
  every ACC_NATIVE body and nothing releases it. Runtime Java classes are
  registered as AOT classes (runtime_java.go:3484) with flags |0x0100
  (runtime_java.go:3411). A Host InvokeVirtual on a title's card that does not
  override, for example, showNotify (runtime_java.go:1757) or pointerNotify
  fails bytecode resolution and goes through invokeAOTFallback (jvm/vm.go:671)
  -> invokeAOTFromJVM -> FindAOTMethod. That walks up to the runtime class's
  native-form method, so it reaches runAOTMethod. However, docs/ktf.md:58-59 and
  docs/history/testing.md:2059 record this as a known decision: 'argument
  containers retain their arena lifetime ... not a new reclamation policy'. The
  cost is 4-16 bytes per dispatch, and only on inherited-callback paths such as
  card show/hide or Jlet pause/resume. The frequent ones, paint and keyNotify,
  are normally overridden by the title's own methods, which are not ACC_NATIVE.
  Unbounded growth is therefore slow, and the rooting effect only matters for
  titles that throw away many cards. Real, documented, low.
- **Fixed:** `runAOTMethod` zeroes and releases the native-form argument
  container after the body and any handler it resumes into have returned.

### R089 Native blit loop bound defeated by negative source/target positions

`internal/platform/ktf/native_graphics.go:255` · ktf2 · untrusted-input · CONFIRMED

- **Defect:** drawImage bounds rows/columns by Max-SourceX and Max-X, which grow
  without limit when the guest passes negative positions, then skips the
  negative part one iteration at a time. The comment claims the loop is bounded
  by where the copy can land; with X and SourceX near -2^31 and a large width it
  is not.
- **Failure:** A blit with X=SourceX=-2^31, Width=2^31-1, Height=50 makes
  columns=2^31-1, so ~1e11 `continue` iterations run on the host with the native
  client lock held and no context check, freezing the session for minutes per
  blit.
- **Evidence:** native_graphics.go:252-255 rows/columns = min(...,
  bounds.Max.X-blit.SourceX, frame.Bounds().Max.X-blit.X); :256-273 nested loops
  `if sourceX < bounds.Min.X || targetX < 0 { continue }`; blit values come from
  guest registers/stack at :189-210.
- **Verification:** native_graphics.go:252-255 bounds columns by min(Width,
  bounds.Max.X-SourceX, frame.Max.X-X). When both X and SourceX are near -2^31,
  both differences exceed 2^31, so columns = Width (2^31-1). The nested loops
  (:256-273) then step through every negative column with `continue`: about 50 x
  2^31 iterations, with no ctx check, while the native client lock is held. The
  comment's claim that the loop is bounded by where the copy can land fails for
  negative positions. The input is contrived: both X and SourceX must be
  extreme, and the one local title's blits are sprite-shaped. The loop still
  terminates.
- **Fixed:** The native drawImage loops start at the first row and column where
  both source and target are in range, so blits at far negative positions no
  longer step through the off-screen span.

### R090 Failed clip load while sounding leaves the title's music flag stuck

`internal/platform/ktf/native_audio.go:116` · ktf2 · correctness · PLAUSIBLE

- **Defect:** setClip closes the playing clip and zeroes platform.clip before
  loading the new one; if Load fails, clip stays 0 while sounding stays true.
  advanceSound returns early whenever clip == 0, so it never clears sounding or
  delivers the end event.
- **Failure:** Title plays clip A (sounding=true), then sets clip B whose SMAF
  the decoder refuses: clip=0, sounding=true forever; the module's own
  'sounding' flag is never cleared by its listener, so a title that waits for
  its music to end waits indefinitely.
- **Evidence:** native_audio.go:111-115 closes old clip and sets platform.clip =
  0; :116-121 Load error returns with clipRefusals++ and sounding unchanged;
  :166 `if !platform.sounding || platform.clip == 0 || ... { return nil }`.
- **Verification:** The code path is as described. setClip closes the current
  clip and sets clip = 0 (native_audio.go:111-115). On a Load error it returns
  with sounding unchanged (:116-121), and advanceSound returns early while clip
  == 0 (:166), so the end event is never delivered. This needs an MMMD-headed
  clip that the decoder rejects, set while another clip is playing. The other
  refusal paths (bad magic, oversize) return before the old clip is closed, so
  they are unaffected. That a title would hang waiting is unshown: with
  platform.audio == nil, playClip never sets sounding and no end event is ever
  delivered, which is the same outcome.
- **Fixed:** `advanceSound` treats "sounding with no clip" as ended, so the end
  event is delivered and the sounding flag no longer sticks after a refused clip
  load.

### R091 destroyObject frees the image object but leaks its bitmap copy

`internal/platform/ktf/native_platform.go:561` · ktf2 · resource-leak · PLAUSIBLE (reviewer said medium)

- **Defect:** createObject allocates two arena blocks per image: the platform's
  copy of the bitmap (up to 4 MiB) and the 8-byte object. destroyObject deletes
  the images entry and frees only the object, so the bitmap copy is unreachable
  and never returned to the 32 MiB native arena.
- **Failure:** An earlier-package title that rebuilds its images per scene and
  destroys the old ones through platform slot 0xbc leaks every bitmap copy;
  after enough scene changes Allocate fails with 'KTF native arena exhausted'
  and the frame/handler returns an error that ends the run. The leaked blocks
  are also carried in every quick-save record.
- **Evidence:** native_image.go:126 kept, err :=
  platform.client.Allocate(length); :141 object := Allocate(8); :150
  images[object] = &nativeImage{data: kept,...}. native_platform.go:553-564
  destroyObject: delete(platform.images, object); platform.client.Free(object) —
  source.data (kept) is never passed to Free.
- **Verification:** The leak is real. createObject allocates the bitmap copy
  `kept` (native_image.go:126) and the 8-byte object (:141), recording
  images[object].data = kept (:150). destroyObject (native_platform.go:553-564)
  deletes the map entry and frees only the object, so `kept` is never released,
  and no other code frees it. Impact is weaker than claimed: the history
  describes the local native title as building fourteen images
  (native_graphics.go:233-236 and docs/history/ktf.md:3836). Nothing shows it
  destroying and rebuilding images per scene, so exhausting the 32 MiB arena
  (native_client.go:95) is not established.
- **Fixed:** destroyObject frees the image's bitmap copy together with the
  8-byte object.

### R092 Native platform logs grow per call for the whole session

`internal/platform/ktf/native_text.go:148` · ktf2 · resource-leak · PLAUSIBLE

- **Defect:** Several NativePlatform slices are appended on every guest call
  with no bound: messages on every drawText, opens on every file open,
  resourceNotes on every unanswered resource request, listeners on every
  registration. listenerFor also returns the first registration ever made, so a
  re-registered listener is never the one called.
- **Failure:** A later module that draws its status text every frame appends a
  string per call forever (megabytes per hour, like the ordered call log that
  was capped for the same reason). Once listeners exceed 65536 entries a quick
  save is refused by validate; a title that re-registers its sound listener with
  a new context keeps receiving end events on the stale first context.
- **Evidence:** native_text.go:148 platform.messages = append(platform.messages,
  text); native_file.go:313 platform.opens = append(...);
  native_platform.go:591-593 note appends resourceNotes (called from
  native_resource.go:190,217); native_events.go:356-360 listeners append,
  :278-285 listenerFor returns first match; native_client.go:595-602 shows the
  call log was bounded for this reason; native_state.go:232-235 record limit.
- **Verification:** Each drawText appends to platform.messages with no bound
  (native_text.go:148). opens (native_file.go:313) and resourceNotes
  (native_platform.go:591-593) grow the same way, unlike the call log capped at
  native_client.go:595-602. How fast this grows depends on how often the title
  draws text, which is unverified. The listener part is speculative. listenerFor
  does return the first match (native_events.go:278-285), but addListener's
  comment (:350-352) records that the module registers before anything is
  delivered, and no re-registration has been observed. The 65536-entry
  checkpoint limit (native_state.go:16, 232-235) is only reachable with such
  re-registration.
- **Fixed:** The native package's message, file-open and resource-note logs keep
  their first 4096 entries. A second listener registration on a source is
  accepted and not recorded, so the first stays the one called, as before and as
  in a checkpoint that holds several (P04 of the PR #234 review).

### R093 Notify fan-out iterates a listener slice the guest mutates in place

`internal/platform/ktf/runtime_event.go:374` · ktf2 · correctness · PLAUSIBLE

- **Defect:** dispatchNotifyEvent ranges over runtime.jletListeners while
  invoking guest notifyEvent; removeJletEventListener deletes with append(s[:i],
  s[i+1:]...), which shifts the shared backing array under the active range.
- **Failure:** Listeners [A, B, C]; A removes itself inside notifyEvent; the
  array becomes [B, C, C] while the range keeps its original length, so B is
  skipped and C is notified twice for one event.
- **Evidence:** runtime_event.go:374-383 for _, listener := range
  runtime.jletListeners { vm.InvokeVirtual(listener, notifyEvent) }; :808-811
  runtime.jletListeners = append(runtime.jletListeners[:index],
  runtime.jletListeners[index+1:]...).
- **Verification:** The mechanics are correct. The range at runtime_event.go:374
  copies the slice header once. removeJletEventListener
  (runtime_event.go:808-811) shifts the shared backing array in place with
  append(s[:i], s[i+1:]...). If A removes itself during notifyEvent, the range
  reads [A, C, C]: B is skipped and C is notified twice. Reaching it needs at
  least two registered Jlet listeners and a listener that removes itself during
  dispatch. No title is shown to do that.
- **Fixed:** `dispatchNotifyEvent` iterates a snapshot of the Jlet listener
  list, so a listener removing itself no longer skips the next one or notifies
  the last twice.

### R094 Key/pointer dispatch indexes a card stack the guest can shrink

`internal/platform/ktf/runtime_event.go:474` · ktf2 · correctness · PLAUSIBLE

- **Defect:** dispatchKeyToCards and dispatchPointerToCards walk
  runtime.displayCards by a precomputed descending index while calling guest
  keyNotify/pointerNotify, which can pop or remove cards. If a handler shrinks
  the stack by two or more and returns true (propagate), the next index is past
  the end and the Go slice access panics.
- **Failure:** Cards [A, B, C]; C.keyNotify calls Display.removeAllCards() then
  pushCard(Title) and returns true; displayCards becomes [Title], the loop
  continues with index 1 and runtime.displayCards[1] panics; the session guard
  recovers it as a fatal session error.
- **Evidence:** runtime_event.go:473-486 for index :=
  len(runtime.displayCards)-1; index >= 0; index-- { card :=
  runtime.displayCards[index]; InvokeVirtual(card, keyNotify) ... }; :494-508
  same for pointers; runtime_java.go:1785-1788 removeAllCards sets displayCards
  = nil, :1695-1701 popCard shrinks it.
- **Verification:** The loop at runtime_event.go:473-486 (and 494-508) reads
  runtime.displayCards[index] fresh on each pass. popCard, removeCard and
  removeAllCards change the slice immediately (runtime_java.go:1695-1701,
  1758-1788). A handler that shrinks the stack by two or more and returns true
  makes the next index out of range. The resulting panic is recovered by
  session.guarded (session/guard.go:50) and ends the session. Reaching it needs
  an unusual guest: per the spec (Card.keyNotify returns true to pass the event
  to the card below), a handler that tears down the stack has to ask to
  propagate to cards it just removed. No title is known to do this.
- **Fixed:** Key and pointer dispatch walk a snapshot of the card stack and skip
  cards no longer on it, so a handler that tears the stack down cannot cause an
  index panic.

### R095 MC_fsGetFileStat reports a removed file as existing

`internal/platform/ktf/wipic_filesystem.go:663` · ktf3 · correctness · CONFIRMED

- **Defect:** wipicFileStatByName falls back to loadSave("db/"+name) and
  guestFile(name) without checking the removal list. MC_fsRemove and MC_fsRename
  leave an empty 'db/&lt;name>' entry behind, so after a remove the stat
  succeeds (size 0, or the packaged size), while MC_fsIsExist and MC_fsOpen(mode 1) answer M_E_NOENT for the same name.
- **Failure:** A title deletes its save with MC_fsRemove("save.dat") to start a
  new game, then checks with MC_fsGetFileStat. It gets 0 (success), so a title
  that branches only on the return code believes a save exists. Its next
  MC_fsOpen("save.dat", 1) answers M_E_NOENT, which sends it down its I/O-error
  path. The same thing happens after renaming the scratch file away.
- **Evidence:** wipic_filesystem.go:661-667: the lookup order is cFiles, then
  loadSave, then guestFile, then directories, with no removedDatabases() check.
  Line 216 writes {"db/"+name: nil}, and backend storeSave writes an empty file
  for nil data, so loadSave finds it. In contrast, databaseSeed (472-481) and
  wipicFileExists (164-171) consult the removal list.
- **Verification:** wipicFileDelete writes {"db/"+name: nil} together with the
  removal list (216). The multi-entry DirectorySaveStore.storeSaves
  (save_batch.go:94) stores nil as an empty file, so loadSave("db/"+name)
  reports it present with zero bytes. wipicFileStatByName (661-667) checks
  cFiles, then loadSave, then guestFile, and never consults removedDatabases(),
  so after MC_fsRemove it returns 0 with size 0. MC_fsIsExist (164-171) goes
  through databaseSeed, which checks the removal list first (472-475), and
  answers M_E_NOENT. Rename leaves the old name in the same state (342).
  docs/history/ktf.md:6815-6818 treats slots 5 and 16 answering one question two
  ways as a defect. Impact is limited: after a remove the size is always 0 (the
  packaged-size branch is not reached, because the empty save file is found
  first), and titles that compare out[2] against a size threshold (comment at
  629-630) still see no save.
- **Fixed:** MC_fsGetFileStat resolves through `databaseSeed`, which checks the
  removal list, then the save, then the packaged copy, so after MC_fsRemove it
  answers missing like MC_fsIsExist.

### R096 Framebuffer bpl check overflows on width\*2

`internal/platform/ktf/wipic_graphics.go:119` · ktf3 · untrusted-input · PLAUSIBLE

- **Defect:** readWIPICSurface checks `framebuffer.bpl < framebuffer.width*2` in
  uint32. For widths of 2^31 and above the product wraps, so a record with a
  huge width and a tiny bpl passes, and the 8 MB size limit (bpl\*height) passes
  too. Later code trusts the width.
- **Failure:** A guest-written framebuffer record has width=0x80000001,
  height=1, bpl=2, bpp=16. MC_grpFillRect(x=0, w=0x7fffffff) clips only to
  width, so fillFramebufferRect allocates int(width)\*2 ≈ 4 GiB (line 332). If
  the record is the screen or a fill target, fillWIPICFramebuffer allocates a
  2-byte row (width\*2 wraps) and PutUint16(row[column\*2:]) panics with index
  out of range (1004-1006). convertScreen allocates width\*height\*4.
- **Evidence:** wipic_graphics.go:109-124: only bpp, a nonzero width/height,
  `bpl < width*2` (uint32) and `bpl*height <= 8MB` are checked.
  maxWIPICFramebufferSide (37) is enforced only at creation (870), not when a
  record is read.
- **Verification:** The overflow is real. `framebuffer.bpl <
  framebuffer.width*2` is uint32, so width=0x80000001 gives width\*2=2 and
  passes with bpl=2. The 8 MB check uses bpl\*height, which also passes.
  fillFramebufferRect (318-334) would then allocate and fill about 4 GiB because
  clipRect clips only to the bogus width. Reaching it is latent, though.
  Framebuffer records are created only by newWIPICFramebufferRecord, which
  bounds the sides at 870-875, so the guest would have to corrupt or forge the
  record in its own memory. The fillWIPICFramebuffer sub-claim cannot be
  reached: its only caller (runtime_java.go:2108-2112) fills a record that
  newWIPICFramebufferRecord created a moment earlier with bounded size, and no
  guest code runs in between. convertScreen reads only the platform's own screen
  record.
- **Fixed:** `readWIPICSurface` compares bpl with width\*2 in uint64, so a
  record with width >= 2^31 and a tiny bpl is refused.

### R097 Guest-sized host buffers allocated before validating the range

`internal/platform/ktf/wipic_graphics.go:478` · ktf3 · untrusted-input · PLAUSIBLE (reviewer said medium)

- **Defect:** Two calls allocate a host buffer whose size comes straight from a
  guest argument, before checking the memory range. wipicReadString
  (MC_grpDrawString) allocates `uint32(length)*unit` bytes for any positive
  length, with no cap; the -1 path is capped at 1024. checkGuestRange
  (user-memory pool create) allocates `size` bytes, up to about 4 GiB.
- **Failure:** MC_grpDrawUnicodeString with length 0x7fffffff, or
  MXUserMemInterf create(pool=0x10, size=0xFFFFFFE0). Go allocates about 4 GiB
  before memory.Read rejects the unmapped range. On iOS, Android or a server
  with limited memory, the allocation fails with a fatal, unrecoverable 'out of
  memory' that kills the process. On 32-bit targets it panics. The guest only
  had to pass one bad length.
- **Evidence:** wipic_graphics.go:467-480: `data = make([]byte,
  uint32(length)*unit)` for any length > 0, while the terminator path at 483 is
  bounded to 1024. wipic_usermem.go:132-138 → 183-187 `data := make([]byte,
  size)` with only an overflow check. initialization.go:1662 also calls
  wipicReadString with a guest length.
- **Verification:** The code is as described. wipic_graphics.go:478 allocates
  uint32(length)\*unit for any positive length, up to 0xFFFFFFFE bytes for the
  unicode variant, before memory.Read. wipic_usermem.go:183-187 allocates `size`
  after only an address-overflow check. The impact is smaller than claimed.
  Every shipped target is 64-bit (Makefile:18 DIST_PLATFORMS; Android and iOS
  arm64), so the 32-bit panic claim does not apply. On overcommitting hosts the
  untouched allocation succeeds virtually, memory.Read then fails, and the call
  returns an ordinary error that ends only that session, which garbage input
  would do anyway. A fatal runtime OOM needs a commit-limited host such as
  Windows or a small-memory Linux or phone. Reaching it also needs a garbage
  length passed to DrawString or the user-memory create.
- **Fixed:** `wipicReadString` and the user-memory pool (`checkGuestRange`)
  validate the guest range before allocating a buffer of the guest-given size.

### R098 Checkpoint class ElementBytes is not validated

`internal/platform/lgt/checkpoint_java_state.go:902` · lgt1 · untrusted-input · CONFIRMED

- **Defect:** javaState.validate bounds Slots and Instance for each class record
  but not ElementBytes, which restoreJavaState copies verbatim. javaArrayCopy
  multiplies it by an element count when sizing a host buffer.
- **Failure:** A slot sets ElementBytes = 0x80000000 on an array class. After
  load, System.arraycopy between two arrays of that class (lengths come from
  guest memory, up to maxJavaArrayLength) computes make([]byte,
  count\*0x80000000), about 2^53 bytes. That panics with 'len out of range' or
  exhausts memory instead of returning a validation error at load.
- **Evidence:** checkpoint_java_state.go:897-905 checks only class.Slots and
  class.Instance. checkpoint_java_state.go:1182 restores ElementBytes as-is.
  java_array.go:316 `data := make([]byte, uint64(count)*uint64(sourceBytes))`,
  where sourceBytes comes from javaArrayElementBytes (java_array.go:328-337).
  Valid widths are only 1, 2, 4 and 8 (java_array.go:57-77).
- **Verification:** javaState.validate checks only class.Slots and
  class.Instance (checkpoint_java_state.go:901-904). restoreJavaState copies
  ElementBytes verbatim (1182). javaArrayElementBytes rejects only zero
  (java_array.go:333). javaArrayCopy then calls make([]byte,
  uint64(count)\*uint64(sourceBytes)) (316). count is bounded by javaArrayBlock
  to maxJavaArrayLength = 1&lt;&lt;22 (java_array.go:348). With ElementBytes
  0x80000000, count 1 asks for 2 GiB and larger counts ask for up to 2^53 bytes,
  which panics with len out of range. A live guest can only reach 32 MiB this
  way, so the gap adds reach. Valid widths are 1, 2, 4 and 8 (57-77). This is
  reachable only from a crafted checkpoint, so severity is low.
- **Fixed:** `javaState.validate` refuses a class ElementBytes other than 0, 1,
  2, 4 or 8, the only widths live code sets, so records from earlier builds
  still load.

### R099 Arena free-list insert and allocate are linear, so sweeps are quadratic

`internal/platform/lgt/arena.go:100` · lgt1 · resource-leak · PLAUSIBLE

- **Defect:** arena.insert linearly searches the free list, shifts it, and then
  rebuilds and coalesces the entire list on every release. allocate is a
  first-fit linear scan. A sweep that frees D objects (2D blocks) over a
  fragmented free list of F spans costs O(D·F) Go time outside the guest's
  instruction budget, and every later allocation costs O(F).
- **Failure:** A Java title interleaves long-lived and short-lived allocations
  (strings and arrays) until the 16 MB data arena holds tens of thousands of
  separated free spans. One collection that frees 50k objects (100k releases)
  against a free list of about 50k spans does about 1e10 slice operations, so a
  single tick stalls for seconds. The collector also runs inside allocation
  failures (collectForAllocation), so the stall lands mid-call.
- **Evidence:** arena.go:58-71 scans a.free from index 0 on every allocate.
  arena.go:100-118: the insert loop `for index < len(a.free) &&
  a.free[index].start < block.start`, then copy, then a full `for _, current :=
  range a.free` merge pass per release. collect.go:534-543 releases every dead
  object and block one at a time.
- **Verification:** The mechanics are as described. allocate is a first-fit
  linear scan (arena.go:58-71). insert does a linear search, then a copy, then a
  full merge pass on every release (100-118). sweepJavaObjects releases two
  blocks per dead object one at a time (collect.go:534-543), so a sweep costs
  O(D·F). The claimed stall needs tens of thousands of separate free spans in
  the data arena. Local titles show small heaps (collect.go:144-147 cites 2168
  tracked objects), and no realistic title has been shown to fragment this
  badly. This is a latent performance issue, not a demonstrated stall.
- **Partly fixed:** The sweep zeroes dead objects, then returns all their blocks
  through one sorted merge (`arena.releaseAll`), provably the same free list as
  releasing one at a time; allocate stays a first-fit scan per call.

### R100 Restored file cursor bound (MaxInt32) lets the next write allocate 2 GiB

`internal/platform/lgt/checkpoint_state.go:810` · lgt1 · untrusted-input · PLAUSIBLE

- **Defect:** A checkpoint record's open-file Cursor is only checked against
  math.MaxInt32, and rebuildRestoredStorage deliberately keeps the cursor past
  the end of the file. The first write through that handle zero-fills from
  len(data) to the cursor, so the record decides the size of a host allocation
  that its bytes do not pay for. This contradicts the stated maxStateBytes
  policy.
- **Failure:** A slot whose Files entry has Cursor 0x7fffffff over a small save
  passes validate and restores. On the title's next File.write(int)
  (java_file.go:197-198), sink drain (java_file.go:358-360) or C MC_fsWrite,
  `append(file.data, make([]byte, cursor+1-len(data))...)` allocates about 2
  GiB. The server process risks OOM, and otherwise writes a 2 GB save.
- **Evidence:** checkpoint_state.go:810 `file.Cursor < 0 || file.Cursor >
  math.MaxInt32`, while Length is held to maxStateBytes on the same line.
  checkpoint_storage.go:42-43 documents that the cursor stays past the end.
  checkpoint_storage.go:371-384 does not clamp file.cursor. java_file.go:197-198
  and 358-360 grow data up to the cursor.
- **Verification:** The record's Cursor is bounded only by MaxInt32
  (checkpoint_state.go:810). Restore keeps it by design
  (checkpoint_storage.go:42-43). The next java File.write(int)
  (java_file.go:197-198) or sink drain (358-360) zero-fills up to the cursor. So
  a tampered record can force an allocation of about 2 GiB. Impact is smaller
  than claimed. A live Clet can already reach the same cursor with MC_fsSeek:
  seekFile (wipic_file.go:554-576) only rejects negative positions, and
  transferFile (544-547) then grows the data the same way. The MaxInt32 bound
  matches what a live guest can reach. Only a Java-only title gains a new
  capability, and only through a tampered checkpoint. maxStateBytes bounds bytes
  held in the record, not cursors.
- **Fixed:** `File.write(int)` and the stream drain go through the same bounded
  grow as the C write, so a restored cursor past 16 MiB fails instead of
  allocating up to 2 GiB; checkpoint validation is unchanged so older slots
  still restore.

### R101 Java sinks, vectors and clip data grow without any host-side cap

`internal/platform/lgt/java_stream.go:736` · lgt2 · resource-leak · CONFIRMED

- **Defect:** ByteArrayOutputStream/DataOutputStream sinks, Vector contents and
  Java Clip.putData data are host Go slices that grow on every guest call with
  no limit. The C media path caps a clip at maxClipBytes, and Java arrays are
  capped at maxJavaArrayLength.
- **Failure:** A Java title's runaway loop calls `v.addElement(x)` or
  `out.write(buf)` on a worker thread. Each 4M-step slice adds tens of thousands
  of entries and slices continue every tick, so host memory grows until the
  process OOMs and takes other sessions down. On a handset this would be an
  OutOfMemoryError in the guest.
- **Evidence:** java_stream.go:736, :756, :772, :860-862 (append to
  runtime.sinks); java_vector.go:48 append to runtime.vectors; java_sound.go:127
  `clip.data = append(clip.data, data[offset:offset+length]...)` with no
  maxClipBytes check (compare wipic_media.go:459-485).
- **Verification:** Sink writes (java_stream.go:736, :756, :772),
  Vector.addElement (java_vector.go:48) and Java Clip.putData
  (java_sound.go:127) all append to host slices with no cap. The C putData path
  enforces maxClipBytes (wipic_media.go:459-485), and Java arrays are capped at
  maxJavaArrayLength (java_array.go:51). The process sets no memory limit. A
  runaway guest loop grows host memory until the process dies. That needs an
  already-broken title, so it is low.
- **Fixed:** Byte-sink writes and Vector add/insert return an error past
  `maxJavaArrayLength` (4 Mi bytes or references, the largest array the platform
  builds), and Java putData takes at most a 4 MiB clip and answers the count
  taken, as the C slot does.

### R102 StringBuffer append is O(n) per call, quadratic for append loops

`internal/platform/lgt/java_string.go:463` · lgt2 · correctness · CONFIRMED

- **Defect:** Each StringBuffer.append re-encodes the whole buffer. append(char)
  converts the full text to UTF-16, appends one unit, converts back, and
  setJavaText then re-validates the whole string. The common idiom of building
  text one character at a time is therefore quadratic in time and allocation.
- **Failure:** A Java title reads a 50 KB text resource with `while ((c =
  in.read()) != -1) sb.append((char)c);`. That is about 50,000 appends of O(n)
  work each, roughly 10^9 unit operations and gigabytes of garbage, so loading
  stalls for seconds and the frame hitches.
- **Evidence:** java_string.go:455-465 `javaTextOfUnits(append(utf16Units(held),
  uint16(arguments[1])))`; :363 `held+javaTextValue(...)`; :43-50 setJavaText
  runs utf8.ValidString over the whole text on every mutation.
- **Verification:** append(char) re-encodes the whole buffer each time:
  `javaTextOfUnits(append(utf16Units(held), ...))` (java_string.go:463). Text
  appends build `held+...` (:363), and setJavaText runs utf8.ValidString over
  the full text on every mutation (:43-50). Building text one char at a time is
  therefore quadratic in time and garbage. It is a performance issue only.
  Feature-phone text resources are usually a few KB, where the cost is
  negligible, so it is low.
- **Not fixed:** String and StringBuffer text is an immutable Go string shared
  by every text accessor and the checkpoint strings table, so amortized O(1)
  appends need a separate mutable representation touching every accessor and the
  checkpoint format; the cost is negligible at the few-KB text sizes titles use.

### R103 Timer cancelled or re-armed by an earlier callback still fires

`internal/platform/lgt/wipic.go:1359` · lgt2 · correctness · CONFIRMED

- **Defect:** serviceTimers snapshots all due timers, disarms them, and then
  calls each one unconditionally. A callback that runs MC_knlUnsetTimer or
  MC_knlSetTimer on another timer in the same batch does not stop that timer's
  callback.
- **Failure:** Timers A and B come due in the same tick. A's callback calls
  MC_knlUnsetTimer(B), but B's callback still runs. If A instead re-arms B with
  MC_knlSetTimer(B, 1000, p), B runs immediately with the new param and stays
  armed (setTimer set armed=true), so it fires a second time 1000 ms later. This
  breaks the documented cancel semantics and double-fires the re-armed timer.
- **Evidence:** wipic.go:1334-1353 collect `due` and set armed=false; :1358-1362
  loop calls entry.callback/entry.param without re-checking entry.armed or a
  generation; :477-479 unsetTimer only clears armed; :1295-1297 setTimer sets
  param/dueAt/armed=true.
- **Verification:** serviceTimers collects the due timers and sets armed=false
  (wipic.go:1334-1353). It then releases mu and calls every entry (:1358-1362)
  without re-checking entry.armed or a generation. MC_knlUnsetTimer only clears
  armed (:477-479), so B's callback still runs after A cancels it. setTimer on B
  sets param, dueAt and armed=true (:1295-1297), so B fires now with the new
  param, stays armed and fires again at the new deadline. This needs two timers
  due in one tick where one cancels or re-arms the other, so it is low.
- **Fixed:** `serviceTimers` disarms each due timer when its turn comes and
  skips it if an earlier callback in the same batch cancelled, redefined or
  re-armed it; the firing order is unchanged.

### R104 Platform file lists share the guest file namespace (fs/.removed)

`internal/platform/lgt/wipic_file.go:32` · lgt2 · save-integrity · CONFIRMED

- **Defect:** The removal and creation indexes are stored under the save keys
  fs/.removed and fs/.created. Those are exactly the keys a guest file named
  '.removed' or '.created' maps to, so guest data and platform metadata
  overwrite each other.
- **Failure:** A title writes a file named '/.created'. storeFile writes the
  game's bytes to fs/.created, and the next markFileCreated rewrites that key
  with the platform's name list, corrupting the game's file. In the next session
  createdFiles() parses the game's binary data as path names, which the
  directory listing then reports. The same applies to '.removed', where a bad
  parse hides arbitrary files as deleted.
- **Evidence:** wipic_file.go:32 `fileRemovedKey = fileSaveScope + ".removed"`,
  :40 fileCreatedKey; :819-821 fileSaveKey(name) = NormalizeSaveKey("fs/"+name)
  with no reserved-name check; :704-716 storeFileList writes these keys;
  :670-679 and :787-795 parse them as newline lists.
- **Verification:** fileRemovedKey and fileCreatedKey are 'fs/.removed' and
  'fs/.created' (wipic_file.go:32, :40). fileSaveKey('/.created') is
  NormalizeSaveKey('fs/.created') (:819-821, backend/save.go:63-84) with no
  reserved-name check, so guest data and the platform lists share a key.
  storeFileList overwrites it (:704-716), and createdFiles/removedFiles parse
  whatever is there as path lists (:670-679, :787-795). A guest file at the root
  named '.created' or '.removed' is very unlikely, so this is latent.
- **Fixed:** `fileSaveKey` refuses the bare scope key `fs` and any guest path
  whose first segment is `.removed` or `.created` (any case), so guest files
  never reach the platform lists; the on-disk layout is unchanged.

### R105 MC_mdaClipFree never returns the clip's guest record to the arena

`internal/platform/lgt/wipic_media.go:185` · lgt2 · resource-leak · CONFIRMED

- **Defect:** createClip allocates a 32-byte record in the 16 MiB platform data
  arena for every clip. MC_mdaClipFree deletes the map entry and the sound but
  never calls arena.release, so a title that creates and frees a clip per sound
  effect slowly fills the arena.
- **Failure:** A C title runs create, putData, play, free for every sound
  effect. After a few hundred thousand effects, a long session, allocate() fails
  with 'LGT platform data space exhausted' on an unrelated platform allocation
  and the session ends.
- **Evidence:** wipic_media.go:421 `client.allocateBytes(make([]byte,
  mediaClipRecordSize))`; :175-186 slotClipFree only releaseClipSound +
  delete(client.clips, handle); no arena.release anywhere for C clips
  (collect.go only frees Java objects).
- **Verification:** createClip takes a 32-byte block from the platform arena
  (wipic_media.go:421, client.allocate -> arena.allocate). slotClipFree only
  calls releaseClipSound and delete(client.clips, handle) (:175-186).
  arena.release is called only for Java objects (collect.go:569/574) and
  surfaces (graphics.go:315). collectForAllocation frees only Java objects
  (collect.go:810-831), so C clip records never come back. At 32 bytes a block,
  exhausting the arena takes hundreds of thousands of create/free cycles, so it
  is a latent leak.
- **Fixed:** MC_mdaClipFree returns a C clip's 32-byte record to the platform
  arena; Java clips are left to the collector.

### R106 Guest thread stacks are never reclaimed; address space runs out

`internal/platform/lgt/java_thread.go:340` · lgt2 · resource-leak · PLAUSIBLE

- **Defect:** Every Thread.start maps a fresh 1 MiB stack at javaThreadStackBase + threadStacks\*1MiB and increments threadStacks forever. Finished workers'
  stacks are never unmapped or reused. The 16-thread cap counts thread objects,
  not running threads, and javaThreadStart for an unseen Thread subclass
  bypasses that cap entirely.
- **Failure:** A Java title starts a short-lived Thread per scene load or per
  sound. After about 3000 starts the next base passes 0xFFFFFFFF, so
  Memory().Map fails with an address overflow and the session dies. Pages each
  finished thread touched also stay allocated. Separately, constructing a 17th
  Thread object before the collector frees finished ones ends the run with
  'started more than 16 threads'.
- **Evidence:** java_thread.go:338-357 (base from runtime.threadStacks,
  `runtime.threadStacks++`, no unmap); :119-121 limit on len(runtime.threads)
  only in the constructor; :139-145 javaThreadStart inserts unknown threads with
  no limit; armcore/memory.go:347-350 Map rejects end > 1&lt;&lt;32.
- **Verification:** startJavaWorker maps a stack at javaThreadStackBase +
  threadStacks\*1MiB and increments threadStacks forever
  (java_thread.go:338-344). Nothing unmaps or reuses it. The collector deletes
  runtime.threads entries for finished, unreachable threads (collect.go:424-431,
  :605), so the 16-object constructor cap (:119-121) lets more threads start,
  and javaThreadStart for an unseen subclass skips the cap (:139-145). The
  address space ends after about 3000 starts. Before that, the 1 MiB maps reach
  the unmapped return sentinel 0x7fff0000 (client.go:70) after about 1000. A
  nearer effect: checkpoint restore rejects ThreadStacks > maxJavaThreads
  (checkpoint_state.go:931, checkpoint_java_state.go:892), so the 17th thread
  start in a session already makes quick saves unloadable. Titles here are
  documented as starting one thread, so reachability is unconfirmed.
- **Fixed:** `startJavaWorker` reuses the lowest stack slot no held worker runs
  on, clearing only its committed pages, and the 16-thread constructor bound
  counts only threads that have not finished.

### R107 strcpy/strcat/strcmp/strchr/strstr/atoi/%s die on strings over 4095 bytes

`internal/platform/lgt/stdlib.go:663` · lgt2 · correctness · PLAUSIBLE (reviewer said medium)

- **Defect:** These C library slots and the printf %s path read their operands
  with readCString, whose 4096-byte bound is meant for names. A valid
  NUL-terminated string longer than 4095 bytes returns an error, which ends the
  session. strlen and strtok already handle this with the 1 MiB bound, because
  titles do pass multi-kilobyte text buffers.
- **Failure:** A title loads a text resource of several kilobytes, as strtok's
  comment says one does, and copies or searches it with
  strcpy/strcat/strchr/strstr, or formats it with sprintf("%s"). readCString
  returns 'not terminated within 4096 bytes', handleStdlibSVC returns that
  error, and the run stops on valid C input.
- **Evidence:** client.go:1038-1055 readCString limit 4096; stdlib.go:166
  (strchr/strrchr), :198/202 (strspn/strcspn/strpbrk), :250/254 (strcmp),
  :269/273 (strstr), :288 (atoi), :663 and :671 (strcpy/strcat source and
  destination); wipic_format.go:79 (%s argument); contrast stdlib.go:148 strlen
  -> cStringLength (maxCStringLength 1&lt;&lt;20) and :590 strtok.
- **Verification:** readCString has a fixed 4096-byte bound
  (client.go:1038-1055). strchr/strrchr (stdlib.go:166), strspn/strcspn/strpbrk
  (:198/:202), strcmp (:250/:254), strstr (:269/:273), atoi (:288),
  strcpy/strcat (:663, :671) and %s without precision (wipic_format.go:79) all
  use it, and the error ends the run. The defect is real. Unlike C, strchr and
  atoi also read the whole string rather than stopping early. Only strlen has
  been observed failing this way (docs/history/lgt.md 'A strlen is not a name'),
  and after that fix every local archive booted. No title is shown reaching
  these other slots with a string over 4 KiB, so reachability is unconfirmed.
- **Fixed:** The C string functions (strchr through strcat, atoi) and printf
  `%s` read operands through `readCStringBuffer` with strlen's 1 MiB bound
  instead of the 4096-byte name bound.

### R108 readString allocates up to 2 GiB from a guest draw length

`internal/platform/lgt/wipic_draw.go:247` · lgt2 · untrusted-input · PLAUSIBLE (reviewer said medium)

- **Defect:** For a positive length, readString does `make([]byte, length)` with
  the guest's int32 before reading guest memory. MC_grpDrawString's len and
  MC_grpGetStringWidth's len reach it without any cap.
- **Failure:** A title passes an uninitialised length such as 0x7ffffff0 to
  MC_grpDrawString or MC_grpGetStringWidth. The host allocates about 2 GiB
  before Memory().Read fails. On mobile hosts, or under memory pressure, this
  aborts the process with a fatal out-of-memory error rather than a session
  error. Repeated calls churn the GC.
- **Evidence:** wipic_draw.go:241-251 (only `length < 0` and `== 0` are
  special-cased, then `text := make([]byte, length)`); callers wipic_draw.go:81
  `client.readString(uint32(values[3]), values[4])` and wipic.go:716
  `client.readString(pointer, int32(length))`.
- **Verification:** The unbounded `make([]byte, length)` (wipic_draw.go:247)
  from the guest int32 is real, reached from wipic_draw.go:81 and wipic.go:716.
  The impact is smaller than claimed. Memory().Read validates the whole range
  before copying (armcore/memory.go:452-455), so the 2 GiB buffer is never
  touched and stays a transient virtual reservation. A length that large cannot
  be backed by one guest mapping, so Read fails and readString returns the
  error, and the session would end on that bad pointer/length anyway. All dist
  targets are 64-bit (Makefile:18). A fatal OOM is possible only where the OS
  refuses the virtual reservation, such as strict overcommit or a small-RAM
  host. I could not confirm that on the mobile hosts.
- **Fixed:** `readString` refuses a counted length over `maxCStringLength` (1
  MiB) before allocating.

### R109 Second open or stat of a file with unflushed writes reads stale data

`internal/platform/lgt/wipic_file.go:580` · lgt2 · correctness · PLAUSIBLE

- **Defect:** Writes stay in the open handle until close, flush-at-exit or a
  quick save, but readFile, which backs MC_fsOpen, MC_fsFileAttribute,
  MC_fsIsExist, resource reads and rename, looks only at the store and the
  archive. Another view of the same path sees the old bytes and size, whereas a
  handset's write goes through to the filesystem.
- **Failure:** A title opens save.dat RDWR at startup and keeps it open, writes
  new progress, then opens save.dat RDONLY (or stats it) to reload or verify. It
  reads the pre-write store contents and length. A rename of the path while its
  handle is dirty also copies the stale store bytes, and the later close writes
  the old name back.
- **Evidence:** wipic_file.go:580-590 readFile consults removedFiles, loadSave
  and archive.Resource only; :531-551 writes only touch file.data; :285-297
  rename uses readFile; openFile :415 reads through readFile.
- **Verification:** readFile looks only at removedFiles, the store and the
  archive (wipic_file.go:580-590). Writes touch only file.data (:544-550).
  MC_fsOpen (:415), MC_fsFileAttribute (:226), MC_fsIsExist (:208) and rename
  (:285-297) therefore see the pre-write store bytes while another handle on the
  path is dirty. A rename during that window copies stale bytes, and the later
  close re-creates the old name through writeFile/markFileRemoved(false). The
  divergence from write-through handset behavior is real. No title is shown
  re-opening or stat-ing a path with an unflushed handle open.
- **Fixed:** Every handle on a path shares one buffer, so a second open, a stat,
  an existence check, a resource read and a rename see a waiting write at once,
  and the last close can no longer put an older copy over another handle's
  write.

### R110 Delete list keyed differently from save key; other spelling sees file

`internal/platform/lgt/wipic_file.go:581` · lgt2 · correctness · PLAUSIBLE

- **Defect:** The removal list (and the created list) uses canonicalFileName,
  which only lowercases and strips one leading '/'. The store key comes from
  NormalizeSaveKey, which drops '.' and empty components. A delete under one
  spelling therefore does not hide the same save under another spelling,
  although fileEpochKey's comment shows titles use several spellings for one
  file.
- **Failure:** A title deletes './save.dat' (or '//save.dat') to start a new
  game and then calls MC_fsIsExist('save.dat'). readFile checks
  removed['save.dat'], which is not set because the entry is './save.dat', loads
  fs/save.dat from the store and answers that the file exists. The title skips
  its new-game path, which is the exact failure fileRemovedKey's comment
  describes.
- **Evidence:** wipic_file.go:773-775 canonicalFileName vs :819-821 fileSaveKey
  -> backend.NormalizeSaveKey (save.go:63-84 drops '.' and empty parts); :581
  readFile uses canonicalFileName; :483-489 fileEpochKey already normalizes via
  fileSaveKey for this reason.
- **Verification:** The removal and created sets are keyed by canonicalFileName,
  which only lowercases and strips one leading '/' (wipic_file.go:773-775). The
  store key is NormalizeSaveKey, which drops '.' and empty components
  (backend/save.go:63-84). MC_fsRemove('./save.dat') marks './save.dat'
  (:255-261, removeFile). MC_fsIsExist('save.dat') then misses it in
  removedFiles (:581), loads fs/save.dat and answers success (:208).
  fileEpochKey (:483-489) already normalizes for exactly this reason. No title
  is shown mixing spellings between delete and exist.
- **Fixed:** The removed and created lists are keyed by the normalized save path
  with its case kept (names that differ only by case are different files on
  every host); entries an earlier build wrote case-folded keep their old meaning
  above a marker line and are cleared by a write under any case.

### R111 65th queued clip transition ends the session instead of being dropped

`internal/platform/lgt/wipic_media.go:613` · lgt2 · error-handling · PLAUSIBLE

- **Defect:** mediaClip.notify returns an error once a clip with a callback has
  64 undelivered transitions. That error propagates out of MC_mdaStop/MC_mdaPlay
  and ends the run, although the bound exists only to cap memory.
- **Failure:** A C title with a clip callback restarts a short effect by calling
  stop then play repeatedly within one tick, for example during a long loading
  callback that runs many iterations in one call. After 32 stop/play pairs the
  65th transition returns 'LGT media callback queue exceeds 64 transitions' and
  the session dies.
- **Evidence:** wipic_media.go:608-620 (error at len(pending) >= 64); :257 and
  :540 propagate notify errors; pending is cleared only in serviceMediaCallbacks
  (:652) once per tick or in releaseClipSound.
- **Verification:** notify returns an error at 64 pending transitions
  (wipic_media.go:612-614). MC_mdaStop (:257) and play (:540) return that error
  as a session failure. pending is cleared once per tick in
  serviceMediaCallbacks (:648-651). The behavior is as described: a memory cap
  ends the run instead of dropping the oldest transition. Reaching it takes more
  than 32 stop/play alternations on one clip with a callback inside a single
  tick. A repeated play with no stop does not append, because the status is
  unchanged (:609-611). No title is shown doing that.
- **Fixed:** `mediaClip.notify` drops the oldest of its 64 queued transitions
  instead of returning a session-ending error; 64 is still the checkpoint's
  limit.

### R112 GameCanvas drawBuffer writes the destination unlocked and ignores translation

`internal/platform/skt/lcdui_game.go:172` · skt1 · concurrency · CONFIRMED

- **Defect:** gameCanvasDrawBuffer calls context.drawRGBA directly. Every other
  drawing native goes through withDestinationWrite, so this one writes the
  screen frameRGBA (or an Image) without renderMu or image.mu. It also always
  blits at (0,0), ignoring the Graphics translation, although MIDP says
  GameCanvas.paint rendering is subject to the clip and origin translation.
- **Failure:** The Host repaints a GameCanvas (setCurrent → queueCanvasPaint →
  paintPendingCanvas → paint → drawBuffer) while the title's own thread calls
  flushGraphics. presentGameBuffer writes and copies frameRGBA under renderMu
  while drawBuffer writes the same bytes with no lock: a data race and torn
  frames. A title that translates g before calling super.paint(g) also gets the
  buffer at the wrong position.
- **Evidence:** lcdui_game.go:163-174 has no withDestinationWrite and passes 0,0
  rather than translateX/Y. render.go:782-804 shows the locking contract every
  other drawing native follows (render_image.go:32,84,123; render.go:595).
  lcdui_game.go:136-152 presentGameBuffer writes frameRGBA under renderMu.
- **Verification:** gameCanvasDrawBuffer calls context.drawRGBA(…, 0, 0)
  directly (lcdui_game.go:172). It skips withDestinationWrite, the lock every
  other drawing native takes (render_image.go:32,84,123; render.go:794-804).
  paintPendingCanvas deliberately runs paint without renderMu
  (render.go:327-332) and hands it the screen context, whose lock is
  &runtime.renderMu (render.go:382). drawBuffer therefore writes frameRGBA
  unlocked, while presentGameBuffer on a guest thread writes the same bytes
  under renderMu (lcdui_game.go:136-152). blendPixel applies the clip but no
  translateX/translateY (render_image.go:176-181), whereas drawGraphicsRGB adds
  them (render_image.go:118-119), so the Graphics translation is ignored. The
  impact is a transient torn frame or misplaced blit on a GameCanvas title, so
  severity is low.
- **Fixed:** GameCanvas drawBuffer blits at the Graphics translation, inside the
  clip and under `withDestinationWrite`.

### R113 Keypad TextBox editing ignores NUMERIC/UNEDITABLE constraints

`internal/platform/skt/lcdui_input.go:99` · skt1 · correctness · CONFIRMED

- **Defect:** handleTextKey feeds digits to a textinput.State created with no
  mode or constraint, starting in lowercase-letter mode. It never consults
  content.constraint, so a NUMERIC or PHONENUMBER TextBox receives letters and
  an UNEDITABLE TextBox is editable. The Host IME path (validHostText, the
  textFieldUneditable checks) enforces both.
- **Failure:** A title shows TextBox("Count", "", 3, TextField.NUMERIC). The
  player, or a scripted CLI run, presses '2' on the keypad and the field holds
  "a". The title's Integer.parseInt(getString()) throws NumberFormatException in
  commandAction. An UNEDITABLE TextBox that a title uses as a read-only text
  view can likewise be modified from the keypad.
- **Evidence:** lcdui_input.go:77-107 calls editor.Key(rune(keyCode), ...) with
  no constraint check. lcdui_input.go:112-122: textEditor → textinput.NewUTF16
  with default ModeLowercase (textinput.go:24,75). Compare
  host_text_input.go:84,434,545-571, where the IME path checks the uneditable
  flag and the constraint.
- **Verification:** handleTextKey never reads content.constraint
  (lcdui_input.go:77-107). textEditor creates the editor with
  textinput.NewUTF16, which starts in ModeLowercase (textinput.go:24,75-79), and
  nothing calls a mode or constraint setter. A '2' in a NUMERIC TextBox
  therefore inserts 'a', and an UNEDITABLE TextBox accepts keypad edits.
  docs/lcdui.md:124-135 ('Deliberately incomplete') documents only Latin/digits
  and no Hangul; it does not document ignoring constraints. The Host IME path
  does enforce constraints (host_text_input.go). LCDUI TextBoxes are uncommon in
  SKT titles, so severity is low.
- **Fixed:** Keypad typing ignores UNEDITABLE TextBoxes and puts NUMERIC,
  PHONENUMBER and DECIMAL boxes into the editor's digit mode.

### R114 Form ChoiceGroup sub-selection can become -1 and later index elements[-1]

`internal/platform/skt/lcdui_input.go:254` · skt1 · correctness · CONFIRMED

- **Defect:** Pressing RIGHT on a focused ChoiceGroup with no elements sets
  subSelection = min(sub+1, len-1) = -1. If the title then appends elements,
  FIRE computes index = min(-1, len-1) = -1 and indexes
  data.choice.elements[-1].
- **Failure:** A Form focuses an empty ChoiceGroup (or one the title emptied
  with deleteAll). The player presses RIGHT, which sets subSelection to -1. The
  title appends options from its own thread or a callback. The player presses
  FIRE: elements[-1] panics and the session ends. A quicksave taken in that
  state is also rejected at restore, because checkpoint_heap_restore.go:297
  refuses subSelection -1 when the choice is non-empty.
- **Evidence:** lcdui_input.go:249-251 can produce subSelection -1.
  lcdui_input.go:253-257 takes min(content.subSelection, len-1) with no lower
  clamp, then data.choice.elements[index]. Only UP/DOWN/LEFT reset it to >= 0.
- **Verification:** With the focus on an empty ChoiceGroup (itemIsInteractive is
  true for itemChoice, lcdui_render.go:235-240), RIGHT sets subSelection =
  min(sub+1, len-1) = -1 (lcdui_input.go:250). Only UP/DOWN/LEFT reset it, and
  nothing resets it on append (refreshItemOwner only queues a paint,
  lcdui.go:726-731). After the title appends elements, for example from a
  soft-key commandAction or its own thread, FIRE passes the len>0 guard (line
  253), computes index = min(-1, len-1) = -1 (line 254) and indexes elements[-1]
  at line 256 or 258, which panics. Restore accepts -1 only while the choice is
  empty (checkpoint_heap_restore.go:297), so a quicksave taken after the append
  is rejected. This needs a specific sequence of inputs, so it is an edge case.
- **Fixed:** RIGHT on an empty ChoiceGroup keeps its element focus at 0, FIRE
  clamps it, and a restored -1 becomes 0 once the group gains an element.

### R115 Case-insensitive property lookup picks a random key, so save owner can flip

`internal/platform/skt/descriptor.go:81` · skt1 · save-integrity · PLAUSIBLE

- **Defect:** Descriptor.Property ranges over a Go map with strings.EqualFold
  and returns the first match. It does not prefer the exact key, and map
  iteration order is random. When a descriptor carries two keys that differ only
  in case, the result changes from run to run. SaveOwner uses
  Property("DD-ProgName") (and jarSaveOwner falls back to descriptor properties)
  to choose the RMS save directory.
- **Failure:** A repacked .msd or manifest contains both "DD-ProgName: 1234" and
  "DD-PROGNAME: 5678". On some launches the saves go to owner 1234 and on others
  to 5678, so the player's progress seems to disappear and reappear, and new
  saves are split across two directories. MIDlet-1 has the same problem, making
  the main class nondeterministic.
- **Evidence:** descriptor.go:31 stores keys verbatim, so case variants stay
  distinct. descriptor.go:80-86: for key, value := range descriptor.Properties {
  if strings.EqualFold(key, name) { return value, true } }. skt.go:253-257:
  SaveOwner → Property("DD-ProgName").
- **Verification:** Descriptor.Property ranges over a Go map and returns the
  first EqualFold match, with no exact-key preference (descriptor.go:80-86).
  Keys are stored verbatim (descriptor.go:31), so two case variants of
  DD-ProgName make SaveOwner (skt.go:253-257) nondeterministic across launches;
  the same applies to MIDlet-1 and MIDlet-Name. The trace holds, but it needs a
  descriptor that carries case-variant duplicate keys, and no real archive is
  known to do that. The issue is latent.
- **Fixed:** `Descriptor.Property` prefers the exact key, then the first case
  variant in byte order, so the save owner and main class no longer change
  between launches.

### R116 wrapText/trimToWidth are cubic/quadratic in guest text length

`internal/platform/skt/lcdui.go:2055` · skt1 · untrusted-input · PLAUSIBLE

- **Defect:** wrapText splits an over-long word by decrementing cut from
  len(remaining) and re-measuring the whole prefix each step: O(n^2) per line
  and O(n^3/width) per word. trimToWidth re-measures every prefix plus the
  ellipsis: O(n^2). Each Hangul measurement goes through a mutex-guarded glyph
  cache. Both run on every Screen paint and in Item.getPreferredWidth/Height.
  drawTextBody also builds a password mask with repeated string +=.
- **Failure:** A title puts a few thousand characters with no spaces into a
  StringItem, Alert, TextBox or title (for example a long line of Korean, digits
  or '='). For n=2000 at about 15 characters per line, wrapText makes about 9e7
  charAdvance calls per paint, so each key press or repaint stalls for seconds
  on the Host thread. A 50k-character title makes trimToWidth run about 1e9
  measurements.
- **Evidence:** lcdui.go:2053-2060 does for cut > 1 &&
  font.textWidth(remaining[:cut]) > width { cut-- }. lcdui.go:2013-2016 calls
  textWidth on every shortened prefix. lcdui_render.go:122,190,257,358,375 call
  these on each paint. lcdui_render.go:353-355 builds the mask with display +=
  "\*".
- **Verification:** The complexity is as described. lcdui.go:2054-2057
  re-measures remaining[:cut] while decrementing cut from len(remaining), which
  is O(n^2) per line and about O(n^3/k) per long word. trimToWidth
  (lcdui.go:2013-2016) re-measures every prefix plus the ellipsis. charAdvance
  does a pixelFace().Render lookup for non-ASCII (font.go:377-380). Both run on
  every paintScreen (lcdui_render.go:122,128,357,375). The password mask is
  built with repeated += (lcdui_render.go:354). Reaching it needs guest text
  thousands of characters long with no spaces, which no real title is shown to
  produce, so the risk is latent, guest-driven Host stalls.
- **Fixed:** wrapText and trimToWidth add up character widths in one pass, so
  they are linear in the text, with identical output.

### R117 GameCanvas.getKeyStates is not latched; quick taps are lost

`internal/platform/skt/lcdui_game.go:191` · skt1 · correctness · PLAUSIBLE (reviewer said medium)

- **Defect:** MIDP 2.0 requires getKeyStates to report a key as pressed if it
  was pressed at least once since the previous call, even if it has since been
  released. recordGameCanvasKey clears the bit on release, and
  gameCanvasKeyStates returns the live bits without any latch.
- **Failure:** A GameCanvas title polls getKeyStates() once per frame (about
  50-100 ms). On a touch keypad or a fast keyboard tap, press and release both
  arrive through SendKey between two polls. The bit is set and cleared before
  the game thread reads it, so the press never shows up: jumps, shots or menu
  moves are dropped intermittently. The spec's latch exists precisely so that a
  rapid press and release is caught however slowly the loop runs.
- **Evidence:** lcdui_game.go:186-193: KeyReleased does keyStates &^= bit
  immediately. lcdui_game.go:81-91: gameCanvasKeyStates returns game.keyStates
  and clears no latch. No other latch state exists in gameCanvasData
  (lcdui.go:160-166).
- **Verification:** The code does not latch. recordGameCanvasKey clears the bit
  on release (lcdui_game.go:190-191), and gameCanvasKeyStates returns the live
  bits (lines 81-91), whereas MIDP 2.0 requires a press since the last call to
  be reported. No Host-side minimum hold exists (web/key-holds.js), so a press
  and release can both land between two polls. However,
  docs/history/skt.md:136-140 records a static read of the local corpus showing
  that none of the titles uses getKeyStates. The impact is therefore latent, not
  the intermittent drops the reviewer describes.
- **Fixed:** getKeyStates reports keys held now or pressed since the previous
  call and clears that record when read (MIDP's latch); the quicksave keeps it
  in the high 16 bits of the existing KeyStates field, so older quicksaves
  restore with no latch.

### R118 Case-distinct RMS/XFile names share one file on case-insensitive hosts

`internal/platform/skt/rms.go:252` · skt2 · save-integrity · PLAUSIBLE

- **Defect:** MIDP record store names are case sensitive, and
  recordStoreKey/xFileKey pass names through unchanged to DirectorySaveStore
  paths. On macOS (default APFS), iOS and Windows, 'Save' and 'save' are one
  file, so the two stores overwrite each other while the index lists both. On
  Windows a ':' in a name opens an NTFS alternate data stream, and \*?"&lt;>|
  fail the write, which RMS logs only at Debug.
- **Failure:** A title keeps settings in store "Option" and progress in
  "option". On a macOS or iOS host, each persist of one rewrites the shared
  file, so reopening either store yields the other's records and progress is
  corrupted. The packaged-store format itself writes upper-case letters as '#X'
  in file names for exactly this reason (rms_packaged.go:12-13).
- **Evidence:** rms.go:251-259 recordStoreKey = NormalizeSaveKey("rms/"+name)
  with no case or character folding. skvm_file.go:62-70 does the same for "fs/".
  backend/save.go:63-84 rejects only '\\', NUL and the path separator.
  backend/save.go:88-96 maps the key straight to filepath.Join. rms.go:419-421
  logs StoreSave failures only at Debug.
- **Verification:** recordStoreKey (rms.go:251-259) and xFileKey
  (skvm_file.go:62-70) pass names through NormalizeSaveKey, which rejects only
  '..', '\\', NUL and the separator (backend/save.go:63-84). savePath maps the
  key straight to filepath.Join (88-96). On case-insensitive APFS, iOS or NTFS,
  'Option' and 'option' therefore share one file, and on Windows a ':' in a name
  addresses an alternate data stream. MIDP store names are case sensitive
  (validRecordStoreName's comment). The packaged '#X' encoding
  (rms_packaged.go:12-13) is the handset's own on-disk scheme, which supports
  the concern. It needs a title using two store or file names that differ only
  in case, or names with Windows-reserved characters. Nothing shows any title
  does.
- **Fixed:** The directory save store writes an upper-case letter in a path
  segment as `#` and the letter and a `#` as `##` (keys, owner directories and
  quick-save slots unchanged), so names that differ only by case are separate
  files on every host; a folder written by an earlier build is converted whole
  on its first use under the folder lock, resumable at every step, with a record
  file (`.wfeature-names#1`) marking it, and a file two names shared on a
  case-insensitive host goes to the name its on-disk spelling matches. Checked
  on a copy of 372 real debug save folders (1,196 keys, 318 with an upper-case
  letter or `#`): every key read back with identical bytes and the listing was
  unchanged. Any other letter with case outside ASCII is now written by its
  number (`#+` and six hex digits), because APFS folds `ß` into `ss`, `ﬁ` into
  `fi` and the Kelvin sign into `k`; of an earlier name and an encoded one for
  the same key, the one written later is the save (P06, P10, P13 of the PR #234
  review).

### R119 openRecordStore(create) overwrites existing records the index lacks

`internal/platform/skt/rms.go:377` · skt2 · save-integrity · PLAUSIBLE (reviewer said medium)

- **Defect:** Store existence comes only from the .index list. When a name is
  missing from the index but rms/&lt;name> holds a non-empty record list,
  openStore(name, true) builds an empty store and persist() writes an empty list
  over the real data. An index write that failed is retried only by
  closeRecordStore or a checkpoint, so it can stay unwritten for the whole
  session. A deletion is distinguishable (it leaves an empty list), but the code
  does not look.
- **Failure:** Session 1: openRecordStore("save", true). storeIndex's StoreSave
  fails transiently (the error is logged only at Debug and kept as pending),
  persist succeeds, and later addRecord calls persist records but never rewrite
  the index because contains() is true. The title keeps the store open, so
  retryRMSClose never runs and the session ends. Session 2: loadIndex finds no
  'save', found=false, openRecordStore("save", true) creates records=nil and
  persist() writes the 4-byte empty list over the player's save.
- **Evidence:** rms.go:332-334 `found := state.contains(name)`. Lines 355-371
  read the save data only `if found`. Lines 372-378 for !found call storeIndex
  then persist(store) with nil records. rms.go:389 persistStore rewrites the
  index only when unwritten or !contains. rms.go:220-222 logs index failures at
  Debug. rms.go:575 retries pending writes only on full close.
  checkpoint_storage.go:39-42 keeps the failed write as pending.
- **Verification:** The code is as described: existence comes only from the
  index (rms.go:332), records are read only when found (355-371), and a create
  of an unlisted name writes an empty list (372-378). The comment at 326-331
  says this is deliberate, because deletion leaves an empty file. The data-loss
  scenario needs storeSave(rms/.index) at 220 to fail while persist() a moment
  later succeeds against the same DirectorySaveStore, root and lock. On top of
  that, no later index rewrite may happen in the session: none from another
  store's create (377) or a delete (548), none from closeRecordStore's retry
  (575-600) and none from a checkpoint flush (checkpoint_storage.go:85-101).
  lockSaveTree waits on contention instead of failing (save_lock.go:47-50). Real
  but a narrow edge case.
- **Fixed:** Creating a store the index does not list reads its key: a non-empty
  record list is adopted and re-indexed instead of overwritten, an empty list
  still starts empty, and undecodable data raises RecordStoreException.

### R120 RMS persist encodes and stores non-atomically; stale snapshot can win

`internal/platform/skt/rms.go:419` · skt2 · concurrency · PLAUSIBLE (reviewer said medium)

- **Defect:** Mutators release store.mu, then persistStore/persist encodes the
  records under store.mu and calls storeSave after unlocking. Two guest threads
  mutating the same store can therefore issue their StoreSave calls in the
  opposite order to their encodes. The older encoding lands last, and nothing
  rewrites it until the next mutation.
- **Failure:** Thread A calls addRecord: it appends, unlocks, and persist
  encodes v1 and releases store.mu. Thread B calls setRecord: it mutates to v2,
  encodes v2 and StoreSaves v2. Thread A's StoreSave(v1) then overwrites it.
  Memory holds v2 but disk holds v1. If the session ends or the user quits, B's
  change (for example the latest save slot) is lost.
- **Evidence:** rms.go:705-716 (and 734-749, 763-773) unlock store.mu before
  persistStore. rms.go:416-419 encodes under store.mu and calls
  runtime.storeSave(key, encoded) after unlocking. checkpoint_storage.go:22-43
  serializes only the StoreSave call itself under savePendingMu. SKT guest
  threads are goroutines (jvm/builtins.go:193), so these natives run
  concurrently.
- **Verification:** Correct as described: persist encodes under store.mu
  (rms.go:416-418) and calls storeSave after releasing it (419). The mutators
  release store.mu before persistStore (e.g. 714-715). storeSaveVersion
  serializes only the StoreSave call itself (checkpoint_storage.go:22-43), and
  SKT natives run on concurrent goroutines. The comment at 411-415 shows the
  author considered concurrent mutation but not write ordering. The loss needs
  two guest threads mutating the same store, with the lost interleaving in a
  window of a few instructions, and then no further mutation before the session
  ends. Saves normally come from one thread. Real but a low-likelihood race.
- **Fixed:** RMS `persist` encodes inside `storeSaveEncoded`, under the same
  lock as the backend's atomic StoreSave, so an older encoding can no longer
  land after a newer one.

### R121 RMS record count unbounded; past MaxSaveRecords the store is unreadable

`internal/platform/skt/rms.go:711` · skt2 · untrusted-input · PLAUSIBLE

- **Defect:** addRecord limits only total payload bytes (rmsCapacity), so
  zero-length records, or add/delete cycles that leave tombstones, grow
  store.records without bound. Every mutation re-encodes the whole list
  (quadratic I/O). Once the list passes backend.MaxSaveRecords (1&lt;&lt;20),
  the persisted store can no longer be decoded.
- **Failure:** A title logs an event per frame with addRecord(null,0,0), or
  saves with add-then-delete each time. After about 1,048,577 slots, the next
  session's openRecordStore fails with RecordStoreException 'record store ... is
  unreadable' (DecodeSaveRecords count limit), and the checkpoint rebuild fails
  the same way. The player's save becomes inaccessible.
- **Evidence:** rms.go:706 checks only storeBytes(store)+len(data) >
  rmsCapacity. Line 711 appends unconditionally. Line 417
  EncodeSaveRecords(store.records) runs per mutation. backend/save.go:277
  MaxSaveRecords = 1&lt;&lt;20 and line 310 refuses larger counts.
  rms.go:361-367 turns a decode failure into RecordStoreException.
- **Verification:** addRecord checks only payload bytes (rms.go:706), so
  zero-length records and tombstones grow store.records without a count limit.
  Every mutation re-encodes the whole list (417), and DecodeSaveRecords refuses
  more than MaxSaveRecords=1&lt;&lt;20 (backend/save.go:277, 310). Past that,
  openRecordStore reports 'unreadable' (rms.go:361-367). Reaching about 1M slots
  needs about 1M add calls, each rewriting and fsyncing a list that grows toward
  about 4 MB. The quadratic cost makes the store unusable long before that, and
  no realistic save pattern gets close. Latent, reachable only by a hostile or
  pathological title.
- **Fixed:** `addRecord` throws RecordStoreFullException once a store has used
  `backend.MaxSaveRecords` (1,048,576) ids, the most a save can decode on the
  next launch.

### R122 SGS save load uses LoadSave, so read errors look like an empty save

`internal/platform/skt/script.go:334` · skt2 · save-integrity · PLAUSIBLE (reviewer said medium)

- **Defect:** The SGS NV load service reads nv/data through SaveStore.LoadSave,
  which drops read errors (DirectorySaveStore.LoadSave discards the error from
  ReadSave). A transient I/O or lock failure is indistinguishable from 'no
  save': the guest receives all zeros, and its next save (0x99) replaces the
  real nv/data with zeros plus whatever it changed.
- **Failure:** On startup an SGS title calls service 0x98. ReadSave fails
  (EMFILE, a lock error, a recoverSaveReplacement error), LoadSave reports
  (nil,false), and the title sees zeroed progress. It treats this as a first run
  and immediately saves its defaults with 0x99, overwriting the player's real
  save on disk.
- **Evidence:** script.go:333-336 `if saved, ok :=
  s.options.SaveStore.LoadSave("nv/data"); ok { copy(data, saved) }`, otherwise
  zeros. script.go:348-349 later StoreSave("nv/data", data).
  backend/save.go:97-101 LoadSave discards the error, and ReadSave at 105-118
  returns lock or recovery errors. RMS (rms.go:356-358) and XFile
  (skvm_file.go:119-121) use backend.ReadSave and surface errors.
- **Verification:** script.go:333-336 uses SaveStore.LoadSave, and
  DirectorySaveStore.LoadSave discards ReadSave's error
  (backend/save.go:97-101). A read failure therefore looks like an absent save:
  the guest gets zeros, and a later 0x99 writes nv/data whole
  (script.go:348-349). Every other SKT path uses backend.ReadSave and surfaces
  errors (rms.go:356-358, skvm_file.go:119-121), and docs/skvm.md promises
  error-aware reporting, so this is an inconsistent gap. It needs a read error
  at startup (EACCES/EIO/recovery failure). Lock contention waits rather than
  fails (save_lock.go:47-50). The claim that the title 'immediately saves
  defaults' depends on the title. Real, but triggered only by uncommon I/O
  failures.
- **Fixed:** The SGS NV load (0x98) uses `backend.ReadSave` and stops the script
  with the read error instead of handing the title zeros it would save over its
  progress.

### R123 XFile writes persist only on flush/close; failures and teardown lose them

`internal/platform/skt/skvm_file.go:348` · skt2 · save-integrity · PLAUSIBLE (reviewer said medium)

- **Defect:** XFile/WIPI File writes stay in an in-memory buffer until flush or
  close. persistXFile clears dirty before the store write, so a failed write is
  no longer dirty and is retried only by a checkpoint (pendingSaves is not
  retried on close, flush of another file, or teardown). Nothing flushes open
  dirty handles when the runtime is destroyed. The guest is never told: flush
  and close return void.
- **Failure:** A title writes its save through an XFile it keeps open for reuse
  (on the handset every write reached flash immediately), and the player closes
  the session. Destroy(true) runs destroyApp, and nothing persists the dirty
  buffer, so the save is lost. Or close() hits a transient StoreSave error:
  dirty=false, the bytes sit only in pendingSaves, and with no checkpoint before
  the session ends the save is lost while the title believes it succeeded.
- **Evidence:** skvm_file.go:277-279 xFileWrite only marks dirty. Lines 341-361
  persistXFile sets file.dirty=false at line 348 before storeSaveVersion, and on
  error only logs at Debug. pendingSaves are flushed only in
  checkpoint_storage.go:71-104, plus RMS keys in rms.go:580-600. No other caller
  of persistXFile exists, and runtime.go Destroy/transition do not flush files.
  Compare wipi_file_stream.go:312-332, which keeps dirty on failure.
- **Verification:** Writes do stay buffered until flush/close
  (skvm_file.go:277-279). persistXFile clears dirty (348) before
  storeSaveVersion, and only checkpoints retry pendingSaves
  (checkpoint_storage.go:85-101). Runtime.Destroy (runtime.go:488-523) flushes
  no open handles. Buffer-until-close is documented as deliberate
  (docs/architecture.md:882-895: 'On LGT and SKT the buffer of an open file is
  the file as it was ... plus what was written'). Failed writes kept by key are
  also documented (docs/skt-checkpoints.md:76-79). The loss needs a title that
  never flushes or closes its save file before the session ends, or a transient
  StoreSave failure with no later checkpoint. The WIPI stream path does keep
  dirty on failure (wipi_file_stream.go:312-332), so the asymmetry is real, but
  the trigger is an edge case.
- **Fixed:** Every handle on a path shares one file per session, `persistXFile`
  keeps a refused write dirty, and the end of the session (Host destroy,
  `notifyDestroyed`, `System.exit` or a failure) stores every dirty path, so a
  write never flushed or closed is no longer lost.

### R124 Exit inside a delivered key repeat drops Progress.ExitReason

`internal/session/session.go:858` · runtime · correctness · CONFIRMED

- **Defect:** When the handset key repeat that Tick delivers makes the guest
  exit, repeatDue returns Progress{Exited: true} without ExitReason, although
  noteExit has already recorded it in s.exitReason. The browser Host reports
  exits from progress.ExitReason, so this ending is shown and stored with no
  reason, which the Progress.ExitReason doc says must not happen.
- **Failure:** A KTF title's menu quits on a held key. The player holds the key,
  the 600 ms repeat fires inside Tick, and deliverRepeat → s.ktf.SendKey returns
  ErrGuestExited. endedOrFailed → noteExit sets s.exitReason and closes the
  session. repeatDue returns Progress{Flushes, Exited: true} with an empty
  ExitReason. webhost/session.go:516-517 then calls endGame(endedByExit("")) and
  sends serverExited with an empty message.
- **Evidence:** session.go:854-858: `case errors.Is(err, ErrExited): return
  Progress{Flushes: flushes, Exited: true}, true, nil`, with no ExitReason:
  s.exitReason. The other exit paths set it: session.go:571-574, 599-602 and
  608-611. webhost/session.go:510-517 reads progress.ExitReason.
- **Verification:** Traced. tick (session.go:556-565) calls repeatDue first,
  which calls deliverRepeat (:883-891). For KTF that is
  s.endedOrFailed(s.ktf.SendKey(ctx, ktf.KeyRepeated, code)), and endedOrFailed
  (:944-953) calls noteExit, which sets s.exitReason and closes the session,
  then returns ErrExited. repeatDue then returns `Progress{Flushes: flushes,
  Exited: true}` at :858 with no ExitReason, unlike the other exit paths at
  :571-574, :587-590, :599-602, :608-611 and :626-630. The webhost loop
  (webhost/session.go:510-517) reads only progress.ExitReason in this branch, so
  endGame(endedByExit("")) runs and serverExited goes out with an empty Message,
  which the Progress.ExitReason doc (:497-505) says must not happen. Only KTF is
  affected, because the SKT path (:888-889) does not go through endedOrFailed.
  The trigger is a title that exits on a held key's repeat; titles commonly
  treat a repeat like a press. The effect is a missing diagnostic string, so
  low.
- **Fixed:** When the key repeat delivered inside Tick ends the game, Progress
  carries the ExitReason that `endedOrFailed` already recorded.

### R125 SameOrigin is bypassed by DNS rebinding

`internal/wsproto/handshake.go:147` · runtime · security · CONFIRMED

- **Defect:** The default origin policy, the only one the server uses, accepts a
  handshake when the Origin host equals the request's Host header. Both values
  come from the attacking page under DNS rebinding, so a remote site can open
  game sessions on a LAN or localhost server. The comment above CheckOrigin says
  this is the attack the check exists to stop.
- **Failure:** The person visits evil.example:8080. The page rebinds
  evil.example to 127.0.0.1 or the server's LAN IP and opens
  ws://evil.example:8080/... The browser sends Origin http://evil.example:8080
  and Host evil.example:8080; SameOrigin compares them, finds them equal and
  upgrades. The attacking page can now start and drive sessions, including quick
  save and load commands, on the person's server.
- **Evidence:** handshake.go:138-148 returns strings.EqualFold(parsed.Host,
  request.Host). handshake.go:51-56 says the check exists so that 'any page the
  user visits' cannot drive a session. webhost/session.go:108 builds the
  Upgrader without its own CheckOrigin and no Host allowlist.
- **Verification:** SameOrigin (handshake.go:138-148) accepts when Origin's host
  equals request.Host, and under DNS rebinding the attacker's page controls
  both. webhost/session.go:108 builds the Upgrader with no CheckOrigin, and
  nothing in Server.ServeHTTP (webhost.go:186-214) or anywhere else in
  webhost/cmd/server checks the Host header. A rebinding page at
  evil.example:&lt;port> therefore passes the check that handshake.go:51-55 and
  docs/architecture.md:400-402 say exists so that 'any page the user visits'
  cannot drive a session. One more point: with no Host allowlist, the same
  rebinding also exposes every plain HTTP route, including /api/saves,
  /api/savepack POST and /api/shutdown, as same-origin, so the gap is not
  specific to the WebSocket. Severity stays low: the attack needs the server
  running, a known port, and a browser without local-network-access gating,
  which current Chrome partly provides.
- **Not fixed:** Passed by the user's decision on 2026-10-16. Any Host allowlist
  would refuse names the server cannot know (mDNS `.local` names, router DNS
  names, a reverse proxy's hostname), and forwarding headers cannot mark proxied
  requests because a rebinding page can set them on same-origin requests; the
  limitation is documented in docs/running.md.

### R126 Decoded save pack is not checked for what WriteSaveTree can apply

`internal/backend/savepack.go:242` · runtime · untrusted-input · PLAUSIBLE (reviewer said medium)

- **Defect:** DecodeSavePack checks each key on its own but never checks the
  pack as a whole. It accepts packs that no export produces and that
  WriteSaveTree cannot apply safely: (1) zero entries, (2) an unbounded entry
  count (64 MB of 9-byte entries is about 7M keys), and (3) keys that conflict
  as file and directory (`a` and `a/b`), or keys the destination file system
  refuses, such as non-UTF-8 names on macOS. WriteSaveTree writes entries in
  order and prunes last, so these packs wipe the tree, stall for hours, or stop
  partway with part of the live save already overwritten.
- **Failure:** (1) A header-only container with length 0 and CRC 0 decodes to
  zero entries. WriteSaveTree then writes nothing, pruneSaveTree removes every
  save of that game, and the import reports success. (2) Millions of tiny
  distinct keys each go through storeSave, which runs CreateTemp, an fsync of
  the file, a rename and an fsync of the directory. The import HTTP handler
  holds the save claim and the tree lock for hours and creates millions of files
  in the owner directory. (3) Pack keys `fs/a` and `fs/a/b`: `fs/a` is written
  over the player's file, then storeSave(`fs/a/b`) fails in MkdirAll with
  ENOTDIR. WriteSaveTree returns written=1, the prune never runs, and the tree
  is a mix of backup and live entries, which is the state the doc says must not
  happen. The same partial write happens when an older live tree has a file
  where the pack needs a directory.
- **Evidence:** savepack.go:245-283 loops with no entry-count bound and checks
  only NormalizeSaveKey and sort order per key. savepack.go:449-466 writes all
  entries before pruneSaveTree. By contrast, save_snapshot.go:14-40 caps
  snapshots at 65536 entries and rejects parent-file conflicts.
  webhost/savepack.go:67 allows a 64 MB upload, and webhost/savepack.go:226
  calls WriteSaveTree on the decoded entries.
- **Verification:** The missing checks are real. decodeSavePackPayload
  (savepack.go:242-284) has no bound on entry count and no check for
  file/directory conflicts, while validateSnapshotSaves (save_snapshot.go:14-40)
  has both. A header-only pack with length 0 and CRC 0 decodes to zero entries
  (:221-227). WriteSaveTree writes in order and prunes last (:449-465). Keys
  `fs/a` and `fs/a/b` fail in MkdirAll after `fs/a` is already written. Three
  parts of the impact claim are overstated. (a) The import must carry the target
  archive's own SHA-256 (webhost/savepack.go:200), and any crafted pack that
  passes that check can already overwrite every save with junk, so a zero-entry
  pack does no extra harm. (b) A partial write leaving mixed old and new files
  is not 'the state the doc says must not happen'. WriteSaveTree's own comment
  (savepack.go:460-464) calls that state 'the recoverable half', and
  docs/quickload-saves-plan-2026-10-02.md:215-216 records an interrupted .wfs
  import leaving mixed files as 'Left as it is by decision'. The failure is also
  reported to the page as a 500. (c) The entry count is limited indirectly by
  the 64 MB body (webhost/savepack.go:67), to roughly 2-7M entries. That is a
  real memory and fsync amplification, which conflicts with the untrusted-input
  bounding rule. Reaching it needs a crafted, identity-matched file. The server
  has no auth, so a LAN peer could post one, but such a peer can already
  overwrite or delete saves through /api/saves. File/directory conflicts in a
  real export are impossible for keys within one tree. Across a live tree and a
  pack they need the same title to have turned a path from a file into a
  directory, which is rare. The defect is real; the impact is smaller than
  claimed.
- **Fixed:** The save pack decoder refuses packs with more than 65,536 entries
  (checked before allocating) and file/directory key conflicts, export refuses
  to write such a pack, WriteSaveTree validates the whole entry set and the live
  tree's shape before its first write, and the HTTP import refuses an empty pack
  with 422.

### R127 Import prune deletes a just-restored save on case/NFD-insensitive FS

`internal/backend/savepack.go:502` · runtime · save-integrity · PLAUSIBLE (reviewer said medium)

- **Defect:** WriteSaveTree writes each pack entry with storeSave and records
  the pack key in keep. pruneSaveTree then rebuilds keys from the names it finds
  on disk and removes every file whose key is not in keep. On APFS (the macOS
  default) and other case- or normalization-insensitive file systems, renaming
  onto an existing file whose name differs only in case or Unicode normalization
  replaces the contents but keeps the old directory name. The prune walk then
  sees the old spelling, finds no matching key and deletes the file that holds
  the restored data.
- **Failure:** On macOS, the owner tree holds `rms/Data`, or a Korean name
  stored as NFD. The person imports a backup whose key is `rms/data`, or the NFC
  spelling. storeSave renames the temp file onto `.../rms/data`; APFS replaces
  the contents of the existing entry and keeps the name `Data`. pruneSaveTree
  walks, derives key `rms/Data`, sees keep[`rms/Data`] is false and calls
  os.Remove. The restored save is gone and the import reports success with
  removed=1. I reproduced the name-keeping rename behavior with rename(2) on
  this machine's APFS for both case and NFC/NFD.
- **Evidence:** savepack.go:449-459 fills keep from the pack keys after
  storeSave. savepack.go:490-505 derives the key from filepath.Rel of the
  on-disk name and removes it when `!keep[key]`. save.go:231 uses os.Rename onto
  the target path. A scratch test on APFS: an NFD file plus a rename to the NFC
  name left one NFD entry holding the new contents; a file `Save` plus a rename
  to `save` left `Save`.
- **Verification:** The mechanism is real. I ran rename(2) in the scratchpad on
  this machine's APFS. With an existing `Data`, renaming onto `data` left one
  entry still named `Data` that held the new bytes, and the same happened with
  an NFD Hangul name renamed onto its NFC spelling. In the code, storeSave
  (save.go:231 os.Rename) writes through that existing entry. pruneSaveTree
  (savepack.go:490-505) takes the key from the on-disk name with filepath.Rel,
  so the key is `rms/Data`, which is not in keep (filled with the pack spelling
  at :457), and calls os.Remove. Nothing before WriteSaveTree
  (webhost/savepack.go:181-226) or in NormalizeSaveKey (save.go:63-84) folds
  case or normalizes Unicode. What I could not show is a realistic way to get
  the precondition: the live tree must already hold a file whose name differs
  from the pack key only in case or normalization, for the same archive
  identity. The same title writes the same key bytes on every install. Keys
  built from guest strings are NFC, and APFS keeps NFC as written. LGT code
  (lgt/wipic_file.go:770-775) does record titles that spell one file in more
  than one case, but the mismatch only bites if the two installs created the
  file under different spellings first. The one deterministic case is a save
  root on HFS+, which stores NFD and so drops any decomposable Korean key on
  every import, and that is an uncommon setup. Silent data loss, but only under
  an edge-case precondition, so low.
- **Fixed:** The import prune does not delete a file whose on-disk spelling
  differs from a restored key when it is the same file (`os.SameFile`) the
  restore just wrote, so a save restored onto a case- or NFD-folded name on APFS
  survives.

### R128 Library scan skips platform groups that are symbolic links

`internal/gameroot/gameroot.go:45` · runtime · correctness · PLAUSIBLE

- **Defect:** Entries uses DirEntry.IsDir() from os.ReadDir, which is false for
  a symbolic link. A group directory that is a link, such as var/games/ktf
  pointing at a library on another disk, is treated as an extension-less file in
  the root and dropped. The picker lists none of its games, and the save-owner
  collision scans built on gameroot.Paths never check them, even though the
  server opens those archives fine by path because os.Stat follows the link.
- **Failure:** The person makes var/games/ktf a symlink to /Volumes/Library/ktf.
  gameroot.Entries reads the root, gets item.IsDir()==false for `ktf`, skips the
  group loop and appends {Name: "ktf"} as a root file, which listRoot drops
  because it has no game extension. The picker shows no KTF games, and
  ktf.SaveOwnerCollisions over the root never sees them, so two titles sharing a
  PID in that group can overwrite each other's saves without a warning.
- **Evidence:** gameroot.go:44-47 `if !item.IsDir() { continue }` and
  gameroot.go:67-71 add non-directories as root files. webhost/games.go:98-100
  filters by extension. webhost/games.go:148-167 and 197-201 open the same path
  with os.Stat, which follows links. ktf/saveowner.go:55, lgt/saveowner.go:57
  and skt/saveowner.go:65 use gameroot.Paths.
- **Verification:** The behavior is real. os.ReadDir returns a DirEntry whose
  Type() is ModeSymlink for a link, so IsDir() is false, gameroot.go:45 skips
  the group, and :67-71 add it as a root file that webhost/games.go:98 drops for
  having no game extension. A symlinked platform group therefore lists no games.
  The impact is smaller than claimed. The save-owner collision scans are
  deliberately aligned with the picker: ktf/saveowner.go:51-54 says 'The scan
  stops where the Host's picker stops. An archive deeper than that cannot be
  started from a Host'. Since the page only gets paths from games.json, games in
  a skipped group cannot be started from the Host, and a collision the scan
  misses there is one no Host save can suffer. The scans are called only from
  the CLI diagnostic (cmd/cli/main.go:2142-2152). No doc says symlinked groups
  are supported, though symlinked save folders are (docs/running.md:499). This
  needs an unusual hand-made layout and amounts to a missing listing, so low.
- **Fixed:** `gameroot.Entries` follows a symbolic link to a directory, so a
  linked platform group is listed.

### R129 RecordEnumeration previous\* breaks the MIDP contract at the start and after next

`internal/api/midp/rms.go:250` · host · correctness · CONFIRMED (reviewer said medium)

- **Defect:** The cursor is modelled between records. On a fresh or reset
  enumeration, previousRecord throws InvalidRecordIDException and
  hasPreviousElement is false, but MIDP returns the last record there. After
  nextRecord returned A then B, previousRecord returns B again instead of A.
- **Failure:** A title finds its newest save with
  `rs.enumerateRecords(null,null,false).previousRecord()` (MIDP: 'if after
  creation the first call is to previousRecord(), the record data of the last
  element … will be returned'). Here it gets InvalidRecordIDException, or skips
  the read because hasPreviousElement()==false, and treats the store as having
  no save. A forward-then-back walk reads the same record twice.
- **Evidence:** rms.go:238-257 previousRecordId throws at position&lt;=0 and
  returns ids[position-1] (the record just returned by next); rms.go:259-282
  hasPreviousElement = position>0; rms.go:284-290 reset sets position 0;
  setRecordIDs (154) sets position 0 after every rebuild.
- **Verification:** previousRecordId throws when position&lt;=0 (rms.go:250) and
  otherwise returns ids[position-1]. That is the record nextRecordId just
  returned, so after next A, next B, previous returns B. hasPreviousElement is
  position>0 (273). reset (284-290) and every rebuild (154) set position to 0.
  The MIDP javadoc says a first previousRecord after creation returns the last
  element; a cursor that sits on the last returned record also makes previous
  after next A, next B return A. The repo fixture (RecordStoreMIDlet.java:166)
  encodes the between-records model, but no doc records handset evidence for it.
  Severity lowered because no title is known to rely on the previous-first
  pattern.
- **Fixed:** The enumeration cursor sits on the last returned record as MIDP
  specifies (a first previousRecord returns the last record, previous after next
  A, next B returns A); forward positions keep their stored meaning, so
  checkpoints are unaffected.

### R130 Stop reports Drained while the server is still draining games

`internal/launcher/launcher.go:332` · host · correctness · CONFIRMED

- **Defect:** waitUntilStopped treats a free port as stopped. The server's drain
  calls http.Server.Shutdown, which closes the listener first, while
  CloseSessions is still closing games and writing their saves. Stop therefore
  returns Drained at the first poll after the request, before the drain the
  outcome promises has finished.
- **Failure:** Running `stop` while a game is open prints 'stopped' within about
  100 ms, but the process is still closing sessions for up to 10 s. A
  stop-then-start upgrade runs the new server beside the old one: the old
  process still holds the save-directory locks, so the new server refuses to
  start that game as busy. On Windows, replacing the executable fails because
  the process is still alive.
- **Evidence:** launcher.go:240-243 Drained when waitUntilStopped returns true;
  328-346 returns on State==Free; cmd/server/main.go:486-509 drain runs Shutdown
  (closes listeners) concurrently with CloseSessions.
- **Verification:** drain runs CloseSessions alongside httpServer.Shutdown
  (cmd/server/main.go:486-509). Shutdown closes the listeners immediately, so
  Query's dial fails and returns State Free (launcher.go:144-153).
  waitUntilStopped then returns true and Stop reports Drained
  (launcher.go:240-243) while sessions may still be closing and holding save
  locks for up to the 10 s deadline. The window is usually short.
- **Fixed:** Stop reports Drained only after the process the server named has
  exited, not when the port goes free; drainWait goes from 10 s to 15 s, past
  the server's own 10 s drain (Windows waits with OpenProcess and
  WaitForSingleObject).

### R131 Key step leaves the key held when its hold is cut short

`internal/route/route.go:361` · host · correctness · CONFIRMED

- **Defect:** In a `key` step with Hold>0, a stop from tickFor (budget
  exhausted, guest stalled, ctx error) returns before the release is sent. The
  serve tap path releases in the same situation; the route runner does not.
- **Failure:** In a serve session, a `route` command whose `key FIRE` hold hits
  the route tick budget returns a not-completed result. The session stays alive
  (no error), but FIRE is still pressed in the guest. Later serve `step`
  commands run with a stuck key, and the player character keeps firing or
  moving.
- **Evidence:** route.go:350-368 returns stop/err at 361-362 before
  SendKey(false) at 365; serve.go:451-461 shows the intended release-on-stop
  behaviour for taps; serve.go:581-595 keeps the session open after an
  incomplete route.
- **Verification:** In a key step, the code returns as soon as tickFor answers
  stop or err (route.go:361-362), before SendKey(false) at 365. tickFor stops on
  an exhausted budget, a stall or a ctx error (route.go:396-415). serve.route
  keeps the session alive when the route did not complete (serve.go:581-595),
  unlike the tap path, which releases the key (serve.go:451-461). This only
  affects the development CLI serve tool.
- **Fixed:** A route key step whose hold is cut short (tick budget used up,
  stall or tick error) still sends the release before returning.

### R132 Game removal deletes any file in the added root, including .adopted

`internal/webhost/remove.go:60` · host · rule-violation · CONFIRMED

- **Defect:** serveGameRemoval checks only that the path is under the added root
  and is a regular file. It accepts any name, not just .zip/.jar archives, so
  the adoption marker and in-flight .upload-\* temp files can be deleted.
  Deleting .adopted turns the one-time adoption back on.
- **Failure:** DELETE /api/games?game=ext/.adopted removes the marker. On the
  next start, AdoptLooseGames moves every archive the user dropped by hand into
  the var/games root over to var/ext. Those files are then shown as page-added
  and page-deletable, which is what the root split was meant to prevent ('The
  library beside the server is not the page's to delete').
- **Evidence:** remove.go:44-73 no gameExtensions check; games.go:148-167
  gameFile accepts any component; adopt.go:38,51-52 marker controls the move;
  adopt.go:69-99 moves loose archives.
- **Verification:** serveGameRemoval (remove.go:44-73) checks only added==true
  and that the target is a regular file. pathComponents (webhost.go:354-369)
  allows dot-names, and gameFile has no gameExtensions check, so DELETE
  /api/games?game=ext/.adopted removes the marker. AdoptLooseGames then runs
  again at the next start (adopt.go:51-52) and moves loose library archives into
  ext. This needs a hand-made request because the page never sends it. DELETE is
  not a simple method, so cross-site use is blocked by preflight. The server is
  documented as unauthenticated on the network (running.md:58-61).
- **Fixed:** Removal from the added root accepts only names the upload route
  could have written (.zip/.jar, no leading dot), so `.adopted`, `.upload-*`
  temporary files and other files are refused with 400.

### R133 WebSocket 'report' writes log files in release and bypasses the rate limit

`internal/webhost/session.go:1440` · host · rule-violation · CONFIRMED

- **Defect:** The HTTP debug-log route is disabled in release builds and
  rate-limited to 10 per minute. The session's clientReport command calls
  server.storeReport directly, with no diagnostics check and no
  allowDebugReport.
- **Failure:** Any page, in a release build, sends {"kind":"report"} in a loop.
  Each message writes a new file under var/logs and runs a full ReadDir prune.
  Disk churn is bounded only by the 64 MiB prune budget, against the documented
  rule that a release 'does not answer here at all' and that the rate bound
  applies to scripts.
- **Evidence:** session.go:700-701 clientReport → storeReport; 1427-1446 →
  writeReport → server.storeReport (1468-1475); debuglog.go:87-105 diagnostics
  gate and allowDebugReport exist only on the HTTP route; debuglog.go:149-162.
- **Verification:** clientReport, then storeReport, then writeReport, then
  server.storeReport (session.go:700-701, 1427-1475). This path has neither the
  s.diagnostics gate nor allowDebugReport (debuglog.go:87-105, 132-148), and the
  WebSocket accepts clients that send no Origin header
  (wsproto/handshake.go:138-142). A scripted client in a release build can
  therefore write unlimited report files. The disk is bounded only by the prune
  budget. The release page has removed its report button (app.js initDebugLog),
  so only hand-made clients reach this.
- **Fixed:** The WebSocket `report` command writes nothing in release builds and
  answers with an error; in debug builds it goes through `allowDebugReport`, the
  10-per-minute budget /api/debug-log uses.

### R134 Scheduling an already-scheduled TimerTask starts a second worker

`internal/api/midp/timer.go:189` · host · correctness · PLAUSIBLE

- **Defect:** startTimerThread refuses only a cancelled Timer or a cancelled
  task. java.util.Timer throws IllegalStateException for a task that is already
  scheduled. Here a second schedule starts another TimerThread running the same
  task in parallel.
- **Failure:** A title calls `timer.scheduleAtFixedRate(task,0,100)` each time a
  screen opens and relies on the IllegalStateException (caught) on later calls.
  Here every call adds a worker, so the task runs 2x, 3x… as often, which speeds
  up animation or game logic and runs task.run concurrently with itself.
- **Evidence:** timer.go:178-218 checks only the Timer and task 'cancelled'
  booleans; no 'scheduled' state is checked or set before start (the 'scheduled'
  field at 63 only holds the execution time).
- **Verification:** startTimerThread (timer.go:178-218) checks only the Timer
  and task 'cancelled' flags. No 'already scheduled' state is set or checked, so
  a second schedule of the same task starts another TimerThread. That
  contradicts java.util.Timer's IllegalStateException contract. Reachability
  depends on a shipped title scheduling one task twice and relying on the
  exception, which has not been shown.
- **Fixed:** Each TimerTask has a private `queued` flag set on its first
  schedule; scheduling it again on any Timer throws IllegalStateException
  instead of starting a second worker.

### R135 FileOutputStream(name)/(name,false) does not truncate the existing file

`internal/api/skvm/xce.go:108` · host · save-integrity · PLAUSIBLE

- **Defect:** The non-append constructors open XFile(name, WRITE), which keeps
  the existing bytes with the cursor at 0. The append=false branch does nothing
  extra. Writing a shorter save overwrites only the prefix and leaves stale
  trailing bytes, unlike the java.io constructor these signatures mirror.
  Whether the original handset truncated is unverified.
- **Failure:** The save file is 100 bytes. The title runs new
  FileOutputStream("/s") (or ("/s", false)), writes a 60-byte save and closes.
  The file is still 100 bytes, with 40 bytes of the old save at the end. A title
  that reads to EOF or uses filesize() parses the leftover bytes as data and
  gets a corrupt or rejected save.
- **Evidence:** xce.go:68-88 openStreamFile uses mode xfileWrite; xce.go:92-113
  append==0 returns without truncation; platform/skt/skvm_file.go:107-145
  write-mode open loads the existing data and keeps it.
- **Verification:** FileOutputStream(String) and (String,false) open XFile in
  write mode (xce.go:92-110, definitions.go:332-333). initXFileName loads the
  existing data and keeps it with the cursor at 0 (skt/skvm_file.go:107-145), so
  a shorter write leaves stale tail bytes. Nothing in docs or the spec available
  to me says what the handset's com.xce.io stream did, so the divergence from
  java.io is not established as a handset bug. The reviewer also notes it is
  unverified.
- **Fixed:** `FileOutputStream(String)` and `(String,false)` open the file and
  empty it through `XFile.truncate`, stored at once as the one save rule has it,
  without going through the removal list; `(String,true)` appends and creates a
  missing file.

### R136 FileInputStream.read(b, off, 0) returns -1 instead of 0

`internal/api/skvm/xce.go:237` · host · correctness · PLAUSIBLE

- **Defect:** Every count&lt;=0 from XFile.read is mapped to -1 (EOF), including
  a zero-length request. The java.io contract returns 0 when len is 0, so a
  zero-length read is reported as end of stream.
- **Failure:** A loader calls in.read(buf, off, remaining) with remaining 0 for
  an empty field, or read(new byte[0]). It gets -1 and treats the save as
  truncated (EOFException or abort), even though bytes remain.
- **Evidence:** xce.go:220-241 `if count <= 0 { return -1 }` without checking
  arguments[3]==0; xce.go:199-216 read([B) forwards length 0 for an empty array.
- **Verification:** skt xFileRead returns 0 when length==0
  (skvm_file.go:237-240), and fileInputStreamReadRange maps every count&lt;=0 to
  -1 (xce.go:237-238). read([B) on an empty array therefore reports EOF. This
  contradicts the java.io contract the comment at xce.go:219 says it follows.
  DataInputStream.readFully(…,0) never calls read, and no title is known to
  issue a zero-length read, so reachability is unconfirmed.
- **Fixed:** `FileInputStream.read(b, off, 0)` returns 0 instead of -1.

### R137 EventQueue.SOFT1/SOFT2 declared 21/22; spec and platform use 90/91

`internal/api/wipi/definitions.go:61` · host · protocol · PLAUSIBLE

- **Defect:** The WIPI spec gives SOFT1=90 and SOFT2=91. The SKT platform also
  translates the soft keys to 90/91 before delivering them to a Card. The
  published EventQueue constants say 21 and 22, so any code that reads the field
  at runtime compares against the wrong number.
- **Failure:** A Jlet (or a library class compiled without constant inlining)
  runs `if (key == EventQueue.SOFT1)` through getstatic and gets 21. keyNotify
  delivers 90 (wipiKeySoft1), so the soft-key menu never opens.
  getKeyName/getGameAction tables built from the field disagree with the
  delivered code the same way.
- **Evidence:** wipi/definitions.go:61-62 SOFT1=21, SOFT2=22;
  platform/skt/wipi.go:343-345 wipiKeySoft1=90, wipiKeySoft2=91,
  wipiKeyClear=99; WIPI wiki EventQueue: 'SOFT1 … 90으로', 'SOFT2 … 91로' (SOFT3=92
  not declared).
- **Verification:** The values are wrong. The spec says 'SOFT1 ... 90으로 지정',
  'SOFT2 ... 91로 지정' (wipi-wiki llms-full.txt, lines around 64738 and 128326),
  and docs/history/skt.md:1836 itself names 90 as EventQueue.SOFT1. The platform
  delivers 90/91 (skt/wipi.go:343-345). Reachability is low: these are static
  final constant fields, so javac inlines 90 into title bytecode and getstatic
  is not normally emitted. The claim that getKeyName/getGameAction tables are
  built from the field is wrong, because wipiKeyName uses the Go constants
  wipiKeySoft1/2 (skt/wipi.go:437).
- **Fixed:** EventQueue.SOFT1/SOFT2 are 90/91, matching the specification and
  the codes `wipiKeyOfDevice` already hands a Card.

### R138 One unopenable .class entry aborts platform classification

`internal/platform/detect/detect.go:362` · media · error-handling · CONFIRMED

- **Defect:** referencesSKVM returns an error as soon as a single .class entry
  fails file.Open (unsupported compression method, or a bad local header).
  Classify then refuses the whole archive. The loop's own comment says a corrupt
  entry 'says nothing about the platform; another class in the same JAR still
  can', and read errors are already skipped with continue.
- **Failure:** An SKT title repacked as a plain JAR without its .msd has one
  class entry with a damaged local header, or stored with a compression method
  Go does not register. Classify returns ('', ReasonNoMarker, 'read archive
  entry ...') before it looks at the remaining classes, which reference
  com/skt/m/. It never claims SKT, and it never reaches the KTF native-package
  or script-package checks either.
- **Evidence:** detect.go:360-363 `opened, err := file.Open(); if err != nil {
  return false, fmt.Errorf(...) }`, compared with detect.go:364-370, where a
  ReadAll error does `continue`. Classify at detect.go:193-196 propagates the
  error.
- **Verification:** referencesSKVM (detect.go:351-378) returns an error as soon
  as a .class entry's file.Open fails at line 360-362. That covers an
  unregistered compression method (zip.ErrAlgorithm) or a bad local header. A
  ReadAll failure three lines later is skipped with `continue`, and the comment
  there says a corrupt entry says nothing about the platform. Classify
  propagates the error at 193-196 before the remaining classes,
  isKTFNativePackage, IsScriptPackage or archiveOfArchives are consulted. No
  test or doc makes the hard failure intentional. It needs an archive that has
  no marker and also has an unopenable class entry, so the impact is limited to
  misclassifying unusual repacks.
- **Fixed:** A .class entry that cannot be opened is skipped like one that fails
  to read, so the other classes still decide the platform.

### R139 `run` key names send soft1=6/soft2=7/hangup=-1, wrong for SKT and SGS

`cmd/cli/checkpoint.go:264` · tools · correctness · CONFIRMED

- **Defect:** sharedKeyCode maps soft1 to 6, soft2 to 7, soft3 to 9 and hangup
  to -1. The page sends -6/-7 for the soft keys. The shared session passes codes
  unchanged to the MIDP runtime, which compares against KeyCodeSoft1=-6 and
  KeyCodeSoft2=-7, and the SGS script maps -1 to UP.
- **Failure:** `wfeature run skt.jar -serve` followed by a `key soft1` command
  delivers keyPressed(6), so a MIDlet's soft-key menu or commands never open. On
  an SGS title, `key hangup` presses UP (script key 18).
- **Evidence:** checkpoint.go:259-266. session/session.go:737-746 passes codes
  unchanged for MIDP. skt/lcdui_input.go:17-18: KeyCodeSoft1=-6,
  KeyCodeSoft2=-7. skt/script.go:184: `code == -1 || code == KeyCodeUp` → up.
  web/app.js:106,111: MENU -6, SOFT2 -7.
- **Verification:** sharedKeyCode (checkpoint.go:259-266) maps soft1→6, soft2→7
  and hangup→-1. That is right for KTF and LGT through ktfKeyCode
  (session.go:1413-1443, 6→KeyLeftSoft), but the MIDP branch passes codes
  unchanged (session.go:737-746). The SKT runtime compares against
  KeyCodeSoft1=-6 and KeyCodeSoft2=-7 (lcdui_input.go:17-18), and runskt's own
  table returns -6 and -7 (skt/input.go:245-248). The SGS script maps -1 to UP
  (skt/script.go:184). `run` accepts every platform (checkpoint.go:66-73), and
  docs/cli.md:180 says it uses the shared key table, so `key soft1` on an SKT
  title delivers 6.
- **Fixed:** `run` sends the page's codes (soft1 = -6, soft2 = -7) and refuses
  soft3/ez/hangup on SKT titles (MIDP and SGS).

### R140 runktf -route silently drops -key, -touch, -park, -cheat, -profile-from

`cmd/cli/main.go:1279` · tools · correctness · CONFIRMED (reviewer said medium)

- **Defect:** With -route, runKTF hands the run to runRoute and skips the tick
  loop, which is the only place that delivers keyEvents, touchEvents, the park
  and cheat commands, and that resets the profile at -profile-from. None of
  these options is refused with -route, and -cheat still prints "cheat:
  attached" and starts reading stdin, but no command is ever executed. runLGT
  and runSKT drive their routes through the same advance function, so those
  options do work there.
- **Failure:** `runktf game.zip -route r.txt -park 200:5000 -key 50:fire` runs
  without the park or the key and reports the route result as if it had them.
  `runktf game.zip -route r.txt -cheat` says the console is attached, and typed
  `freeze`/`set` commands are never run.
- **Evidence:** main.go:1279-1299: the route branch calls runRoute, whose runner
  (main.go:1651-1696) only advances via stepKTF. main.go:1300: the loop is
  guarded by `script == nil`. The key, touch, park, cheat and profile handling
  is all inside that loop (main.go:1307-1402). The serveConflict check at
  main.go:1009-1021 applies only to -serve. Compare runLGT's advance closure
  (main.go:2625-2706), used by its route runner.
- **Verification:** With -route, runKTF calls runRoute (main.go:1279-1299).
  runRoute's runner advances only through stepKTF(…, false) (:1651-1696) and
  receives no keyEvents, touchEvents, park, cheat or profileFrom. The loop that
  handles all of those is guarded by `script == nil` (:1300). serveConflict
  refuses these options only together with -serve (:1009-1021). runLGT's advance
  closure, which its route runner uses, does handle them. docs/cli.md:585 does
  say 'a `-route` replay … has no console', but the CLI still prints 'cheat:
  attached' and reads stdin. Nothing documents that -key and -park are dropped.
  Dev-tool option combination, so low.
- **Fixed:** `runktf -route` refuses -key, -touch, -park, -cheat and
  -profile-from by name with exit 2, and `runktf -serve` also refuses
  -profile-from.

### R141 runktf -route treats an explicit -ticks 64 as unbounded

`cmd/cli/main.go:1284` · tools · correctness · CONFIRMED

- **Defect:** The route budget is cleared whenever ticks equals
  defaultProbeTicks, rather than when ticksChosen is false. An explicit `-ticks
  64` therefore does not cap the route, despite the comment that an explicit
  -ticks still caps it. The native path uses ticksChosen correctly.
- **Failure:** `runktf game.zip -route r.txt -ticks 64` on a route that never
  arrives runs until Ctrl-C instead of stopping after 64 ticks.
- **Evidence:** main.go:1283-1286: `if budget == defaultProbeTicks { budget = 0
  }`. ticksChosen is set at main.go:835. native.go:180-183 uses `if
  !run.ticksChosen`.
- **Verification:** main.go:1283-1286 clears the route budget when `budget ==
  defaultProbeTicks` (64, :766), not when ticksChosen is false (set at :835). An
  explicit `-ticks 64` therefore leaves the route unbounded. That contradicts
  the inline comment and docs/cli.md:367-369 ('an explicit `-ticks` is the
  budget'). runLGT (:2500) and the native path (native.go:180-183) both use
  ticksChosen.
- **Fixed:** The route budget is cleared only when -ticks was not given, so an
  explicit `-ticks 64` caps the route.

### R142 Probe-mode -framedir never writes the first lit frame

`cmd/cli/main.go:1315` · tools · correctness · CONFIRMED

- **Defect:** Without -play, the loop breaks as soon as a flushed frame has any
  lit pixel, before the -framedir dump below it. The frame directory therefore
  only ever holds black frames.
- **Failure:** `runktf game.zip -framedir out` writes tickNNNN.png files only
  for all-black presents, then stops at the first real frame without writing it.
  framestats/contactsheet over `out` report a blank boot.
- **Evidence:** main.go:1313-1325: `if !play && frameLit(frame) > 0 { break }`
  comes before the writePNG to frameDir.
- **Verification:** In the loop at main.go:1313-1325, without -play the code
  breaks on `!play && frameLit(frame) > 0` before the -framedir writePNG. After
  the loop only -frame (framePath) is written (:1418-1425). -key, -touch, -speed
  and -cheat all imply -play (docs/cli.md:236-237), so a probe -framedir run
  only ever writes the black frames before the first lit present.
- **Fixed:** The probe loop writes the frame to -framedir before stopping at the
  first lit frame.

### R143 Native-package runktf exits 0 even when a tick failed

`cmd/cli/native.go:322` · tools · error-handling · CONFIRMED

- **Defect:** runKTFNative records a failed tick as tick_error in the summary
  but always returns 0. The descriptor-package path returns
  exitForRun(tickError, ErrGuestExited), and runKTF's comments say sweeps rely
  on that exit code.
- **Failure:** A sweep runs `runktf` over a directory. An earlier-generation KTF
  package faults on tick 3. The run prints tick_error and exits 0, so a batch
  driver reading exit codes counts the crashing title as passing.
- **Evidence:** native.go:250-253: tickError is set and the loop breaks.
  native.go:308-310: tick_error goes into the summary. native.go:322: return 0.
  Compare main.go:1515-1519 and exitForRun at main.go:1568-1576.
- **Verification:** runKTFNative records tick_error in the summary
  (native.go:308-310) and then returns 0 unconditionally (:322). The descriptor
  path returns exitForRun(tickError, ErrGuestExited) (main.go:1519).
  exitForRun's comment (:1550-1566) says batch sweeps rely on the exit code. A
  failed native tick therefore exits 0.
- **Fixed:** runKTFNative returns `exitForRun(tickError, ktf.ErrGuestExited)`,
  so a failed frame exits 1 after the summary.

### R144 Console numbers with a leading zero parse as octal

`internal/cheat/console.go:629` · tools · correctness · CONFIRMED

- **Defect:** parseNumber uses strconv.ParseInt(s, 0, 64), so a decimal typed
  with a leading zero is read as octal, and a number that fails falls back to
  hex. The value written to guest memory differs from the one typed, which
  Encode's "a typo'd cheat value that becomes a different number is worse than
  an error" is meant to prevent.
- **Failure:** `set 0x1234 010` or `freeze 0x1234 0100` writes 8 or 64 instead
  of 10 or 100. `scan = 0250` searches for 168.
- **Evidence:** console.go:627-639: ParseInt base 0, then a ParseUint base-16
  fallback. value.go:156-158: the intent stated on Encode.
- **Verification:** parseNumber (console.go:627-639) uses strconv.ParseInt(s, 0,
  64), so '010' is 8 and '0250' is 168. When that fails it falls back to
  ParseUint(s, 16, 32), so '019' becomes 0x19 = 25. The function's comment
  promises decimal and 0x hex, and Encode's comment (value.go:156-158) states
  the intent to reject a value that silently becomes a different number. Only
  the text console is affected (CLI -cheat or a raw console command). The panel
  passes a numeric operand.
- **Fixed:** A console number with a leading zero is decimal (010 = 10);
  0x/0b/0o prefixes and bare hex such as ffff0000 parse as before. An address
  with a leading zero is zero-padded hex (08048000), as it was before for one
  with an 8 or a 9 in it (P15 of the PR #234 review).

### R145 runlgt/runskt/SGS runs write saves without claiming the directory

`cmd/cli/main.go:2523` · tools · concurrency · PLAUSIBLE (reviewer said medium)

- **Defect:** runktf, run and provision take backend.ClaimSaveDirectory, and so
  do the server's sessions, which also hold a file lock meant to exclude CLI
  tools. runLGT, runSKT and runScript open a DirectorySaveStore on the same
  default tree without any claim. A CLI run and a browser session of the same
  title therefore write one directory at once, and the last writer silently
  wins.
- **Failure:** A player has an LGT title open in the browser from `make run`
  (debug profile, saves in var/savedata/debug/lgt/&lt;owner>). A diagnostic
  `runlgt game.zip -route r.txt` starts on the same archive. The server's claim
  and lock do not stop it, because it never tries to claim. Both write the same
  save files and the player's progress is overwritten by the scripted run's, or
  the reverse, with no error.
- **Evidence:** main.go:2523-2525 and :2529-2532: the LGT saveRoot defaults to
  platformSaveRoot("lgt")/owner with no claim. main.go:374-381: SKT uses
  NewDirectorySaveStore(saveRoot) with no claim. script.go:41-51 does the same
  for SGS. Compare checkpoint.go:175-196 (claimArchiveSaves, used by runKTF at
  main.go:1060 and by runShared) and main.go:2957. webhost/saveclaim.go:37-41:
  the lock excludes CLI tools "that claim the same directory".
- **Verification:** In the code the claim holds. runLGT (main.go:2523-2525),
  runSKT (:374-381) and runScript (script.go:41-51) open a DirectorySaveStore on
  the default tree without ClaimSaveDirectory. runKTF (:1060, via
  claimArchiveSaves), runShared (checkpoint.go:74) and provision (:2957) do
  claim. It is a documented, known limitation, though: docs/cli.md:192-194 says
  '`runktf`, `importsaves` and `provision` also take this claim; `runskt` and
  `runlgt` do not.' It only matters when someone runs a dev or diagnostics CLI
  on the same title, in the same profile tree, while a browser session has it
  open. Impact is lower than claimed.
- **Fixed:** runskt, runlgt and SGS runs claim their save directory with
  backend.ClaimSaveDirectory (exit 1 when busy) and release it only after the
  game is destroyed or closed.

### R146 align >= 2^32 truncates to 0 and panics the first sweep

`internal/cheat/scanner.go:259` · tools · correctness · PLAUSIBLE (reviewer said high)

- **Defect:** setAlign accepts any int64 >= 1 and SetAlign stores it as int.
  sweep then converts it with uint32(scanner.align), so a multiple of 2^32
  becomes 0, and `address % align` panics with integer divide by zero. The
  page's console runs on the webhost session goroutine with no recover, so the
  whole server process dies.
- **Failure:** Over the session socket, or in a CLI -cheat run, send `align
  4294967296` and then `scan u` (no type token, so SetValueType does not reset
  align). sweep reads the first region, evaluates chunkBase % 0 and panics. In
  the server the panic is unrecovered on the runner goroutine, so every
  connected and parked game is killed, along with any guest writes not yet
  persisted.
- **Evidence:** console.go:401-405: parseNumber with only `align < 1` rejected,
  then SetAlign(int(align)). scanner.go:157-158: align = max(align, 1).
  scanner.go:236: align := uint32(scanner.align). scanner.go:259: address %
  align. webhost/session.go:1313-1331 calls console.Execute directly, and grep
  finds no recover() in internal/webhost. Only internal/session/guard.go guards
  platform calls, and the cheat path bypasses it.
- **Verification:** The divide by zero is real. setAlign accepts any value >= 1
  (console.go:401-405). On 64-bit, SetAlign keeps 4294967296 as an int
  (scanner.go:157-158), uint32() turns it into 0 (scanner.go:236), and `address
  % align` at :259 panics. A same-type `scan u32 unknown` does not reset the
  alignment (console.go:416-421). The claim that the server process dies is
  wrong, though. runCheat runs synchronously on the HTTP handler goroutine:
  serveSession → runner.run → r.loop → r.handle (session.go:139, :483-487,
  :698). cmd/server/main.go:438 and appserver.go:116 both use net/http, whose
  conn.serve recovers handler panics even on hijacked connections. Only that
  runner aborts: its game is neither parked nor closed, and it keeps its save
  claim. The trigger is a crafted console command the page never sends, or an
  absurd value typed in a CLI -cheat run (dev tool). Low.
- **Fixed:** `align` accepts only 1 to 4096 and SetAlign clamps to that range,
  so the sweep stride can no longer truncate to zero.

### R147 readRecords treats a corrupt line as end of file

`internal/tools/acceptance/record.go:553` · tools · error-handling · PLAUSIBLE

- **Defect:** Any Decode error, not only io.EOF, ends the loop and returns the
  records read so far with a nil error. A truncated or damaged record file is
  therefore accepted as complete.
- **Failure:** A record file cut off mid-line is used for `-since auto` or
  `-compare`. Every archive after the cut is reported as "No longer in the
  corpus" or "New in the corpus", and the cache plans from a partial run without
  any warning.
- **Evidence:** record.go:551-555: `if err := decoder.Decode(&line); err != nil
  { break }`, then `return run, archives, nil`.
- **Verification:** The defect is real: any Decode error, not only io.EOF, ends
  the loop at record.go:551-555 and returns nil. Reachability is narrow.
  writeRecords (:521-537) is the only writer: it is called once at the end of a
  run (main.go:274) and writes the whole file in one pass. A truncated file
  needs a failed write, such as a full disk or a kill while writing. In that
  case sourceOf (main.go:302) or load (:321) would accept the partial file
  without warning.
- **Fixed:** readRecords stops only at io.EOF; any other decode error, such as a
  truncated line, is returned.

### R148 Keyboard keys reach the game while the confirm dialog is open

`web/app.js:612` · web · correctness · CONFIRMED

- **Defect:** The document keydown handler does not check whether the page's
  confirm dialog is open. The gamepad path does (`dialogOpen()`). With the
  default binding Space→OK, pressing Space while the '다시 시작' question is up
  sends OK to the running game. Because the handler calls preventDefault, Space
  also cannot activate the dialog's focused 취소 button. Arrow keys likewise move
  the game behind the modal.
- **Failure:** During play, the player opens 🔄 재시작, and askToConfirm focuses 취소.
  The player presses Space to cancel. The keydown handler maps Space to 'OK',
  calls preventDefault, and latches OK, which is sent to the game. The dialog
  stays open and the game receives a stray OK press, which may confirm an
  in-game menu item.
- **Evidence:** app.js:591-623 keydown handler: no dialog check; `name = ...
  keyboardMap[event.code]` then `event.preventDefault()` and `holds.latch`;
  keybindings.js:49 `OK: "Space"`; app.js:645 `dialogOpen` exists but is used
  only for pads (app.js:653-655); confirm.js:40 focuses the cancel button and
  only handles Escape (confirm.js:54-59).
- **Verification:** The keydown handler at app.js:591-623 never checks whether
  the confirm dialog is open, while the pad path does (dialogOpen() at
  app.js:645, used at :653-654). With the restart dialog up, focus is on #confirm-cancel (confirm.js:40). That button has no data-key, so the guards at
  :592, :596 and :598 do not apply, and `focused` at :609 is null.
  keyboardMap['Space'] is 'OK' (keybindings.js:49), so :614 calls
  preventDefault, which stops Space from activating 취소, and :622 latches OK to
  the running game. confirm.js:54-59 only intercepts Escape. The restart button
  is shown only while playing (app.js:770), so this path is real. Enter is
  unbound by default and still activates 취소; Space and the arrow keys go to the
  game. Desktop keyboard only, hence low.
- **Fixed:** The keyboard handler returns early while the confirm or text-input
  dialog is open (`dialogOpen()`), so Space and Enter press the dialog's focused
  button and no key reaches the game behind it.

### R149 Speed/screen menus show another game's values after the picker resets

`web/app.js:917` · web · correctness · CONFIRMED

- **Defect:** The speed and screen menus are refreshed only on the picker's
  'change' event and once at init. initGameSelect runs again on every transition
  to 'ready': after a wake, an `online` event, or an add/remove. It sets
  `select.value = lastGame()` programmatically, which fires no change event. The
  menus then keep showing the previously selected game's stored values while a
  different game is selected, and that game's own stored size and speed are what
  start actually uses.
- **Failure:** lastGame is A, stored at 176x220; the player selects B, and the
  menu shows B's 240x320. The tab is hidden and shown again, so wake() →
  connecting → ready → initGameSelect resets the picker to A, but the menu still
  says 240x320. The player presses 실행 and A starts at 176x220 although the
  settings panel says 240 × 320. The same happens when lastGame is no longer
  listed (for example after deleting it): the first option is selected while the
  menus show the deleted game's values.
- **Evidence:** app.js:915-918 `select.value = previous` (no change event);
  app.js:771-778 sessionStateChanged('ready') → `void initGameSelect()`;
  app.js:1177-1181 and 1195-1202 showStoredSpeed/showStored are bound only to
  the select's 'change' event; app.js:826-828 start uses `storedScreen(path)`
  for the selected path.
- **Verification:** The menus are refreshed only at init and on the select's
  'change' event (app.js:1177-1181, 1195-1202). Every transition to 'ready'
  re-runs initGameSelect (app.js:771-778). That includes returning to the tab:
  suspend() is a no-op outside 'playing', and wake() in state 'ready' runs
  change('connecting') → resume → answer() → change('ready')
  (session-link.js:84-124). initGameSelect rebuilds the options and sets
  `select.value = previous` (lastGame) at app.js:915-918, which fires no change
  event. lastGame is only written on start (app.js:936, :839), so a selection
  that was made but never started is replaced while the menus still show the old
  game's speed and screen. Start then uses storedScreen(select.value)
  (app.js:826-828, :928). A deleted lastGame gives the same mismatch: the first
  option ends up selected while chosenGame() at init returned the stale path.
  Editing the menu writes to the correct game (chosenGame() at
  app.js:1204-1206), so only the displayed value is wrong.
- **Fixed:** After rebuilding the picker and selecting the last game,
  `initGameSelect` dispatches a `change` event so the speed and screen menus and
  the delete button show the game that is actually selected.

### R150 Any keypad resize turns rapid fire off and drops held input

`web/keypad-editor.js:407` · web · correctness · CONFIRMED

- **Defect:** The board's ResizeObserver calls `releaseInput()` on every size
  change. That includes `rapidFire.reset()`, which sets the mode back to 'off'.
  Toggling 전체화면 hides the keypad (`display: none`), which resizes the board, so
  entering fullscreen silently turns off rapid fire the player just enabled.
  Rotating the phone or resizing the window does the same.
- **Failure:** A controller player presses L3 (default RAPID_FIRE) to turn on 연사
  auto, then R3 (default FULLSCREEN). `:root.fullscreen .button-container {
  display: none }` shrinks the board to 0x0. The ResizeObserver callback runs
  releaseInput() → rapidFire.reset(), so mode becomes 'off' and the auto key is
  released. The keypad that shows the 연사 label is hidden in fullscreen, so the
  player cannot see that auto-fire stopped.
- **Evidence:** keypad-editor.js:407-411 `new window.ResizeObserver(() => {
  releaseInput(); gesture = null; measure(); }).observe(board)`; app.js:690-696
  releaseInput calls `rapidFire.reset()`; rapid-fire.js:78-87 reset sets `mode =
  "off"`; style.css:1189-1192 hides .button-container in fullscreen;
  gamepad.js:124-125 default b10=RAPID_FIRE, b11=FULLSCREEN; the rapid-fire.js
  header lists only input loss, editing and session transitions as reset points.
- **Verification:** keypad-editor.js:407-410 calls releaseInput() on every
  ResizeObserver callback for #keypad-grid. releaseInput (app.js:690-696) calls
  rapidFire.reset(), which sets mode to 'off' (rapid-fire.js:78-86).
  style.css:1189-1192 sets `:root.fullscreen .button-container` (the board's
  parent, index.html:159-160) to display:none. An element that stops being
  rendered reports a 0x0 size to ResizeObserver, so entering or leaving
  fullscreen fires the callback and turns rapid fire off. gamepad.js:124-125
  binds b10 to RAPID_FIRE and b11 to FULLSCREEN by default, and
  fullscreenChanged (app.js:384-395) does nothing to restore the mode.
  docs/rapid-fire.md lists the reset points (blur, hidden/pagehide, session
  transitions, keypad layout changes or editing, native text input). Fullscreen
  and window or orientation resizes are not among them, and the keypad that
  carries the 연사 label is hidden in fullscreen. Since the switch fails toward
  off, low.
- **Fixed:** The keypad's ResizeObserver no longer calls `releaseInput()`, so
  fullscreen, rotation or a window resize keeps held keys and the rapid-fire
  mode; it only re-measures and drops the editor's drag gesture.

### R151 Raw English browser network errors shown to players

`web/save-backup.js:163` · web · rule-violation · CONFIRMED

- **Defect:** When a fetch fails at the network level, the page shows the
  browser's own `TypeError` message to the player as status text, for example
  'Failed to fetch', 'Load failed' or 'NetworkError when attempting to fetch
  resource.'. The rule is that player-facing text is Korean. Only HTTP error
  responses get Korean fallbacks.
- **Failure:** The server stops or the Wi-Fi drops while the picker is open. The
  player presses 세이브 내보내기: `exportSave` → fetch rejects → busy() catch →
  `onStatus(error.message)` shows 'Failed to fetch'. The same happens for adding
  a game (each failed file's reason) and for the game list (reportError →
  setStatus).
- **Evidence:** save-backup.js:162-163 `catch (error) {
  onStatus?.(error.message); }`; save-backup.js:53 and :61 bare `fetcher(...)`;
  add-game.js:48-49 `failed.push({ name, reason: error.message })` and
  add-game.js:179-180; app.js:891 fetch games.json → app.js:948-949
  `reportError(error)` → setStatus(message).
- **Verification:** exportSave and importSave (save-backup.js:53, :61) call
  fetch directly. The service worker does not handle api/ requests or POSTs
  (service-worker.js:75-84 returns early), so a network failure arrives as the
  browser's TypeError and save-backup.js:163 shows its English message ('Failed
  to fetch' / 'Load failed') through setStatus. The add-game upload path does
  the same (add-game.js:48-49, POST, also not handled by the worker). The
  games.json example is partly masked: it is a GET outside /api/, so
  service-worker.js:97-100 serves the cached copy when there is one, and
  reportError only sees the TypeError when nothing is cached. The save buttons
  stay usable in the 'offline' state (app.js:779-785 only disables game-start),
  so this can be reached on a LAN or desktop setup. It is rare in the phone app,
  where the server runs in-process.
- **Fixed:** Fetches for save export/import, game upload/delete and games.json
  go through a `reach` helper (web/reach.js) that turns a network-level
  rejection into a Korean message and keeps the browser error as `cause`.

### R152 Reconnect backoff never resets after reconnecting without a game

`web/session-link.js:69` · web · correctness · CONFIRMED

- **Defect:** `delay` is reset to 1 s only when an answer carries `started`. A
  successful reconnect that answers 'ready' or 'occupied' keeps the doubled
  delay. After one outage, every later automatic reconnect on the picker waits
  up to 15 s, even when the server is back immediately.
- **Failure:** A player at the game picker sees the server restart (about 10 s
  down). Retries happen at 1, 2, 4 and 8 s, the page reconnects and the answer
  is 'ready', but delay stays at 15000. An hour later a brief network blip
  closes the socket again. lost() → again() schedules the retry 15 s out, and
  the player looks at '서버 연결을 기다리고 있습니다' for 15 s although the server answered
  within a second.
- **Evidence:** session-link.js:42-46 again() schedules with `delay` then
  doubles it (cap 15000); session-link.js:60-73 answer(): `delay = 1000` only in
  the `response.started` branch; the 'ready' branch (69-72) and 'occupied'
  branch leave delay unchanged.
- **Verification:** session-link.js:42-46 again() schedules a retry with `delay`
  and then doubles it (cap 15000). The only reset is `delay = 1000` in the
  response.started branch at :64-68; the 'ready' branch (:69-72) and the
  'occupied' branch (:61-63) leave it alone. After an outage at the picker that
  ends with a 'ready' answer, delay stays at 15000. The next lost() → again()
  then waits 15 s. A blip that fires the browser's 'online' event is spared,
  because app.js:793 calls wake(), which runs clearRetry(). A server restart or
  a socket drop without a network-state change waits the full backoff.
  session-link.test.mjs has no test that pins the backoff reset.
- **Fixed:** The reconnect backoff resets to 1 s on any server answer (ready,
  occupied or started), not only when a game resumes.

### R153 Quick save/load failure note is erased at once on disconnect

`web/checkpoint.js:53` · web · error-handling · PLAUSIBLE

- **Defect:** If a quick save or quick load fails because the connection
  dropped, the failure note is shown and then cleared in the same turn. The
  `finally` block calls `refresh()`, and `refresh()` clears the feedback
  whenever the state is not 'playing', which it no longer is by then. The player
  never sees that the quick save failed.
- **Failure:** The player presses 퀵세이브 and the socket drops mid-save (or a
  heartbeat ping fails and session-link discards the socket). The GameSession
  close handler rejects the pending quickSave, then calls onClosed, which leads
  to change('offline') and refresh(), which clears the note. In the microtask
  after that, the catch sets '퀵세이브 실패: 세션 연결이 끊어졌습니다.' and the finally's
  `refresh()` clears it again straight away. The player saw '퀵세이브 저장 중…'
  disappear and may assume the checkpoint exists, then later quick-load an older
  state.
- **Evidence:** checkpoint.js:22-31 refresh(): `if (!playing || editing())
  feedback("")`; checkpoint.js:49-53 catch sets the failure feedback, then
  `finally { busy = false; refresh(); }`; session.js:130-141 rejects pending
  before `handlers.onClosed`; session-link.js:47-53 lost() calls
  change('offline'); app.js:753-754 sessionStateChanged calls
  checkpointControls.refresh().
- **Verification:** The trace is right. session.js:130-141 rejects pending
  requests and then calls handlers.onClosed in the same synchronous turn. That
  reaches session-link.js:47-51 lost() → change('offline') → app.js:754
  checkpointControls.refresh(), which clears the note (checkpoint.js:30). In the
  next microtask the catch at checkpoint.js:49-50 sets the failure note, and the
  finally's refresh() at :53 clears it straight away because state() is
  'offline'. The impact is smaller than claimed, though. On 'offline',
  app.js:784 shows the status '서버 연결을 기다리고 있습니다…', so the player is told the
  connection dropped. After a reconnect, checkpoint.js:65-67 started() resets
  `available` from the server's has_checkpoint, so quick load reflects what the
  server really has. Only the specific '퀵세이브 실패' wording is lost, and clearing
  the note outside play is a deliberate rule in refresh().
- **Fixed:** A quick save or quick load failure note stays for its 8 s through
  the offline, connecting and playing transitions of a dropped connection;
  returning to the picker, a new action or the keypad editor still clears it.

## Review of PR #234 (2026-10-17)

`/code-review` at extra-high effort read about 9,000 changed lines of PR #234
(the two rounds above) and reported 15 findings, numbered P01 to P15 here in
the order it ranked them. All 15 were fixed on the same branch, each with a
regression test. Most were first run against the code before the fix and
failed there; P01, P08 and P09 changed the shape of the code they test, and the
slice hook of P05 is reached only through the helper it calls, so those tests
pin the new behavior instead.

- **P01** `cmd/cli`: the end-of-run `destroyApp` (runskt) and Clet close
  (runlgt) ran after the run's interrupt context had fired, with nothing to
  cancel them, so a destroy that hung ignored every further Ctrl-C. **Fixed:**
  `endRun` gives the step its own interrupt context; a Ctrl-C cancels it and,
  if it does not return within two seconds, abandons it so the process exits.
- **P02** `internal/api/midp/rms.go`: a kept-updated enumeration whose current
  record was deleted could not say it was between records, so the previous step
  wrapped to the last record or skipped one. **Fixed:** a negative `position`
  holds the gap; deleting the first record leaves nothing behind, deleting the
  last nothing ahead.
- **P03** `internal/jvm`: `synchronized (Foo.class)` locked the canonical Class
  object and `static synchronized` the class monitor, so they did not exclude
  each other. **Fixed:** locking, waiting on and notifying a Class object use
  its class monitor; a checkpoint from an earlier build that holds both is
  refused. Java fixture `ClassLock`, and a ninth check in the SKT JVM contracts
  JAR.
- **P04** `internal/platform/ktf/native_events.go`: a second listener
  registration on a source replaced the first, so a fresh session dispatched to
  the newest while a restored one still dispatched to the first. **Fixed:** the
  first registration stays the one called, as it always was; a later one is
  accepted and not recorded, so the list still does not grow.
- **P05** `internal/platform/ktf/runtime_file.go`: File writes held in a
  DataOutputStream batch stayed held when the thread stopped at its slice
  limit inside the call, where other threads and a checkpoint could see them
  missing from the store, and a refused batch left other Files on the path with
  the refused bytes. **Fixed:** what a batch holds is stored at the end of every
  worker slice and before a checkpoint capture, and putting a path back counts
  as a write so every File reads it again.
- **P06** `internal/backend/save_names.go`: in a converted folder the encoded
  name always won, so progress an earlier build wrote under the raw name after
  a downgrade was shadowed for good. **Fixed:** of the two, the one written
  later is the save; a read moves it over the encoded file and a listing or an
  export chooses it the same way.
- **P07** `internal/webhost/session.go`: a start was refused for any owner
  `saveDirectory` rejects, but only SKT owners were mapped, so a repacked KTF or
  LGT archive with `:` or `?` in its PID no longer started. **Fixed:** KTF and
  LGT owners are mapped the same way (`backend.AcceptedSaveOwner`), and the
  command line's raw directories move to them.
- **P08** `internal/platform/skt/skt.go`: the owner move checked the target and
  renamed without claiming it, so a second tab's empty directory could be
  replaced under a live session, and a raw owner `.` made the platform root the
  source. **Fixed:** `backend.MigrateSaveOwner` claims both directories, checks
  the target under the claims, and never follows `.`, `..` or the reserved
  directory.
- **P09** `internal/webhost/checkpoint.go`: the quick-load check, documented as
  read-only and run without the claim, moved save directories, and every check,
  backup and start parsed the archive again. **Fixed:** `session.Inspect`
  reports the earlier owner, the Hosts move from that without a second parse,
  and the quick-load check reads the slot where it is (`session.SaveDirectory`).
- **P10** `internal/backend/save_names.go`: only upper-case letters were
  marked, but case-insensitive file systems fold more: on the APFS volume here
  `ß` and `ss`, `ﬁ` and `fi`, the Kelvin sign and `k`, `ς` and `σ`, `ǅ` and `ǆ`
  were each one file. **Fixed:** any other letter with case outside ASCII is
  written by its number, `#+` and six lower-case hex digits; what is written
  as it is is ASCII or has no case. Names a converted folder already holds in
  the earlier form are found as earlier names and moved on first use.
- **P11** `internal/platform/lgt/elf.go`: the new file-range overlap check did
  not exempt empty sections, so a zero-size section whose offset fell inside
  `.text` refused the module. **Fixed:** empty sections overlap nothing, in
  memory or in the file.
- **P12** `cmd/cli`: `runktf` refused `-speed` for the earlier KTF package,
  whose session takes one. **Fixed:** the multiplier is passed through.
- **P13** `internal/backend/save_names.go`: every operation read the names
  record again, and telling `Save` from `save` listed the directory each time.
  **Fixed:** the record is known by its identity, size and time once read, and
  a listing is kept until its directory's modification time changes.
- **P14** `internal/platform/skt/skvm_file.go`: `exists`, `filesize` and a
  removal kept a node for every path ever named, absent ones included.
  **Fixed:** a path that is not there is answered without being kept.
- **P15** `internal/cheat/console.go`: a zero-padded hex address made only of
  decimal digits parsed as decimal after R144. **Fixed:** an address with a
  leading zero is hex; a value keeps R144's decimal reading.

## Remaining limits found while fixing

The fixes surfaced these limits, none of them a reviewed finding. Each is a
candidate for later work, and the TODO lists them. The ones the TODO listed were
taken up on 2026-10-17; see [Follow-up of the remaining
limits](#follow-up-of-the-remaining-limits-2026-10-17).

- **Host names (R125).** Passed by decision. Closing DNS rebinding needs an
  operator-configured host list (for example a `-hosts` option or
  `WFEATURE_HOSTS` in cmd/server); refusing unknown names by default would break
  `.local`, router DNS and reverse-proxy hostnames.
- **Case conversion (R118).** Names that differ only in Unicode normalization
  still share a file on macOS; characters Windows refuses in a name are not
  handled; a name of more than about 127 upper-case letters exceeds the 255-byte
  limit once written as `#` and the letter; an earlier version opened on a
  converted folder does not find the keys with an upper-case letter or `#`,
  whose files are intact under their written names.
- **SKT file names.** `FileSystem.remove` of a missing name throws nothing
  although the specification says IOException, and `unlink` of one answers 0, as
  before.
- **LGT files.** In a session with no store, a write is seen only while a buffer
  holds it; a save whose name differs only by case from a packaged file is not
  added to the created list, so `MC_fsList` may not show it; an input stream
  keeps a copy once its `File` is closed; the `__databases__.db` index shares
  the guest namespace.
- **LGT threads.** Starting an unseen Thread subclass has no concurrent-thread
  cap, and more than 16 threads alive at once makes checkpoints unloadable.
- **iOS save import.** `accept=".wfs"` may be greyed out in the iOS Files picker
  because mobile/ios/Info.plist declares no type for it.
- **Android.** A recreated activity's old web view is not destroyed (it stays
  hidden and parks its session, so it is harmless), and BACK on a task started
  from the home screen, which Android 12 and later move to the back, is not
  exercised; an app finished and relaunched within seconds can still find the
  old server draining on port 11541.
- **Cheat console.** The web host still routes the raw console op, which the
  page never sends; the page does not yet show the new Korean message when an
  unknown-value scan is refused.
- **SKT LCDUI.** Ticker natives are not under `textMu`; the vendor XTextField
  keypad path still ignores input constraints; `EventQueue.SOFT3` (92) is not
  declared; getRGBPixels with |scanlength| < width can do up to len(array)^2
  writes.
- **SKT quick saves from before this branch** keep Card and V3/A3 bounds under
  the bare keys, so a getfield on a restored object still reads 0 (getX/getY and
  the 3D transforms read them through the fallback).
- **SKT owner migration** does not move a raw owner whose live directory is
  missing because a quick load was interrupted part-way; those saves stay under
  the old reserved directory.
- **JVM.** `static synchronized` methods lock `vm.classMonitors` rather than the
  Class object, so they do not exclude `synchronized(Foo.class)`;
  superinterfaces with default methods are not initialized; there is no global
  heap budget.
- **Audio.** A quick save from an earlier build that holds an out-of-range sound
  event restores, and playing it marks the recorder invalid as before.
- **gdb stub.** A manual session (break, continue, Ctrl-C, detach, reconnect)
  has not been run against a real gdb client.

## Follow-up of the remaining limits (2026-10-17)

The limits above that the TODO listed were taken up on 2026-10-17 on the branch
`fix/review-followups-2026-10-17`, from `eb93831` (`main` after #234). Every
code change came with a regression test first run against `eb93831`. Host names
(R125), the earlier SKT quick saves and the audio leftover were not on that
list and are unchanged.

The gates pass: gofmt, go vet (default, debug and Windows), make test, make
test-debug and go test -race ./internal/.... Every local archive (536) ran 500
ticks with `eb93831` and with the branch: every LGT title ended on the same
frame, and the KTF and SKT titles that did not varied as much when `eb93831`
was run twice (an animation's phase under load). A sample of 50 archives
through the stages of `make acceptance` reached the same rung on both.

- **Case conversion (R118).** Fixed. The save store also writes by its number
  (`#+` and six hex digits) every character a host folds or refuses: a
  character outside ASCII that Unicode normalization changes or joins to the
  one before it, seven Greek capitals that normalization turns into others, the
  controls below 0x20 and `<>:"\|?*`, a `.` or space at the end of a segment,
  and the first letter of a Windows device name (`con`, `prn`, `aux`, `nul`,
  `conin$`, `conout$`, `com0` to `com9`, `lpt0` to `lpt9`, and `com` or `lpt`
  with `¹`, `²` or `³`). Every encoded name is its own NFC form. On the APFS
  volume of the development Mac each raw pair (`é` and `e`+U+0301, `;` and
  U+037E, U+F900 and U+8C48, `가` and its jamo, `が` and `か`+U+3099, U+0386
  and U+1FBB) was one file and each pair of their encoded names two. A name
  reads back as a key only when that key is written as exactly that name, so
  the encoding is a bijection. A key with a segment longer than 255 bytes once
  written out has no save: a read finds none, and a write, a batch or a restore
  is refused up front with the limit named, where the file system's error used
  to reach a KTF title as a failed read that blocked every later save of the
  session; a name an earlier build stored stays readable and writable where it
  is. A v0.5.2 folder converts the new names on its first use; a folder the
  first form of the encoding (`eb93831`) converted keeps its record,
  `.wfeature-names#1`, reads that form's names as their keys and moves each one
  when it is read. A copy of the 372 real debug save folders (1,196 keys)
  converted from v0.5.2 and from the first form with no mismatch. An earlier
  version opened on a converted folder still does not find names with a changed
  character, which the release notes say.
- **SKT file names.** `FileSystem.remove` of a missing name, or of one it cannot
  remove, throws the specification's `IOException`; `XFile.unlink` keeps
  answering 0 and -1. A scan of the 91 distinct local SKT archives found 3
  `remove` call sites, each behind the title's own existence check and inside a
  handler, and 44 `unlink` call sites in 17 titles, each discarding the answer
  ([SKVM](skvm.md)).
- **LGT files.** Fixed, with the on-disk layout and the record formats
  unchanged:
  - The database index is no guest path: `fileSaveKey` refuses
    `__databases__.db` and anything beneath it, the index is read and written
    by its own key, and `openDataBase` refuses the name `__databases__`.
  - A `File.close`, a stream's flush or a stream's close that the store refuses
    throws the `IOException` each declares, as SKT's WIPI streams do, and the
    `File` stays open with the write waiting. Closing a closed `File` is
    nothing, where it stopped the run.
  - A `DataBase` the collector reclaims while it holds a write the store
    refused is stored at once, or kept for the ending, the quick steps and the
    next open of the database. A reclaimed `File` was already stored at its
    close or kept its handle.
  - The created and removed lists record and read only paths that have a save
    key.
  - A session with no store keeps what it stores in memory for the session, up
    to 64 MiB, so a write is seen after its close.
  - A save whose name differs only by case from a packaged file is recorded in
    the created list, and `MC_fsList` shows it.
  - A stream holds its file open after its `File` closes, reading or writing the
    file as it is now until the stream closes.
- **LGT threads.** Fixed. The 16-thread bound counts the stacks handed out and
  is met at every `Thread.start`, whichever way the thread was built, so no
  session maps more stacks than a checkpoint restores. A start past it throws
  an `OutOfMemoryError` the title can catch, and building a `Thread` is not
  counted. No local Java title ran more than two threads at once.
- **iOS save import.** `Info.plist` exports `com.movingwoo.wfeature.save-backup`
  for `.wfs`, conforming to `public.data`, with no MIME type. The WebKit of the
  iOS 26.5 simulator maps an accepted extension through its MIME type and offers
  every file when none maps, so a backup was not greyed out there; the picker
  has not been looked at on a phone.
- **Android.** Fixed. `onDestroy` removes and destroys the activity's web view,
  whether the activity is rebuilt or finished. A finished activity's server is
  kept until it exits, and the next activity starts a server only after that,
  with no time limit; the start-up wait takes `/api/status` only from a wfeature
  server that is a child of the app. BACK on a task started from the launcher
  was exercised: it only stops the activity, and the game stays parked through
  the app's freeze and drains once at shutdown. The lifecycle harness passed 19
  of 19 checks three times, and `eb93831` failed its old-page and relaunch
  checks.
- **Cheat console.** Fixed. The web host answers only the panel's operations: a
  text console line from a page is refused as an unknown operation, and only a
  test server (`Server.textConsole`) runs one. A refused unknown-value search
  already reached the page as an error in the panel's Korean words, which the
  page shows in its message popup above the panels; a test now pins the server
  half.
- **SKT LCDUI.** Fixed. The Ticker natives run under `textMu`; the vendor
  XTextField keypad path applies the constraints its constructor states, as
  TextBox does (an UNEDITABLE field takes no key, a numeric one stays in digit
  mode); `EventQueue.SOFT3` is declared as 92; getRGBPixels writes each array
  element once with the same result, so overlapping rows cost at most the array.
- **Save owner migration** (all three platforms since P07). Fixed. An earlier
  owner whose directory a stopped quick load set aside is settled first and
  then moved; nothing is merged into an existing owner directory.
- **JVM.** Fixed. Initializing a class now initializes, after its superclass,
  each superinterface that declares a default method, its own superinterfaces
  first (JVMS 5.5). The VM accepts class files up to version 70, so a title
  could have one; the 2,232 local class files are versions 45.3 to 47.0 and
  none does. Calling a default method is still not implemented. Each VM has a
  heap budget (`Options.MaxHeapBytes`, 256 MiB by default): objects, arrays,
  library strings and StringBuffer storage are charged through sampled weak
  pointers, an allocation that would pass it after a collection throws a
  catchable `OutOfMemoryError`, and a restored heap is not charged, so a
  checkpoint always loads. The largest live heap measured over 93 SKT and 294
  KTF runs was 10 MB, and the cost per allocation is one atomic subtraction
  outside a sample.
- **gdb stub.** A session with lldb 2103, which speaks the GDB remote protocol,
  ran on a KTF title: break, continue to the breakpoint, Ctrl-C (stopped in 0.17
  s), detach with the game running on, and reconnect. It found that a client
  connecting to a running game was told it had stopped while the game ran on,
  and read an empty register file; `?` now stops the game at its next
  instruction. lldb needs a target definition for the classic ARM register
  file, which the stub does not describe ([CLI](cli.md)); gdb itself was not
  run.

These follow-ups left limits of their own:

- LGT: `Thread.start` on a thread already started ends the session, because the
  specification's `IllegalThreadStateException` is not in the platform's class
  set. `DataBase` insert, update and close still take a refused store without an
  exception, since a title that retries an insert would add the record twice. A
  refused emptying or creation at an open is not reported to the title. A
  title's own `DataInputStream` around `File.openInputStream()` does not follow
  the file.
- KTF's and LGT's `FileSystem.remove` of a missing name throw nothing.
- SKT `getRGBPixels` and `setRGBPixels` read their last argument as an element
  stride where the specification says bytes per line; the one local title that
  has them passes 1, from a method nothing calls. A `getstatic` of a field the
  platform does not declare reads 0 without an error, which is how the missing
  `SOFT3` went unnoticed.
- A new key with a segment longer than 255 bytes once written out (more than
  127 upper-case letters, or more than 31 characters written by their number)
  is refused where v0.5.2 stored it under its raw name; no local key is near
  it.

## Review of PR #235 (2026-10-18)

`/code-review` at max effort read PR #235 (`0ba8f2b`, the follow-up above)
against `eb93831` and reported 15 findings, numbered Q01 to Q15 here in the
order it ranked them. Q16 to Q22 are findings its verifiers also confirmed or
found plausible that fell past its cap of 15. Most were reproduced by probe
tests against both commits; the probes are not in the repository. All of them,
and the candidates, were taken up the same day; see [Follow-up of the review of
PR #235](#follow-up-of-the-review-of-pr-235-2026-10-18). The two reports the
verifiers refuted are under [Refuted](#refuted), and the candidates no verifier
checked follow the list.

- **Q01** `internal/platform/lgt/java_file.go:486` (also :506): closing a
  truncate-mode (mode 3) `File` hands its handle to the streams still open on
  it without clearing `openFile.truncated`. A quick save taken while such a
  stream lives records a finished save as a rewrite in progress, and a later
  quick load cuts the path's shared buffer to that length
  (`checkpoint_storage.go:436-441`): a longer save written after the
  checkpoint, by the title or by a `.wfs` import, reads back cut, and the next
  append loses its tail for good. `eb93831` closed the handle at `File.close`
  and kept the whole newer save. Suggested fix: clear the flag when the handle
  passes to a stream.
- **Q02** `internal/backend/save_names.go:184`: runes that are not
  `BoundaryBefore` (Hangul medial and final jamo, kana voicing marks) are
  written by their number, which breaks Hangul and kana saves on volumes that
  list names decomposed (HFS+, and exFAT on macOS). `planSaveNames` turns the
  NFD spelling it lists into a numbered-jamo name (`fs/가나` becomes
  `fs/ᄀ#+001161ᄂ#+001161`), and the title's NFC key, taking the fast path at
  :1020, never finds it. On HFS+ and exFAT disk images even this build's own
  fresh `StoreSave` read back as missing; an earlier build's save, a restored
  export and an APFS folder copied from HFS+ do the same, where `eb93831` read
  them. The macOS package lets the save folder live on any drive. Suggested
  fix: NFC-normalize listed names before planning or decoding them.
- **Q03** `internal/platform/lgt/wipic_file.go:1016`: `markFileCreated` now
  reads the created list before its `packagedAs` check, while
  `prepareIssuedWriteLists` (`checkpoint_storage.go:198`) still skips that
  read for packaged paths, so a quick save that stores a packaged file reads
  `fs/.created` on the live client. If the read fails, the sticky
  `saveReadError` refuses the capture and stops the title at its next native
  call, and a second dirty packaged file is marked clean without being stored,
  so a later quick load loses its write. `eb93831` read nothing and stored
  both. Suggested fix: check `packagedAs` first, or have the pre-read cover
  packaged paths.
- **Q04** `internal/jvm/heap_budget.go:150`: `sample()` re-arms the countdown
  before deciding to refuse and never charges a refused sample's weight, so
  once the budget is full every sampled allocation is refused while the ones
  between samples pass the fast path (:134, :227) uncharged. A title that
  catches `OutOfMemoryError` grows without bound while `HeapUsage` stays at the
  limit: under an 8 MiB budget a node loop kept 95 MB (11.3 times the budget)
  with `HeapUsage` at 8.14 MB, and at the default 256 MiB the live heap passed
  the limit at about 2.6 MB/s with no ceiling, where [JVM](jvm.md) promises at
  most about 256 KiB over.
- **Q05** `internal/jvm/builtins.go:1167`: the text the `String` constructors
  store is never charged: from `char[]` (:1167), `byte[]` (:1116, :1119, :1124,
  `builtins_lang.go:894`, :897), `StringBuffer` (`builtins_lang.go:922`) and
  `String` (:1076). The only charge is `objectHeapBytes` at `new`
  (`vm.go:929`), weighed before the text exists. Under a 4 MiB budget, 400
  `new String(buf)` from one 50,000-character array kept 60,000,000 bytes with
  `HeapUsage` at 0 and no refusal, where `String.valueOf(buf)` was refused
  after 12.
- **Q06** `mobile/android/src/com/movingwoo/wfeature/MainActivity.java:163`: a
  reopened activity waits for the ended server with `previous.waitFor()` and no
  limit, but the server's drain does not bound closing a parked game:
  `internal/webhost/resume.go:118-121` drops parked games synchronously before
  its 10 s deadline loop, `cmd/server` waits on `<-games` with no timeout, and
  SKT `destroyApp` gets new step windows until it returns. An SKT title whose
  `destroyApp` never returns, parked by the page's `visibilitychange` before
  `onDestroy`'s SIGTERM (BACK finishing the activity on Android 8 to 11),
  leaves the reopened app on '이전 실행을 마치는 중...' until its process is
  killed. The server-side hang predates the PR, which turned it into a screen
  that never moves; LGT is bounded at about 3e9 steps (minutes), KTF at 15 s.
- **Q07** `internal/platform/skt/media_progress.go:94`: the SKT host audio tick
  (`AdvanceAudio`, `observePlayerLocked`, `queuePlayerTimeLocked`) boxes the
  event time with `VM.NewObject(java/lang/Long)`, which this PR made
  refusable; a refusal there reaches `runtime.fail("advance Player")`
  (`media.go:95`) instead of the title and ends the session. Reproduced at tick
  340 under an 8 MiB budget, while refusals in guest code in the same run were
  absorbed. The PR's own rule is that a host allocation is charged and never
  refused (`NewString`, `NewAOTArray`).
- **Q08** `internal/jvm/corelib_util.go:1050`: `growHashtable` stores the grown
  keys array before allocating the values array, so an `OutOfMemoryError` from
  the second `newArray` (refusable since this PR, `vm.go:1131`) leaves keys at
  2n+1 and values at n, and growth, gated only on `size < keys.Length()`, is
  never retried: every later put of a new key throws
  `ArrayIndexOutOfBoundsException` ('index 8 for length 8'), even after
  `clear()`. With the default sampler 15 of 60 refusals hit that window. MIDP
  `Timer` has the same shape: `internal/api/midp/timer.go:213` marks the task
  queued before the refusable `NewObject(TimerThread)` at :222, so the task
  never runs and rescheduling it throws `IllegalStateException`.
- **Q09** `internal/backend/save_names.go:187` (plausible): a trailing `.` or
  space is now written by its number, but Win32 stored such a key under the
  stripped name, and no lookup tries that spelling (`earlierSaveSpellings`
  :389, `exactSavePath` :1036, `findEarlierSave` :1095 through `spelledExactly`
  :961). On Windows a v0.5.2 save for `fs/save.`, or for a space-padded
  `jdb/SAVE    `, is not found after the upgrade, and the next store writes
  `fs/save#+00002e` beside it. The new tests skip Windows; no local title is
  known to use such a key.
- **Q10** `internal/platform/lgt/java_thread.go:396`: the 16-stack start bound
  also counts workers that finished but are still listed. `runOtherJavaWorkers`
  (a platform thread's `Object.wait`) and the monitor-enter grant mark workers
  done without dropping them, and only the `ServiceJavaThreads` defer prunes.
  A `startApp` that loads 17 resources one thread at a time (`start()`, then
  `wait()` on a lock) gets `OutOfMemoryError` '16 guest threads are already
  running' with none alive, and the title does not start. `eb93831` ran all 20
  cycles, though it mapped 20 stacks and refused later quick saves.
- **Q11** `internal/gdbstub/session.go:157`: `?` to a live target that is not
  parked goes through `run()` and waits, with no timeout, for the guest's next
  ARM instruction. A client attaching while the host runs no guest code
  (`runktf -serve -gdb` waiting on stdin, an idle title, a guest that ended)
  gets no stop reply: lldb's `gdb-remote` fails after about 20 s with 'no stop
  reply packet was received' (`eb93831` attached in 0.1 s), gdb times out,
  Ctrl-C only re-arms the step, and the late `S05` or `S09` is read as the
  reply to a later packet. Suggested fix: answer `?` from the last registers
  when no guest thread is inside `core.Run`, and arm the stop for the next
  entry.
- **Q12** `internal/gdbstub/session.go:113`: routing `?` through `run()`
  exposes its hang-up race on the first packet every client sends. With the
  client's EOF already queued, `run()` and `Serve`'s deferred `endClient()`
  both finish before the resume goroutine, which then sets `idle=false` and
  `StepOnce`, so the guest parks with no client attached and the game stays
  frozen until another client attaches; the hook waits only on `wakeup`, so
  Ctrl-C cannot end the CLI. A client that sent `$?#3f` and closed at once froze
  the game in 45 of 50 rounds at `0ba8f2b` and 0 of 50 at `eb93831`; `s` had
  the same race before the PR.
- **Q13** `internal/jvm/vm.go:1316`: the budget's `OutOfMemoryError` can now be
  raised inside class initializers, Go-written ones included that could not
  fail before (`List` and `AlertType` in `internal/api/midp/lcdui.go:180` and
  :146, `Boolean`, `System`, the SKVM toolkit). The failure is cached in
  `initErrors` for good and `CaptureHeapState` (`heap_state.go:189`) refuses
  every later quick save, so a caught OOM is not fully recoverable: after the
  552-byte `Command` allocation in `List.<clinit>` was refused and the title
  freed its memory, every use of `List` rethrew the cached error. It needs the
  refusal to land on that allocation (about 0.2%).
- **Q14** `internal/backend/save_names.go:362`: `saveNamesFit` and
  `exactSavePath` (:979) measure a segment in UTF-8 bytes, but APFS and NTFS
  allow 255 UTF-16 units, so a save an earlier build stored under a 256 to
  509 byte name whose encoding is unchanged (86 to 169 Hangul syllables) can
  no longer be read, written or restored, and `findEarlierSave` (:1104) skips
  it. `ReadSaveTree` still exports it, and `WriteSaveTree` refuses that export
  whole, so none of the backup's other saves are restored. `eb93831` read,
  overwrote and restored it. Very unlikely in practice.
- **Q15** `internal/platform/lgt/wipic_file.go:1044`: `packagedAs` compares the
  title's raw path bytes with the archive's decoded entry keys
  (`archive.go:370`), so a C Clet that rewrites a packaged Korean-named file
  under its EUC-KR spelling puts that path on the created list (`eb93831`
  asked `archive.Resource`, which decodes EUC-KR), and `MC_fsList` shows the
  file twice, in later sessions too. It needs a Linux or Android host or a
  session with no store; macOS refuses EUC-KR names.
- **Q16** `internal/platform/lgt/java_file.go:478` (low): a read-only
  `File.close` throws `IOException` while another handle's refused write waits
  on the same path; the `File` closes once the store recovers. C `MC_fsClose`
  has behaved the same since before the PR.
- **Q17** `internal/platform/lgt/wipic_file.go:700` (low): `MC_fsRename` onto a
  name with no save key (newly `__databases__.db` in this PR) answers success,
  and the file is under neither name, with or without a store, while the
  removal list hides the source for good; `eb93831` moved the file. Suggested
  fix: refuse when `!hasSaveKey(newName)`.
- **Q18** `internal/platform/lgt/wipic_file.go:1216` (plausible):
  `markFileRemoved` and `parseFileList` skip names with no save key while
  `archive.Resource` accepts a backslash spelling, so
  `MC_fsRemove("data\\hello.txt")` no longer hides the packaged file, and the
  entries an earlier build wrote for such names are erased. No title using a
  backslash is known; the specification uses `/`.
- **Q19** `internal/platform/skt/text_input.go:390` (plausible, deliberate):
  `holdDigitMode` keeps a vendor XTextField with a NUMERIC or DECIMAL
  constraint in digit mode, so `-` and `.` cannot be typed (`eb93831` gave `-3`
  and `1.5` where the head gives `111111113` and `115`). It copies TextBox's
  rule (R113), and every known call site passes constraint 0.
- **Q20** `internal/jvm/heap_budget.go:186` (low, cost): every refused sample
  runs a full `runtime.GC()` under `budget.mu`, even when the sample alone is
  over the limit: 30 refusals ran 30 collections of about 7.8 ms each, and a
  concurrent VM ran about 30% slower. `System.gc()` already allows the same.
  Suggested fix: refuse at once when the pending charge and the weight pass the
  limit, and throttle the collection.
- **Q21** `internal/platform/lgt/java_layout.go:102` (low): LGT maps
  `OutOfMemoryError` to `Error`, skipping `VirtualMachineError`, so
  `catch (VirtualMachineError)` does not catch the new `Thread.start` refusal.
  The mapping predates the PR, which makes it reachable; it needs 16 stacks and
  such a catch.
- **Q22** `internal/api/wipi/definitions.go:65` (low): `EventQueue.SOFT3` reads
  0 after a quick save from before this branch is restored, because statics
  are restored whole and the defined constants are seeded only at
  initialization; `SOFT1` and `SOFT2` changed in #234 the same way. Only quick
  saves from development builds carry it, and no key is delivered as 92.

Not verified (no verifier checked these; some predate the PR; each held and is
fixed in the follow-up):

- `internal/platform/lgt/java_stream.go:426`: closing a title's
  `DataInputStream` around a `File` stream after the `File` closed leaves the
  inner stream's handle open until a collection (probes by several finders: one
  handle at the head, none at `eb93831`).
- `mobile/android/src/com/movingwoo/wfeature/MainActivity.java:145` and :153:
  `finished` is per activity, so a recreated activity's stale start thread can
  start a server after its successor finished and orphan it; the unbounded
  wait widens the window. A finishing activity's late `onDestroy` (:605) can
  stop the server its successor adopted, mostly as before the PR.
- `internal/webhost/session.go:1338`: `CheatConsole()` now runs for every cheat
  message, creating the console and giving panel-saved tables the image key.
- `internal/platform/lgt/wipic_file.go:845`: `storeless()` counts the
  certificate-100 adapter over no store as storeless though it keeps every
  write in its own map, so files are held twice and the 64 MiB cap can refuse a
  write the adapter already holds.
- `internal/backend/save_names.go:331`: `readSaveName` accepts numbers for `.`,
  `\` and NUL, so a raw `#+00002e` decodes to `.` and `SnapshotSaves` fails
  whole. `saveNamesFit` (:360) checks segments but not the path limit (1,024
  bytes on macOS), as `eb93831` did not either.
- `internal/jvm/vm.go:1343`: a static written during a superinterface's
  `<clinit>` recursion is reset to 0 (a javac 1.8 fixture: 5 on HotSpot, 0
  here); it needs class file version 52. `vm.go:1404` loads every
  superinterface, so `ClassCensus` lists unused interfaces as missing.
- `internal/platform/ktf/runtime_input_method.go:126` and :184: the pending
  character is cleared before a refusable `NewArray`, so a refusal drops it.
- `internal/jvm/aot.go:251`: an AOT instance is charged about 550 bytes though
  its fields live in the guest arena, which could take a KTF title with many
  small objects past the budget.
- `internal/jvm/string_intern.go:34`: interned literals are charged through
  `NewString`, though [JVM](jvm.md) says they are not.
- `internal/platform/lgt/java_database.go:742`: a refused index store loses the
  database name from `listDataBases` for good, though the rewritten comment
  says it recovers (predates the PR).
- Before the PR: SKT `getRGBPixels` reads the screen without its lock
  (`internal/platform/skt/wipi.go:539`, seen by `-race`); `contextARGB` puts
  alpha in the top byte (`wipi.go:556`); `FileSystem.remove` returns normally
  when the store refuses (`skvm_file.go:840`); non-UTF-8 keys are written raw,
  colliding as U+FFFD on Windows and refused on macOS (`save_names.go:184`);
  NTFS 8.3 short names may alias saves; a Windows `ERROR_INVALID_NAME` reads as
  an error (`save_names.go:982`).

## Refuted

The verifiers refuted these reports. They are kept so that the same claims are
not raised again without new evidence.

- `internal/jvm/vm.go:1137` Array checkcast/instanceof ignore array covariance:
  The behaviour matches the code (vm.go:1133-1138), but it is a documented,
  deliberate limitation. docs/jvm.md:39-41 states: 'An array is an instance of
  its own type, Object, Cloneable and Serializable only (no covariance)'.
  docs/jvm.md:509 lists 'array covariance in assignability checks' under
  'Deliberately incomplete'. The KTF AOT path makes the same choice
  (platform/ktf/aot.go:850-853). This is a known scoped gap, not a new defect.
- `internal/platform/ktf/native_events.go:420` Frame-less native titles never
  get save flushes or sound/timer events: The premise that later-generation
  modules run frame-less is contradicted by the documented behavior.
  docs/history/ktf.md:4220-4246 says the later module posts to itself at
  start-up, and once that post arrives it registers its own 80ms frame and the
  existing loop runs. The early return at native_events.go:420 covers only the
  bootstrap window before that frame exists. After that, advanceSound,
  advanceTimed and FlushSaves (:426-452) run on every frame. NativeSession.Close
  also flushes through closeSaves (native_session.go:378-386). A title that
  never registers a frame is hypothetical.
- `internal/platform/lgt/graphics.go:279` newFramebuffer leaks the pixel block
  when the surface record allocation fails: newFramebuffer does return without
  releasing the pixel block when mapSurface fails (graphics.go:268-281). The
  failure scenario, a title that keeps retrying, cannot happen. Every caller
  returns that Go error unchanged: wipic.go:549-552, wipic_image.go:97-99 and
  124-126, java_screen.go:535-537, java_stream.go:293-295 and 332-334. None
  turns it into a return code or a Java exception. absorbUncaughtCallback only
  absorbs a javaUncaughtThrow (uncaught.go:36-43). The error then reaches
  Session.Tick, and the webhost ends the game on any tick error
  (webhost/session.go:505-508). The leaked block can never be observed by a
  later allocation in the same run.
- `internal/webhost/session.go:740` Start closes the running game before
  admission can refuse or ask: The page that is served never sends start while a
  game is running. session-link.js:145-146 rejects start unless state ===
  "ready". "ready" is set only after a stop reply (170), a declined confirmation
  (155), a non-started answer (71) or an exit (108). On the server an exit
  already ran endGame, then stopGame (session.go:505-517). A declined
  confirmation leaves the page in "ready" with no game, because nothing was
  running when it asked. The WebSocket's Origin check keeps foreign pages out.
  stopGame at line 740 therefore has nothing to close on any path the client
  takes.
- `internal/webhost/saveclaim.go:152` Parked holder is closed before the
  replacement claim is known to succeed: The scenario cannot occur. A parked
  game's claim holds the exclusive, non-waiting 'session' kernel lock and the
  in-process gate for that directory (backend.ClaimSaveDirectory,
  save_lock.go:20-80), so no CLI tool or second server can hold it at the same
  time. dropLocked closes the game and releases the claim synchronously
  (resume.go:163-169), and takeSaveClaimLocked runs right after under parkedMu.
  Only a microsecond race with another process could make the re-claim fail. The
  admitStart case evicts a parked victim in a different directory, which the
  user has explicitly approved with a dialog warning that unsaved progress will
  be lost (startapproval.go:62-83).
- `cmd/cli/main.go:1075` Native KTF runs silently ignore -touch, -park, -speed,
  -profile-from: This is documented behaviour. docs/cli.md:243-248 says that for
  the earlier package '`-touch`, `-park` and the multiplier of `-speed` are not
  passed to that package's loop', next to the list of refused flags.
  -profile-from has no effect without -profile, which is refused for the native
  package (main.go:1077-1081), and profileFrom is read only when profiling
  (:1307). The native.go:21-27 comment ('refused by name rather than ignored')
  has drifted from the documented contract, but the user-facing docs state the
  behaviour the finding calls silent.
- `mobile/ios/src/App.swift:180` Save export stages the page-supplied name as a
  path unsanitized: The page's name cannot contain a path separator. games.json
  builds each path as PathEscape(group) + '/' + PathEscape(entry.Name)
  (webhost/games.go:107-114), where entry.Name is a filesystem basename from
  gameroot.Entries. PathEscape encodes '%' as %25 and '/' as %2F. backupFileName
  (web/save-backup.js:40-48) takes the last '/' segment and decodes it once,
  which gives back exactly that basename. A file literally named
  '..%2F..%2Fx.zip' decodes to '..%2F..%2Fx' and contains no slash. The message
  handler is reachable only from the app's own loopback page.
- `internal/jvm/heap_budget.go:153` A refusal weighs the charged total and the
  sample's weight rather than the allocation's size, so with the live set near
  the limit every sampled small allocation is refused: this is the documented
  estimator, centred on the limit; the effect is small beside the estimator's
  own spread and out of reach at the default 256 MiB.
- `internal/gdbstub/core.go:130` Every attach parks the guest and the hook
  ignores the context, so Ctrl-C cannot stop the CLI while a debugger is
  attached: the hook ignored the context before PR #235, and `eb93831` behaves
  the same after `?` and `s`; stopping on a bare attach is the documented
  intent, and an interrupt is delayed, not lost.

## Follow-up of the review of PR #235 (2026-10-18)

Every finding above, Q01 to Q22, and every candidate in the "Not verified" list
was taken up on 2026-10-18 on the same branch,
`fix/review-followups-2026-10-17`, from `0ba8f2b`. Each candidate was reproduced
first, and each one held. Every code change came with a regression test first
run against `0ba8f2b`, where it fails with the reported symptom or is still
waiting at its deadline. Q19 is kept by decision, and Q13 departs from JVMS 5.5
on purpose.

The gates pass: gofmt, go vet (default, debug and Windows), make test, make
test-debug and go test -race ./internal/.... Every local archive (536) ran 60
and then 500 ticks with `0ba8f2b`, twice, and with the branch. Every LGT title
(130) ended on the same frame. Of the KTF and SKT titles, 14 ended on a frame
neither `0ba8f2b` run had (2 KTF, 12 SKT); run again side by side, each build
gave frames the other also gave, the phase of a title's own animation under
load, which the 31 titles whose two `0ba8f2b` runs disagreed already show.

- **Q01** Fixed. A truncate-mode `File` closed while a stream is still open on
  it has finished its rewrite: the flag is cleared when its handle passes to
  the stream, so a quick save taken then records an ordinary handle, and a
  quick load keeps whole a longer save written after it. At `0ba8f2b` that save
  read back as `a lon`, the length the `File` had held.
- **Q02** Fixed ([save keys on disk](session.md#save-keys-on-disk)). An entry
  whose name is decomposed and whose composed name opens the same file is read
  under the composed name wherever names are listed: planning a conversion,
  listing a directory, a snapshot, an export and a restore's pruning. On disk
  images made with `hdiutil`, HFS+ and exFAT read 3 of 4 fresh saves and 1 of 4
  earlier ones at `0ba8f2b`, and on FAT the conversion stopped; each now reads 4
  of 4, as APFS did. Verifying it found the AppleDouble companions macOS writes
  beside each file on exFAT and FAT (`._` and the file's name): a snapshot
  there listed 11 entries for 4 keys. A companion is no save now, and a file of
  that name that is not AppleDouble still is. A decomposed key an earlier build
  stored on APFS reads as its composed key, which that volume made one file
  with it; a name with a singleton such as the Greek question mark is read as
  it is.
- **Q03** Fixed. `markFileCreated` asks `packagedAs` before it reads the created
  list, so a quick save that stores rewritten packaged files reads nothing on
  the live session. At `0ba8f2b` an unreadable list refused the quick save and
  every write of the session after it.
- **Q04** Fixed. A refused sample keeps its sampling point: every allocation
  after a refusal is weighed in turn until one fits, and only an admitted
  sample draws the next point, while the charge is within the limit. Under a 4
  MiB budget a title that catches the error and goes on kept 53,413 of 60,000
  small objects where 7,598 fit (and spent 28.9 s in 6,589 collections); it now
  keeps 7,903.
- **Q05** Fixed. The text a `String` constructor builds from a `char[]`, a
  `byte[]` (each charset path) or a `StringBuffer` is charged when it exists,
  and a refusal leaves the String unset; one built from another String shares
  that one's text, which adds no memory. Under a 4 MiB budget, 400 copies of a
  20,000-character text were all kept before and are now refused within the
  budget.
- **Q06** Fixed, on each side. An SKT `destroyApp` that a close asks for may
  run for 15 seconds, as long as KTF lets a Jlet's run (its service wait):
  then no step window is renewed, the machine's waits and sleeps are let go,
  and the call ends. An LGT `destroyClet` gets the same 15 seconds, where it
  ran under a context that never ended. Stopping the server closes each parked
  game on a goroutine of its own, which keeps the game's save folder claimed
  until the close returns, and waits for those closes and for the attached
  games within the drain's 10 seconds; a close still running then is named in
  the log and left behind. A reopened Android activity waits 30 seconds for the
  server a finished one ended and then ends it (`destroyForcibly`). The tests
  use a `destroyApp` that loops, one that waits for a notification nobody
  sends, and an LGT fixture whose `destroyClet` branches to itself: at `0ba8f2b`
  the SKT destroy and the LGT close were still running after 10 seconds and
  `CloseSessions` after 5; now each ends at its bound, and `CloseSessions` at
  its deadline, with the claim held until the parked game's close returns 15
  seconds later. The Android lifecycle check has a step for it (F, see
  [testing](testing.md)): on the Android 17 emulator, with the hang fixture
  parked and the app opened again right after BACK finished it, the old server
  of `0ba8f2b` was still there 255 seconds after `onDestroy` and the app still
  waiting, while the branch's ended by itself 10.1 seconds after `onDestroy`
  and the next server started after it. The hang needs a game already parked
  when the drain begins: finished with the game attached, the activity reaches
  the drain before the page parks it, and `0ba8f2b` closed that game through
  its runner within the deadline too.
- **Q07** Fixed. `VM.NewHostObject` charges an object the host makes and never
  refuses it, as `NewString` and `NewAOTArray` do, and the SKT audio tick boxes
  a Player event's time with it. Of the 47 places a platform or API package
  allocates, every other one is inside a guest's native call, where a refusal
  reaches the title (the KTF input method's `char[]` among them, which lost a
  character to one; below), or is the MIDlet's construction at start, which
  meets a fresh budget.
- **Q08** Fixed. `Hashtable` grows, and is made, with both arrays allocated
  before either is published, so a refusal leaves it as it was and the next put
  grows it; at `0ba8f2b` the test left 287 keys over 143 values. MIDP `Timer`
  makes the task's worker before it marks the task queued, checks again under
  its lock, and unmarks the task if the worker cannot start; with only that
  change reverted, the task stayed marked scheduled with no worker.
- **Q09** Fixed, for reads. On Windows a key with a `.` or space at the end of
  a segment that finds nothing under its own names reads the name without the
  ending, where Win32 stored it. The file is never moved, because it is also
  the save of the key without the ending, and the key's first write goes to its
  own name. The test forces the Windows rule on every host.
- **Q10** Fixed. A thread that has ended gives its stack back at the next
  `Thread.start`, whichever call saw it end, so the 16-stack bound counts only
  threads a checkpoint restores. 48 loaders started one at a time by a
  `startApp`, each ending inside the platform thread's wait or lock, ran on one
  stack and the session saved and loaded; 16 threads running at once still
  refuse the seventeenth.
- **Q11** Fixed. `?`, and a Ctrl-C during a continue, wait at most 250 ms for
  the guest's next instruction. A guest that is running stops within it and is
  answered with the registers it stopped with; when no guest code runs (a run
  waiting on its driver, a session between ticks, a long host call) the stop is
  answered anyway from the registers of the last stop and stays armed, and the
  guest parks at its next instruction without a second reply, so no late `S05`
  answers a later packet. A continue sent before the guest gets there runs past
  that stop, and a step stops at it. A guest whose run has ended answers `W00`,
  or `X09` after a kill, instead of a stop reply, and `k` has no reply, as the
  protocol says. lldb 2103 attached to `runktf -serve -gdb` waiting on stdin in
  0.32 s, where `0ba8f2b` failed after 20.55 s with "no stop reply packet was
  received".
- **Q12** Fixed. Each connection carries the count of clients that had ended
  when it attached, and a resume whose client has ended since does nothing, so
  a `?`, `s` or `c` overtaken by its client's hang-up can no longer stop the
  guest after the client has gone; a client's end also drops a step it armed.
  Fifty clients each that sent `?`, `s`, `?` and `s`, or `?` into a reset
  connection, and closed at once, left the guest running every time (`0ba8f2b`
  hung), and the run ended on a cancel with nobody attached.
- **Q13** Fixed, departing from JVMS 5.5 on purpose ([JVM](jvm.md)): an
  initializer that ends in the budget's `OutOfMemoryError` leaves its class
  uninitialized rather than erroneous, so the next use initializes it from the
  start and quick saves are taken again. At `0ba8f2b` the capture was refused
  with 'JVM heap has an active or failed initializer'.
- **Q14** Fixed. A segment is measured as the host measures a name: in UTF-16
  units on macOS, iOS and Windows, 255 of them, and in bytes elsewhere. On the
  development Mac a name of 255 Hangul syllables (765 bytes) was a file and one
  of 256 was not. A name of 150 syllables, refused at `0ba8f2b`, is stored and
  read, and so is one an earlier build stored.
- **Q15** Fixed. `packagedAs` turns the title's path into an archive key as the
  archive lookup does (EUC-KR decoded, a backslash a separator, the path
  cleaned), so a C Clet that rewrites a packaged Korean file under its own
  bytes rewrites the packaged file, which stays off the created list, and
  `MC_fsList` names it once.
- **Q16** Fixed. A handle opened only to read has nothing of its own to store,
  so its close, in C or in Java, succeeds while another handle's refused write
  waits; the write is stored by its own handle's close.
- **Q17** Fixed. A rename onto a name with no save key answers the
  specification's `M_E_BADFILENAME` (-3), and the file stays where it was; at
  `0ba8f2b` it answered 0 and the file was under neither name.
- **Q18** Fixed. LGT's save key reads a backslash as the separator, as the
  archive lookup does, so removing `data\hello.txt` hides the packaged file in
  this session and the next, and a write of a backslash path, which had no save
  before, is stored under the slash path. An entry an earlier build wrote with
  a backslash still hides that spelling, and a rewrite of the list keeps it.
- **Q19** Kept, by decision. A vendor XTextField with a NUMERIC or DECIMAL
  constraint stays in the handset's digit mode on the keypad, and `-` and `.`
  come from the page's text input, which a test now pins (`-3`, `1.5` and
  `-0.25`); [native text input](native-text-input.md) says so.
- **Q20** Fixed. A refusal that no collection could help is made without one;
  the first refusal in a row and then the second, fourth, eighth and so on run
  `runtime.GC`, and between them the samples are looked at only after Go's
  collector has finished a cycle. 30 arrays larger than the budget ran 30
  collections and now run none; 51 refusals while full ran 53 and now 7 (9 in
  the process).
- **Q21** Fixed. LGT's `OutOfMemoryError` is a `VirtualMachineError`, as in
  CLDC, so a `catch (VirtualMachineError)` takes the refused `Thread.start`; no
  other class in the platform's exception table was placed differently from
  CLDC.
- **Q22** Fixed. A restore seeds again the defined constants of every class it
  restores as initialized, in name order, once the statics are adopted, so a
  quick save from before a constant changed reads this build's value: `SOFT1`,
  `SOFT2` and `SOFT3` read 21, 22 and 0 at `0ba8f2b` and now 90, 91 and 92.

The candidates no verifier checked all held, and are fixed:

- **A `DataInputStream` around a `File` stream.** Closing the wrapper lets go
  at once of the file every object sharing the stream holds; at `0ba8f2b` one
  handle stayed open until a collection.
- **Android activities.** Only the activity created last starts or ends a
  server: a start still in flight for an older activity launches nothing, and
  a finished activity's late `onDestroy` leaves alone the server a newer one
  took over.
- **`CheatConsole()`.** The web host makes the text console only for a text
  console line on a test server, so a panel operation neither creates it nor
  gives a table the panel saves the image key.
- **The certificate-100 adapter over no store** keeps only the file it rewrites,
  and the session keeps the title's other files itself, within its bound,
  once.
- **Names for segments no key holds, and long paths.** A name that reads as
  `.`, `..`, or a segment with `\` or NUL is no encoded name, so a raw
  `#+00002e` reads as an earlier build's key of its own and a snapshot no
  longer fails over it. A key whose path below the save folder is longer than
  the host opens (1,023 bytes on macOS and iOS, where 1,024 gave
  `ENAMETOOLONG`, and 4,095 on Linux and Android) has no save: a read finds none
  and a write is refused with the limit named.
- **Statics stored during a superinterface's initializer.** A class's statics
  are prepared and its constants set before its superclass and superinterfaces
  are initialized, so a store from their initializers stands: the javac 1.8
  fixture reads 5, 7 and 6, as on HotSpot, where `0ba8f2b` read 0, 7 and 0. The
  default-method walk probes a superinterface without listing it as missing.
- **The KTF input method** takes the composed character only after its
  `char[]` exists, so a refused delivery keeps it for the next.
- **AOT instances** cost about 222 bytes here (232 with the binding) and were
  charged 552; they are charged 224.
- **Interned literals** went through `NewString` and were charged for the VM's
  life; the pool, which has bounds of its own, makes them uncharged, as
  [JVM](jvm.md) says.
- **The LGT database index.** An index the store refuses waits in the session:
  `listDataBases` reads it first, the next create starts from it, and the next
  create, the ending and the quick steps store it again; a quick load drops it
  with the rest of the session's state.
- **Before the PR.** SKT `getRGBPixels` reads the screen under its lock
  (`-race` reported the race at `0ba8f2b`) and answers `0x00RRGGBB`, as the
  specification and KTF do, where it put alpha in the top byte.
  `FileSystem.remove` throws an `IOException` when the store refuses the
  removal, which stands and is stored at the ending. A byte that is not UTF-8
  is written as `#-` and two hex digits, so macOS holds the save, and restores
  a backup carrying one, which it refused whole, and Windows no longer makes two
  such keys one. A `~` before a digit is written by its
  number, so an NTFS short name cannot reach another save. On Windows
  `ERROR_INVALID_NAME` reads as no save rather than as an error.

These follow-ups left limits of their own:

- A stopping server still leaves behind a title whose ending takes between 10
  and 15 seconds: the drain's deadline is shorter than the platforms' bound. A
  parked game that a start, a takeover or a second park of its token ends is
  still closed under the server's lock, so one whose ending never returns holds
  the lock for those 15 seconds.
- Q09 and `ERROR_INVALID_NAME` have not run on Windows: the first is tested
  with the Windows rule forced on, the second only compiles there (`go vet`).
- The gdb stub has still been driven by lldb only.
