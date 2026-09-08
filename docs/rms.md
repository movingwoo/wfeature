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

## The stores a title brought with it

An archive taken off a handset carries the title's record stores, in an `rs`
directory beside the JAR: two files per store, `NAME.sb` and `NAME.db`. This
runtime looked at neither, so a title whose save came with it was told it had
none — a MIDlet asking to continue answered `저장된 자료가 없습니다`, over a
saved game that was sitting in its own archive. It is the same defect the KTF
platform had with the databases its archives ship, and it has the same shape:
present data, a readable format, and an API that never asked.

`.sb` is the store and `.db` is its bytes. Every field is big-endian:

```
u32       the id the next record will take
u16 + n   the store's name
u32       the version, what getVersion answers
u32       how many records follow
u32       how many bytes the data file holds
u64       when it was last modified, in milliseconds
per record: u32 id, u32 offset into the data file, u32 length
```

What says that reading is right rather than plausible is arithmetic that holds
for all twelve packaged stores in the local set: the declared length is the
data file's exact size, the entries tile it end to end, and the fixed part plus
twelve bytes per record is the `.sb` file's own length.

**The name comes out of the file, not out of the path.** A handset writes an
upper-case letter in a file name as `#X`, so the store `TowerSaveGame` is the
file `#Tower#Save#Game.sb`. The `.sb` carries the name in full, which is a
better answer than un-escaping a path — and the path is gone by then anyway,
because a container's files are mounted by their bare names.

**The Host has the last word, and that is what makes seeding safe.** A
packaged store is only used where the save store holds nothing under that
store's key. The title's own writes therefore win from the moment it writes,
and a store the title *deleted* stays deleted: deleting writes an empty record
list under the key, so the key answers and the archive's copy is not seeded
over it.

**Serving one writes nothing, not even its name.** The index is the list of
stores that exist, and a store the archive carries exists because the archive
carries it — so naming it there while nothing has written it would leave a
store behind that the archive no longer has to back: the next session would
find the name, find no bytes under it, and answer "it exists and is empty",
which is what a title reads as a save rather than as a first run. The name goes
into the index with the store's **first write** instead (`persistStore`), which
is also the moment the Host's copy takes over from the archive's.

**The index is written whole, so the leaving-out belongs in `storeIndex`**
rather than at the call sites. Keeping the rule at the call sites looks like it
works and does not: the index serializes every name the session knows, so the
first write to *one* carried store publishes the names of all the others beside
it. That is why `unwritten` is filled when the stores are seeded rather than
when one is opened, and why `storeIndex` subtracts it. Both halves are pinned
by tests — one carried store, and two with only one of them written — because
each is silent when it is wrong.

**A `.sb` is a file anybody can craft**, so every field that sizes an
allocation is bounded: the record count, the next-record id the store's length
comes from, and each entry's own id, which is a second way to the same room — a
one-record table naming id 16384 grows the store to 16384 slots and makes
`getNextRecordID` answer past every id the title ever reserved. An id the store
holds is below the id it hands out next. **The bytes the entries ask for
between them are bounded by the data file they point into**, because they may
all point at the same place: two thousand entries each naming a megabyte of a
one-megabyte file asked for two gigabytes, which no count or slot limit sees. A
real store tiles its data file end to end, so that file's own length is the
ceiling. **And what a whole archive may ask for is bounded as well as what each
store may**: five hundred crafted indexes of twenty-seven bytes each asked for
thirty-two million slots and most of a gigabyte, on the first RMS call. A store
over that budget is skipped rather than ending the walk, so one large store
early in name order does not suppress every smaller one after it.

**The entries are read in name order.** What this seeds is written out as a
list, so ranging a map put different bytes in `rms/.index` from identical input
on every launch — every save-tree comparison then reported a difference that
was not one, and a real index regression would have hidden inside that noise.
The order also decides which file wins when two decode to one store name.

**A record of no bytes is a record.** MIDP writes one for
`addRecord(null, 0, 0)`, and `append([]byte(nil))` answers nil — which is this
runtime's tombstone for an id the store no longer has, so a legitimate empty
record decoded as a deleted one and the first write back made the loss
permanent.

**A store name out of an archive is checked the way a name from the guest is.**
It has to be a MIDP name, it has to be a save key, and it cannot be `.index` —
the name this runtime keeps its store list under, which a container supplies on
its own with no guest cooperation.

**A store the Host holds wins even when it holds no record**, and the KTF half
of this work explains why the opposite was tried and withdrawn: the shape an
earlier build left is the shape a delete-and-create leaves, and serving the
archive over it hands back a save somebody cleared.

**Deleting a carried store ends both of the things carrying it means.** The
archive's copy is not to be served again, and the name is not to be filtered
out of the index the next create puts it in: without the second, a title that
cleared its slot to start a new game created the store, wrote the index that
left it out, and found nothing at all on the next launch — the flow this whole
change exists to protect.

A store that does not parse is left out rather than reported. An archive is
untrusted input, and a title with no save is a title on its first run.

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

`$stub_dir/classes` is the signature classpath from
[`testing.md`](testing.md); build it once per session.

```sh
fixture_dir="$(mktemp -d /tmp/wfeature-rms-fixture.XXXXXX)"
javac -source 1.8 -target 1.8 -g:none -cp "$stub_dir/classes" \
  -d "$fixture_dir" internal/platform/skt/testdata/src/RecordStoreMIDlet.java
mkdir -p "$fixture_dir/META-INF"
cp internal/platform/skt/testdata/RECORDSTORE.MF "$fixture_dir/META-INF/MANIFEST.MF"
(cd "$fixture_dir" && zip -X -q "$fixture_dir/recordstore.jar" \
  META-INF/MANIFEST.MF RecordStoreMIDlet*.class)
cp "$fixture_dir/recordstore.jar" internal/platform/skt/testdata/recordstore.jar
```
