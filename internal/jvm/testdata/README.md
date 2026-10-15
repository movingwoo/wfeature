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
