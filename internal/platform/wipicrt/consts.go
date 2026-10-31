package wipicrt

import (
	"errors"
)

// This file collects the constants the WIPI-C handlers are written against: the
// guest memory layout every platform on this engine maps, the kernel and
// interface-table numbers, and the error codes.

const (
	// The guest image sits at ImageBase; a platform maps whatever data and BSS
	// its program carries below the stack.
	ImageBase       uint32 = 0x00100000
	ReturnAddress   uint32 = 0x7f000000
	ThreadStackBase uint32 = 0x20000000
	ThreadStackSize uint64 = 1 << 20

	// The platform arena hands out guest allocations; the stub arena sits above
	// it and holds the callback veneers the root table points into.
	PlatformDataBase uint32 = 0x30000000
	PlatformDataSize uint64 = 64 << 20
	PlatformCodeBase uint32 = 0x35000000
	PlatformCodeSize uint64 = 1 << 20

	MaxPlatformAllocation uint64 = 4 << 20
)

// SVCCategoryWIPIC is the immediate a WIPI-C interface stub carries. A
// platform's own stubs carry its own categories, and its supervisor call
// handler sends this one to HandleWIPICCall.
const SVCCategoryWIPIC uint32 = 3

// The WIPI-C kernel interface numbers, as the specification orders them and the
// handlers in this package expect them.
const (
	WIPICKernelPrintk          uint32 = 0
	WIPICKernelSprintk         uint32 = 1
	WIPICKernelGetExecNames    uint32 = 2
	WIPICKernelLoad            uint32 = 5
	WIPICKernelExit            uint32 = 7
	WIPICKernelGetCurProgramID uint32 = 9
	WIPICKernelGetAccessLevel  uint32 = 13
	WIPICKernelGetProgramName  uint32 = 14
	WIPICKernelAlloc           uint32 = 20
	WIPICKernelCalloc          uint32 = 21
	WIPICKernelFree            uint32 = 22
	WIPICKernelGetTotalMemory  uint32 = 23
	WIPICKernelGetFreeMemory   uint32 = 24
	WIPICKernelDefTimer        uint32 = 25
	WIPICKernelSetTimer        uint32 = 26
	WIPICKernelUnsetTimer      uint32 = 27
	WIPICKernelCurrentTime     uint32 = 28
	WIPICKernelGetSysProperty  uint32 = 29
	WIPICKernelSetSysProperty  uint32 = 30
	WIPICKernelGetResourceID   uint32 = 31
	WIPICKernelGetResource     uint32 = 32
	wipicKernelReserved1       uint32 = 33
	wipicKernelGetDLLInterface uint32 = 36

	// An MC_knlAlloc handle is indirect: the first word points at the handle's
	// own record and payload data starts twelve bytes in.
	WIPICAllocationOverhead uint32 = 12

	// wipicAccessLevel is the group mask MC_knlGetAccessLevel reports: every
	// group is granted, because the question is what the handset permits rather
	// than what this platform then does about a particular call.
	wipicAccessLevel uint32 = 0xff
)

// WIPI-C error codes (M_E_*), as a title's own error paths expect them.
const (
	WIPICErrorNotFound    uint32 = 0xfffffff4 // M_E_NOENT (-12)
	WIPICErrorInvalid     uint32 = 0xfffffff7 // M_E_INVALID (-9)
	WIPICErrorBadFormat   uint32 = 0xfffffffa // M_E_BADFORMAT (-6)
	WIPICImageDone        uint32 = 1          // MC_GRP_IMAGE_DONE
	WIPICErrorShortBuf    uint32 = 0xffffffee // M_E_SHORTBUF (-18)
	WIPICErrorExists      uint32 = 0xfffffffb // M_E_EXIST (-5)
	wipicErrorBadParam    uint32 = 0xffffffea // M_E_BADRECID (-22)
	WIPICErrorGeneric     uint32 = 0xffffffff // -1
	WIPICErrorBadFD       uint32 = 0xfffffffe // M_E_BADFD (-2)
	WIPICErrorBadFileName uint32 = 0xfffffffd // M_E_BADFILENAME (-3)
	WIPICErrorNotEmpty    uint32 = 0xfffffff1 // M_E_NOTEMPTY (-15)
	WIPICErrorLongName    uint32 = 0xfffffff5 // M_E_LONGNAME (-11)
	WIPICErrorNoSpace     uint32 = 0xfffffff3 // M_E_NOSPACE (-13)

	// WipiErrorCode is the generic WIPI-C failure a call that cannot be served
	// answers with.
	WipiErrorCode uint32 = 0xffffffff
)

// The WIPI-C interface tables follow the original ordering. A platform that
// orders its tables otherwise maps its numbers onto these, as the SKT Clet's
// root table does.
const (
	WIPICTableUtil           = 1
	WIPICTableMisc           = 2
	WIPICTableGraphics       = 3
	WIPICTableInputMethod    = 4
	wipicTableRecordDatabase = 5
	WIPICTableFilesystem     = 7
	wipicTableUIC            = 9
	WIPICTableMedia          = 10
	WIPICTableNet            = 11

	// A handset library reached through MC_knlGetDLLInterface is dispatched
	// through the same category under a table number no interface table can
	// take.
	wipicTableUserMemory = 64

	// WIPICTableFunctions bounds each WIPI-C interface table.
	WIPICTableFunctions = 64
)

// The net table's first two functions are connect and close.
const (
	WIPICNetConnect = 0
	WIPICNetClose   = 1
)

const (
	// SessionDefaultTimerLimit bounds how many timers a service round fires.
	SessionDefaultTimerLimit = 4
	// MaxPendingTimers bounds the live timer set a program can arm.
	MaxPendingTimers = 256
	// CollectionFloor is the arena growth that triggers a platform's first
	// collection and the smallest growth that triggers a later one.
	CollectionFloor uint64 = 256 << 10
)

// Storage scopes name the save-key prefix each table's files live under. The
// WIPI-C file table is "db"; the others belong to a platform's Java storage,
// and ReservedStorageName knows every scope so no table can store under
// another's bookkeeping names. See wipic_filesystem.go.
const (
	GuestFileScope      = "fs"
	CFileScope          = "db"
	RecordDatabaseScope = "rdb"
	JavaDatabaseScope   = "jdb"
)

// ReservedStorageNames is the set of names, per scope, that address a list the
// table itself keeps rather than a file a title may store.
var ReservedStorageNames = map[string]map[string]bool{
	GuestFileScope:      {".removed": true, ".dirs": true},
	CFileScope:          {".removed": true, ".dirs": true},
	RecordDatabaseScope: {".removed": true},
	JavaDatabaseScope:   {".removed": true, ".sizes": true},
}

const (
	// GuestStringBuffer is the stack buffer a guest-string scan reads through.
	GuestStringBuffer = 256
	// hookWindowLimit is the largest scratch window a memory hook keeps
	// between calls.
	hookWindowLimit = 256 << 10
	// MaxReleasedWIPIC is how many given-back identifiers are remembered, so a
	// second free of one is told from a free of something never allocated.
	MaxReleasedWIPIC = 256
	// MaxWIPIFileName is the longest WIPI file name, in bytes.
	MaxWIPIFileName = 255
	// SVCStubSize is how many bytes one supervisor-call stub occupies.
	SVCStubSize = 16
	// WIPICGraphicsContextSize is the size of the guest's graphics context.
	WIPICGraphicsContextSize = 52
	// MaxTimerDelayMillis caps a timer delay at an hour of guest time.
	MaxTimerDelayMillis = 60 * 60 * 1000
	// MaxAOTCallDepth bounds how deeply guest calls nest on one thread.
	MaxAOTCallDepth = 64

	// DiagnosticNameLimit bounds how many distinct call-site-qualified names
	// are counted, so a guest cannot grow the map without limit, and
	// DiagnosticNameCeiling how many names are kept at all once their sites
	// are collapsed.
	DiagnosticNameLimit   = 4096
	DiagnosticNameCeiling = 2 * DiagnosticNameLimit

	// StorageBudget is the on-disk size the WIPI-C file table stays within.
	StorageBudget = WIPICFileStorageLimit
)

// wipicZeroFill is a page of zeroes the handlers clear guest memory with.
var wipicZeroFill [1024]byte

// guestStringChunk is how many bytes scanGuestString asks memory for in its
// first read. The names a title registers its classes, methods and fields
// under, and the lines it draws, are a few dozen bytes, so one read usually
// holds the whole of one.
const guestStringChunk = 64

// ErrGuestStringOverflow is the answer for a string that runs off the top of
// the address space before it ends.
var ErrGuestStringOverflow = errors.New("string address overflows guest memory")

const (
	// execNameSeparator divides a listing's two columns and execNameTrailer
	// ends the entry. Their values are not known — the one caller never reads
	// them, it only steps over the separator — so they are the least
	// surprising printable choice rather than a claim.
	execNameSeparator = "\t"
	execNameTrailer   = "\t000"
)

// guestFileRemovedKey is the save key of the removal list the guest's files
// keep.
const guestFileRemovedKey = "fs/.removed"
