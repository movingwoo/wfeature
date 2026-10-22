# WIPI storage: one contract for KTF, LGT and SKT

WIPI is one standard across the three carriers, so a title's files, directories
and databases answer the same way on every platform here. This page is that
contract. It covers WIPI C `MC_fs*` (KTF and LGT), the WIPI C record table
`MC_db*` (KTF; LGT's is not served, see [LGT](lgt.md)) and WIPI Java
`org.kwis.msp.io.File`, `FileSystem` and `org.kwis.msp.db` (all three). It does
not cover SKVM `XFile`, MIDP RMS or BREW, which have their own documents.

The specification decides wherever it speaks
([wipi-wiki](https://mirusu400.github.io/wipi-wiki/)). Where it is silent, or
where following it could cost a player a save, the choice is written here once
and every platform makes it. A platform that answers differently from this page
has a defect, not a variant.

## Names and areas

- A name is a path of segments. A backslash separates segments as a slash
  does, as LGT's archives always read it. Leading separators, empty segments
  and `.` segments are dropped, so `/save.dat`, `./save.dat`, `data\save.dat`'s
  `data/save.dat` and `save.dat` are each one file. A `..` segment is refused.
- `""`, `/` and `.` name the title's own directory, the root.
- The access flag (`PRIVATE_ACCESS` 1, `SHARED_ACCESS` 2, `SYSTEM_ACCESS` 3)
  addresses the one private area each title has. Nothing answers
  `SecurityException`.
- The longest name is 255 bytes, which `FileSystem.getMaxFilenameLength`
  answers, counted on the path as the rule above leaves it: a C string's own
  bytes, a Java `String`'s UTF-8 bytes. A longer name is refused:
  `M_E_LONGNAME` in C, `IOException` in Java. A name the store cannot keep is
  `M_E_BADFILENAME` or `IOException`.
- A C name is read up to 4096 bytes looking for its end; one that runs past
  that is not a name and stops the run.
- A `null` name is `NullPointerException` to a `File` or `FileSystem` call, and
  `DataBaseException` to a `DataBase` call, the exception every one of those
  declares.

## What a directory is

A name is a **file** when a file is stored there, written by the title or
shipped in its archive, and not removed. A name is a **directory** when it is
the root, when `mkdir` recorded it, or when a file lies below it. A name that is
a file is not a directory.

The store keeps files under their full names and makes no directory entries, so
a file may be written below a directory nobody made: the parent is implied. The
specification's `M_E_NOENT` for a missing parent is not applied, to `mkdir` or
to a file, because saves have always been written that way here. `mkdir`
records its name in a list beside the saves, so a directory made and left empty
is still there after a restart.

| Question | C | Java |
| --- | --- | --- |
| make a directory | `MC_fsMkDir`: 0; a name that is a file or a directory `M_E_EXIST` | `mkdir`: returns; a name that is a file or a directory `IOException` |
| remove a directory | `MC_fsRmDir`: 0; something below it `M_E_NOTEMPTY`; not a directory `M_E_NOENT`; the root `M_E_BADFILENAME` | `rmdir`: returns; the same three cases `IOException` |
| exists | `MC_fsIsExist`: 0 for a file or a directory, else `M_E_NOENT` | `exists`: true for a file or a directory |
| kind | `MC_fsFileAttribute`: 0; a directory sets `MC_FILE_IS_DIR` (`0x01`, the HAL value) with size 0; a missing name `M_E_NOENT` | `isFile`, `isDirectory` |
| entries | `MC_fsList`: the names directly below, files and directories alike, each once, in name order, `"a\0b\0\0"`; an empty or missing directory `"\0\0"`; a short buffer `M_E_SHORTBUF` and nothing written | `list`: the same names in a `Vector`; a name that is not a directory `IOException` |
| count | `MC_fsGetCounts`: how many entries `MC_fsList` names | — |
| mode | `MC_fsSetMode`: modes 1 to 3 and `0x10`, `0x20`, `0x30` (the two spellings the specification uses) are accepted for a file or a directory and kept nowhere; another mode `M_E_INVALID`; a missing name `M_E_NOENT` | — |
| creation time | `MC_fsFileAttribute`'s creation time is 0 | `getCreationTime`: 0 for a file, -1 for a directory, `IOException` for neither |
| remove a file | `MC_fsRemove`: 0; a name that is not a file `M_E_NOENT` | `remove(name)`: `IOException` for a name that is not a file; `remove(name, flag)` returns for one, as the specification words the flagged form; a removal the store refuses is `IOException` to both |

A listing reads the saves of earlier sessions as well as this one's, and lists
the files the archive ships. The C listing of a missing directory is an empty
list because the specification gives `MC_fsList` no code for it and a title in
the corpus walks the empty answer; Java's `list` has the specification's
`IOException`.

## Space

Every title has 1 MiB. `MC_fsTotalSpace` and `FileSystem.totalSpace` answer it.
`MC_fsAvailable`, `FileSystem.available` and `DataBase.getSizeAvailable` answer
1 MiB less what the title has stored, never below zero: for each file and
database the title has written, its size less the size of the file of the same
name its archive ships, if any, and never less than zero for any one of them. The platform's own lists are not counted.
Nothing is refused for space: the figure is reported, and a save is never
turned away to keep it true.

What is refused is size beyond a bound on untrusted input: one file, or one
database's records together, holds at most 4 MiB. A write that would pass it
fails as a write does: `M_E_NOSPACE` in C, `IOException` from a `File` or its
streams, `DataBaseException` from a database. No title's save comes near it.

## Databases (Java)

- `DataBaseException` and `DataBaseRecordException` both extend `Exception`.
  Neither is the other's subclass, so a handler for one never takes the other.
- A database name is a name like a file's, read by the same rule, so the
  spellings of one path are one database.
- `openDataBase(name, recordSize, create)`: a name the store cannot keep is
  `DataBaseException`; `create` with a record size of 0 or less is
  `IllegalArgumentException`, whether or not the database exists; a database
  that is not there is `DataBaseException` without `create` and is made at
  once with it, empty and listed.
- The record size is the one the database was created with. A database whose
  stored form does not carry one, written by an earlier build, takes the size
  of the open that finds it and keeps it from then on.
- Record ids start at 0. An insert takes the lowest id a delete freed, and
  otherwise the next one.
- The record size is not enforced: a longer record is stored whole. The
  specification refuses it; a title that has always written such records here
  would lose its save on the next write.
- `selectRecord` answers the bytes stored, as long as they are. The form that
  fills a buffer copies them at the offset, and a buffer they do not fit is
  `IllegalArgumentException`.
- An id that holds no record is `DataBaseRecordException` to `selectRecord`,
  `updateRecord` and `deleteRecord`.
- `getNumberOfRecords` counts the records held; `getDataBaseSize` sums their
  bytes.
- `getLastModified` is the guest time of the last write in this session, or of
  the session's first open when nothing has been written since.
- `getAccessMode(name)` is `PRIVATE_ACCESS` (1) for a database that exists and
  `DataBaseException` for one that does not.
- `listDataBases` names every database the title has, saved in any session or
  opened in this one, in name order, and `null` when there is none.
- `deleteDataBase` of a database that is not there is `DataBaseException`. An
  object opened before the delete and written through afterwards makes the
  database again.
- `closeDataBase` releases nothing a title can see: the object stays usable,
  and closing it twice is not an error.
- `sortRecord(filter, comparator)`: the records `filter` accepts (all when it
  is `null`), in id order when `comparator` is `null`, and otherwise ordered by
  it, a positive answer placing its first record after the second
  (`FOLLOWS` 1, `EQUIVALENT` 0, `PRECEDES` -1); records the comparator calls
  equal keep their id order. No record answers `null`. An exception the filter
  or the comparator throws and does not catch is the title's: it reaches the
  title's handler around its `sortRecord` call.

- The databases a title keeps are not files: no file call, C or Java, lists,
  finds or opens the storage a database is kept in, and a database named
  `dir/scores` does not make `dir` a directory.

## The C record table (KTF)

`MC_dbSortRecords` and `MC_dbGetAccessMode` are described in
[KTF](history/ktf.md#ktf-storage-spec-2026-10-22). The C table numbers its
records from 1, as its titles observe, and keeps its own conventions where the
C specification differs from the Java one (a positive compare places the first
record first).
