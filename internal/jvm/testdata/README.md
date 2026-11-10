# JVM fixtures

Every source here is authored for `wfeature`, and every class file is its Java 8
(major version 52) output, compiled without debug information. Tests embed the
class files, so a changed source needs its classes rebuilt. One command rebuilds
all of them; with `javac 1.8.0_504` it reproduces the committed files byte for
byte:

```sh
javac -encoding UTF-8 -source 1.8 -target 1.8 -nowarn -g:none \
  -d internal/jvm/testdata internal/jvm/testdata/*.java
```

`-encoding UTF-8` is required: several sources have non-ASCII comments, and
under a non-UTF-8 locale (`LC_ALL=C`, for example) `javac` refuses them.

| Source | Classes | What it verifies | Test |
|---|---|---|---|
| `Arithmetic.java` | `Arithmetic` and four nested classes | arithmetic, both switch formats, objects, arrays, a guest thread, exceptions | `javac_fixture_test.go` |
| `Constructed.java` | `Constructed` | `VM.NewObject` invoking a guest constructor | `construction_test.go` |
| `CallLoop.java` | `CallLoop` | the guest call path with the Host out of the way: instance, static, native and allocating calls inside one execution | `BenchmarkGuestCallLoop*` in `interpreter_bench_test.go` |
| `Stores.java` | `Stores` | the store observer: one method performs an instance-field, a class-static and an array store, and all three are reported and attributed to it | `store_observer_test.go` |
| `Streams.java` | `Streams` | interface dispatch and assignability across the `java.io` streams: a record written through a `DataOutput` parameter and read back through a `DataInput` one | `streams_test.go` |
| `CoreMembers.java` | `CoreMembers`, `CoreMembers$Source` | the class-library link surface: members a compiled title resolves through their class (the char-array append, the boxed flag and its published instances, the character replace, and a stream subclass reading the protected buffer), each of which once had a body and no declaration | `corelib_fixture_test.go` |
| `Inherited.java` | `Inherited`, `InheritedBase` | field resolution: a superclass writes an instance field and a static, and the subclass reads both under its own name, as a compiler emits it | `corelib_fixture_test.go` |
| `InterfaceIdentity.java` | `InterfaceIdentity` and seven nested types | inherited and diamond interfaces through interpreted `instanceof`, `checkcast` and `invokeinterface`, including nulls and an unrelated cast. Native metadata tests separately cover cyclic and over-budget graphs | `instanceof_test.go` |
| `StringIdentity.java` | `StringIdentity`, `StringIdentity$Peer` | literal sharing across calls and classes, `ConstantValue`, explicitly interned receivers and distinct String construction, and that retained strings keep those relationships through heap restore | `string_identity_test.go` |
| `NestedText.java` | `NestedText` and three nested classes | library callbacks on the caller's execution: a synchronized `toString` reached through a concatenation, `String.valueOf` and `StringBuffer.append` from a synchronized caller, a class initializer formatting its own instance, and `readUTF` over a stream whose `read` is synchronized | `native_state_test.go` |
| `StaticInit.java` | `StaticInit` and four nested types | `getstatic`/`putstatic` initialize the declaring class or interface only: an interface array constant read through an implementing class, and an inherited static read and written through a subclass | `class_init_test.go` |
| `LibraryContracts.java` | `LibraryContracts` and four nested classes | class-library contracts a title's error handling relies on: `EOFException` at the end of `DataInput`, canonical `Class` objects, `wait(long, int)`, a sleep of `Long.MAX_VALUE`, `setLength` and `multianewarray` bounds, null `String` arguments, a shared `StringBuffer`, `InputStreamReader` after a bad byte, an empty exception message, one interrupt per sleep, and strict number parses | `corelib_fixture_test.go` (the fixture), used by `builtins_lang_test.go`, `streams_test.go`, `monitor_test.go`, `guest_thread_test.go`, `exception_test.go`, `value_array_test.go` and `heap_state_test.go` |
| `ThreadRestart.java` | `ThreadRestart` | a second `Thread.start` on the same object: refused once its run has returned unless the VM is built with `RestartableThreads`, and refused while it is still running either way | `guest_thread_test.go` |
| `LoneSurrogate.java` | `LoneSurrogate` | a string constant holding half a surrogate pair loads, and reads back as U+FFFD | `classdef_test.go` |
| `SuperinterfaceInit.java` | `SuperinterfaceInit` and seven nested types | initializing a class runs its superclass's initializer, then those of the superinterfaces that declare a default method, each one's own superinterfaces first, and not those of interfaces with only abstract or static methods | `class_init_test.go` |
| `StaticOrder.java` | `StaticOrder` and four nested types | a class's statics are prepared before its superclass and default-method superinterfaces initialize: a store their initializers make into the class whose initialization started the chain stands, and the class's own initializer still runs after its superclass's | `class_init_test.go` |
| `HeapBudget.java` | `HeapBudget`, `HeapBudget$Node` | the heap budget: arrays, small objects and a `StringBuffer` kept until a catchable `OutOfMemoryError`, garbage many times the budget never refused, one array larger than the budget refused before it is made, a restored heap larger than the budget, `setLength` zeroing reused storage, a title that goes on allocating after each refusal, the text `String` constructors build, and the collections refusals run | `heap_budget_test.go`, `builtins_lang_test.go`, `class_init_test.go` |
| `LibraryMembers.java` | `LibraryMembers` and twelve nested types | the CLDC 1.1 members KTF and SKT share, linked through a compiled constant pool: `String.regionMatches`, `startsWith(String, int)` and `valueOf(boolean)`; `StringBuffer`'s capacity, its other insert forms and `reverse`; `Vector`'s searches from an index and its text; `Hashtable`'s `contains`, `rehash` and text; the readers' lock and mark pair; `Writer` and `OutputStreamWriter`; the static `readUTF(DataInput)` over a `DataInput` of its own; `ByteArrayOutputStream`'s text; and `Class.newInstance` with both refusals. Each method answers one line, comparing the runtime with itself wherever the charset decides the bytes | `corelib_fixture_test.go`, `builtins_lang_test.go` (`made`, for a caller in another package); also packaged in `internal/platform/skt/testdata/library-members.jar` |

## Checkpoint fixtures

| Source | What it verifies | Test |
|---|---|---|
| `NativeCheckpointProbe.java` | a native-only execution record refuses an active bytecode caller and leaves that caller able to finish | `native_state_test.go` |
| `HeapCheckpointProbe.java` | heap restore: shared and cyclic array references, a mutated `StringBuffer`, counters advanced after restoration, Calendar calls for restored timezone history, and an initialized static marker that detects an accidental rerun of class initialization | `heap_state_test.go` |
| `BytecodeCheckpointProbe.java` | nested bytecode continuation, shared references, wide locals, exceptions and forwarding `Thread.run` calls | `bytecode_state_test.go` |
| `ThreadCheckpointProbe.java` | worker barriers and restoration of a sleeping thread holding a recursive monitor | `thread_checkpoint_test.go` |
| `WaitCheckpointProbe.java` | wait, notification, join, contended monitor entry, and synchronized native entry with wide arguments and an aliased object | `thread_checkpoint_test.go` |

`NativeCheckpointProbe` and `HeapCheckpointProbe` are also packaged in the
authored SKT fixture `internal/platform/skt/testdata/native-checkpoint.jar`.
