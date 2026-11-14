package tray

import "encoding/binary"

// icoBytes renders a 32x32 tray icon in memory so the binary stays a single
// file with no external assets.
func icoBytes() []byte {
	const size = 32
	px := make([]byte, size*size*4)

	set := func(x, y int, r, g, b, a byte) {
		if x < 0 || y < 0 || x >= size || y >= size {
			return
		}
		i := (y*size + x) * 4
		px[i] = b
		px[i+1] = g
		px[i+2] = r
		px[i+3] = a
	}

	// Dark rounded slab.
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			if corner(x, y, size) {
				continue
			}
			shade := byte(24)
			if y < 4 {
				shade = 30
			}
			set(x, y, shade, shade+4, shade+10, 255)
		}
	}
	// Cyan "L" + baseline, the LCS mark.
	for y := 8; y <= 23; y++ {
		for x := 9; x <= 12; x++ {
			set(x, y, 90, 210, 235, 255)
		}
	}
	for y := 20; y <= 23; y++ {
		for x := 9; x <= 22; x++ {
			set(x, y, 90, 210, 235, 255)
		}
	}
	// Accent dot.
	for y := 9; y <= 13; y++ {
		for x := 17; x <= 21; x++ {
			set(x, y, 240, 244, 250, 255)
		}
	}

	mask := make([]byte, size*size/8)
	xor := make([]byte, 0, len(px))
	// ICO bitmaps are bottom-up.
	for y := size - 1; y >= 0; y-- {
		xor = append(xor, px[y*size*4:(y+1)*size*4]...)
	}

	const header = 6 + 16 + 40
	imageSize := 40 + size*size*4 + len(mask)

	out := make([]byte, header, header+imageSize)
	binary.LittleEndian.PutUint16(out[0:], 0)
	binary.LittleEndian.PutUint16(out[2:], 1)
	binary.LittleEndian.PutUint16(out[4:], 1)

	out[6] = size
	out[7] = size
	out[8] = 0
	out[9] = 0
	binary.LittleEndian.PutUint16(out[10:], 1)
	binary.LittleEndian.PutUint16(out[12:], 32)
	binary.LittleEndian.PutUint32(out[14:], uint32(imageSize))
	binary.LittleEndian.PutUint32(out[18:], 22)

	h := out[22:]
	binary.LittleEndian.PutUint32(h[0:], 40)
	binary.LittleEndian.PutUint32(h[4:], size)
	binary.LittleEndian.PutUint32(h[8:], size*2)
	binary.LittleEndian.PutUint16(h[12:], 1)
	binary.LittleEndian.PutUint16(h[14:], 32)
	binary.LittleEndian.PutUint32(h[16:], 0)
	binary.LittleEndian.PutUint32(h[20:], uint32(size*size*4+len(mask)))

	out = append(out, xor...)
	out = append(out, mask...)
	return out
}

func corner(x, y, size int) bool {
	r := 5
	if x >= r && x < size-r && y >= r && y < size-r {
		return false
	}
	cx, cy := r, r
	if x >= size-r {
		cx = size - r - 1
	}
	if y >= size-r {
		cy = size - r - 1
	}
	dx, dy := x-cx, y-cy
	if dx < 0 {
		dx = -dx
	}
	if dy < 0 {
		dy = -dy
	}
	return dx*dx+dy*dy > r*r
}
