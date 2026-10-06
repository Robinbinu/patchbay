package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"math"
)

// trayState is what the tray icon tells the user at a glance.
type trayState int

const (
	stateOK trayState = iota
	stateNeedsLogin
	stateProxyDown
)

// stateFor picks the icon state. A proxy that failed to listen outranks a
// lapsed login: no request works until it is fixed.
func stateFor(proxyErr error, needLogin bool) trayState {
	switch {
	case proxyErr != nil:
		return stateProxyDown
	case needLogin:
		return stateNeedsLogin
	default:
		return stateOK
	}
}

// templateIconPNG is the macOS menu-bar icon: solid black on transparent so it
// works as a template image (the system tints it for any bar appearance). The
// state badge is drawn in the same single color. fyne.io/systray sizes the
// image to 16pt, so 32px keeps it sharp on Retina displays.
func templateIconPNG(st trayState) []byte {
	return pngBytes(renderIcon(32, color.NRGBA{A: 255}, st, false))
}

// windowsIconICO is the Windows notification-area icon. Windows has no
// template images, so the glyph is inked for the taskbar theme and the state
// badge is colored. The ICO carries one PNG per common DPI scale (100%–200%).
func windowsIconICO(st trayState, lightTaskbar bool) []byte {
	ink := color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	if lightTaskbar {
		ink = color.NRGBA{A: 255}
	}
	var pngs [][]byte
	var sizes []int
	for _, px := range []int{16, 20, 24, 32} {
		pngs = append(pngs, pngBytes(renderIcon(px, ink, st, true)))
		sizes = append(sizes, px)
	}
	return encodeICO(sizes, pngs)
}

// The glyph is a rounded "patch board" with a 2x2 grid of jacks and one cable
// patching the top-left jack to the bottom-right. The badge sits on the
// top-right corner — over the board outline and an unpatched jack, never the
// cable — inside a knocked-out ring so it stays legible.
//
// Geometry is fitted to each pixel size rather than scaled: strokes round to
// whole pixels and centerlines snap to pixel centers (odd widths) or edges
// (even widths), so small Windows sizes stay crisp instead of blurring.
const supersample = 8

var (
	badgeAmber = color.NRGBA{R: 0xEF, G: 0x9F, B: 0x27, A: 255}
	badgeRed   = color.NRGBA{R: 0xE2, G: 0x4B, B: 0x4A, A: 255}
)

type pt struct{ x, y float64 }

// layout is the glyph in pixel coordinates for one icon size.
type layout struct {
	center       float64 // board is centered here on both axes
	stroke       float64 // board stroke width
	cableW       float64 // cable stroke width
	boardHalf    float64 // half-size of the board's stroke centerline
	boardR       float64 // corner radius of that centerline
	lo, hi       float64 // jack centers on each axis
	jackR, plugR float64 // open jack, patched jack (plug)
	cable        []pt
	badge        pt
	badgeR       float64 // single-color badge; colored discs are 0.5px larger
	knockoutR    float64
	hideJackTR   bool // the top-right jack would be clipped by the knockout
}

// snap places a centerline of the given pixel width so its edges land on the
// pixel grid: odd widths center on a pixel, even widths on a pixel edge.
func snap(v, width float64) float64 {
	if int(width)%2 == 1 {
		return math.Floor(v) + 0.5
	}
	return math.Round(v)
}

// layoutFor fits the design (drawn on a 24-unit grid) to px pixels.
func layoutFor(px int) layout {
	size := float64(px)
	u := size / 24 // pixels per design unit
	l := layout{center: size / 2}
	l.stroke = math.Max(1, math.Round(1.7*u))
	l.cableW = l.stroke
	if l.stroke == 1 {
		l.cableW = 1.5 // a 1px diagonal antialiases to gray; give it the weight of the straight edges
	}
	l.boardHalf = l.center - snap(2.5*u, l.stroke)
	l.boardR = 5.5 * u
	jackD := math.Max(2, math.Round(3*u))
	off := snap(4*u, jackD)
	l.lo, l.hi = l.center-off, l.center+off
	l.jackR, l.plugR = jackD/2, jackD/2+1
	span := l.hi - l.lo
	l.cable = flattenCubic(pt{l.lo, l.lo}, pt{l.lo, l.lo + 0.875*span},
		pt{l.hi, l.lo + 0.125*span}, pt{l.hi, l.hi}, 48)
	l.badgeR = math.Round(0.26*size) / 2
	l.badge = pt{size - l.badgeR, l.badgeR}
	l.knockoutR = l.badgeR + 0.5 + math.Max(1, math.Round(0.06*size))
	l.hideJackTR = dist(pt{l.hi, l.lo}, l.badge) < l.knockoutR+l.jackR
	return l
}

func (l *layout) glyphCovers(p pt, badged bool) bool {
	c := pt{l.center, l.center}
	if math.Abs(sdRoundRect(p, c, l.boardHalf, l.boardR)) <= l.stroke/2 {
		return true
	}
	if dist(p, pt{l.lo, l.hi}) <= l.jackR {
		return true
	}
	if !(badged && l.hideJackTR) && dist(p, pt{l.hi, l.lo}) <= l.jackR {
		return true
	}
	if dist(p, pt{l.lo, l.lo}) <= l.plugR || dist(p, pt{l.hi, l.hi}) <= l.plugR {
		return true
	}
	return distPolyline(p, l.cable) <= l.cableW/2
}

// badgeCovers reports whether p is part of the state badge. In single-color
// (template) mode the states differ by shape — a dot for "needs login", a
// cross for "proxy down"; in colored mode both are discs and color carries it.
func (l *layout) badgeCovers(p pt, st trayState, colored bool) bool {
	if colored {
		r := l.badgeR + 0.5
		return dist(p, pt{l.badge.x - 0.5, l.badge.y + 0.5}) <= r
	}
	switch st {
	case stateNeedsLogin:
		return dist(p, l.badge) <= l.badgeR
	case stateProxyDown:
		arm := l.badgeR - l.stroke/2
		b := l.badge
		return distSegment(p, pt{b.x - arm, b.y - arm}, pt{b.x + arm, b.y + arm}) <= l.stroke/2 ||
			distSegment(p, pt{b.x + arm, b.y - arm}, pt{b.x - arm, b.y + arm}) <= l.stroke/2
	}
	return false
}

// renderIcon rasterizes the glyph at px×px, supersampling each pixel for
// antialiased edges.
func renderIcon(px int, ink color.NRGBA, st trayState, colored bool) *image.NRGBA {
	l := layoutFor(px)
	badgeInk := ink
	if colored {
		badgeInk = badgeAmber
		if st == stateProxyDown {
			badgeInk = badgeRed
		}
	}
	img := image.NewNRGBA(image.Rect(0, 0, px, px))
	const n = supersample * supersample
	for y := 0; y < px; y++ {
		for x := 0; x < px; x++ {
			// Premultiplied sums, so edge pixels between two colors blend correctly.
			var r, g, b, a float64
			for sy := 0; sy < supersample; sy++ {
				for sx := 0; sx < supersample; sx++ {
					p := pt{
						float64(x) + (float64(sx)+0.5)/supersample,
						float64(y) + (float64(sy)+0.5)/supersample,
					}
					c, ok := l.sampleColor(p, st, colored, ink, badgeInk)
					if !ok {
						continue
					}
					r += float64(c.R)
					g += float64(c.G)
					b += float64(c.B)
					a++
				}
			}
			if a == 0 {
				continue
			}
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8(math.Round(r / a)),
				G: uint8(math.Round(g / a)),
				B: uint8(math.Round(b / a)),
				A: uint8(math.Round(255 * a / n)),
			})
		}
	}
	return img
}

func (l *layout) sampleColor(p pt, st trayState, colored bool, ink, badgeInk color.NRGBA) (color.NRGBA, bool) {
	badged := st != stateOK
	if badged {
		if l.badgeCovers(p, st, colored) {
			return badgeInk, true
		}
		if dist(p, l.badge) <= l.knockoutR {
			return color.NRGBA{}, false
		}
	}
	if l.glyphCovers(p, badged) {
		return ink, true
	}
	return color.NRGBA{}, false
}

// sdRoundRect is the signed distance from p to a square of half-size half
// centered on c with corner radius r (negative inside).
func sdRoundRect(p, c pt, half, r float64) float64 {
	qx := math.Abs(p.x-c.x) - half + r
	qy := math.Abs(p.y-c.y) - half + r
	outside := math.Hypot(math.Max(qx, 0), math.Max(qy, 0))
	return outside + math.Min(math.Max(qx, qy), 0) - r
}

func flattenCubic(p0, p1, p2, p3 pt, steps int) []pt {
	out := make([]pt, steps+1)
	for i := range out {
		t := float64(i) / float64(steps)
		u := 1 - t
		out[i] = pt{
			u*u*u*p0.x + 3*u*u*t*p1.x + 3*u*t*t*p2.x + t*t*t*p3.x,
			u*u*u*p0.y + 3*u*u*t*p1.y + 3*u*t*t*p2.y + t*t*t*p3.y,
		}
	}
	return out
}

func distPolyline(p pt, line []pt) float64 {
	best := math.Inf(1)
	for i := 1; i < len(line); i++ {
		best = math.Min(best, distSegment(p, line[i-1], line[i]))
	}
	return best
}

func distSegment(p, a, b pt) float64 {
	dx, dy := b.x-a.x, b.y-a.y
	t := ((p.x-a.x)*dx + (p.y-a.y)*dy) / (dx*dx + dy*dy)
	t = math.Max(0, math.Min(1, t))
	return math.Hypot(p.x-(a.x+t*dx), p.y-(a.y+t*dy))
}

func dist(a, b pt) float64 { return math.Hypot(a.x-b.x, a.y-b.y) }

func pngBytes(img image.Image) []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err) // encoding an in-memory NRGBA cannot fail
	}
	return buf.Bytes()
}

// encodeICO wraps PNG images in an ICO container (PNG entries are supported
// since Windows Vista). Sizes must be ≤ 256.
func encodeICO(sizes []int, pngs [][]byte) []byte {
	var buf bytes.Buffer
	le := binary.LittleEndian
	_ = binary.Write(&buf, le, [3]uint16{0, 1, uint16(len(pngs))}) // reserved, type=icon, count
	offset := 6 + 16*len(pngs)
	for i, data := range pngs {
		dim := uint8(sizes[i] % 256) // 0 encodes 256
		_ = binary.Write(&buf, le, struct {
			W, H, Colors, Reserved uint8
			Planes, BitCount       uint16
			Size, Offset           uint32
		}{dim, dim, 0, 0, 1, 32, uint32(len(data)), uint32(offset)})
		offset += len(data)
	}
	for _, data := range pngs {
		buf.Write(data)
	}
	return buf.Bytes()
}
