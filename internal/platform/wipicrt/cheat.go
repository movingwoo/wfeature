package wipicrt

// CheatLabel names a committed region of the engine's address space for the
// cheat engine, by where the region starts, and says whether it holds
// instructions rather than state. client reports that the region starts in the
// title's own image, which the platform knows and the engine does not.
func CheatLabel(base uint32, client bool) (string, bool) {
	switch {
	case client:
		return "client", false
	case base >= ThreadStackBase && base < PlatformDataBase:
		return "stack", false
	case base >= PlatformCodeBase && uint64(base) < uint64(PlatformCodeBase)+PlatformCodeSize:
		// The 64 MiB data arena overlaps the read/execute stub arena, so the
		// stub pages scan as writable. They are the platform's own veneers:
		// nothing a game's state is ever in, and a whole arena of instructions
		// for a sweep to walk.
		return "stubs", true
	case base >= PlatformDataBase && uint64(base) < uint64(PlatformDataBase)+PlatformDataSize:
		return "platform", false
	default:
		return "data", false
	}
}
