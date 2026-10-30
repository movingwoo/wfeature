package clet

import "errors"

// Storage and string helpers the copied handlers expect.
const (
	// execNameSeparator and execNameTrailer frame the program listing
	// MC_knlGetExecNames writes.
	execNameSeparator = "\t"
	execNameTrailer   = "\t000"

	// guestStringChunk is how many bytes the guest-string scan reads at a time.
	guestStringChunk = 64

	// The removal-list save keys each storage table keeps. A Clet uses only the
	// WIPI-C file table, but the shared helpers name all of them.
	guestFileRemovedKey      = "fs/.removed"
	recordDatabaseRemovedKey = "rdb/.removed"
	javaDatabaseRemovedKey   = "jdb/.removed"
)

var errGuestStringOverflow = errors.New("SKT Clet string address overflows guest memory")
