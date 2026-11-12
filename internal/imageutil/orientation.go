package imageutil

import (
	"encoding/binary"
	"image"
)

// A JPEG's EXIF orientation says how to turn its stored pixels for display. Uploads
// are re-encoded without metadata, so the turn is applied to the pixels first:
// otherwise a portrait phone photo would be stored on its side.

// jpegOrientation reads the EXIF orientation (1–8) from a JPEG's APP1 segment. It
// returns 1, upright, when there is none or it cannot be read.
func jpegOrientation(data []byte) int {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return 1
	}
	for at := 2; at+4 <= len(data); {
		if data[at] != 0xFF {
			return 1
		}
		marker := data[at+1]
		if marker == 0xD9 || marker == 0xDA { // end of image, or start of scan: no EXIF before the pixels
			return 1
		}
		length := int(binary.BigEndian.Uint16(data[at+2:]))
		if length < 2 || at+2+length > len(data) {
			return 1
		}
		segment := data[at+4 : at+2+length]
		if marker == 0xE1 && len(segment) > 6 && string(segment[:6]) == "Exif\x00\x00" {
			return exifOrientation(segment[6:])
		}
		at += 2 + length
	}
	return 1
}

// exifOrientation reads tag 0x0112 from the first IFD of a TIFF-structured EXIF block.
func exifOrientation(tiff []byte) int {
	if len(tiff) < 8 {
		return 1
	}
	var order binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 1
	}
	ifd := int(order.Uint32(tiff[4:]))
	if ifd < 8 || ifd+2 > len(tiff) {
		return 1
	}
	entries := int(order.Uint16(tiff[ifd:]))
	for i := 0; i < entries; i++ {
		entry := ifd + 2 + i*12
		if entry+12 > len(tiff) {
			return 1
		}
		if order.Uint16(tiff[entry:]) == 0x0112 {
			if o := int(order.Uint16(tiff[entry+8:])); o >= 1 && o <= 8 {
				return o
			}
			return 1
		}
	}
	return 1
}

// applyOrientation returns img turned and flipped as EXIF orientation o says, so it
// displays upright with no metadata. Orientation 1 (or an unknown value) returns
// img itself.
func applyOrientation(img image.Image, o int) image.Image {
	if o <= 1 || o > 8 {
		return img
	}
	src := toNRGBA(img)
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	dw, dh := w, h
	if o >= 5 { // 5–8 swap width and height
		dw, dh = h, w
	}
	dst := image.NewNRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var dx, dy int
			switch o {
			case 2: // mirrored
				dx, dy = w-1-x, y
			case 3: // upside down
				dx, dy = w-1-x, h-1-y
			case 4: // mirrored upside down
				dx, dy = x, h-1-y
			case 5: // mirrored, turned left
				dx, dy = y, x
			case 6: // turned left: rotate 90° clockwise to show
				dx, dy = h-1-y, x
			case 7: // mirrored, turned right
				dx, dy = h-1-y, w-1-x
			case 8: // turned right: rotate 90° anticlockwise to show
				dx, dy = y, w-1-x
			}
			s := y*src.Stride + x*4
			d := dy*dst.Stride + dx*4
			copy(dst.Pix[d:d+4], src.Pix[s:s+4])
		}
	}
	return dst
}
