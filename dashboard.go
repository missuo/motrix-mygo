package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"motrix-mygo/engine"
)

// tile is a card of the dashboard.
func tile(c *ui.Context, p palette) ui.Element {
	return ui.Column(c).MinHeight(162).Padding(16).Radius(18).Background(p.tile).Clip().
		Shadow(0, 1, 2, 0, p.shadow).Shadow(0, 10, 28, -12, p.shadow)
}

// bigNumber is a tile's figure with its unit beside it.
func bigNumber(c *ui.Context, value, unit string) {
	ui.Row(c).Gap(5).AlignItems(ui.End).Margin(6, 0, 0, 0).Children(func() {
		ui.Text(c, value).FontSize(34).Bold().LetterSpacing(-0.6).FontFeatures("tnum").FixedLineHeight(38).SingleLine()
		if unit != "" {
			ui.Text(c, unit).FontSize(12.5).FontWeight(500).Margin(0, 0, 5, 0).SingleLine()
		}
	})
}

func (a *app) dashboardPage(c *ui.Context, p palette) {
	a.headerRow(c, 84, 10).AlignItems(ui.Center).DragWindow().Children(func() {
		ui.Text(c, T("Dashboard")).FontSize(24).Bold().LetterSpacing(-0.4).SingleLine()
	})
	// Four tiles a row, or two where the window is too narrow for them.
	columns := 4
	if width, _ := c.Size(); width-a.sidebarShown() < 640 {
		columns = 2
	}
	ui.Scroll(c).Grow(1).Padding(4, 16, 24, 10).Children(func() {
		ui.Grid(c).Columns(columns).Gap(16).Children(func() {
			a.engineTile(c, p)
			a.speedLimitTile(c, p)
			a.chartTile(c, p, T("UPLOAD"), a.stats.UploadHistory, p.up, p.upFill, func() {
				value, unit := splitBytes(a.stats.UploadSpeed)
				bigNumber(c, value, unit+"/s")
			})
			a.chartTile(c, p, T("DOWNLOAD"), a.stats.History, p.down, p.downFill, func() {
				value, unit := splitBytes(a.stats.Speed)
				bigNumber(c, value, unit+"/s")
			})
			a.activeTile(c, p)
			a.transferTile(c, p)
			a.activityTile(c, p)
		})
	})
}

func (a *app) engineTile(c *ui.Context, p palette) {
	tile(c, p).Children(func() {
		caption(c, p, T("ENGINE"))
		state := map[engine.State]string{
			engine.Ready: "Ready", engine.Starting: "Starting", engine.Stopped: "Stopped", engine.Broken: "Error",
		}[a.stats.State]
		ui.Text(c, T(state)).FontSize(22).Bold().LetterSpacing(-0.3).Margin(8, 0, 0, 0).SingleLine()
		if a.stats.State == engine.Broken {
			ui.Text(c, a.stats.Err).FontSize(11.5).TextColor(p.muted).MaxLines(3).Margin(4, 0, 0, 0)
		}
		ui.Spacer(c)
		_, light := a.engineLabel(p)
		ui.Row(c).AlignItems(ui.Center).Children(func() {
			version := "aria2"
			if a.stats.Version != "" {
				// The build's suffix says little beside the version.
				v, _, _ := strings.Cut(a.stats.Version, "-")
				version = "aria2 v" + v
			}
			ui.Text(c, version).FontSize(12.5).TextColor(p.muted).SingleLine().Grow(1).Basis(0)
			ui.Box(c).Size(28, 28).Radius(14).Center().Background(light.Alpha(0.14)).Children(func() {
				ui.Box(c).Size(10, 10).Radius(5).Background(light)
			})
		})
	})
}

func (a *app) speedLimitTile(c *ui.Context, p palette) {
	tile(c, p).Children(func() {
		caption(c, p, T("SPEED LIMIT"))
		mode := 0
		for i, tm := range turtleModes {
			if tm.id == a.set.SpeedLimit.Turtle {
				mode = i
			}
		}
		seg := ui.SegmentedBase(c.Key("mode"), &mode, len(turtleModes))
		seg.Track.Gap(6).Margin(10, 0, 0, 0).Label(T("Speed mode")).Children(func() {
			for i, tm := range turtleModes {
				s := seg.Segment(i).Size(38, 38).Radius(10).Center().Label(T(tm.label)).Tooltip(T(tm.label))
				switch {
				case i == mode:
					s.Background(p.accent).TextColor(ui.RGB(255, 255, 255))
				case s.Hovered():
					s.Background(p.hover).TextColor(p.muted)
				default:
					s.TextColor(p.muted)
				}
				s.Children(func() { ui.Icon(c, icon(tm.icon)).FontSize(19) })
			}
		})
		// Changed applies the click to mode in this pass.
		if seg.Track.Changed() || turtleModes[mode].id != a.set.SpeedLimit.Turtle {
			a.setTurtle(turtleModes[mode].id)
		}
		ui.Spacer(c)
		if a.limits.Download > 0 || a.limits.Upload > 0 {
			ui.Text(c, limitsLabel(a.limits)).FontSize(12).TextColor(p.muted).FontFeatures("tnum").SingleLine()
		}
		ui.Text(c, T(turtleModes[mode].label+" mode")).FontSize(13).FontWeight(600).SingleLine()
	})
}

// limitsLabel writes the limits in force, as "↓ 512 KB/s  ↑ 64.0 KB/s".
func limitsLabel(l engine.Limits) string {
	one := func(v int64) string {
		if v <= 0 {
			return "∞"
		}
		return formatSpeed(v)
	}
	return "↓ " + one(l.Download) + "  ↑ " + one(l.Upload)
}

// chartTile is a tile whose figure stands over the last minute of it,
// drawn along the tile's bottom.
func (a *app) chartTile(c *ui.Context, p palette, title string, history []int64, line, fill ui.Color, figure func()) {
	tile(c, p).Children(func() {
		ui.Box(c).Absolute().Left(0).Right(0).Bottom(0).Height(84).PassThrough().Draw(func(pt *ui.Painter, r ui.Rect) {
			drawSparkline(pt, r, history, line, fill)
		})
		caption(c, p, title)
		figure()
	})
}

// drawSparkline draws samples as a line over a filled area, which rests
// 36 DIPs above the box's bottom while they are all zero.
func drawSparkline(pt *ui.Painter, r ui.Rect, samples []int64, line, fill ui.Color) {
	const rest = 36
	var peak int64 = 1
	for _, v := range samples {
		peak = max(peak, v)
	}
	n := max(len(samples), 2)
	at := func(i int) (float32, float32) {
		var v int64
		if i < len(samples) {
			v = samples[i]
		}
		x := r.X + r.W*float32(i)/float32(n-1)
		y := r.Y + r.H - rest - (r.H-rest-2)*float32(v)/float32(peak)
		return x, y
	}
	var area, curve ui.Path
	area.MoveTo(r.X, r.Y+r.H)
	for i := range n {
		x, y := at(i)
		area.LineTo(x, y)
		if i == 0 {
			curve.MoveTo(x, y)
		} else {
			curve.LineTo(x, y)
		}
	}
	area.LineTo(r.X+r.W, r.Y+r.H)
	area.Close()
	pt.FillPath(&area, fill)
	pt.StrokePath(&curve, 2, line)
}

func (a *app) activeTile(c *ui.Context, p palette) {
	tile(c, p).ColumnSpan(2).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Children(func() {
			caption(c, p, T("ACTIVE TASKS")).Grow(1)
			light := p.muted.Alpha(0.45)
			if a.stats.Active > 0 {
				light = p.accent
			}
			ui.Box(c).Size(10, 10).Radius(5).Background(light)
		})
		bigNumber(c, fmt.Sprint(a.stats.Active), "")
		ui.Spacer(c).MinHeight(18)
		ui.Row(c).Gap(24).Dividers(1, p.border).Children(func() {
			count := func(n int, label string) {
				ui.Column(c).Gap(2).Shrink(0).Children(func() {
					ui.Text(c, fmt.Sprint(n)).FontSize(20).Bold().FontFeatures("tnum").SingleLine()
					caption(c, p, label)
				})
			}
			count(a.stats.Active, T("DOWNLOADING"))
			count(a.stats.Waiting, T("WAITING"))
			count(a.stats.Seeding, T("SEEDING"))
		})
	})
}

func (a *app) transferTile(c *ui.Context, p palette) {
	tile(c, p).ColumnSpan(2).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Children(func() {
			caption(c, p, T("TRANSFER")).Grow(1)
			ui.Segmented(c, &a.transferAll, T("Today"), T("All Time")).Label(T("TRANSFER")).FontSize(12)
		})
		bytes, none, some := a.stats.Today, T("No transfer recorded today."), T("Downloaded today.")
		if a.transferAll == 1 {
			bytes, none, some = a.stats.AllTime, T("No transfer recorded yet."), T("Downloaded in total.")
		}
		value, unit := splitBytes(bytes)
		bigNumber(c, value, unit)
		ui.Spacer(c).MinHeight(18)
		line := some
		if bytes == 0 {
			line = none
		}
		ui.Text(c, line).FontSize(13.5).TextColor(p.muted).SingleLine()
	})
}

func (a *app) activityTile(c *ui.Context, p palette) {
	tile(c, p).ColumnSpan(-1).Children(func() {
		caption(c, p, T("ACTIVITY"))
		now := time.Now()
		ui.Box(c).Height(heatmapHeight).Margin(8, 0, 0, 0).Draw(func(pt *ui.Painter, r ui.Rect) {
			drawHeatmap(pt, r, p, a.stats.Daily, now)
		})
		ui.Row(c).Gap(5).Justify(ui.End).AlignItems(ui.Center).Margin(8, 0, 0, 0).Children(func() {
			ui.Text(c, T("Less")).FontSize(11.5).TextColor(p.muted).Margin(0, 3, 0, 0)
			for level := range 5 {
				ui.Box(c).Size(11, 11).Radius(3).Background(heatColor(p, level))
			}
			ui.Text(c, T("More")).FontSize(11.5).TextColor(p.muted).Margin(0, 0, 0, 3)
		})
	})
}

const (
	heatWeeks     = 53
	heatLabels    = 18 // the months above the days
	heatGutter    = 40 // the weekdays before them
	heatmapHeight = heatLabels + 7*15
)

// heatColor is the color of a day of a level from 0, nothing, to 4.
func heatColor(p palette, level int) ui.Color {
	if level == 0 {
		return p.hover
	}
	return p.down.Alpha([]float32{0, 0.3, 0.5, 0.75, 1}[level])
}

// drawHeatmap draws the bytes of each day of the last year as a week a
// column, as a contribution graph.
func drawHeatmap(pt *ui.Painter, r ui.Rect, p palette, daily map[string]int64, now time.Time) {
	pitch := min((r.W-heatGutter)/heatWeeks, 15)
	if pitch < 4 {
		return
	}
	cell := pitch - 3
	var peak int64
	for _, n := range daily {
		peak = max(peak, n)
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, now.Location())
	// The last column is this week, from its Sunday.
	start := today.AddDate(0, 0, -int(today.Weekday())-7*(heatWeeks-1))
	weekdays := []string{"Mon", "Wed", "Fri"}
	if chinese {
		weekdays = []string{"周一", "周三", "周五"}
	}
	for i, name := range weekdays {
		pt.Text(r.X, r.Y+heatLabels+float32(1+2*i)*pitch-1, name, 11, p.muted)
	}
	month := time.Month(0)
	for week := range heatWeeks {
		x := r.X + heatGutter + float32(week)*pitch
		first := start.AddDate(0, 0, 7*week)
		if first.Month() != month {
			if month != 0 || first.Day() <= 7 {
				label := first.Format("Jan")
				if chinese {
					label = fmt.Sprintf("%d月", int(first.Month()))
				}
				pt.Text(x, r.Y, label, 11, p.muted)
			}
			month = first.Month()
		}
		for day := range 7 {
			date := first.AddDate(0, 0, day)
			if date.After(today) {
				break
			}
			level := 0
			if n := daily[engine.DayKey(date)]; n > 0 && peak > 0 {
				level = 1 + int(3*float64(n)/float64(peak))
			}
			pt.Fill(ui.Rect{X: x, Y: r.Y + heatLabels + float32(day)*pitch, W: cell, H: cell}, heatColor(p, min(level, 4)), 3)
		}
	}
}
