package ktf

import ()

// The directory slots of WIPI C table 7: MC_fsRmDir at 9, MC_fsList at 10,
// MC_fsSetMode at 13 and MC_fsGetCounts at 14, each where the specification's
// `MC_fs*` order prints it (see wipic_filesystem.go).
//
// The store behind this table is one flat map from a name to its bytes, so a
// directory is a prefix the names share, plus the names MC_fsMkDir wrote
// down. What a directory holds is therefore every name the table can open,
// cut at the directory: the stores this session has open, the saves earlier
// sessions wrote, the files the archive ships and the Java side's files, all
// less what was removed. The saves are the part that needs the save store to
// list, because nothing else in a fresh session names them — and a title that
// lists its save folder to find its slots, told the folder is empty, would
// start a new game over them.
