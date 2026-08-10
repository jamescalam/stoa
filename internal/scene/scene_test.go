package scene

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// visibleWidth counts printable cells, skipping ANSI SGR escape sequences.
func visibleWidth(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			continue
		}
		if s[i]&0xc0 != 0x80 { // count UTF-8 lead bytes only
			n++
		}
	}
	return n
}

// TestColonnadeGeometry checks the frame is exactly rows×cols.
func TestColonnadeGeometry(t *testing.T) {
	cols, rows := 60, 20
	frame := Colonnade{}.Frame(cols, rows, 60*time.Second, ModeColorRamp)
	lines := strings.Split(frame, "\n")
	if len(lines) != rows {
		t.Fatalf("got %d lines, want %d", len(lines), rows)
	}
	for i, l := range lines {
		if w := visibleWidth(l); w != cols {
			t.Errorf("line %d width = %d, want %d", i, w, cols)
		}
	}
}

// TestColonnadePreview prints a luminance-ASCII view at several times of day so
// the composition can be eyeballed. It asserts nothing.
func TestColonnadePreview(t *testing.T) {
	for _, tc := range []struct {
		label   string
		elapsed time.Duration
	}{
		{"dawn (phase 0.10)", time.Duration(0.10 * dayPeriod * float64(time.Second))},
		{"midday (phase 0.50)", time.Duration(0.50 * dayPeriod * float64(time.Second))},
		{"dusk (phase 0.85)", time.Duration(0.85 * dayPeriod * float64(time.Second))},
	} {
		c := NewCanvas(72, 22)
		drawColonnade(c, tc.elapsed)
		fmt.Printf("\n===== %s =====\n%s\n", tc.label, asciiPreview(c))
	}
}

func asciiPreview(c *Canvas) string {
	const ramp = " .:-=+*#%@"
	var b strings.Builder
	rows := c.H / 2
	for row := 0; row < rows; row++ {
		for x := 0; x < c.W; x++ {
			top := c.pix[(row*2)*c.W+x]
			bot := c.pix[(row*2+1)*c.W+x]
			lum := (luma(top) + luma(bot)) / 2
			idx := int(lum / 255 * float64(len(ramp)-1))
			b.WriteByte(ramp[idx])
		}
		b.WriteByte('\n')
	}
	return b.String()
}
