package lgt

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
)

// Assemble connected functions at independent addresses with invented tokens.
func certificate100Fixture(t *testing.T, delta uint32) (*Module, []byte, []byte) {
	t.Helper()
	code, data := make([]byte, 0x3000), make([]byte, 0x800)
	textBase, dataBase := fixtureTextBase+delta, fixtureDataBase+0x10000+delta
	module := &Module{Sections: []Section{{Address: textBase, Size: uint32(len(code)), Data: code, Executable: true}, {Address: dataBase, Size: uint32(len(data)), Data: data}}}
	call := func(offset, target uint32) {
		d := target - (textBase + offset) - 4
		binary.LittleEndian.PutUint16(code[offset:], 0xf000|uint16(d>>12)&0x7ff)
		binary.LittleEndian.PutUint16(code[offset+2:], 0xf800|uint16(d>>1)&0x7ff)
	}
	literal := func(offset, value uint32) {
		op := binary.LittleEndian.Uint16(code[offset:])
		pool := ((textBase + offset + 4) &^ 3) + uint32(op&255)*4
		binary.LittleEndian.PutUint32(code[pool-textBase:], value)
	}
	parts := []struct {
		offset  uint32
		pattern []uint16
	}{{0x400, certificate100Reader}, {0x800, certificate100Writer}, {0xc00, certificate100Gate}, {0x1000, certificate100Decoder}, {0x1400, certificate100Encoder}, {0x1800, certificate100HeaderReader}, {0x2000, certificate100Subscriber}}
	for _, p := range parts {
		for i := 0; i < len(p.pattern); i++ {
			off := p.offset + uint32(i*2)
			op := p.pattern[i]
			if op == 0 {
				call(off, textBase+0x2800)
				i++
				continue
			}
			binary.LittleEndian.PutUint16(code[off:], op)
			if op&0xf800 == 0x4800 {
				literal(off, dataBase+0x700)
			}
		}
	}
	for _, off := range []uint32{0x43a, 0x82a} {
		call(off, textBase+0x2804)
	}
	for _, off := range []uint32{0x1854, 0x186c, 0x1876, 0x1882, 0x188e, 0x189a, 0x18a8, 0x18b6, 0x18c4, 0x18d0, 0x18de, 0x18ec, 0x18f8, 0x1904, 0x1910} {
		call(off, textBase+0x2808)
	}
	call(0xc16, textBase+0x400)
	for _, p := range [][2]uint32{{0x468, textBase + 0x1001}, {0x183a, textBase + 0x1001}, {0x83a, textBase + 0x1401}, {0x424, textBase + 0x2001}, {0x814, textBase + 0x2001}, {0x46c, 0x1571}, {0x836, 0x1571}, {0x1834, 0x1bb2}, {0xc02, dataBase + 0x400}, {0x872, dataBase + 0x400}, {0x1864, dataBase + 0x400}} {
		literal(p[0], p[1])
	}
	for _, off := range []uint32{0x404, 0x842, 0x1804} {
		literal(off, dataBase+0x100)
	}
	for _, off := range []uint32{0x42a, 0x81a} {
		literal(off, dataBase+0x140)
	}
	for _, off := range []uint32{0x432, 0x820} {
		literal(off, dataBase+0x180)
	}
	for _, off := range []uint32{0x434, 0x826} {
		literal(off, dataBase+0x1c0)
	}
	literal(0x2006, dataBase+0x200)
	for _, off := range []uint32{0x1018, 0x1418} {
		literal(off, dataBase+0x240)
	}
	for _, off := range []uint32{0x101a, 0x141a} {
		literal(off, dataBase+0x244)
	}
	copy(data[0x100:], "state.dat\x00")
	copy(data[0x140:], "Example\x00")
	copy(data[0x180:], "%s%s%s\x00")
	copy(data[0x1c0:], "TESTAPP\x00")
	copy(data[0x200:], "PHONENUMBER\x00")
	binary.LittleEndian.PutUint32(data[0x240:], 1951)
	binary.LittleEndian.PutUint32(data[0x244:], 42537)
	for i, op := range []uint16{0x4718, 0x4720, 0x4728} {
		binary.LittleEndian.PutUint16(code[0x2800+i*4:], op)
	}
	return module, code, data
}

func TestCertificate100RequiresConnectedContracts(t *testing.T) {
	for _, delta := range []uint32{0, 0x310000} {
		module, code, data := certificate100Fixture(t, delta)
		contract := authenticationCertificate100(module)
		if contract == nil || contract.name != "state.dat" || contract.application != "TESTAPP" || contract.token != "Example" {
			t.Fatalf("unrecognized fixture at %#x: %+v", delta, contract)
		}
		for _, p := range []struct {
			offset  int
			pattern []uint16
		}{{0x400, certificate100Reader}, {0x800, certificate100Writer}, {0xc00, certificate100Gate}, {0x1000, certificate100Decoder}, {0x1400, certificate100Encoder}, {0x1800, certificate100HeaderReader}, {0x2000, certificate100Subscriber}} {
			for i := range p.pattern {
				off := p.offset + i*2
				code[off+1] ^= 0x80
				matched := authenticationCertificate100(module)
				code[off+1] ^= 0x80
				if matched != nil {
					t.Fatalf("changed instruction %#x accepted", off)
				}
			}
		}
		data[0x200] = 'X'
		if authenticationCertificate100(module) != nil {
			t.Fatal("wrong subscriber property accepted")
		}
		data[0x200] = 'P'
		// Two independently relocated contracts are ambiguous.
		other, _, _ := certificate100Fixture(t, delta+0x620000)
		module.Sections = append(module.Sections, other.Sections...)
		if authenticationCertificate100(module) != nil {
			t.Fatal("ambiguous reader accepted")
		}
	}
	if authenticationCertificate100(nil) != nil {
		t.Fatal("nil module accepted")
	}
}

func TestCertificate100PreservesProgressAndOriginalCertificate(t *testing.T) {
	module, _, _ := certificate100Fixture(t, 0)
	contract := authenticationCertificate100(module)
	if contract == nil {
		t.Fatal("missing fixture")
	}
	base := backend.NewDirectorySaveStore(t.TempDir())
	store := newAuthenticationCertificate100Store(base, contract)
	original := make([]byte, 820)
	header := make([]byte, 100)
	header[0] = 40
	header[27] = 0
	for i := 100; i < len(original); i++ {
		original[i] = byte(i * 17)
	}
	store.putHeader(original, header, 0)
	if err := store.StoreSave(store.key, original); err != nil {
		t.Fatal(err)
	}
	store.active = true
	store.originalFlag = 0
	store.originalCertificate = bytes.Clone(original[100:200])
	store.certificate = bytes.Repeat([]byte{0x42}, 100)
	view, ok, err := store.ReadSave(store.key)
	if err != nil || !ok {
		t.Fatalf("read %v/%v", ok, err)
	}
	decoded, valid := store.header(view)
	if !valid || decoded[27] != 1 || !bytes.Equal(view[100:200], store.certificate) {
		t.Fatal("private certificate missing")
	}
	if !bytes.Equal(view[200:], original[200:]) {
		t.Fatal("progress changed on read")
	}
	decoded[4] = 23
	store.putHeader(view, decoded, 1)
	view[319] ^= 0xff
	if err := store.StoreSave(store.key, view); err != nil {
		t.Fatal(err)
	}
	persisted, _, err := backend.ReadSave(base, store.key)
	if err != nil {
		t.Fatal(err)
	}
	decoded, valid = store.header(persisted)
	if !valid || decoded[27] != 0 || decoded[4] != 23 || !bytes.Equal(persisted[100:200], original[100:200]) || !bytes.Equal(persisted[200:], view[200:]) {
		t.Fatal("write did not preserve private fields and progress independently")
	}
	for _, size := range []int{0, 99, 100, 199} {
		if _, ok := store.header(make([]byte, size)); ok {
			t.Fatalf("short file %d accepted", size)
		}
	}
	corrupt := bytes.Clone(original)
	corrupt[0] ^= 1
	if _, ok := store.header(corrupt); ok {
		t.Fatal("bad checksum accepted")
	}
	if err := store.StoreSave(store.key, corrupt); err == nil {
		t.Fatal("malformed write accepted")
	}
	after, _, _ := backend.ReadSave(base, store.key)
	if !bytes.Equal(after, persisted) {
		t.Fatal("malformed write changed backing data")
	}
}

func TestCertificate100CipherKnownVector(t *testing.T) {
	// Hand-calculated short stream, including feedback from ciphertext.
	plain := []byte{0, 1, 2, 255}
	encoded := bytes.Clone(plain)
	cryptCertificate100(encoded, 0x1571, 1951*42537, true)
	want := []byte{0x15, 0x2b, 0xb2, 0x3c}
	if !bytes.Equal(encoded, want) {
		t.Fatalf("cipher = %x, want %x", encoded, want)
	}
	cryptCertificate100(encoded, 0x1571, 1951*42537, false)
	if !bytes.Equal(encoded, plain) {
		t.Fatal("cipher did not round trip")
	}
}

func TestCertificate100ActivationAndReinitialization(t *testing.T) {
	client := fixtureClient(t)
	state, err := client.allocate(48)
	if err != nil {
		t.Fatal(err)
	}
	contract := &certificate100Contract{name: "state.dat", application: client.archive.Descriptor.AID, token: "Example", state: state, seed: 0x1571, headerSeed: 0x1bb2, multiplier: 1951 * 42537}
	store := newAuthenticationCertificate100Store(nil, contract)
	client.saveStore = store
	data := make([]byte, 400)
	header := make([]byte, 100)
	header[0] = 40
	store.putHeader(data, header, 0)
	if err := store.StoreSave(store.key, data); err != nil {
		t.Fatal(err)
	}
	plain, valid := store.header(data)
	if !valid {
		t.Fatal("invalid authored header")
	}
	if err := client.core.Memory().Write(state, plain[:48]); err != nil {
		t.Fatal(err)
	}
	if !store.activate(client) {
		t.Fatal("valid live state refused")
	}
	view, _, err := store.ReadSave(store.key)
	if err != nil {
		t.Fatal(err)
	}
	certificate := bytes.Clone(view[100:200])
	expected := client.subscriberNumber + contract.application + contract.token
	cryptCertificate100(certificate[:len(expected)], contract.seed, contract.multiplier, false)
	if string(certificate[:len(expected)]) != expected {
		t.Fatal("wrong session identity")
	}
	// A second guest initialization publishes defaults after the first notice.
	if err := client.core.Memory().Write(state, plain[:48]); err != nil {
		t.Fatal(err)
	}
	if err := store.StoreSave(store.key, data); err != nil {
		t.Fatal(err)
	}
	live := make([]byte, 48)
	if err := client.core.Memory().Read(state, live); err != nil {
		t.Fatal(err)
	}
	if live[27] != 1 {
		t.Fatal("reinitialization lost the private certificate flag")
	}
	persisted := store.volatile[store.key]
	restored, valid := store.header(persisted)
	if !valid || restored[27] != 0 || !bytes.Equal(persisted[100:200], data[100:200]) {
		t.Fatal("private certificate persisted")
	}
	// An unrelated in-memory state must not be overwritten by a saved header.
	live[0]++
	live[27] = 0
	_ = client.core.Memory().Write(state, live)
	_ = store.StoreSave(store.key, data)
	got := make([]byte, 48)
	_ = client.core.Memory().Read(state, got)
	if !bytes.Equal(got, live) {
		t.Fatal("unrelated live state overwritten")
	}
}

func TestCertificate100RejectsMissingDataAndDisconnectedState(t *testing.T) {
	module, code, data := certificate100Fixture(t, 0)
	for _, size := range []int{0, 2, 0x400, 0x490, 0x1800, 0x2808} {
		module.Sections[0].Data = code[:size]
		if authenticationCertificate100(module) != nil {
			t.Fatalf("truncated code %d accepted", size)
		}
	}
	module.Sections[0].Data = code
	for _, size := range []int{0, 0x100, 0x183, 0x200, 0x242} {
		module.Sections[1].Data = data[:size]
		if authenticationCertificate100(module) != nil {
			t.Fatalf("truncated data %d accepted", size)
		}
	}
	module.Sections[1].Data = data
	// Relocate only the gate's state pool, leaving reader and writer untouched.
	address := fixtureTextBase + 0xc02
	op := binary.LittleEndian.Uint16(code[0xc02:])
	pool := ((address + 4) &^ 3) + uint32(op&255)*4 - fixtureTextBase
	binary.LittleEndian.PutUint32(code[pool:], module.Sections[1].Address+0x404)
	if authenticationCertificate100(module) != nil {
		t.Fatal("disconnected state accepted")
	}
}
