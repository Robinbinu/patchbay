package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/png"
	"testing"
)

func TestStateFor(t *testing.T) {
	down := errors.New("listen tcp 127.0.0.1:8317: bind: address already in use")
	cases := []struct {
		proxyErr  error
		stopped   bool
		needLogin bool
		want      trayState
	}{
		{nil, false, false, stateOK},
		{nil, false, true, stateNeedsLogin},
		{down, false, false, stateProxyDown},
		{down, false, true, stateProxyDown}, // a dead proxy outranks a lapsed login
		{nil, true, true, stateStopped},     // the user turned it off; nothing to warn about
	}
	for _, c := range cases {
		if got := stateFor(c.proxyErr, c.stopped, c.needLogin); got != c.want {
			t.Errorf("stateFor(%v, %v, %v) = %v, want %v", c.proxyErr, c.stopped, c.needLogin, got, c.want)
		}
	}
}

func maxAlpha(img image.Image) uint32 {
	var m uint32
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if _, _, _, a := img.At(x, y).RGBA(); a > m {
				m = a
			}
		}
	}
	return m
}

func TestStoppedIconIsDimmed(t *testing.T) {
	mac := decodePNG(t, templateIconPNG(stateStopped))
	if a := maxAlpha(mac); a == 0 || a > 0x7000 {
		t.Errorf("stopped template max alpha %#x; want a visible but dimmed glyph", a)
	}
	for _, lightBar := range []bool{false, true} {
		win := parseICO(t, windowsIconICO(stateStopped, lightBar))[3].img
		if a := maxAlpha(win); a == 0 || a > 0x7000 {
			t.Errorf("lightBar=%v: stopped icon max alpha %#x; want dimmed", lightBar, a)
		}
	}
}

func decodePNG(t *testing.T, b []byte) image.Image {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("decode png: %v", err)
	}
	return img
}

func TestTemplateIconIsBlackOnly(t *testing.T) {
	for _, st := range []trayState{stateOK, stateNeedsLogin, stateProxyDown} {
		img := decodePNG(t, templateIconPNG(st))
		if b := img.Bounds(); b.Dx() != 32 || b.Dy() != 32 {
			t.Fatalf("state %v: size %v, want 32x32 (16pt @2x)", st, b.Size())
		}
		inked := 0
		for y := 0; y < 32; y++ {
			for x := 0; x < 32; x++ {
				r, g, b, a := img.At(x, y).RGBA()
				if r|g|b != 0 {
					t.Fatalf("state %v: pixel (%d,%d) not black; template images must be single-color", st, x, y)
				}
				if a > 0xC000 {
					inked++
				}
			}
		}
		if inked < 60 {
			t.Errorf("state %v: only %d opaque pixels; glyph missing", st, inked)
		}
	}
}

func TestBadgeKnocksOutGlyph(t *testing.T) {
	// On the board's top-right corner arc, inside the ring the badge knocks
	// out: inked in the plain state, cleared once a badge is drawn.
	x, y := 22, 3
	alpha := func(st trayState) uint32 {
		_, _, _, a := decodePNG(t, templateIconPNG(st)).At(x, y).RGBA()
		return a
	}
	if a := alpha(stateOK); a < 0x8000 {
		t.Fatalf("plain glyph should ink the board edge at (%d,%d); alpha %#x", x, y, a)
	}
	for _, st := range []trayState{stateNeedsLogin, stateProxyDown} {
		if a := alpha(st); a != 0 {
			t.Errorf("state %v: knockout ring at (%d,%d) has alpha %#x, want 0", st, x, y, a)
		}
	}
}

type icoEntry struct {
	w, h int
	img  image.Image
}

func parseICO(t *testing.T, b []byte) []icoEntry {
	t.Helper()
	if len(b) < 6 || binary.LittleEndian.Uint16(b[0:]) != 0 || binary.LittleEndian.Uint16(b[2:]) != 1 {
		t.Fatalf("bad ICONDIR header")
	}
	n := int(binary.LittleEndian.Uint16(b[4:]))
	var out []icoEntry
	for i := 0; i < n; i++ {
		e := b[6+16*i:]
		w, h := int(e[0]), int(e[1])
		if w == 0 {
			w = 256
		}
		if h == 0 {
			h = 256
		}
		if planes, bpp := binary.LittleEndian.Uint16(e[4:]), binary.LittleEndian.Uint16(e[6:]); planes != 1 || bpp != 32 {
			t.Fatalf("entry %d: planes=%d bpp=%d, want 1/32", i, planes, bpp)
		}
		size := binary.LittleEndian.Uint32(e[8:])
		off := binary.LittleEndian.Uint32(e[12:])
		if int(off+size) > len(b) {
			t.Fatalf("entry %d overruns file", i)
		}
		img := decodePNG(t, b[off:off+size])
		if img.Bounds().Dx() != w || img.Bounds().Dy() != h {
			t.Fatalf("entry %d: directory says %dx%d, png is %v", i, w, h, img.Bounds().Size())
		}
		out = append(out, icoEntry{w, h, img})
	}
	return out
}

func TestWindowsICOHasDPISizes(t *testing.T) {
	entries := parseICO(t, windowsIconICO(stateOK, false))
	want := []int{16, 20, 24, 32} // 100%, 125%, 150%, 200% scaling
	if len(entries) != len(want) {
		t.Fatalf("got %d entries, want %d", len(entries), len(want))
	}
	for i, e := range entries {
		if e.w != want[i] || e.h != want[i] {
			t.Errorf("entry %d is %dx%d, want %dx%d", i, e.w, e.h, want[i], want[i])
		}
	}
}

// countInk counts fully opaque pixels whose 8-bit color satisfies match;
// antialiased edges are skipped so blended colors can't fake a match.
func countInk(img image.Image, match func(r, g, b uint32) bool) int {
	n := 0
	bd := img.Bounds()
	for y := bd.Min.Y; y < bd.Max.Y; y++ {
		for x := bd.Min.X; x < bd.Max.X; x++ {
			r, g, b, a := img.At(x, y).RGBA()
			if a == 0xFFFF && match(r>>8, g>>8, b>>8) {
				n++
			}
		}
	}
	return n
}

func TestWindowsGlyphFollowsTaskbarTheme(t *testing.T) {
	white := func(r, g, b uint32) bool { return r == 255 && g == 255 && b == 255 }
	black := func(r, g, b uint32) bool { return r == 0 && g == 0 && b == 0 }
	dark := parseICO(t, windowsIconICO(stateOK, false))[3].img
	light := parseICO(t, windowsIconICO(stateOK, true))[3].img
	if countInk(dark, white) < 30 || countInk(dark, black) > 0 {
		t.Errorf("dark taskbar needs a white glyph")
	}
	if countInk(light, black) < 30 || countInk(light, white) > 0 {
		t.Errorf("light taskbar needs a black glyph")
	}
}

func TestWindowsBadgeIsColored(t *testing.T) {
	amber := func(r, g, b uint32) bool { return r > 200 && g > 120 && g < 190 && b < 80 }
	red := func(r, g, b uint32) bool { return r > 200 && g < 100 && b < 100 }
	for _, lightBar := range []bool{false, true} {
		warn := parseICO(t, windowsIconICO(stateNeedsLogin, lightBar))[3].img
		down := parseICO(t, windowsIconICO(stateProxyDown, lightBar))[3].img
		ok := parseICO(t, windowsIconICO(stateOK, lightBar))[3].img
		if countInk(warn, amber) < 8 {
			t.Errorf("lightBar=%v: needs-login badge not amber", lightBar)
		}
		if countInk(down, red) < 8 {
			t.Errorf("lightBar=%v: proxy-down badge not red", lightBar)
		}
		if countInk(ok, amber)+countInk(ok, red) > 0 {
			t.Errorf("lightBar=%v: plain state should carry no badge color", lightBar)
		}
	}
}
