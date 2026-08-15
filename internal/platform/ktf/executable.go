package ktf

import (
	"encoding/binary"
	"fmt"
)

const (
	wipiExecutableWords      = 10
	executableInterfaceWords = 8
	executableFunctionWords  = 7
	maxExecutableNameBytes   = 128
)

// Executable is the validated top-level descriptor returned by a KTF client
// after its entry point has performed self-relocation.
type Executable struct {
	Address   uint32
	Name      string
	Init      uint32
	Unknown1  uint32
	Unknown3  uint32
	Interface ExecutableInterface
}

// ExecutableInterface describes the app-owned interface table reachable from
// Executable.
type ExecutableInterface struct {
	Address   uint32
	Name      string
	Functions ExecutableFunctions
}

// ExecutableFunctions contains the callable app entry points used during KTF
// initialization and later class lookup.
type ExecutableFunctions struct {
	Address       uint32
	Init          uint32
	GetDefaultDLL uint32
	GetClass      uint32
	Unknown2      uint32
	Unknown3      uint32
}

// ReadExecutable follows and validates the WipiExe, ExeInterface, and
// ExeInterfaceFunctions pointers returned by ExecuteEntry. All pointers remain
// guest addresses; callers must still use Core.Call to invoke function fields.
func (client *Client) ReadExecutable(address uint32) (Executable, error) {
	if client == nil || client.core == nil {
		return Executable{}, fmt.Errorf("KTF client is not initialized")
	}
	client.run.Lock()
	defer client.run.Unlock()
	return client.readExecutable(address)
}

func (client *Client) readExecutable(address uint32) (Executable, error) {
	wipi, err := client.readExecutableWords("WipiExe", address, wipiExecutableWords)
	if err != nil {
		return Executable{}, err
	}
	name, err := client.readExecutableName("WipiExe name", wipi[1])
	if err != nil {
		return Executable{}, err
	}
	if name != "WIPI_exe" {
		return Executable{}, fmt.Errorf("KTF WipiExe name = %q, want %q", name, "WIPI_exe")
	}
	for _, function := range []struct {
		name     string
		address  uint32
		required bool
	}{
		{name: "WipiExe unknown function 1", address: wipi[4]},
		{name: "WipiExe init", address: wipi[5], required: true},
		{name: "WipiExe unknown function 3", address: wipi[8]},
	} {
		if err := client.validateExecutableFunction(function.name, function.address, function.required); err != nil {
			return Executable{}, err
		}
	}

	interfaceWords, err := client.readExecutableWords("ExeInterface", wipi[0], executableInterfaceWords)
	if err != nil {
		return Executable{}, err
	}
	interfaceName, err := client.readExecutableName("ExeInterface name", interfaceWords[1])
	if err != nil {
		return Executable{}, err
	}
	if interfaceName != "ExeInterface" {
		return Executable{}, fmt.Errorf("KTF ExeInterface name = %q, want %q", interfaceName, "ExeInterface")
	}

	functionWords, err := client.readExecutableWords("ExeInterfaceFunctions", interfaceWords[0], executableFunctionWords)
	if err != nil {
		return Executable{}, err
	}
	for _, function := range []struct {
		name     string
		address  uint32
		required bool
	}{
		{name: "ExeInterface init", address: functionWords[2], required: true},
		{name: "ExeInterface get-default-dll", address: functionWords[3]},
		{name: "ExeInterface get-class", address: functionWords[4], required: true},
		{name: "ExeInterface unknown function 2", address: functionWords[5]},
		{name: "ExeInterface unknown function 3", address: functionWords[6]},
	} {
		if err := client.validateExecutableFunction(function.name, function.address, function.required); err != nil {
			return Executable{}, err
		}
	}

	return Executable{
		Address:  address,
		Name:     name,
		Unknown1: wipi[4],
		Init:     wipi[5],
		Unknown3: wipi[8],
		Interface: ExecutableInterface{
			Address: wipi[0],
			Name:    interfaceName,
			Functions: ExecutableFunctions{
				Address:       interfaceWords[0],
				Init:          functionWords[2],
				GetDefaultDLL: functionWords[3],
				GetClass:      functionWords[4],
				Unknown2:      functionWords[5],
				Unknown3:      functionWords[6],
			},
		},
	}, nil
}

func (client *Client) readExecutableWords(name string, address uint32, count uint32) ([]uint32, error) {
	size := uint64(count) * 4
	if address&3 != 0 {
		return nil, fmt.Errorf("KTF %s pointer %#x is not word-aligned", name, address)
	}
	if !client.containsImageRange(address, size) {
		return nil, fmt.Errorf("KTF %s range [%#x, %#x) is outside the client image", name, address, uint64(address)+size)
	}
	data := make([]byte, size)
	if err := client.core.Memory().Read(address, data); err != nil {
		return nil, fmt.Errorf("read KTF %s at %#x: %w", name, address, err)
	}
	words := make([]uint32, count)
	for index := range words {
		words[index] = binary.LittleEndian.Uint32(data[index*4:])
	}
	return words, nil
}

func (client *Client) readExecutableName(name string, address uint32) (string, error) {
	if !client.containsImageRange(address, 1) {
		return "", fmt.Errorf("KTF %s pointer %#x is outside the client image", name, address)
	}
	data := make([]byte, 0, maxExecutableNameBytes)
	for offset := uint64(0); offset < maxExecutableNameBytes; offset++ {
		current := uint64(address) + offset
		if current >= uint64(ImageBase)+client.image.MappedSize() {
			return "", fmt.Errorf("KTF %s at %#x is not null-terminated inside the client image", name, address)
		}
		var value [1]byte
		if err := client.core.Memory().Read(uint32(current), value[:]); err != nil {
			return "", fmt.Errorf("read KTF %s at %#x: %w", name, current, err)
		}
		if value[0] == 0 {
			return string(data), nil
		}
		data = append(data, value[0])
	}
	return "", fmt.Errorf("KTF %s at %#x exceeds %d bytes", name, address, maxExecutableNameBytes)
}

func (client *Client) validateExecutableFunction(name string, address uint32, required bool) error {
	if address == 0 {
		if required {
			return fmt.Errorf("KTF %s pointer is null", name)
		}
		return nil
	}
	target := address &^ 1
	size := uint64(2)
	if address&1 == 0 {
		if address&3 != 0 {
			return fmt.Errorf("KTF %s ARM pointer %#x is not word-aligned", name, address)
		}
		size = 4
	}
	if !client.containsImageRange(target, size) {
		return fmt.Errorf("KTF %s pointer %#x is outside the client image", name, address)
	}
	return nil
}

func (client *Client) containsImageRange(address uint32, size uint64) bool {
	start := uint64(address)
	end := start + size
	imageStart := uint64(ImageBase)
	imageEnd := imageStart + client.image.MappedSize()
	return size != 0 && end >= start && start >= imageStart && end <= imageEnd
}
