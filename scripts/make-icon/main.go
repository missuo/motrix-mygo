// Command make-icon draws the app icon with MyGo's own renderer and writes
// it to resources/icon.png, which mygo makes the icons of every platform
// from.
//
//	go run ./scripts/make-icon                    # the chosen design
//	go run ./scripts/make-icon -variant graphite  # another one
//	go run ./scripts/make-icon -sheet sheet.png   # every design, to compare
package main

import (
	"flag"
	"fmt"
	"image/png"
	"log"
	"os"
	"strings"

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

// chosen is the design of resources/icon.png.
const chosen = "paper"

// design is one look of the icon: a background and a glyph in its colors.
type design struct {
	name     string
	from, to string  // the background, from the top left to the bottom right
	glyph    string  // the body of an SVG on a 24 grid; {a} and {b} are its colors
	a, b     string  // the glyph's main and accent colors
	size     float32 // the glyph's size in DIPs
	rim      float32 // how strong the light along the edge is
}

// Glyphs, stroked on a 24 grid.
const (
	// An m whose middle leg is an arrow: Motrix, downloading.
	glyphM = `<g fill="none" stroke-width="2.7" stroke-linecap="round" stroke-linejoin="round" transform="translate(0 -1.2)">
<path stroke="{a}" d="M5 15.6V10a3.5 3.5 0 0 1 7 0"/><path stroke="{a}" d="M12 10a3.5 3.5 0 0 1 7 0v5.6"/>
<path stroke="{b}" d="M12 10v9.6"/><path stroke="{b}" d="M8.5 16.3 12 19.8l3.5-3.5"/></g>`
	// An arrow into a tray.
	glyphTray = `<g fill="none" stroke-width="2.6" stroke-linecap="round" stroke-linejoin="round">
<path stroke="{a}" d="M4.2 14.6v2.8a2.6 2.6 0 0 0 2.6 2.6h10.4a2.6 2.6 0 0 0 2.6-2.6v-2.8"/>
<path stroke="{b}" d="M12 3.8v10.4"/><path stroke="{b}" d="M7.6 10 12 14.4 16.4 10"/></g>`
	// An arrow onto a line.
	glyphLine = `<g fill="none" stroke-width="2.8" stroke-linecap="round" stroke-linejoin="round">
<path stroke="{b}" d="M12 3.8v11"/><path stroke="{b}" d="M6.8 10 12 15.2 17.2 10"/><path stroke="{a}" d="M5.8 20.2h12.4"/></g>`
)

var designs = []design{
	{"graphite", "#34343a", "#131316", glyphM, "#ffffff", "#ffd60a", 560, 0.16},
	{"paper", "#ffffff", "#e6e7ec", glyphTray, "#1d1d1f", "#0a7aff", 520, 0},
	{"ocean", "#22d3d6", "#0b63d6", glyphM, "#ffffff", "#ffffff", 560, 0.3},
	{"ember", "#ffb224", "#ff4d2e", glyphLine, "#ffffff", "#ffffff", 520, 0.3},
	{"mint", "#4ade9b", "#04875f", glyphTray, "#ffffff", "#ffffff", 520, 0.3},
	{"midnight", "#16306e", "#070d24", glyphM, "#e8f1ff", "#4cc9ff", 560, 0.14},
}

func (d design) svg() *ui.SVG {
	body := strings.NewReplacer("{a}", d.a, "{b}", d.b).Replace(d.glyph)
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">` + body + `</svg>`))
}

func (d design) view(c *ui.Context) {
	glyph := d.svg()
	white := ui.RGB(255, 255, 255)
	c.Root().Background(ui.Transparent)
	ui.Box(c).Absolute().Left(inset).Top(inset).Size(shape, shape).Radius(radius).Clip().
		LinearGradient(ui.LinearGradient{From: ui.Hex(d.from), To: ui.Hex(d.to), Angle: 160, Oklab: true}).
		Shadow(0, 16, 32, 0, ui.RGBA(0, 0, 0, 0.24)).
		Shadow(0, 2, 6, 0, ui.RGBA(0, 0, 0, 0.16)).
		Children(func() {
			// Light from above, and along the top edge.
			ui.Box(c).Absolute().Left(0).Top(0).Size(shape, shape*0.55).
				LinearGradient(ui.LinearGradient{From: white.Alpha(d.rim * 0.6), To: white.Alpha(0), Angle: 180})
			at := (shape - d.size) / 2
			ui.Image(c, glyph).Size(d.size, d.size).Absolute().Left(at).Top(at)
		}).
		DrawOver(func(p *ui.Painter, r ui.Rect) {
			if d.rim > 0 {
				p.Stroke(r, white.Alpha(d.rim), radius, 3)
			} else {
				p.Stroke(r, ui.RGBA(0, 0, 0, 0.08), radius, 2)
			}
		})
}

// sheet shows every design beside the others, with its name.
func sheet(c *ui.Context) {
	c.Root().Background(ui.Hex("#d9dbe1"))
	ui.Grid(c).Columns(3).Padding(24).Gap(12).Children(func() {
		for i, d := range designs {
			icon := ui.NewBitmap(ui.Render(d.view, canvas, canvas, 0.4))
			ui.Column(c).AlignItems(ui.Center).Gap(2).Children(func() {
				ui.Image(c, icon).Size(360, 360)
				ui.Text(c, fmt.Sprintf("%d  %s", i+1, d.name)).FontSize(22).FontWeight(600).TextColor(ui.Hex("#1d1d1f"))
			})
		}
	})
}

func main() {
	out := flag.String("o", "resources/icon.png", "the PNG to write")
	variant := flag.String("variant", chosen, "the design to draw")
	sheetOut := flag.String("sheet", "", "write every design to this PNG instead")
	flag.Parse()
	view, width, height, path := func(*ui.Context) {}, canvas, canvas, *out
	if *sheetOut != "" {
		view, width, height, path = sheet, 1176, 860, *sheetOut
	} else {
		found := false
		for _, d := range designs {
			if d.name == *variant {
				view, found = d.view, true
			}
		}
		if !found {
			log.Fatalf("no design named %q", *variant)
		}
	}
	f, err := os.Create(path)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, ui.Render(view, width, height, 1)); err != nil {
		log.Fatal(err)
	}
}
