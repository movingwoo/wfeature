# Runtime-owned Java core classes

These Java sources provide class metadata and small CLDC-compatible method
surfaces for the Go JVM. Go native methods in `internal/jvm` implement services
that require Host state. The classes intentionally cover only the shared subset
currently exercised by repository fixtures and the local real-game probe.

Regenerate the embedded Java 8 class files with:

```sh
javac -source 1.8 -target 1.8 -g:none -d internal/jvm/classdata \
  internal/jvm/java/java/lang/*.java \
  internal/jvm/java/java/io/*.java \
  internal/jvm/java/java/util/*.java
```
