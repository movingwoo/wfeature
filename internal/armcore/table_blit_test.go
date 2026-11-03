package armcore

import (
	"testing"
)

// The palette blit with its source pointer advanced before the byte is read:
// every stood-in pixel would come from one byte too early.
func TestATableBlitThatAdvancesItsSourceFirstIsRefused(t *testing.T) {
	const base, destination = 0x00100000, 0x00200000
	const palette, source = 0x70000000, 0x70000800
	const pixels = 40
	body := []uint16{
		0x3001, // adds r0, #1     — the source, before it is read
		0x7803, // ldrb r3, [r0]
		0x005b, // lsls r3, r3, #1
		0x5ad3, // ldrh r3, [r2, r3]
		0x800b, // strh r3, [r1]
		0x3102, // adds r1, #2
		0x1e63, // subs r3, r4, #1
		0x041b, // lsls r3, r3, #16
		0x0c1c, // lsrs r4, r3, #16
		0x4b04, // ldr  r3, [pc, #16]
		0x429c, // cmp  r4, r3
		0xd1f3, // bne  head
	}
	branchPC := base + uint32(len(body)-1)*2
	build := func() (*Memory, *Context) {
		memory := fillLoopMemory(t, body, base, destination)
		// The limit the counter runs down to, where `ldr r3, [pc, #16]`
		// reaches it.
		if err := memory.Load((base+18+4)&^3+16, []byte{0, 0, 0, 0}); err != nil {
			t.Fatal(err)
		}
		memory.beginQuantum()
		for index := 0; index < 256; index++ {
			if err := memory.writeData16(palette+uint32(index)*2, uint16(0x1000+index*7)); err != nil {
				t.Fatal(err)
			}
		}
		for pixel := 0; pixel <= pixels; pixel++ {
			if err := memory.write8(source+uint32(pixel), byte(pixel*5+3)); err != nil {
				t.Fatal(err)
			}
		}
		memory.endQuantum()
		value := NewContext()
		context := &value
		context.Registers[0] = source
		context.Registers[1] = destination
		context.Registers[2] = palette
		context.Registers[4] = pixels
		context.setThumbPC(base)
		return memory, context
	}
	memory, _ := build()
	memory.beginQuantum()
	loop := memory.analyseTableBlit(base, branchPC)
	memory.endQuantum()
	if loop != nil {
		t.Errorf("a pre-incremented source was recognised as a table blit: %+v", *loop)
	}
	compareStandIn(t, branchPC+2, destination, (pixels+2)*2, build)
}
