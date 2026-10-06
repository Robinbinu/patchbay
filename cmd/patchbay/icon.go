//go:build tray

package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
)

// trayIconPNG renders Patchbay's menu-bar mark: a rounded "patch panel" with a
// 2x3 grid of jack holes, in solid black on transparent so macOS treats it as a
// template image (auto-inverted for light/dark bars). It is drawn at 4x and
// box-downsampled to 22pt for smooth edges.
func trayIconPNG() []byte {
	const scale = 4
	const size = 22
	const hi = size * scale
	src := image.NewRGBA(image.Rect(0, 0, hi, hi))

	// Panel: rounded-rect ring near the edges.
	const inset = 2 * scale
	const radius = 5 * scale
	const border = 2 * scale
	outer := rrect{x0: inset, y0: inset + scale, x1: hi - inset, y1: hi - inset - scale, r: radius}
	inner := rrect{x0: outer.x0 + border, y0: outer.y0 + border, x1: outer.x1 - border, y1: outer.y1 - border, r: radius - border}
	for y := 0; y < hi; y++ {
		for x := 0; x < hi; x++ {
			if outer.contains(x, y) && !inner.contains(x, y) {
				src.SetRGBA(x, y, color.RGBA{A: 255})
			}
		}
	}
	// Jack holes: 2 rows x 3 columns, centered in the panel.
	cols := []int{hi * 30 / 100, hi * 50 / 100, hi * 70 / 100}
	rows := []int{hi * 42 / 100, hi * 62 / 100}
	const hole = 2.1 * scale
	for _, cy := range rows {
		for _, cx := range cols {
			fillDisc(src, cx, cy, hole)
		}
	}
	return pngBytes(downsample(src, scale))
}

type rrect struct {
	x0, y0, x1, y1, r int
}

func (b rrect) contains(x, y int) bool {
	if x < b.x0 || x >= b.x1 || y < b.y0 || y >= b.y1 {
		return false
	}
	// Corner circles.
	cx, cy := x, y
	switch {
	case x < b.x0+b.r && y < b.y0+b.r:
		return sq(cx-(b.x0+b.r))+sq(cy-(b.y0+b.r)) <= sq(b.r)
	case x >= b.x1-b.r && y < b.y0+b.r:
		return sq(cx-(b.x1-b.r-1))+sq(cy-(b.y0+b.r)) <= sq(b.r)
	case x < b.x0+b.r && y >= b.y1-b.r:
		return sq(cx-(b.x0+b.r))+sq(cy-(b.y1-b.r-1)) <= sq(b.r)
	case x >= b.x1-b.r && y >= b.y1-b.r:
		return sq(cx-(b.x1-b.r-1))+sq(cy-(b.y1-b.r-1)) <= sq(b.r)
	}
	return true
}

func fillDisc(img *image.RGBA, cx, cy int, r float64) {
	ri := int(r) + 1
	for y := cy - ri; y <= cy+ri; y++ {
		for x := cx - ri; x <= cx+ri; x++ {
			dx, dy := float64(x-cx), float64(y-cy)
			if dx*dx+dy*dy <= r*r {
				img.SetRGBA(x, y, color.RGBA{A: 255})
			}
		}
	}
}

// downsample box-averages an RGBA image by an integer factor, producing
// antialiased alpha for a template icon.
func downsample(src *image.RGBA, factor int) *image.RGBA {
	w := src.Bounds().Dx() / factor
	h := src.Bounds().Dy() / factor
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	n := factor * factor
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var a int
			for dy := 0; dy < factor; dy++ {
				for dx := 0; dx < factor; dx++ {
					a += int(src.RGBAAt(x*factor+dx, y*factor+dy).A)
				}
			}
			dst.SetRGBA(x, y, color.RGBA{A: uint8(a / n)})
		}
	}
	return dst
}

func pngBytes(img image.Image) []byte {
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func sq(v int) int { return v * v }
