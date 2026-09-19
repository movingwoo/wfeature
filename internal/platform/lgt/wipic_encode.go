package lgt

import (
	"image"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/wipic"
)

// MC_grpEncodeImage(src, x, y, w, h, *len) returns a caller-owned BMP buffer.
// The observed slot follows the decoder and precedes postEvent; its six
// arguments and subsequent createImage call agree with the WIPI contract.
func (client *Client) handleEncodeImage(thread *armcore.Thread) error {
	var args [6]uint32
	for i := range args {
		value, err := client.wipicArgument(thread, i)
		if err != nil {
			return err
		}
		args[i] = value
	}
	if args[5] != 0 {
		if err := client.writeWord(args[5], 0); err != nil {
			return err
		}
	}
	fail := func() error { return thread.SetRegister(0, 0) }
	source := client.framebuffer(args[0])
	if source == nil {
		return fail()
	}
	x, y, w, h := int64(int32(args[1])), int64(int32(args[2])), int64(int32(args[3])), int64(int32(args[4]))
	// Clip in widened arithmetic before allocating or indexing the pixel data.
	right, bottom := min(x+w, int64(source.width)), min(y+h, int64(source.height))
	x, y = max(x, 0), max(y, 0)
	if w <= 0 || h <= 0 || right <= x || bottom <= y {
		return fail()
	}
	if err := client.syncFromGuest(source); err != nil {
		return err
	}
	picture := image.NewRGBA(image.Rect(0, 0, int(right-x), int(bottom-y)))
	for row := y; row < bottom; row++ {
		for col := x; col < right; col++ {
			r, g, b := unpack565(source.pixels[int(row)*source.width+int(col)])
			at := picture.PixOffset(int(col-x), int(row-y))
			picture.Pix[at], picture.Pix[at+1], picture.Pix[at+2], picture.Pix[at+3] = byte(r), byte(g), byte(b), 255
		}
	}
	encoded, err := wipic.EncodeBitmap(picture)
	if err != nil {
		return fail()
	}
	address, ok := client.heap.allocate(uint64(len(encoded)))
	if !ok {
		return fail()
	}
	if err := client.core.Memory().Write(address, encoded); err != nil {
		client.heap.release(address)
		return err
	}
	if args[5] != 0 {
		if err := client.writeWord(args[5], uint32(len(encoded))); err != nil {
			client.heap.release(address)
			return err
		}
	}
	return thread.SetRegister(0, address)
}
