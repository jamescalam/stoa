// Package scene renders the slow Greco-Roman visualizers. Scenes draw into a
// pixel Canvas which is emitted as half-block characters: each terminal cell
// shows two vertically-stacked pixels via "▀" with a foreground (top pixel)
// and background (bottom pixel) colour.
package scene

import (
	"math"
	"strings"
	"time"
)

// RGB is a 24-bit colour.
type RGB struct{ R, G, B uint8 }

// RenderMode selects how a Canvas is turned into text.
type RenderMode int

const (
	// ModeColorRamp picks a glyph by brightness and tints it with the pixel's
	// colour, leaving the cell background transparent — a coloured ASCII look.
	ModeColorRamp RenderMode = iota
	// ModeHalfBlock paints every cell solid with two full-colour pixels.
	ModeHalfBlock
)

// asciiRamp maps brightness (dark→light) to increasing glyph density. It has
// no leading space so every cell carries ink (no holes for the eye to fall
// through) and tops out in a solid block for bright highlights.
var asciiRamp = []rune(".:-=+*#%@█")

// rampGamma < 1 lifts mid and low tones onto heavier glyphs so the image reads
// dense up close rather than needing to be viewed from afar.
const rampGamma = 0.7

// Scene renders a single frame into a cols×rows terminal region for the given
// elapsed animation time and render mode. Implementations must return exactly
// rows lines, each cols cells wide.
type Scene interface {
	Name() string
	Frame(cols, rows int, elapsed time.Duration, mode RenderMode) string
}

// Canvas is a pixel buffer whose height is twice its terminal row count.
type Canvas struct {
	W, H int // pixel dimensions; H is always even
	pix  []RGB
}

// NewCanvas allocates a canvas covering cols terminal columns and rows terminal
// rows (so H == rows*2 pixels tall).
func NewCanvas(cols, rows int) *Canvas {
	w, h := cols, rows*2
	return &Canvas{W: w, H: h, pix: make([]RGB, w*h)}
}

// Set paints a single pixel; out-of-range coordinates are ignored.
func (c *Canvas) Set(x, y int, col RGB) {
	if x < 0 || y < 0 || x >= c.W || y >= c.H {
		return
	}
	c.pix[y*c.W+x] = col
}

// Fill paints every pixel.
func (c *Canvas) Fill(col RGB) {
	for i := range c.pix {
		c.pix[i] = col
	}
}

// Render turns the buffer into text using the given mode.
func (c *Canvas) Render(mode RenderMode) string {
	if mode == ModeHalfBlock {
		return c.String()
	}
	return c.colorRamp()
}

// colorRamp renders one glyph per cell, chosen by the averaged brightness of
// the two stacked pixels and tinted with their averaged colour. Cell
// backgrounds are left untouched so the terminal shows through the gaps.
func (c *Canvas) colorRamp() string {
	var b strings.Builder
	rows := c.H / 2
	b.Grow(c.W * rows * 12)
	steps := float64(len(asciiRamp) - 1)
	for row := 0; row < rows; row++ {
		var last RGB
		have := false
		for x := 0; x < c.W; x++ {
			top := c.pix[(row*2)*c.W+x]
			bot := c.pix[(row*2+1)*c.W+x]
			l := (luma(top) + luma(bot)) / 2
			idx := int(math.Pow(l/255, rampGamma) * steps)
			if idx > len(asciiRamp)-1 {
				idx = len(asciiRamp) - 1
			}
			ch := asciiRamp[idx]
			if ch != ' ' { // spaces need no colour (no ink)
				if col := avg(top, bot); !have || col != last {
					b.WriteString("\x1b[38;2;")
					writeByte(&b, col.R)
					b.WriteByte(';')
					writeByte(&b, col.G)
					b.WriteByte(';')
					writeByte(&b, col.B)
					b.WriteByte('m')
					last, have = col, true
				}
			}
			b.WriteRune(ch)
		}
		b.WriteString("\x1b[0m")
		if row < rows-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// String renders the buffer to half-block characters with truecolor ANSI,
// emitting colour codes only when they change along a line.
func (c *Canvas) String() string {
	var b strings.Builder
	b.Grow(c.W * (c.H / 2) * 16)
	rows := c.H / 2
	for row := 0; row < rows; row++ {
		var lt, lb RGB
		have := false
		for x := 0; x < c.W; x++ {
			top := c.pix[(row*2)*c.W+x]
			bot := c.pix[(row*2+1)*c.W+x]
			if !have || top != lt || bot != lb {
				writeSGR(&b, top, bot)
				lt, lb, have = top, bot, true
			}
			b.WriteString("▀")
		}
		b.WriteString("\x1b[0m")
		if row < rows-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func writeSGR(b *strings.Builder, fg, bg RGB) {
	b.WriteString("\x1b[38;2;")
	writeByte(b, fg.R)
	b.WriteByte(';')
	writeByte(b, fg.G)
	b.WriteByte(';')
	writeByte(b, fg.B)
	b.WriteString(";48;2;")
	writeByte(b, bg.R)
	b.WriteByte(';')
	writeByte(b, bg.G)
	b.WriteByte(';')
	writeByte(b, bg.B)
	b.WriteByte('m')
}

func writeByte(b *strings.Builder, v uint8) {
	if v >= 100 {
		b.WriteByte('0' + v/100)
	}
	if v >= 10 {
		b.WriteByte('0' + (v/10)%10)
	}
	b.WriteByte('0' + v%10)
}

// lerp linearly blends two colours (t clamped to [0,1]).
func lerp(a, b RGB, t float64) RGB {
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	return RGB{
		cu8(float64(a.R) + (float64(b.R)-float64(a.R))*t),
		cu8(float64(a.G) + (float64(b.G)-float64(a.G))*t),
		cu8(float64(a.B) + (float64(b.B)-float64(a.B))*t),
	}
}

// scale multiplies a colour's brightness, clamping per channel.
func scale(c RGB, f float64) RGB {
	return RGB{cu8(float64(c.R) * f), cu8(float64(c.G) * f), cu8(float64(c.B) * f)}
}

// luma is the perceived brightness of a colour, 0..255.
func luma(c RGB) float64 {
	return 0.3*float64(c.R) + 0.59*float64(c.G) + 0.11*float64(c.B)
}

// avg blends two colours equally.
func avg(a, b RGB) RGB {
	return RGB{
		uint8((uint16(a.R) + uint16(b.R)) / 2),
		uint8((uint16(a.G) + uint16(b.G)) / 2),
		uint8((uint16(a.B) + uint16(b.B)) / 2),
	}
}

func cu8(v float64) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v + 0.5)
}

func clamp(v, lo, hi float64) float64 {
	return math.Max(lo, math.Min(hi, v))
}

func sign(v float64) float64 {
	if v < 0 {
		return -1
	}
	return 1
}
