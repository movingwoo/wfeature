# MIDP record stores (RMS)

This is part of the **SKT** platform: an SKT title is a MIDlet, and
`javax.microedition.rms` is how it saves. This page covers the class surface,
where the bytes go, the stores an archive carries, and the two decisions that
are not obvious from the JSR.

## Where the records live

Record data lives on the Go side (`internal/platform/skt/rms.go`); the classes
declared in Go in `internal/api/midp` (`definitions.go`, with bodies in
`rms.go`) are the guest's view. One store is a list of records addressed by id.

**Ids never shift.** MIDP hands a record id to the game and the game keeps it,
so deleting record 2 of 3 must not renumber record 3. The list is indexed by
`id - 1` and a deleted slot holds `nil`, which `backend.EncodeSaveRecords`
represents with a tombstone; RMS and KTF's Java `DataBase` therefore share one
encoding.

Persistence goes through `backend.SaveStore`, the same boundary KTF writes
through, under the same directory layout:

```
var/savedata/<profile>/skt/<owner>/rms/<store name>
```

`<owner>` is `skt.SaveOwner`: the handset's program number (`DD-ProgName`)
where the archive has one, otherwise the manifest's `MIDlet-Name`, otherwise the
main class for a JAR that omits it. KTF keys its owner by PID because several
KTF games share an AID; a bare JAR has no such identifier, and `MIDlet-Name` is
stable across rebuilds and recognizable in a directory listing.
`wfeature checkgames` reports owner directories that archives with different
main classes both claim (`skt.SaveOwnerCollisions`). A descriptor key is read
case-insensitively, but the exact spelling wins, and among other spellings the
first in byte order does, so a descriptor carrying two spellings of
`DD-ProgName` names the same owner on every launch.

**The owner is one both Hosts accept, on every platform.** The browser Host
refuses an owner over 64 bytes, `.`, `..`, the reserved `.wfeature-quicksave`,
or one carrying a path or Windows-reserved character, a control character or an
invisible direction mark (`backend.SaveOwnerRefused`), and a title it refused
ran with no save store at all. Such an owner is mapped to one the Host accepts
(`backend.AcceptedSaveOwner`), for an SKT program number or MIDlet name and a
KTF or LGT PID alike: a refused character becomes `_`, a name too long keeps
its start, and the result ends in `~` and eight hex digits of a digest of the
whole name, so `A: B`, `A? B` and a title really named `A_ B` keep three
directories. Every owner that already passed is unchanged, so no existing save
moves. Only the command line ever wrote under a refused name;
`backend.MigrateSaveOwner(root, earlier, owner)` renames that directory to the
mapped owner below the platform save root, together with the quick-save slots
and set-aside generation the save store keeps for that name in
`.wfeature-quicksave/owners/`. It claims both directories for the move, so it
leaves a directory a session or tool has claimed for the next start, and it
does so only when the mapped directory does not exist yet, never merges into
or replaces one that does, and never follows a raw name that is not one
directory directly below the root (`.` and `..` included). An earlier build's
quick load that stopped under the raw name between its two renames left no
directory there, only the saves it set aside and its record in
`.wfeature-quicksave/owners/<raw>/`; the move puts them back first, as the save
store does on its first operation, and then moves them, and the raw name is
where saves are read from until it has. A Host calls it,
with the earlier owner `session.Inspect` already found, before it claims the
title's save directory; a quick-load check, which runs before a start and
without the claim, only reads the slot where it is.

Store names come from the game, so they go through `backend.NormalizeSaveKey`
like every other save key: a name that escapes the owner directory is rejected
rather than sanitized.

## The stores a title brought with it

An archive taken off a handset carries the title's record stores in an `rs`
directory beside the JAR: two files per store, `NAME.sb` and `NAME.db`
(`rms_packaged.go`). Before they were read, a title whose save came with it was
told it had none — a MIDlet asking to continue answered `저장된 자료가 없습니다`
over a saved game in its own archive. The KTF platform had the same defect with
the databases its archives ship: present data, a readable format, and an API
that never asked.

`.sb` is the store index and `.db` its bytes. Every field is big-endian:

```
u32       the id the next record will take
u16 + n   the store's name
u32       the store version (not kept; see below)
u32       how many records follow
u32       how many bytes the data file holds
u64       the last modification time, in milliseconds (not kept)
per record: u32 id, u32 offset into the data file, u32 length
```

The reading is verified by arithmetic that holds for all twelve packaged
stores in the local set: the declared length is the data file's exact size, the
entries tile it end to end, and the fixed part plus twelve bytes per record is
the `.sb` file's own length.

**The name comes from the file, not the path.** A handset writes an upper-case
letter in a file name as `#X`, so a store named `GameSaveData` is the file
`#Game#Save#Data.sb`. The `.sb` carries the name in full, which is more
reliable than un-escaping a path, and the path is gone by then because a
container's files are mounted by their bare names.

**The Host has the last word, which makes seeding safe.** A packaged store is
used only where the save store holds nothing under that store's key, even an
empty record list. The title's own writes win from the moment it writes, and a
store the title deleted stays deleted: deleting writes an empty record list
under the key, so the key answers and the archive's copy is not seeded over it.
The opposite rule (an empty store does not count) was tried and withdrawn,
because the shape an earlier build left is the shape a delete-and-create leaves,
and serving the archive over it hands back a save the player cleared.

**Serving a carried store writes nothing, not even its name.** Naming it in the
index while nothing has written it would leave a name the archive no longer
backs: the next session would find the name, find no bytes, and answer "it
exists and is empty", which a title reads as a save rather than a first run.
The name enters the index with the store's **first write** (`persistStore`),
which is also when the Host's copy takes over from the archive's.

**The index is written whole, so the exclusion belongs in `storeIndex`**, not
at the call sites: the index serializes every name the session knows, so the
first write to one carried store would publish the names of all the others.
`unwritten` is therefore filled when the stores are seeded rather than when one
is opened, and `storeIndex` subtracts it. Tests pin both halves — one carried
store, and two with only one written — because each fails silently.

**Deleting a carried store ends both consequences of carrying it.** The
archive's copy is not served again, and the name is no longer filtered out of
the index the next create puts it in. Without the second, a title that cleared
its slot to start a new game created the store, wrote an index that left it
out, and found nothing on the next launch.

**A `.sb` is untrusted input**, so every field that sizes an allocation is
bounded:

- the record count and the next-record id the store's length comes from, each
  at most 65,536 (`packagedStoreMaxRecords`), with the count no larger than the
  ids below the next one;
- each entry's id, which must be below the next id — otherwise a one-record
  table naming id 16384 grows the store to 16384 slots and makes
  `getNextRecordID` answer past every id the title reserved;
- each record, at most 1 MiB (`rmsMaxRecordBytes`);
- the bytes all entries ask for together, at most the data file's length,
  because entries may all point at the same place: two thousand entries each
  naming a megabyte of a one-megabyte file asked for two gigabytes, which no
  count or slot limit sees;
- the slots a whole archive may allocate, at most 131,072
  (`packagedStoreMaxSlots`): five hundred crafted indexes of twenty-seven bytes
  each asked for thirty-two million slots and most of a gigabyte on the first
  RMS call. A store over that budget is skipped rather than ending the walk, so
  one large store early in name order does not suppress the smaller ones after
  it.

A store that does not parse is left out rather than reported: the archive is
untrusted input, and a title with no save is a title on its first run. A store
name from an archive is checked like one from the guest: it must be a MIDP name
and a save key, and it cannot be `.index`, the name this runtime keeps its store
list under.

**Entries are read in name order.** The seeded names are written out as a list,
and ranging a map put different bytes in `rms/.index` from identical input on
every launch; save-tree comparisons then reported false differences that would
have hidden a real index regression. The order also decides which file wins
when two decode to one store name.

**A record of no bytes is a record.** MIDP writes one for
`addRecord(null, 0, 0)`, but `append([]byte(nil))` answers nil, which is this
runtime's tombstone for a deleted id; a legitimate empty record therefore once
decoded as a deleted one, and the first write back made the loss permanent.

**The version and modification time are not kept.** `backend.EncodeSaveRecords`
carries records only, so the first write — which a plain `closeRecordStore`
performs — would lose them, and `getVersion` would answer the container's
number in the session that opened the store and zero in every later session.
Answering zero throughout is at least consistent. Carrying them needs a save
format that holds more than records, on both Hosts and in the backup container,
and no local title has been seen to read either.

**The `fs/` scope is shared with KTF.** An `XFile` writes under `fs/`
deliberately, so one owner directory holds a title's files whichever platform
wrote them, and both keep their list of removed paths at `fs/.removed`, in the
same newline-separated form: `XFile.unlink` and WIPI `FileSystem.remove` write
it on this side ([SKVM](skvm.md#saves-and-sound)). A title writing that path
would put its own bytes where the list belongs, so the name, and any path
below it, is reserved (`xFileReservedNames` in `skvm_file.go`). Files differ
from record stores in one way: the list, not an index, says a file was
removed, because a path nothing lists is found in the save or the archive by
its own name.

## The two decisions worth knowing

**The index, not the file, says a store exists.** `backend.SaveStore` can
write and read but not remove, so deleting a store leaves its file behind
holding an empty record list. If `openRecordStore` decided existence by looking
for that file, a game that deleted its save would find it again on the next
launch. `rms/.index` therefore lists the stores that exist, `listRecordStores`
reads it, and `deleteRecordStore` removes the name from it. Every write this
runtime makes updates both, so the two never disagree — unless the index write
failed while the records reached the Host. Creating a store the index does not
name therefore still reads its key: an empty list is a deleted store and the
new one starts empty, but records there are taken as the store's own instead of
being overwritten with an empty list, and the name goes back into the index.
Records that do not decode are reported as an unreadable store, as they are for
a listed one.

A store's records are encoded inside the write's own turn at the save
boundary, so when two guest threads change one store, the older encoding can
never reach the Host after the newer one.

**The enumeration calls the application's own objects.** `RecordFilter` and
`RecordComparator` are application objects. `RecordEnumeration` is a
runtime-owned class, `RecordSet`, whose Go bodies call the filter's `matches`
and the comparator's `compare` through the VM as ordinary guest calls.
`RecordSet` also implements `RecordListener`, which is how
`keepUpdated(true)` works: it registers itself with the store and rebuilds on
each notification.

The sort is an insertion sort. It is stable, so records the comparator calls
equivalent keep the store's order, which a game sorting by one field expects
for records that share it.

**The cursor is on a record, as MIDP has it.** The `position` field is one past
the record the last step returned, and zero before any step. After
`nextRecord` answered A and then B, `previousRecord` answers A; a first
`previousRecord` on a fresh or reset enumeration answers the last record, and
`hasPreviousElement` is false only on the first record or an empty selection.
`reset` and an explicit `rebuild` go back to the start. A kept-updated
enumeration reselects on every change but keeps its place: on the record it was
on, or, when that record left the selection, between records, which a negative
`position` holds. The next step then answers the next record that is still
there and the previous step the one before the deleted record, so deleting the
first record leaves nothing behind and deleting the last nothing ahead. Going
back to the start on each notification made a loop that rewrites each record it
visits meet the first record again after every write.

## What is answered with a fixed value

`getSizeAvailable` reports `rmsCapacity` minus the store's own bytes, with
`rmsCapacity` a fixed 512 KiB per store. Handsets answered with the free space
of a small flash partition, and games use the number to decide whether a save
will fit; a fixed budget is more honest than claiming unbounded space.
`addRecord` and `setRecord` enforce it with `RecordStoreFullException`, which
also answers a single record over 1 MiB, and an `addRecord` once the store has
used `backend.MaxSaveRecords` (1,048,576) record ids: ids are never reused, so
empty and deleted records take a slot too, and a longer list could not be read
back on the next launch. An `XFile` reports the same 512 KiB capacity
(`xFileCapacity`).

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

Regenerate the fixture as follows. `$stub_dir/classes` is the signature
classpath built by
[the fixture guide](history/testing.md#implementation-compiling-a-java-fixture);
build it once per session.

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
