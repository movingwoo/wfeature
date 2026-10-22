# WIPI Java class library

`definitions.go` declares the `org.kwis.msp` and `org.kwis.msf` surface in Go,
the way `internal/api/midp` declares MIDP's: class metadata with the Go body of
every method this layer implements itself, no Java sources and no JDK in the
build. `Define` (`library.go`) installs it on a VM that already has MIDP,
because every class here names a MIDP class as its superclass. `IsJlet` tells a
Host whether an archive's main class is a WIPI application rather than a MIDlet,
since the two are packaged identically.

It exists because this vendor's container carries two kinds of program: most
archives hold a MIDlet, and a few hold a Jlet, the same packaging with a WIPI
application class inside. Rather than a second runtime, each class here is its
MIDP counterpart plus what the WIPI standard has and MIDP does not. The package
comment in `library.go` has the reasoning, and
[the SKT history](../../../docs/history/skt.md#implementation-three-archives-hold-a-jlet-not-a-midlet),
"Three archives hold a Jlet, not a MIDlet", has what it took to make three real
titles run.

| Package | Classes |
|---|---|
| `org.kwis.msp.lcdui` | `Jlet`, `Card`, `Display`, `Graphics`, `Image`, `Font`, `EventQueue` |
| `org.kwis.msp.handset` | `BackLight` |
| `org.kwis.msp.io` | `File`, `FileSystem` |
| `org.kwis.msp.db` | `DataBase`, `DataBaseException`, `DataBaseRecordException`, `DataFilter`, `DataComparator` |
| `org.kwis.msp.media` | `Clip`, `Player`, `PlayListener`, `Vibrator` |
| `org.kwis.msf.io` | `Network`, `Socket`, `URL` |

Everything that touches the Host (the card stack, the pixels, the save
directory, the sound) is declared native here and registered by
`internal/platform/skt`, in `wipi.go` and the `wipi_*.go` files beside it.
`Vibrator.on` and `off` record the request on the `backend.Vibrator` passed to
`Define`; a nil one makes them no-ops. The `org.kwis.msf.io` classes connect
nothing: `Network.connect` answers -1, and `Socket` streams and `URL.find` throw
`IOException`, so a title reaches its own error path (see
[`docs/network.md`](../../../docs/network.md)).

## Storage

`File`, `FileSystem` and `DataBase` answer as
[the WIPI storage contract](../../../docs/wipi-storage.md) says, the one KTF and
LGT answer by too: names and the root, what a directory is, the space figures
and the 4 MiB bound, and every database rule. This section is only where SKT
keeps what the contract asks for. The platform's half is
`wipi_file_stream.go`, `wipi_filesystem.go` and `wipi_database.go` in
`internal/platform/skt`, and
[the SKT history](../../../docs/history/skt.md#skt-wipi-storage-2026-10-22)
records how it came to be.

| Key | Holds |
|---|---|
| `fs/<path>` | a file: the key `XFile` uses, so `FileSystem` answers about the files a title opens |
| `fs/.removed` | the paths removed, which hides an earlier session's save or the archive's file ([`docs/skvm.md`](../../../docs/skvm.md)) |
| `wipi/directories` | the directories `mkdir` made, one path below `fs/` a line, in name order |
| `jdb/<name>` | a database's records in `backend.EncodeSaveRecords`' form: a record's id is its index, and a deleted record keeps its slot as a tombstone |
| `jdb/.index` | each database's record size, one per line: the size in decimal, a space and the name, in name order |

- **The directory list is a platform key outside every scope a title names**,
  so no path, `XFile` name or database name reaches it. A save tree without it
  has no directory made.
- **A database exists when the index names it or its record list holds
  something.** A record list the index does not name takes the record size of
  the open that finds it, and the index keeps it from then on. A deletion
  leaves the name off the index and an empty record list, which is no
  database.
- **Every change is stored when it is made, and a change the store refuses is
  not made**: a database write throws `DataBaseException`, and a `mkdir`,
  `rmdir` or `rename` throws `IOException`, with the session as the store has
  it. A rename stores the new path and the old path's removal in one batch,
  and a database's creation or deletion its records and the index.
- An index or a record list this runtime did not write is refused whole and
  never written over.
- A quick save keeps a database's name, record size and modified time; a load
  reads its records, and the directory list, from the saves as they are then.
- `XFile` keeps its own answers: names longer than 255 bytes, a backslash
  refused rather than read as a separator, a 16 MiB bound with -1, and
  `fsavail`'s 512 KiB.
