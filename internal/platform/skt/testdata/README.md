# SKT fixtures

`Watched.java` and its Java 8 class-format output are test fixtures newly
authored for `wfeature`. It has two fields written from two methods, which is
what a write-watch test needs: a hit has to land on the address of the field
that was written and name the method that wrote it.

Regenerate it with:

```sh
javac -source 1.8 -target 1.8 -g:none internal/platform/skt/testdata/Watched.java
```
