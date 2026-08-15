package ktf

import (
	"context"
	"fmt"
	"os"
	"testing"
)

// TestLocalSymbolizeProbe resolves the crash addresses of a local archive to
// AOT method names. It is a throwaway investigation aid: it skips unless the
// archive and address list are supplied.
func TestLocalSymbolizeProbe(t *testing.T) {
	path := os.Getenv("WFEATURE_SYMBOLIZE_ARCHIVE")
	if path == "" {
		t.Skip("set WFEATURE_SYMBOLIZE_ARCHIVE")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	session, err := StartSession(context.Background(), data, SessionOptions{})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer session.Close()
	// Classes register as they load, so the index is only useful after the
	// game has actually run for a while.
	for round := 0; round < 1500; round++ {
		if _, err := session.Tick(context.Background()); err != nil {
			t.Logf("tick %d: %v", round, err)
			break
		}
	}
	runtime := session.Client.runtime
	// Is 0x188d48 a registered class, and what is class bn?
	if class, ok := session.Client.vm.AOTClassAt(0x188d48); ok {
		fmt.Printf("  0x188d48 is CLASS %s (vtable %#x, methods %d)\n", class.Name, class.VTableAddress, len(class.Methods))
	} else {
		fmt.Printf("  0x188d48 is not a registered class\n")
	}
	for _, name := range []string{"bn", "bf", "bl", "ad"} {
		class, ok := session.Client.vm.AOTClass(name)
		if !ok {
			fmt.Printf("  class %s: not registered\n", name)
			continue
		}
		fmt.Printf("  class %s @%#x super=%s methods=%d\n", name, class.Address, class.SuperName, len(class.Methods))
		for _, m := range class.Methods {
			fmt.Printf("      %s%s addr=%#x body=%#x native=%#x flags=%#x vt=%d\n",
				m.Name, m.Descriptor, m.Address, m.Body, m.NativeBody, m.AccessFlags, m.VTableIndex)
		}
	}
	_ = runtime
	// Compare each cached dispatch alias against the class record it was
	// copied from. Word 0 is deliberately overwritten, so it is skipped.
	stale, checked := 0, 0
	for classAddress, alias := range runtime.classAliases {
		record, err1 := runtime.readAOTBytes(classAddress, javaClassSize, "probe class")
		copied, err2 := runtime.readAOTBytes(alias, javaClassSize, "probe alias")
		if err1 != nil || err2 != nil {
			continue
		}
		checked++
		name := "?"
		if m, ok := session.Client.vm.AOTClassAt(classAddress); ok {
			name = m.Name
		}
		for off := 4; off < javaClassSize; off++ {
			if record[off] != copied[off] {
				stale++
				fmt.Printf("  STALE alias %s class=%#x alias=%#x\n    class=%x\n    alias=%x\n",
					name, classAddress, alias, record, copied)
				break
			}
		}
	}
	fmt.Printf("  dispatch aliases checked=%d stale=%d\n", checked, stale)
	symbols := session.Client.runtime.aotSymbolIndex()
	fmt.Printf("  registered method bodies: %d\n", len(symbols))
	for _, address := range []uint32{0x134d3c, 0x134d71, 0x12c6f9, 0x169243, 0x167825, 0x195ca4, 0x1961b0, 0x1689e4, 0x16788d, 0x12d2ef, 0x12d31b, 0x168a0f} {
		symbol, ok := symbolizeAOTAddress(symbols, address)
		if !ok {
			symbol = "(unresolved)"
		}
		fmt.Printf("  %#x  %s\n", address, symbol)
	}
}
