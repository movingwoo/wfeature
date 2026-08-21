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
