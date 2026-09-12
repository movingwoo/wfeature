package skt

// decodeScriptSISReferenceTransform applies a mode-one reference stream to an
// already snapshotted object. A stream is a sequence of tile indexes and
// replacement tiles, terminated by the all-ones index. The native helper does
// not safely constrain indexes; this implementation rejects indexes outside
// the referenced object before reading or writing a replacement.
func decodeScriptSISReferenceTransform(r *scriptSISBitReader, indexWidth int, object *scriptSISLiteralObject) bool {
	if indexWidth == 0 {
		return true
	}
	terminator := uint(1<<indexWidth) - 1
	tileColumns := object.width / 8
	tileCount := tileColumns * (object.height / 8)
	for {
		tileIndex, ok := r.read(indexWidth)
		if !ok {
			return false
		}
		if tileIndex == terminator {
			return true
		}
		if int(tileIndex) >= tileCount {
			return false
		}
		coded, ok := r.read(1)
		if !ok {
			return false
		}
		encodedPixels, ok := decodeScriptSISTile(r, coded != 0)
		if !ok {
			return false
		}

		var raster [8]byte
		for encodedPosition, pixel := range encodedPixels {
			if pixel == 0 {
				continue
			}
			x, y := scriptSISLiteralPosition(encodedPosition)
			raster[y] |= 0x80 >> x
		}
		tileX := int(tileIndex) % tileColumns
		tileY := int(tileIndex) / tileColumns
		for y, row := range raster {
			object.pixels[(tileY*8+y)*tileColumns+tileX] = row
		}
	}
}
