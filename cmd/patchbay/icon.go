//go:build tray

package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
)

// trayIconPNG draws Patchbay's menu-bar mark: a 2x2 grid of filled "jack holes"
// on a transparent ground, rendered in solid black so macOS can treat it as a
// template image (auto-inverted for light/dark menu bars). 22x22 is the
// standard macOS status-item size.
func trayIconPNG() []byte {
	const s = 22
	img := image.NewRGBA(image.Rect(0, 0, s, s))
	black := color.RGBA{A: 255}
	centers := [][2]int{{7, 7}, {15, 7}, {7, 15}, {15, 15}}
	const r = 3
	for _, c := range centers {
		fillCircle(img, c[0], c[1], r, black)
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func fillCircle(img *image.RGBA, cx, cy, r int, col color.RGBA) {
	for y := cy - r; y <= cy+r; y++ {
		for x := cx - r; x <= cx+r; x++ {
			dx, dy := x-cx, y-cy
			if dx*dx+dy*dy <= r*r {
				img.SetRGBA(x, y, col)
			}
		}
	}
}
