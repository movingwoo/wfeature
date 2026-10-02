// Package testfixture builds newly authored archives for tests across Host and
// runtime packages. No game or original-runtime bytes are included.
package testfixture

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
)

const ktfImageBase uint32 = 0x00100000

// KTFCheckpointStartupCounter is incremented by the fixture's startApp body.
// Restoring a checkpoint must recover this word without incrementing it again.
const KTFCheckpointStartupCounter = ktfImageBase + 0x900

// KTFCheckpointArchive is a complete descriptor/JAR/AOT archive that starts
// through the ordinary loader. Its only behavior is a startup counter; worker,
// graphics and service continuation have separate, more detailed fixtures.
func KTFCheckpointArchive() ([]byte, error) {
	data := make([]byte, 0x1000)
	words := func(offset int, values ...uint32) {
		for index, value := range values {
			binary.LittleEndian.PutUint32(data[offset+4*index:], value)
		}
	}
	thumb := func(offset int, values ...uint16) {
		for index, value := range values {
			binary.LittleEndian.PutUint16(data[offset+2*index:], value)
		}
	}
	// Entry returns the executable record. Both initialization callbacks return
	// zero, and GetClass returns the one authored application class.
	thumb(0x00, 0x4800, 0x4770) // ldr r0,[pc]; bx lr
	words(0x04, ktfImageBase+0x40)
	thumb(0x20, 0x2000, 0x4770) // movs r0,#0; bx lr
	thumb(0x28, 0x4800, 0x4770)
	words(0x2c, ktfImageBase+0x400)
	words(0x40, ktfImageBase+0x80, ktfImageBase+0xc0, 0, 0, 0, ktfImageBase+0x21, 0, 0, 0, 0)
	words(0x80, ktfImageBase+0xa0, ktfImageBase+0xd0, 0, 0, 0, 0, 0, 0)
	words(0xa0, 0, 0, ktfImageBase+0x21, 0, ktfImageBase+0x29, 0, 0)
	copy(data[0xc0:], "WIPI_exe\x00")
	copy(data[0xd0:], "ExeInterface\x00")
	thumb(0x100, 0x4770) // constructor: bx lr
	// startApp: load the counter, increment it, store it, return.
	thumb(0x110, 0x4802, 0x6801, 0x3101, 0x6001, 0x4770, 0x46c0)
	words(0x11c, KTFCheckpointStartupCounter)
	// Class, descriptor, method table, empty field table and vtable. Method
	// names retain the AOT length-prefix byte preceding each descriptor/name.
	words(0x400, ktfImageBase+0x404, 0, ktfImageBase+0x440, ktfImageBase+0x630)
	binary.LittleEndian.PutUint16(data[0x410:], 1)
	words(0x440, ktfImageBase+0x800, 0, 0, ktfImageBase+0x600, 0, ktfImageBase+0x620)
	binary.LittleEndian.PutUint16(data[0x440+26:], 4)
	binary.LittleEndian.PutUint16(data[0x440+28:], 0x21)
	words(0x500, ktfImageBase+0x101, ktfImageBase+0x400, 0, ktfImageBase+0x820, 0, 1<<16, 0)
	words(0x520, ktfImageBase+0x111, ktfImageBase+0x400, 0, ktfImageBase+0x840, 0, 1<<16, 0)
	words(0x600, ktfImageBase+0x500, ktfImageBase+0x520, 0)
	words(0x630, ktfImageBase+0x500, ktfImageBase+0x520)
	copy(data[0x800:], "fixture/Checkpoint\x00")
	copy(data[0x820:], "\x00()V+<init>\x00")
	copy(data[0x840:], "\x00([Ljava/lang/String;)V+startApp\x00")
	jar, err := fixtureZIP([]fixtureEntry{{"client.bin0", data}})
	if err != nil {
		return nil, err
	}
	return fixtureZIP([]fixtureEntry{{"__adf__", []byte("AID:fixture\nPID:P0001\nMClass:fixture.Checkpoint\n")}, {"fixture.jar", jar}})
}

type fixtureEntry struct {
	name string
	data []byte
}

func fixtureZIP(entries []fixtureEntry) ([]byte, error) {
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, entry := range entries {
		file, err := writer.Create(entry.name)
		if err != nil {
			return nil, err
		}
		if _, err := file.Write(entry.data); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}
