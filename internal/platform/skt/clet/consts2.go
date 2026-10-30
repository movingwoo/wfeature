package clet

// Storage scopes name the save-key prefix each table's files live under; a
// Clet uses only the WIPI-C file table ("db"), but reservedStorageNames keeps
// every scope so the shared helpers compile unchanged. See wipic_filesystem.go.
const (
	guestFileScope      = "fs"
	cFileScope          = "db"
	recordDatabaseScope = "rdb"
	javaDatabaseScope   = "jdb"
)

// reservedStorageNames is the set of names, per scope, that address a list the
// table itself keeps rather than a file a title may store.
var reservedStorageNames = map[string]map[string]bool{
	guestFileScope:      {".removed": true, ".dirs": true},
	cFileScope:          {".removed": true, ".dirs": true},
	recordDatabaseScope: {".removed": true},
	javaDatabaseScope:   {".removed": true, ".sizes": true},
}

const (
	guestStringBuffer        = 256
	hookWindowLimit          = 256 << 10
	maxReleasedWIPIC         = 256
	maxWIPIFileName          = 255
	svcStubSize              = 16
	wipicGraphicsContextSize = 52
	maxTimerDelayMillis      = 60 * 60 * 1000
	maxAOTCallDepth          = 64

	diagnosticNameLimit   = 4096
	diagnosticNameCeiling = 2 * diagnosticNameLimit

	// storageBudget is the on-disk size the WIPI-C file table stays within.
	storageBudget = wipicFileStorageLimit
)

// wipicZeroFill is a page of zeroes the handlers clear guest memory with.
var wipicZeroFill [1024]byte
