package scene

import (
	"math"
	"time"
)

// dayPeriod is how long a full dawn→dusk light cycle takes.
const dayPeriod = 240.0 // seconds

// Colonnade is a row of Doric columns beneath an open sky. A light source
// arcs across over dayPeriod, shading the marble and swinging the shadows —
// essentially a slow sundial.
type Colonnade struct{}

func (Colonnade) Name() string { return "colonnade" }

func (Colonnade) Frame(cols, rows int, elapsed time.Duration, mode RenderMode) string {
	c := NewCanvas(cols, rows)
	if c.W >= 2 && c.H >= 2 {
		drawColonnade(c, elapsed)
	}
	return c.Render(mode)
}

func drawColonnade(c *Canvas, elapsed time.Duration) {
	W, H := float64(c.W), float64(c.H)
	phase := math.Mod(elapsed.Seconds()/dayPeriod, 1.0) // 0..1 across the day
	if phase < 0 {
		phase += 1
	}
	day := math.Sin(phase * math.Pi) // 0 at horizon, 1 at midday
	gy := int(H * 0.80)               // ground line (pixels)

	// Sun position: travels left→right, rising and setting.
	sunX := phase * W
	sunY := float64(gy) - day*float64(gy)*0.82

	drawSky(c, gy, day)
	drawSun(c, gy, sunX, sunY, H, day)
	drawGround(c, gy)

	// Column geometry.
	n := c.W / 14
	n = int(clamp(float64(n), 3, 9))
	slot := W / float64(n)
	shaftHalf := slot * 0.24
	beamTop := int(H * 0.14)
	beamBot := int(H * 0.24)
	shaftTop := beamBot
	baseH := int(math.Max(1, H*0.02))

	drawGroundShadows(c, n, slot, shaftHalf, gy, sunX, day)
	drawBeam(c, beamTop, beamBot, day)
	for i := 0; i < n; i++ {
		cx := slot * (float64(i) + 0.5)
		drawColumn(c, cx, shaftHalf, shaftTop, gy, baseH, sunX, W, day)
	}
}

func drawSky(c *Canvas, gy int, day float64) {
	zenith := lerp(RGB{40, 30, 60}, RGB{24, 40, 74}, day)
	horizon := lerp(RGB{224, 132, 76}, RGB{150, 170, 196}, day)
	for y := 0; y < gy; y++ {
		t := float64(y) / float64(gy)
		col := lerp(zenith, horizon, t)
		for x := 0; x < c.W; x++ {
			c.Set(x, y, col)
		}
	}
}

func drawGround(c *Canvas, gy int) {
	top := RGB{78, 64, 52}
	bot := RGB{28, 23, 20}
	span := float64(c.H - gy)
	if span < 1 {
		span = 1
	}
	for y := gy; y < c.H; y++ {
		t := float64(y-gy) / span
		col := lerp(top, bot, t)
		for x := 0; x < c.W; x++ {
			c.Set(x, y, col)
		}
	}
}

func drawSun(c *Canvas, gy int, sunX, sunY, H, day float64) {
	sunCol := lerp(RGB{255, 176, 96}, RGB{255, 244, 214}, day) // golden low, white high
	r := math.Max(2.0, H*0.05)
	reach := r * 3.4
	for y := 0; y < gy; y++ {
		for x := 0; x < c.W; x++ {
			dx := float64(x) - sunX
			dy := float64(y) - sunY
			d := math.Sqrt(dx*dx + dy*dy)
			if d > reach {
				continue
			}
			base := c.pix[y*c.W+x]
			switch {
			case d < r:
				c.Set(x, y, sunCol)
			default:
				g := 1 - (d-r)/(reach-r)
				c.Set(x, y, lerp(base, sunCol, g*g*0.85))
			}
		}
	}
}

func drawBeam(c *Canvas, top, bot int, day float64) {
	marble := scale(RGB{206, 198, 180}, 0.7+0.3*day)
	under := scale(marble, 0.7)
	for y := top; y < bot && y < c.H; y++ {
		col := marble
		if y >= bot-2 {
			col = under // shadowed underside of the architrave
		}
		for x := 0; x < c.W; x++ {
			c.Set(x, y, col)
		}
	}
}

func drawColumn(c *Canvas, cx, shaftHalf float64, shaftTop, gy, baseH int, sunX, W, day float64) {
	marble := lerp(RGB{214, 206, 188}, RGB{234, 208, 160}, (1-day)*0.5) // warmer at dusk
	amb := 0.68 + 0.32*day
	// Horizontal light angle from the sun's position relative to this column.
	lightTheta := clamp((sunX-cx)/(W*0.5), -1, 1) * (math.Pi / 2)

	capHalf := shaftHalf * 1.35
	capBot := shaftTop + baseH + 1
	for y := shaftTop; y < gy; y++ {
		half := shaftHalf
		if y < capBot { // capital flares out at the top
			half = capHalf
		} else if y > gy-baseH-1 { // base flares out at the bottom
			half = shaftHalf * 1.3
		}
		x0 := int(cx - half)
		x1 := int(cx + half)
		for x := x0; x <= x1; x++ {
			u := (float64(x) - cx) / shaftHalf // -1..1 across the shaft
			theta := clamp(u, -1, 1) * (math.Pi / 2)
			shade := 0.45 + 0.55*math.Cos(theta-lightTheta)
			flute := 0.92 + 0.08*math.Cos(u*math.Pi*5) // subtle vertical fluting
			shade = clamp(shade*flute*amb, 0.22, 1.15)
			c.Set(x, y, scale(marble, shade))
		}
	}
}

func drawGroundShadows(c *Canvas, n int, slot, shaftHalf float64, gy int, sunX, day float64) {
	// Longer, softer shadows near dawn/dusk; short and faint at midday.
	length := shaftHalf * (2.0 + (1-day)*7.0)
	depth := int(math.Max(2, float64(c.H-gy)*0.5))
	for i := 0; i < n; i++ {
		cx := slot * (float64(i) + 0.5)
		dir := sign(cx - sunX) // shadow falls away from the sun
		for y := gy; y < gy+depth && y < c.H; y++ {
			fall := 1 - float64(y-gy)/float64(depth) // fades with distance down
			x0 := cx
			x1 := cx + dir*length
			if x1 < x0 {
				x0, x1 = x1, x0
			}
			for x := int(x0); x <= int(x1); x++ {
				if x < 0 || x >= c.W {
					continue
				}
				edge := 1 - math.Abs(float64(x)-cx)/(length+1)
				k := clamp(0.55*fall*edge, 0, 0.55)
				base := c.pix[y*c.W+x]
				c.Set(x, y, scale(base, 1-k))
			}
		}
	}
}
