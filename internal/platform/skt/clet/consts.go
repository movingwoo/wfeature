package clet

// This file collects the platform constants the Clet runtime shares with the
// WIPI-C handlers it copied from the KTF engine. A Clet is a WIPI-C program run
// with no Java around it, so only the WIPI-C half of the KTF constant surface
// is kept here; the interface-table numbers, error codes and memory layout are
// the ones the handlers in this package were written against.

const (
	// The guest image sits at ImageBase; the data and BSS a Clet carries sit
	// below the stack at their own linked address (see native_executable.go).
	ImageBase       uint32 = 0x00100000
	ReturnAddress   uint32 = 0x7f000000
	ThreadStackBase uint32 = 0x20000000
	ThreadStackSize uint64 = 1 << 20

	// The platform arena hands out guest allocations; the stub arena sits above
	// it and holds the callback veneers the root table points into.
	platformDataBase uint32 = 0x30000000
	platformDataSize uint64 = 64 << 20
	platformCodeBase uint32 = 0x35000000
	platformCodeSize uint64 = 1 << 20

	maxPlatformAllocation uint64 = 4 << 20
)

const (
	// svcCategoryWIPIC is the immediate a WIPI-C interface stub carries; the
	// Clet root table's stubs carry svcCategoryClet, and the semihosting
	// immediate is answered only for a Clet. See clet.go and runtime.go.
	svcCategoryWIPIC uint32 = 3
	svcCategoryClet  uint32 = 10
	svcSemihosting   uint32 = 0xab
)

// The WIPI-C kernel interface numbers, as the specification orders them and the
// handlers in this package expect them.
const (
	wipicKernelPrintk          uint32 = 0
	wipicKernelSprintk         uint32 = 1
	wipicKernelGetExecNames    uint32 = 2
	wipicKernelLoad            uint32 = 5
	wipicKernelExit            uint32 = 7
	wipicKernelGetCurProgramID uint32 = 9
	wipicKernelGetAccessLevel  uint32 = 13
	wipicKernelGetProgramName  uint32 = 14
	wipicKernelAlloc           uint32 = 20
	wipicKernelCalloc          uint32 = 21
	wipicKernelFree            uint32 = 22
	wipicKernelGetTotalMemory  uint32 = 23
	wipicKernelGetFreeMemory   uint32 = 24
	wipicKernelDefTimer        uint32 = 25
	wipicKernelSetTimer        uint32 = 26
	wipicKernelUnsetTimer      uint32 = 27
	wipicKernelCurrentTime     uint32 = 28
	wipicKernelGetSysProperty  uint32 = 29
	wipicKernelSetSysProperty  uint32 = 30
	wipicKernelGetResourceID   uint32 = 31
	wipicKernelGetResource     uint32 = 32
	wipicKernelReserved1       uint32 = 33
	wipicKernelGetDLLInterface uint32 = 36

	// An MC_knlAlloc handle is indirect: the first word points at the handle's
	// own record and payload data starts twelve bytes in.
	wipicAllocationOverhead uint32 = 12

	// wipicAccessLevel is the group mask MC_knlGetAccessLevel reports: every
	// group is granted, because the question is what the handset permits rather
	// than what this platform then does about a particular call.
	wipicAccessLevel uint32 = 0xff
)

// WIPI-C error codes (M_E_*), as a title's own error paths expect them.
const (
	wipicErrorNotFound    uint32 = 0xfffffff4 // M_E_NOENT (-12)
	wipicErrorInvalid     uint32 = 0xfffffff7 // M_E_INVALID (-9)
	wipicErrorBadFormat   uint32 = 0xfffffffa // M_E_BADFORMAT (-6)
	wipicImageDone        uint32 = 1          // MC_GRP_IMAGE_DONE
	wipicErrorShortBuf    uint32 = 0xffffffee // M_E_SHORTBUF (-18)
	wipicErrorExists      uint32 = 0xfffffffb // M_E_EXIST (-5)
	wipicErrorBadParam    uint32 = 0xffffffea // M_E_BADRECID (-22)
	wipicErrorGeneric     uint32 = 0xffffffff // -1
	wipicErrorBadFD       uint32 = 0xfffffffe // M_E_BADFD (-2)
	wipicErrorBadFileName uint32 = 0xfffffffd // M_E_BADFILENAME (-3)
	wipicErrorNotEmpty    uint32 = 0xfffffff1 // M_E_NOTEMPTY (-15)
	wipicErrorLongName    uint32 = 0xfffffff5 // M_E_LONGNAME (-11)
	wipicErrorNoSpace     uint32 = 0xfffffff3 // M_E_NOSPACE (-13)

	// wipiErrorCode is the generic WIPI-C failure a call that cannot be served
	// answers with.
	wipiErrorCode uint32 = 0xffffffff
)

// The WIPI-C interface tables follow the original ordering. The Clet root table
// (clet_tables.go) maps SKT's own ordering onto these numbers.
const (
	wipicTableUtil           = 1
	wipicTableMisc           = 2
	wipicTableGraphics       = 3
	wipicTableInputMethod    = 4
	wipicTableRecordDatabase = 5
	wipicTableFilesystem     = 7
	wipicTableUIC            = 9
	wipicTableMedia          = 10
	wipicTableNet            = 11

	// A handset library reached through MC_knlGetDLLInterface is dispatched
	// through the same category under a table number no interface table can
	// take.
	wipicTableUserMemory = 64

	// wipicTableFunctions bounds each WIPI-C interface table.
	wipicTableFunctions = 64
)

// The net table's first two functions are connect and close.
const (
	wipicNetConnect = 0
	wipicNetClose   = 1
)

const (
	// sessionDefaultTimerLimit bounds how many timers a service round fires.
	sessionDefaultTimerLimit = 4
	// maxPendingTimers bounds the live timer set a Clet can arm.
	maxPendingTimers = 256
	// collectionFloor is the arena size the collector first triggers at; a Clet
	// has no Java heap to collect, but the arena keeps the floor as its initial
	// high-water mark.
	collectionFloor uint64 = 256 << 10
)
