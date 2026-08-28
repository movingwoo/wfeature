# JVM fixtures

`Arithmetic.java`, `Constructed.java`, and their Java 8 class-format outputs are
test fixtures newly authored for `wfeature`.

Regenerate them with:

```sh
javac -source 1.8 -target 1.8 -g:none internal/jvm/testdata/Arithmetic.java internal/jvm/testdata/Constructed.java
```

`Stores.java` is a fixture for the store observer: one method that performs an
instance-field store, a class-static store and an array store, so a test can
check that all three are reported and attributed to the method that ran them.

`Streams.java` is a fixture for interface dispatch across the java.io stream
classes: it writes a record through a `DataOutput` parameter and reads it back
through a `DataInput` one, so a test can check that the call lands on the
stream that was passed rather than on an interface this runtime never declares.

```sh
javac -source 1.8 -target 1.8 -g:none internal/jvm/testdata/Streams.java
```

`CoreMembers.java` is a fixture for the class-library link surface: one method
that reaches, through a constant pool, the members a compiled title resolves
through their class — the char-array append, the boxed flag and its published
instances, the character replace, and a stream subclass that reads the
protected buffer. Each of those had a working body and no declaration behind
it, which is a member only a native dispatch can reach.

`Inherited.java` is a fixture for field resolution: a superclass writes an
instance field and a static beside it, and the subclass reads both under its
own name for them, which is the name a compiler emits.

```sh
javac -source 1.8 -target 1.8 -g:none -d internal/jvm/testdata \
    internal/jvm/testdata/CoreMembers.java internal/jvm/testdata/Inherited.java
```
