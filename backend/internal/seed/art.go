package seed

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"math"
)

// Development photos: simple illustrations of each machine type, drawn in
// code so the seed needs no image files. Each photo varies the scene
// (time of day, viewing side) so listings have several distinct images.

type canvas struct {
	img  *image.RGBA
	w, h int
	flip bool
}

func newCanvas(w, h int, flip bool) *canvas {
	return &canvas{img: image.NewRGBA(image.Rect(0, 0, w, h)), w: w, h: h, flip: flip}
}

func (c *canvas) set(x, y int, col color.RGBA) {
	if c.flip {
		x = c.w - 1 - x
	}
	if x >= 0 && y >= 0 && x < c.w && y < c.h {
		c.img.SetRGBA(x, y, col)
	}
}

// Coordinates are in a 1000 x 750 design space, scaled to the canvas.
func (c *canvas) sx(v float64) int { return int(v * float64(c.w) / 1000) }
func (c *canvas) sy(v float64) int { return int(v * float64(c.h) / 750) }

func (c *canvas) rect(x0, y0, x1, y1 float64, col color.RGBA) {
	for y := c.sy(y0); y < c.sy(y1); y++ {
		for x := c.sx(x0); x < c.sx(x1); x++ {
			c.set(x, y, col)
		}
	}
}

func (c *canvas) circle(cx, cy, r float64, col color.RGBA) {
	px, py, pr := c.sx(cx), c.sy(cy), float64(c.sx(r))
	for y := py - int(pr); y <= py+int(pr); y++ {
		for x := px - int(pr); x <= px+int(pr); x++ {
			if math.Hypot(float64(x-px), float64(y-py)) <= pr {
				c.set(x, y, col)
			}
		}
	}
}

// line draws a thick segment.
func (c *canvas) line(x0, y0, x1, y1, width float64, col color.RGBA) {
	steps := int(math.Hypot(x1-x0, y1-y0) / 2)
	for i := 0; i <= steps; i++ {
		t := float64(i) / float64(max(steps, 1))
		c.circle(x0+(x1-x0)*t, y0+(y1-y0)*t, width/2, col)
	}
}

func (c *canvas) wheel(cx, cy, r float64) {
	c.circle(cx, cy, r, rgb(35, 35, 38))
	c.circle(cx, cy, r*0.55, rgb(120, 120, 125))
	c.circle(cx, cy, r*0.2, rgb(60, 60, 64))
}

func (c *canvas) track(x0, x1, y, h float64) {
	c.rect(x0+h/2, y, x1-h/2, y+h, rgb(40, 40, 42))
	c.circle(x0+h/2, y+h/2, h/2, rgb(40, 40, 42))
	c.circle(x1-h/2, y+h/2, h/2, rgb(40, 40, 42))
	for x := x0 + h/2; x < x1-h/2; x += h * 0.9 {
		c.circle(x, y+h/2, h*0.22, rgb(95, 95, 98))
	}
}

func rgb(r, g, b uint8) color.RGBA { return color.RGBA{R: r, G: g, B: b, A: 255} }

func shade(col color.RGBA, f float64) color.RGBA {
	m := func(v uint8) uint8 { return uint8(math.Min(255, float64(v)*f)) }
	return rgb(m(col.R), m(col.G), m(col.B))
}

var scenes = []struct{ skyTop, skyBottom, ground color.RGBA }{
	{rgb(120, 180, 235), rgb(215, 235, 250), rgb(150, 120, 85)},  // midday
	{rgb(245, 170, 110), rgb(250, 225, 180), rgb(120, 95, 70)},   // evening
	{rgb(170, 190, 205), rgb(225, 230, 235), rgb(125, 125, 120)}, // overcast yard
	{rgb(100, 160, 220), rgb(200, 225, 245), rgb(170, 150, 110)}, // site
}

// Photo draws a machine of category slug; variant picks scene and side.
func Photo(slug string, body color.RGBA, variant int) []byte {
	sc := scenes[variant%len(scenes)]
	c := newCanvas(1024, 768, variant%2 == 1)
	for y := 0; y < c.h; y++ {
		t := float64(y) / float64(c.h)
		var col color.RGBA
		if t < 0.68 {
			k := t / 0.68
			col = rgb(uint8(float64(sc.skyTop.R)*(1-k)+float64(sc.skyBottom.R)*k),
				uint8(float64(sc.skyTop.G)*(1-k)+float64(sc.skyBottom.G)*k),
				uint8(float64(sc.skyTop.B)*(1-k)+float64(sc.skyBottom.B)*k))
		} else {
			col = shade(sc.ground, 1-(t-0.68)*0.6)
		}
		for x := 0; x < c.w; x++ {
			c.img.SetRGBA(x, y, col)
		}
	}
	// distant hills
	for x := 0.0; x < 1000; x += 4 {
		h := 470 + 25*math.Sin(x/90+float64(variant)) + 15*math.Sin(x/37)
		c.rect(x, h, x+4, 512, shade(sc.ground, 0.75))
	}
	dark := shade(body, 0.6)
	glass := rgb(150, 195, 215)
	switch slug {
	case "backhoe-loader":
		c.rect(330, 420, 690, 530, body)
		c.rect(560, 300, 680, 430, dark)
		c.rect(575, 315, 665, 400, glass)
		c.line(690, 450, 820, 360, 22, body)
		c.line(820, 360, 870, 470, 18, body)
		c.rect(845, 470, 900, 520, dark)
		c.line(330, 470, 250, 520, 20, body)
		c.rect(200, 500, 270, 560, dark)
		c.wheel(400, 555, 62)
		c.wheel(640, 545, 72)
	case "excavator":
		c.track(300, 700, 540, 70)
		c.rect(330, 420, 680, 535, body)
		c.rect(560, 330, 670, 430, dark)
		c.rect(575, 345, 655, 410, glass)
		c.line(420, 430, 300, 250, 30, body)
		c.line(300, 250, 180, 420, 24, body)
		c.rect(150, 420, 230, 490, dark)
	case "bulldozer":
		c.track(320, 720, 540, 75)
		c.rect(380, 400, 700, 540, body)
		c.rect(560, 300, 690, 410, dark)
		c.rect(575, 315, 675, 395, glass)
		c.rect(230, 380, 280, 600, dark)
		c.line(280, 480, 390, 460, 18, dark)
	case "crane":
		c.rect(250, 440, 760, 540, body)
		c.rect(640, 360, 760, 445, dark)
		c.rect(655, 372, 745, 430, glass)
		c.line(330, 440, 860, 150, 26, body)
		c.line(860, 150, 860, 420, 4, rgb(30, 30, 30))
		c.rect(845, 420, 875, 450, dark)
		for _, x := range []float64{300, 420, 560, 700} {
			c.wheel(x, 555, 48)
		}
	case "dump-truck":
		c.rect(560, 360, 760, 520, body)
		c.rect(600, 380, 740, 450, glass)
		c.rect(220, 330, 560, 500, shade(body, 0.85))
		c.line(220, 330, 560, 330, 10, dark)
		c.rect(220, 500, 760, 530, dark)
		for _, x := range []float64{290, 410, 680} {
			c.wheel(x, 555, 55)
		}
	case "wheel-loader":
		c.rect(380, 420, 700, 530, body)
		c.rect(560, 300, 680, 430, dark)
		c.rect(575, 315, 665, 400, glass)
		c.line(380, 470, 270, 420, 22, body)
		c.rect(170, 380, 290, 520, dark)
		c.wheel(450, 545, 78)
		c.wheel(660, 545, 78)
	case "road-roller":
		c.rect(420, 420, 720, 520, body)
		c.rect(560, 320, 680, 425, dark)
		c.rect(575, 335, 665, 405, glass)
		c.rect(250, 430, 420, 470, dark)
		c.circle(300, 525, 85, rgb(70, 70, 74))
		c.circle(300, 525, 50, rgb(110, 110, 115))
		c.wheel(650, 550, 60)
	case "forklift":
		c.rect(420, 400, 650, 530, body)
		c.rect(500, 300, 640, 405, dark)
		c.rect(510, 312, 630, 395, glass)
		c.rect(360, 220, 385, 540, rgb(50, 50, 55))
		c.rect(280, 520, 390, 535, rgb(50, 50, 55))
		c.rect(280, 470, 360, 520, rgb(170, 130, 80)) // pallet load
		c.wheel(460, 555, 45)
		c.wheel(610, 555, 45)
	case "concrete-mixer":
		c.rect(590, 360, 780, 520, body)
		c.rect(630, 380, 760, 450, glass)
		for i := 0; i < 360; i += 2 {
			a := float64(i) * math.Pi / 180
			c.circle(400+150*math.Cos(a), 420+80*math.Sin(a)-0.25*150*math.Cos(a), 6, shade(body, 0.9))
		}
		c.circle(400, 420, 90, shade(body, 0.9))
		c.rect(240, 500, 780, 530, dark)
		for _, x := range []float64{300, 430, 700} {
			c.wheel(x, 555, 52)
		}
	case "tractor":
		c.rect(380, 440, 640, 510, body)
		c.rect(560, 330, 660, 445, dark)
		c.rect(572, 342, 648, 430, glass)
		c.rect(330, 410, 420, 450, body)
		c.wheel(400, 540, 55)
		c.wheel(620, 515, 95)
	default: // other machinery: a generator set on a skid
		c.rect(300, 380, 740, 540, body)
		c.rect(330, 410, 520, 510, dark)
		for y := 420.0; y < 500; y += 16 {
			c.rect(550, y, 710, y+8, dark)
		}
		c.rect(280, 540, 760, 560, rgb(60, 60, 62))
	}
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, c.img, &jpeg.Options{Quality: 82})
	return buf.Bytes()
}

// minimalPDF is a valid, inert PDF for development documents.
const minimalPDF = "%PDF-1.4\n1 0 obj << /Type /Catalog /Pages 2 0 R >> endobj\n2 0 obj << /Type /Pages /Kids [] /Count 0 >> endobj\ntrailer << /Root 1 0 R >>\n%%EOF\n"
