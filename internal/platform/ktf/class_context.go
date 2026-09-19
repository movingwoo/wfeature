package ktf

import (
	"fmt"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// Keep the context beside the relocated image so an object's signed 27-bit
// class displacement can retain the image's canonical class address. Runtime
// classes outside that range still use bounded dispatch aliases. Older modules
// retain their fixed context layout and well-known class slots.
func (runtime *initializationRuntime) prepareClassContext() (uint32, error) {
	const contextSize = (3 + 128) * 4
	if runtime.client.module {
		return runtime.allocateWords(make([]uint32, 3+128))
	}
	const arenaSize = 4 << 20
	base := (uint64(ImageBase) + runtime.client.mapped + 4095) &^ uint64(4095)
	if base+arenaSize > uint64(ThreadStackBase) {
		return 0, fmt.Errorf("KTF class context overlaps the thread stack")
	}
	if err := runtime.client.core.Memory().Map(uint32(base), arenaSize, armcore.PermissionReadWrite); err != nil {
		return 0, fmt.Errorf("map KTF class context: %w", err)
	}
	runtime.classArena = newGuestArena(uint32(base), arenaSize)
	return runtime.allocateClassAlias(make([]byte, contextSize))
}

func (runtime *initializationRuntime) allocateClassAlias(data []byte) (uint32, error) {
	if runtime.classArena == nil {
		return runtime.allocateBytes(data)
	}
	address, ok := runtime.classArena.allocate(uint64(len(data)))
	if !ok {
		return 0, fmt.Errorf("KTF class context space exhausted")
	}
	if err := runtime.client.core.Memory().Write(address, data); err != nil {
		return 0, fmt.Errorf("write KTF class context: %w", err)
	}
	return address, nil
}
