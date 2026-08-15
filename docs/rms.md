# MIDP record stores (RMS)

This is part of the **SKT** platform: an SKT title is a MIDlet, and
`javax.microedition.rms` is how it saves. This is the whole of it: the class
surface, where the bytes go, and the two decisions that are not obvious from
the JSR.

## Where the records live

Record data lives on the Go side (`internal/platform/skt/rms.go`); the Java
classes in `internal/api/midp/java/javax/microedition/rms` are the guest's
view. One store is a list of records addressed by id.

**Ids never shift.** MIDP hands a record id to the game and the game keeps it,
so deleting record 2 of 3 must not renumber record 3. The list is therefore
indexed by `id - 1` and a deleted slot holds `nil`. That is exactly what
`backend.EncodeSaveRecords` already represents with a tombstone, which is why
RMS and KTF's Java `DataBase` share one encoding.

Persistence goes through `backend.SaveStore`, the same boundary KTF writes
through, under the same directory layout:

```
var/savedata/<profile>/skt/<owner>/rms/<store name>
```

`<owner>` is `skt.SaveOwner`: the handset's program number where the archive
has one, falling back to the `MIDlet-Name` from the manifest, itself falling
back to the main class for a JAR that omits it. KTF keys its owner by PID
because several KTF games share an AID; a bare JAR has no such identifier and
`MIDlet-Name` is both stable across rebuilds and what a user recognizes in a
directory listing.

Store names come from the game, so they go through `backend.NormalizeSaveKey`
like every other save key — a name that escapes the owner directory is
rejected rather than sanitized.

## The two decisions worth knowing

**The index, not the file, says a store exists.** `SaveStore` can write and
read but not remove. Deleting a store therefore leaves its file behind holding
an empty record list. If `openRecordStore` decided existence by looking for
that file, a game that deleted its save would find it again on the next
launch. So `rms/.index` lists the stores that exist, `listRecordStores` reads
it, and `deleteRecordStore` removes the name from it. Every write this runtime
makes updates both, so the two never disagree.

**The enumeration is written in Java, not Go.** `RecordFilter` and
`RecordComparator` are application objects. Implementing `RecordEnumeration`
as a native service would mean the Host calling back into the interpreter for
every comparison of a sort; implementing it as a runtime-owned Java class
(`RecordSet`) makes those ordinary guest calls. `RecordSet` also implements
`RecordListener`, which is how `keepUpdated(true)` works: it registers itself
with the store and rebuilds on each notification.

The sort is an insertion sort. It is stable, so records the comparator calls
equivalent keep the order the store listed them in — which is what a game
sorting by one field expects for records that share it.

## What is answered with a fixed value

`getSizeAvailable` reports `rmsCapacity - used`, with `rmsCapacity` a fixed
512 KiB. Handsets answered with the free space of a small flash partition and
games use the number to decide whether a save will fit; an honest fixed budget
is a better answer than claiming unbounded space, and `addRecord`/`setRecord`
enforce it with `RecordStoreFullException`.

`setMode(int, boolean)` checks that the store is open and does nothing else.
Only one MIDlet suite runs at a time, so every store is already private to it
and the sharing mode changes nothing observable.

## Deliberately incomplete

- `RecordStore.openRecordStore(name, vendor, suite)` opens the local store of
  that name rather than another suite's, because no other suite exists.
- A record listener that throws does not undo the change that already
  happened; the store stays authoritative and the failure is logged, matching
  how the other MIDP event callbacks in this runtime treat guest failures.

## Testing

`internal/platform/skt/testdata/recordstore.jar` is a newly authored MIDlet
that exercises the surface and reports one bit per check, so a regression
names the check that broke. `TestRecordStoreSurfaceAndPersistence` runs it,
then starts a **second runtime over the same save directory** — that is the
only way to test what a later launch of the game sees, because nothing may be
carried over in memory — and then a third to confirm a deletion outlived the
session too.

Regenerate the fixture with:

```sh
fixture_dir="$(mktemp -d /tmp/wfeature-rms-fixture.XXXXXX)"
javac -source 1.8 -target 1.8 -g:none -cp internal/api/midp/classdata \
  -d "$fixture_dir" internal/platform/skt/testdata/src/RecordStoreMIDlet.java
mkdir -p "$fixture_dir/META-INF"
cp internal/platform/skt/testdata/RECORDSTORE.MF "$fixture_dir/META-INF/MANIFEST.MF"
(cd "$fixture_dir" && zip -X -q "$fixture_dir/recordstore.jar" \
  META-INF/MANIFEST.MF RecordStoreMIDlet*.class)
cp "$fixture_dir/recordstore.jar" internal/platform/skt/testdata/recordstore.jar
```
