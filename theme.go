package main

import (
	"math"

	"github.com/egoist/mygo/ui"
)

// palette is the colors of the interface, in the light or the dark
// appearance, tinted by the sidebar color.
type palette struct {
	dark bool
	// windowTop and windowBottom are the gradient behind the sidebar.
	windowTop, windowBottom ui.Color
	// card is the panel pages are on, tile a dashboard tile.
	card, tile  ui.Color
	text, muted ui.Color
	border      ui.Color
	// hover and active wash what the pointer is on and what is chosen.
	hover, active  ui.Color
	accent         ui.Color
	down, downFill ui.Color
	up, upFill     ui.Color
	shadow         ui.Color
	success        ui.Color
}

type sidebarColor struct {
	name string
	hue  float32
	gray bool
}

var sidebarColors = []sidebarColor{
	{"blue", 215, false}, {"violet", 264, false}, {"pink", 334, false},
	{"orange", 25, false}, {"gold", 45, false}, {"green", 145, false},
	{"cyan", 186, false}, {"gray", 220, true},
}

func findSidebarColor(name string) sidebarColor {
	for _, sc := range sidebarColors {
		if sc.name == name {
			return sc
		}
	}
	return sidebarColors[0]
}

// swatch is the color that stands for a sidebar color where it is chosen.
func (sc sidebarColor) swatch() ui.Color {
	if sc.gray {
		return hsl(sc.hue, 0.05, 0.62)
	}
	return hsl(sc.hue, 0.78, 0.6)
}

func newPalette(dark bool, sc sidebarColor) palette {
	sat := float32(0.58)
	if sc.gray {
		sat = 0.04
	}
	if dark {
		return palette{
			dark:         true,
			windowTop:    hsl(sc.hue+40, sat*0.72, 0.22),
			windowBottom: hsl(sc.hue, sat*0.72, 0.17),
			card:         ui.RGBA(17, 17, 26, 0.86),
			tile:         ui.RGBA(24, 24, 36, 0.82),
			text:         ui.RGB(250, 250, 250),
			muted:        ui.RGB(161, 161, 170),
			border:       ui.RGBA(255, 255, 255, 0.1),
			hover:        ui.RGBA(255, 255, 255, 0.06),
			active:       ui.RGBA(255, 255, 255, 0.14),
			accent:       ui.Hex("#3b82f6"),
			down:         hsl(193, 0.83, 0.55),
			downFill:     hsl(193, 0.83, 0.55).Alpha(0.18),
			up:           hsl(243, 0.92, 0.72),
			upFill:       hsl(243, 0.92, 0.72).Alpha(0.18),
			shadow:       ui.RGBA(0, 0, 0, 0.35),
			success:      ui.Hex("#22c55e"),
		}
	}
	return palette{
		windowTop:    hsl(sc.hue, sat, 0.96),
		windowBottom: hsl(sc.hue, sat, 0.9),
		card:         ui.RGBA(255, 255, 255, 0.92),
		tile:         ui.RGB(255, 255, 255),
		text:         ui.RGB(10, 10, 10),
		muted:        ui.RGB(115, 115, 115),
		border:       ui.RGBA(0, 0, 0, 0.09),
		hover:        ui.RGBA(0, 0, 0, 0.04),
		active:       ui.RGBA(0, 0, 0, 0.09),
		accent:       ui.Hex("#2f7df6"),
		down:         hsl(193, 0.83, 0.5),
		downFill:     ui.Hex("#e6f8fc"),
		up:           hsl(243, 0.96, 0.66),
		upFill:       ui.Hex("#f1f0ff"),
		shadow:       ui.RGBA(15, 23, 42, 0.08),
		success:      ui.Hex("#10b981"),
	}
}

// theme is the widgets' theme in the palette's colors.
func (p palette) theme(base *ui.Theme) *ui.Theme {
	t := *base
	t.Accent = p.accent
	t.AccentHover = p.accent.Mix(ui.RGB(0, 0, 0), 0.08)
	t.AccentPressed = p.accent.Mix(ui.RGB(0, 0, 0), 0.16)
	t.AccentText = ui.RGB(255, 255, 255)
	t.Text, t.TextMuted, t.Border = p.text, p.muted, p.border
	t.Radius = 8
	return &t
}

// tone is the colors of a status: of its pill and of its progress bar.
type tone struct{ bg, text, bar ui.Color }

func (p palette) tone(status string) tone {
	hex := map[string][3]string{
		"blue":  {"#dbeafe", "#1e40af", "#3b82f6"},
		"amber": {"#fef3c7", "#b45309", "#f59e0b"},
		"green": {"#dcfce7", "#15803d", "#22c55e"},
		"slate": {"#f1f5f9", "#475569", "#22c55e"},
		"red":   {"#fee2e2", "#b91c1c", "#ef4444"},
	}[status]
	t := tone{ui.Hex(hex[0]), ui.Hex(hex[1]), ui.Hex(hex[2])}
	if p.dark {
		// The bar's color carries the tone in the dark, washed for the pill.
		t.bg, t.text = t.bar.Alpha(0.18), t.bar.Mix(ui.RGB(255, 255, 255), 0.45)
		if status == "slate" {
			t.bg, t.text = ui.RGBA(255, 255, 255, 0.1), ui.RGB(203, 213, 225)
		}
	}
	return t
}

// hsl is CSS's hsl(): h in degrees, s and l from 0 to 1.
func hsl(h, s, l float32) ui.Color {
	h = float32(math.Mod(float64(h), 360))
	if h < 0 {
		h += 360
	}
	c := (1 - float32(math.Abs(float64(2*l-1)))) * s
	x := c * (1 - float32(math.Abs(math.Mod(float64(h)/60, 2)-1)))
	m := l - c/2
	var r, g, b float32
	switch {
	case h < 60:
		r, g = c, x
	case h < 120:
		r, g = x, c
	case h < 180:
		g, b = c, x
	case h < 240:
		g, b = x, c
	case h < 300:
		r, b = x, c
	default:
		r, b = c, x
	}
	to8 := func(v float32) uint8 { return uint8(math.Round(float64((v + m) * 255))) }
	return ui.RGB(to8(r), to8(g), to8(b))
}
