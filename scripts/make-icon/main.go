// Command make-icon draws the app icon with MyGo's own renderer and writes
// it to resources/icon.png, which mygo makes the icons of every platform
// from.
//
//	go run ./scripts/make-icon
package main

import (
	"flag"
	"image/png"
	"log"
	"math"
	"os"

	"github.com/egoist/mygo/ui"
)

// The canvas is 1024 DIPs. As Apple's template has it, the shape is 824
// across, with the room around it for its shadow.
const (
	canvas = 1024
	shape  = 824
	inset  = (canvas - shape) / 2
	radius = 186
)

// glyph is an arrow coming down onto a line: a download.
var glyph = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none"
 stroke="currentColor" stroke-width="2.9" stroke-linecap="round" stroke-linejoin="round">
 <path d="M12 3.6v11.2"/><path d="M6.6 9.9 12 15.3l5.4-5.4"/><path d="M5.6 20.2h12.8"/></svg>`))

var (
	indigo = ui.Hex("#3a5bff")
	violet = ui.Hex("#8d5cf6")
	pink   = ui.Hex("#ff7ac3")
	white  = ui.RGB(255, 255, 255)
)

func view(c *ui.Context) {
	c.Root().Background(ui.Transparent)
	ui.Box(c).Absolute().Left(inset).Top(inset).Size(shape, shape).Radius(radius).Clip().
		LinearGradient(ui.LinearGradient{From: indigo, To: violet, Angle: 150, Oklab: true}).
		Shadow(0, 14, 30, 0, ui.RGBA(20, 24, 80, 0.28)).
		Shadow(0, 2, 6, 0, ui.RGBA(20, 24, 80, 0.18)).
		Children(func() {
			// Pink glows from the bottom right corner.
			ui.Box(c).Absolute().Left(0).Top(0).Size(shape, shape).
				LinearGradient(ui.LinearGradient{From: pink.Alpha(0), To: pink, Angle: 135, Start: 0.45, End: 1, Oklab: true})
			// The mosaic of the app's empty page, rising from the bottom.
			ui.Box(c).Absolute().Left(0).Top(0).Size(shape, shape).Draw(mosaic)
			// Light from above.
			ui.Box(c).Absolute().Left(0).Top(0).Size(shape, shape/2).
				LinearGradient(ui.LinearGradient{From: white.Alpha(0.2), To: white.Alpha(0), Angle: 180})
			// The glyph over its own soft shadow, both placed by hand so
			// that the white one paints last.
			const size = 500
			at := float32(shape-size) / 2
			ui.Icon(c, glyph).FontSize(size).TextColor(ui.RGBA(30, 20, 110, 0.22)).Absolute().Left(at).Top(at + 12)
			ui.Icon(c, glyph).FontSize(size).TextColor(white).Absolute().Left(at).Top(at)
		})
}

// mosaic paints squares that fade away from the middle of the bottom edge.
func mosaic(p *ui.Painter, r ui.Rect) {
	const cell = shape / 8
	for gy := range 8 {
		for gx := range 8 {
			u := (float64(gx)+0.5)/8 - 0.5
			v := 1 - (float64(gy)+0.5)/8 // 0 at the bottom
			dist := math.Sqrt(u*u*2.2 + v*v*1.4)
			if dist >= 0.72 {
				continue
			}
			// Each square a little apart from its neighbors in lightness.
			alpha := math.Pow(1-dist/0.72, 1.3) * (0.1 + 0.07*float64((gx*7+gy*3)%4))
			p.Fill(ui.Rect{X: r.X + float32(gx*cell), Y: r.Y + float32(gy*cell), W: cell, H: cell}, white.Alpha(float32(alpha)), 0)
		}
	}
}

func main() {
	out := flag.String("o", "resources/icon.png", "the PNG to write")
	flag.Parse()
	f, err := os.Create(*out)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, ui.Render(view, canvas, canvas, 1)); err != nil {
		log.Fatal(err)
	}
}
