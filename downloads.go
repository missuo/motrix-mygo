package main

import (
	"cmp"
	"fmt"
	"math"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"motrix-mygo/engine"
)

// downloadsState is the state of the downloads page.
type downloadsState struct {
	// filter shows the tasks of one status, "" for all.
	filter    engine.Status
	query     string
	searching bool
	sortBy    string // a column's ID, "" for the engine's order
	sortDesc  bool

	list     ui.ListState
	selected int
	chosen   ui.Selection[string]
	// rows are the tasks the list shows, in its order.
	rows []engine.Snapshot
	// after holds what a row's menu chose, to do once the rows are built.
	after []func()

	deleting  bool
	deleteIDs []string
}

type column struct {
	id, title string
	width     float32 // 0 takes the room the others leave
	align     ui.Align
}

var taskColumns = []column{
	{"name", "Name", 0, ui.Start},
	{"size", "Size", 72, ui.End},
	{"progress", "Progress", 124, ui.Start},
	{"status", "Status", 120, ui.Start},
	{"down", "Down", 80, ui.End},
	{"up", "Up", 80, ui.End},
	{"eta", "ETA", 64, ui.End},
	{"added", "Added", 64, ui.End},
}

const (
	// nameMinWidth is the least the column of names takes: a list narrower
	// than its columns scrolls sideways.
	nameMinWidth = 180
	rowPadding   = 12
)

// rowWidth is the least width a row of the columns takes.
func rowWidth(cols []column) float32 {
	w := float32(2*rowPadding + nameMinWidth)
	for _, col := range cols {
		w += col.width
	}
	return w
}

var statusFilters = []struct {
	status engine.Status
	label  string
}{
	{"", "All Downloads"},
	{engine.Downloading, "Downloading"},
	{engine.Queued, "Waiting"},
	{engine.Seeding, "Seeding"},
	{engine.Paused, "Paused"},
	{engine.Completed, "Completed"},
	{engine.Failed, "Failed"},
}

func statusLabel(s engine.Status) string {
	switch s {
	case engine.Downloading:
		return T("Downloading")
	case engine.Metadata:
		return T("Fetching metadata")
	case engine.Seeding:
		return T("Seeding")
	case engine.Queued:
		return T("Queued")
	case engine.Paused:
		return T("Paused")
	case engine.Completed:
		return T("Completed")
	}
	return T("Failed")
}

func statusTone(p palette, s engine.Status) tone {
	switch s {
	case engine.Paused:
		return p.tone("amber")
	case engine.Seeding:
		return p.tone("green")
	case engine.Completed:
		return p.tone("slate")
	case engine.Failed:
		return p.tone("red")
	}
	return p.tone("blue")
}

// displayName is a task's name, or its link until the server named it.
func displayName(t engine.Snapshot) string {
	if t.Name != "" {
		return t.Name
	}
	return t.URL
}

// visibleColumns are the columns the user did not hide.
func (a *app) visibleColumns() []column {
	cols := make([]column, 0, len(taskColumns))
	for _, col := range taskColumns {
		if col.id == "name" || !slices.Contains(a.set.UI.HiddenColumns, col.id) {
			cols = append(cols, col)
		}
	}
	return cols
}

// filterTasks returns the tasks of the status and the search, sorted.
func (a *app) filterTasks() []engine.Snapshot {
	d := &a.downloads
	query := strings.ToLower(strings.TrimSpace(d.query))
	rows := make([]engine.Snapshot, 0, len(a.tasks))
	for _, t := range a.tasks {
		status := t.Status
		if status == engine.Metadata {
			status = engine.Downloading
		}
		if d.filter != "" && status != d.filter {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(t.Name), query) && !strings.Contains(strings.ToLower(t.URL), query) {
			continue
		}
		rows = append(rows, t)
	}
	if d.sortBy == "" {
		return rows
	}
	slices.SortStableFunc(rows, func(x, y engine.Snapshot) int {
		var n int
		switch d.sortBy {
		case "name":
			n = cmp.Compare(strings.ToLower(displayName(x)), strings.ToLower(displayName(y)))
		case "size":
			n = cmp.Compare(x.Size, y.Size)
		case "progress":
			n = cmp.Compare(x.Progress(), y.Progress())
		case "status":
			n = cmp.Compare(x.Status, y.Status)
		case "down":
			n = cmp.Compare(x.Speed, y.Speed)
		case "up":
			n = cmp.Compare(x.UploadSpeed, y.UploadSpeed)
		case "eta":
			n = cmp.Compare(x.ETA(), y.ETA())
		case "added":
			n = x.CreatedAt.Compare(y.CreatedAt)
		}
		if d.sortDesc {
			n = -n
		}
		return n
	})
	return rows
}

func (a *app) downloadsPage(c *ui.Context, p palette) {
	d := &a.downloads
	d.rows = a.filterTasks()
	if d.selected >= len(d.rows) {
		d.selected = -1
	}

	a.pageHeader(c, "", func() { a.downloadsHeader(c, p) })
	ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			switch {
			case len(a.tasks) == 0:
				a.noDownloads(c, p)
			case len(d.rows) == 0:
				emptyState(c, p, "", T("No matching downloads"), T("Try another filter"))
			default:
				a.taskList(c, p)
			}
		})
		if a.set.UI.InspectorShown {
			a.inspector(c, p)
		}
	})
	a.statusBar(c, p)

	for _, fn := range d.after {
		fn()
	}
	d.after = d.after[:0]
}

func (a *app) downloadsHeader(c *ui.Context, p palette) {
	d := &a.downloads
	title := T("All Downloads")
	for _, f := range statusFilters {
		if f.status == d.filter {
			title = T(f.label)
		}
	}
	ui.ButtonBase(c.Key("filter")).Gap(6).Padding(4, 8).Margin(0, 0, 0, -8).Radius(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, title).FontSize(24).Bold().LetterSpacing(-0.4).SingleLine()
		ui.Icon(c, icon("chevron-down")).FontSize(16).TextColor(p.muted)
	}).Menu(func(m *ui.Menu) {
		for _, f := range statusFilters {
			if m.Item(T(f.label)).Checked(d.filter == f.status).Chosen() {
				d.filter = f.status
				d.selected = -1
			}
		}
	})
	ui.Textf(c, "%d/%d", a.stats.Active, a.stats.Total).FontSize(12.5).FontWeight(600).FontFeatures("tnum").
		Padding(2, 9).Radius(11).Border(1, p.border).SingleLine()
	ui.Spacer(c)

	if d.searching {
		ui.SearchField(c.Key("search"), &d.query).Label(T("Search downloads")).Width(190).AutoFocus()
	}
	solid := p.tile
	ui.Row(c).Height(38).Padding(0, 3).Radius(19).AlignItems(ui.Center).Background(solid).Border(1, p.border).
		Shadow(0, 1, 2, 0, p.shadow).Children(func() {
		iconButton(c, p, "ellipsis", T("More actions")).Menu(func(m *ui.Menu) {
			if m.Item(T("Resume all")).Disabled(a.stats.Paused == 0).Chosen() {
				a.try(a.eng.ResumeAll())
			}
			if m.Item(T("Pause all")).Disabled(a.stats.Active+a.stats.Waiting+a.stats.Seeding == 0).Chosen() {
				a.try(a.eng.PauseAll())
			}
			m.Separator()
			if m.Item(T("Clear completed")).Disabled(a.stats.Completed == 0).Chosen() {
				a.try(a.eng.ClearCompleted())
			}
		})
		iconButton(c, p, "columns-3", T("Columns")).Menu(func(m *ui.Menu) {
			for _, col := range taskColumns[1:] {
				hidden := slices.Contains(a.set.UI.HiddenColumns, col.id)
				if m.Item(T(col.title)).Checked(!hidden).Chosen() {
					if hidden {
						a.set.UI.HiddenColumns = slices.DeleteFunc(a.set.UI.HiddenColumns, func(id string) bool { return id == col.id })
					} else {
						a.set.UI.HiddenColumns = append(a.set.UI.HiddenColumns, col.id)
					}
					a.set.save(a.paths.settings)
				}
			}
		})
		info := iconButton(c, p, "info", T("Details"))
		if a.set.UI.InspectorShown {
			info.Background(p.active)
		}
		if info.Clicked() {
			a.set.UI.InspectorShown = !a.set.UI.InspectorShown
			a.set.save(a.paths.settings)
		}
	})
	search := ui.ButtonBase(c.Key("search-toggle")).Size(38, 38).Radius(19).Center().Label(T("Search")).Tooltip(T("Search")).
		Background(solid).Border(1, p.border).Shadow(0, 1, 2, 0, p.shadow)
	if d.searching {
		search.Background(p.active)
	}
	search.Children(func() { ui.Icon(c, icon("search")).FontSize(17) })
	if search.Clicked() || (!d.searching && c.Shortcut(ui.Cmd, ui.KeyF)) {
		d.searching = !d.searching
		if !d.searching {
			d.query = ""
		}
	}
}

// noDownloads is what the page shows before the first download.
func (a *app) noDownloads(c *ui.Context, p palette) {
	ui.Column(c).Grow(1).Children(func() {
		ui.Box(c).Absolute().Left(0).Right(0).Bottom(0).HeightPercent(52).PassThrough().Draw(func(pt *ui.Painter, r ui.Rect) {
			drawPixels(pt, r, p.dark)
		})
		ui.Column(c).Grow(1).Center().Gap(10).Padding(0, 0, 40, 0).Children(func() {
			ui.Text(c, T("No downloads yet")).FontSize(19).FontWeight(600)
			ui.Text(c, T("Press + to add one")).FontSize(14.5).TextColor(p.muted)
		})
	})
}

// drawPixels paints the mosaic that glows from the bottom of the empty
// page: squares from blue to pink, fading away from its middle.
func drawPixels(pt *ui.Painter, r ui.Rect, dark bool) {
	const cell = 28
	blue, violet, pink := ui.Hex("#5b8ff9"), ui.Hex("#b7a2f0"), ui.Hex("#f9a8dc")
	strength := float32(0.95)
	if dark {
		strength = 0.6
	}
	cols := int(min(r.W, 620) / cell)
	rows := int(r.H / cell)
	if cols < 2 || rows < 1 {
		return
	}
	left := r.X + (r.W-float32(cols*cell))/2
	bottom := r.Y + r.H
	for gy := range rows {
		for gx := range cols {
			u := (float32(gx) + 0.5) / float32(cols)
			v := (float32(gy) + 0.5) / float32(rows) // 0 at the bottom
			dx := (u - 0.5) * 2
			dist := float32(math.Sqrt(float64(dx*dx + v*v)))
			if dist >= 1 {
				continue
			}
			alpha := float32(math.Pow(float64(1-dist), 1.6)) * strength
			t := min(max((u-0.28)/0.44, 0), 1)
			color := blue.Mix(violet, min(t*2, 1))
			if t > 0.5 {
				color = violet.Mix(pink, (t-0.5)*2)
			}
			pt.Fill(ui.Rect{X: left + float32(gx*cell), Y: bottom - float32((gy+1)*cell), W: cell, H: cell}, color.Alpha(alpha), 0)
		}
	}
}

func (a *app) taskList(c *ui.Context, p palette) {
	d := &a.downloads
	cols := a.visibleColumns()

	head := ui.Row(c).Height(30).Padding(0, rowPadding).Clip().BorderWidth(0, 0, 1, 0).BorderColor(p.border).Children(func() {
		for _, col := range cols {
			head := ui.ButtonBase(c.Key(col.id)).Height(30).PaddingX(8).Gap(4).AlignItems(ui.Center).
				FontSize(11.5).FontWeight(500).TextColor(p.muted)
			sizeCell(head, col)
			head.Children(func() {
				ui.Text(c, T(col.title)).SingleLine()
				if d.sortBy == col.id {
					arrow := "arrow-up"
					if d.sortDesc {
						arrow = "arrow-down"
					}
					ui.Icon(c, icon(arrow)).FontSize(11)
				}
			})
			if head.Clicked() {
				switch {
				case d.sortBy != col.id:
					d.sortBy, d.sortDesc = col.id, false
				case !d.sortDesc:
					d.sortDesc = true
				default:
					d.sortBy, d.sortDesc = "", false
				}
			}
		}
	})

	d.list.Key = func(i int) any { return d.rows[i].ID }
	d.list.Label = func(i int) string { return displayName(d.rows[i]) }
	d.list.Selected = &d.selected
	d.list.Selection = &d.chosen
	now := time.Now()
	list := ui.List(c.Key("tasks"), &d.list, len(d.rows)).Grow(1).RowWidth(rowWidth(cols)).Label(T("Downloads")).
		Rows(func(row ui.ListRow) { a.taskRow(row, p, cols, now) })
	head.ScrollWith(list)
	if list.Submitted() && d.selected >= 0 && d.selected < len(d.rows) {
		a.primaryAction(d.rows[d.selected])
	}
	if list.Shortcut(0, ui.KeyDelete) || list.Shortcut(0, ui.KeyBackspace) {
		if ids := a.chosenIDs(); len(ids) > 0 {
			a.removeTasks(ids, false)
		}
	}
}

// sizeCell gives a cell of a column its width and its alignment.
func sizeCell(cell ui.Element, col column) {
	if col.width == 0 {
		cell.Grow(1).Basis(0).MinWidth(nameMinWidth)
	} else {
		cell.Width(col.width).Shrink(0)
	}
	cell.Justify(col.align)
}

func (a *app) taskRow(row ui.ListRow, p palette, cols []column, now time.Time) {
	c := row.Context
	t := a.downloads.rows[row.Index]
	text, muted := p.text, p.muted
	r := ui.Row(c).Height(36).Padding(0, rowPadding).FontSize(12.5).FontFeatures("tnum")
	switch {
	case row.Selected():
		text = ui.RGB(255, 255, 255)
		muted = ui.RGBA(255, 255, 255, 0.8)
	case row.Index%2 == 1:
		r.Background(p.hover)
	}
	r.TextColor(text)
	r.ContextMenu(func(m *ui.Menu) { a.taskMenu(c, m, t) })
	r.Children(func() {
		for _, col := range cols {
			cell := ui.Row(c.Key(col.id)).PaddingX(8).AlignItems(ui.Center)
			sizeCell(cell, col)
			cell.Children(func() {
				switch col.id {
				case "name":
					name := ui.Text(c, displayName(t)).FontWeight(500).SingleLine().Grow(1).MinWidth(0)
					if t.Status == engine.Failed {
						name.Tooltip(t.Err)
					}
				case "size":
					ui.Text(c, formatBytes(t.Size)).SingleLine()
				case "progress":
					taskProgress(c, p, t, row.Selected(), muted)
				case "status":
					tn := statusTone(p, t.Status)
					if row.Selected() {
						// The tones do not show on the accent color.
						tn.bg, tn.text = ui.RGBA(255, 255, 255, 0.22), text
					}
					ui.Text(c, statusLabel(t.Status)).FontSize(11).Padding(2, 6).Radius(4).SingleLine().
						Background(tn.bg).TextColor(tn.text)
				case "down":
					if t.Status == engine.Downloading || t.Status == engine.Metadata {
						ui.Text(c, formatSpeed(t.Speed)).SingleLine()
					}
				case "up":
					if t.BitTorrent && (t.Status == engine.Downloading || t.Status == engine.Seeding) {
						ui.Text(c, formatSpeed(t.UploadSpeed)).SingleLine()
					}
				case "eta":
					if t.Status == engine.Downloading {
						ui.Text(c, formatDuration(t.ETA())).SingleLine()
					}
				case "added":
					ui.Text(c, formatWhen(t.CreatedAt, now)).TextColor(muted).SingleLine()
				}
			})
		}
	})
}

// taskProgress is a task's bar and the share it stands for.
func taskProgress(c *ui.Context, p palette, t engine.Snapshot, selected bool, muted ui.Color) {
	progress := t.Progress()
	bar := statusTone(p, t.Status).bar
	track := p.active
	if selected {
		bar, track = ui.RGB(255, 255, 255), ui.RGBA(255, 255, 255, 0.3)
	}
	ui.Box(c).Grow(1).MinWidth(16).Height(4).Radius(2).Background(track).Draw(func(pt *ui.Painter, r ui.Rect) {
		if progress > 0 {
			pt.Fill(ui.Rect{X: r.X, Y: r.Y, W: max(r.W*float32(progress), r.H), H: r.H}, bar, 2)
		}
	})
	label := "—"
	if progress >= 0 {
		label = fmt.Sprintf("%.0f%%", math.Floor(progress*100))
	}
	ui.Text(c, label).Width(38).TextAlign(ui.End).TextColor(muted).SingleLine().Margin(0, 0, 0, 8)
}

// chosenIDs are the tasks chosen in the list that it still shows.
func (a *app) chosenIDs() []string {
	var ids []string
	for _, t := range a.downloads.rows {
		if a.downloads.chosen.Has(t.ID) {
			ids = append(ids, t.ID)
		}
	}
	return ids
}

// targets are the tasks an action on t applies to: all those chosen when t
// is one of them, else t alone.
func (a *app) targets(t engine.Snapshot) []engine.Snapshot {
	if !a.downloads.chosen.Has(t.ID) {
		return []engine.Snapshot{t}
	}
	var out []engine.Snapshot
	for _, row := range a.downloads.rows {
		if a.downloads.chosen.Has(row.ID) {
			out = append(out, row)
		}
	}
	return out
}

func canResume(t engine.Snapshot) bool {
	return t.Status == engine.Paused || t.Status == engine.Failed
}

func canPause(t engine.Snapshot) bool {
	switch t.Status {
	case engine.Downloading, engine.Queued, engine.Metadata, engine.Seeding:
		return true
	}
	return false
}

// taskMenu fills the menu of a row. What is chosen runs once the rows are
// built, as it may take rows out of them.
func (a *app) taskMenu(c *ui.Context, m *ui.Menu, t engine.Snapshot) {
	d := &a.downloads
	targets := a.targets(t)
	ids := make([]string, len(targets))
	for i, x := range targets {
		ids[i] = x.ID
	}
	later := func(fn func()) { d.after = append(d.after, fn) }

	if slices.ContainsFunc(targets, canResume) {
		label := T("Resume")
		if t.Status == engine.Failed {
			label = T("Retry")
		}
		if m.Item(label).Chosen() {
			later(func() {
				for _, x := range targets {
					if canResume(x) {
						a.try(a.eng.Resume(x.ID))
					}
				}
			})
		}
	}
	if slices.ContainsFunc(targets, canPause) && m.Item(T("Pause")).Chosen() {
		later(func() {
			for _, x := range targets {
				if canPause(x) {
					a.try(a.eng.Pause(x.ID))
				}
			}
		})
	}
	m.Separator()
	if m.Item(T("Open")).Disabled(t.Status != engine.Completed).Chosen() {
		mygo.Shell.OpenPath(t.Path())
	}
	if m.Item(T("Show in folder")).Chosen() {
		showInFolder(t)
	}
	if m.Item(T("Copy link")).Chosen() {
		c.WriteClipboard(t.URL)
		c.Toast(T("Link copied"))
	}
	m.Separator()
	if m.Item(T("Remove")).Chosen() {
		later(func() { a.removeTasks(ids, false) })
	}
	if m.Item(T("Remove and delete files")).Chosen() {
		later(func() { d.deleting, d.deleteIDs = true, ids })
	}
}

// showInFolder shows a task's file in the file manager, or its directory
// before the file is there.
func showInFolder(t engine.Snapshot) {
	if _, err := os.Stat(t.Path()); err != nil {
		mygo.Shell.OpenPath(t.Dir)
		return
	}
	mygo.Shell.ShowItemInFolder(t.Path())
}

// primaryAction is what a double click or Enter does to a task.
func (a *app) primaryAction(t engine.Snapshot) {
	switch {
	case t.Status == engine.Completed:
		mygo.Shell.OpenPath(t.Path())
	case canResume(t):
		a.try(a.eng.Resume(t.ID))
	case canPause(t):
		a.try(a.eng.Pause(t.ID))
	}
}

// removeTasks takes tasks off the list, with their files or without. It
// returns at once: stopping a download and moving its files to the trash
// can take seconds, which must not hold up the window.
func (a *app) removeTasks(ids []string, deleteFiles bool) {
	for _, id := range ids {
		a.downloads.chosen.Remove(id)
	}
	a.downloads.selected = -1
	remove := func() error {
		var first error
		for _, id := range ids {
			if err := a.eng.Remove(id, deleteFiles); err != nil && first == nil {
				first = err
			}
		}
		return first
	}
	if a.win == nil {
		a.try(remove())
		return
	}
	go func() {
		err := remove()
		a.update(func() { a.try(err) })
	}()
}

// deleteDialog asks before files are deleted from the disk.
func (a *app) deleteDialog(c *ui.Context) {
	d := &a.downloads
	if ui.AlertDialog(c, &d.deleting, T("Delete files?"),
		a.deleteWarning(),
		T("Cancel"), T("Delete")) == 1 {
		a.removeTasks(d.deleteIDs, true)
		d.deleteIDs = nil
	}
}

// deleteWarning says where the files of removed tasks go.
func (a *app) deleteWarning() string {
	if a.set.App.FileDeletionMode == "trash" {
		return T("The tasks are removed and their files moved to the trash.")
	}
	return T("The tasks are removed and their files deleted permanently.")
}

// inspector shows the details of the task chosen, beside the list.
func (a *app) inspector(c *ui.Context, p palette) {
	d := &a.downloads
	ui.Scroll(c).Width(280).Shrink(0).Padding(4, 20, 20, 20).Gap(14).BorderWidth(0, 0, 0, 1).BorderColor(p.border).Children(func() {
		if d.selected < 0 || d.selected >= len(d.rows) {
			ui.Text(c, T("Select a download to see its details")).FontSize(13).TextColor(p.muted).Padding(24, 0)
			return
		}
		t := d.rows[d.selected]
		tn := statusTone(p, t.Status)
		ui.Text(c, displayName(t)).FontSize(15).FontWeight(600).MaxLines(3).Selectable()
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, statusLabel(t.Status)).FontSize(11).FontWeight(500).Padding(2, 8).Radius(10).
				Background(tn.bg).TextColor(tn.text)
			if t.Status == engine.Downloading {
				ui.Text(c, formatSpeed(t.Speed)).FontSize(12.5).TextColor(p.muted).FontFeatures("tnum")
			}
		})
		ui.Row(c).AlignItems(ui.Center).FontSize(12.5).FontFeatures("tnum").Children(func() {
			taskProgress(c, p, t, false, p.muted)
		})
		ui.Row(c).Gap(8).Children(func() {
			switch {
			case canResume(t):
				if ui.PrimaryButton(c, T("Resume")).Clicked() {
					a.try(a.eng.Resume(t.ID))
				}
			case canPause(t):
				if ui.Button(c, T("Pause")).Clicked() {
					a.try(a.eng.Pause(t.ID))
				}
			case t.Status == engine.Completed:
				if ui.PrimaryButton(c, T("Open")).Clicked() {
					mygo.Shell.OpenPath(t.Path())
				}
			}
			if ui.Button(c, T("Show in folder")).Clicked() {
				showInFolder(t)
			}
		})
		ui.Divider(c).Background(p.border)
		detail := func(label, value string) {
			ui.Column(c).Gap(3).Children(func() {
				ui.Text(c, label).FontSize(11.5).TextColor(p.muted)
				ui.Text(c, value).FontSize(12.5).Selectable().LineHeight(1.35)
			})
		}
		size := formatBytes(t.Size)
		if t.Status != engine.Completed && t.Size > 0 {
			size = formatBytes(t.Done) + " / " + size
		}
		detail(T("Size"), size)
		if t.Status == engine.Failed {
			detail(T("Error"), t.Err)
		}
		detail(T("Link"), t.URL)
		detail(T("Saved to"), t.Dir)
		if t.Status == engine.Downloading || t.Status == engine.Seeding {
			detail(T("Connections"), fmt.Sprint(t.Conns))
		}
		if t.BitTorrent {
			detail(T("Seeders"), fmt.Sprint(t.Seeders))
			detail(T("Uploaded"), formatBytes(t.Uploaded))
		}
		if t.Status == engine.Downloading {
			detail(T("ETA"), formatDuration(t.ETA()))
		}
		detail(T("Added"), t.CreatedAt.Format("2006-01-02 15:04"))
		if !t.FinishedAt.IsZero() {
			detail(T("Finished"), t.FinishedAt.Format("2006-01-02 15:04"))
		}
	})
}

// statusBar is the band under the list: the speed mode, the speed and the
// engine's state.
func (a *app) statusBar(c *ui.Context, p palette) {
	left := float32(24)
	ui.Row(c).Height(52).Padding(0, 24, 4, left).Gap(8).AlignItems(ui.Center).Children(func() {
		mode := turtleModes[0]
		for _, tm := range turtleModes {
			if tm.id == a.set.SpeedLimit.Turtle {
				mode = tm
			}
		}
		b := ui.ButtonBase(c.Key("mode")).Height(28).Padding(0, 10).Gap(7).Radius(14).AlignItems(ui.Center).
			Background(p.hover).FontSize(13).FontWeight(500).Label(T("Speed mode"))
		if b.Hovered() {
			b.Background(p.active)
		}
		b.Children(func() {
			ui.Icon(c, icon(mode.icon)).FontSize(15)
			ui.Text(c, T(mode.label)).SingleLine()
			ui.Icon(c, icon("chevron-down")).FontSize(13).TextColor(p.muted)
		}).Menu(func(m *ui.Menu) {
			for _, tm := range turtleModes {
				if m.Item(T(tm.label)).Checked(tm.id == mode.id).Chosen() {
					a.setTurtle(tm.id)
				}
			}
		})
		pill(c, p).FontFeatures("tnum").Children(func() {
			ui.Icon(c, icon("arrow-up")).FontSize(13).TextColor(p.muted)
			ui.Text(c, formatSpeed(a.stats.UploadSpeed)).SingleLine()
			ui.Box(c).Size(1, 12).Background(p.border).Margin(0, 3)
			ui.Icon(c, icon("arrow-down")).FontSize(13).TextColor(p.muted)
			ui.Text(c, formatSpeed(a.stats.Speed)).SingleLine()
		})
		ui.Spacer(c)
		a.enginePill(c, p)
	})
}

// turtleModes are the speed modes, in Motrix's order.
var turtleModes = []struct{ id, label, icon string }{
	{"off", "Standard", "rabbit"},
	{"on", "Low speed", "turtle"},
	{"auto", "Automatic", "squirrel"},
}

// engineLabel says what aria2 is doing, and the color of its light.
func (a *app) engineLabel(p palette) (string, ui.Color) {
	switch a.stats.State {
	case engine.Ready:
		return T("Engine ready"), p.success
	case engine.Broken:
		return T("Engine error"), ui.Hex("#ef4444")
	case engine.Stopped:
		return T("Engine stopped"), p.muted
	}
	return T("Engine starting"), ui.Hex("#f59e0b")
}

// enginePill shows the engine's state; a click on a broken engine starts
// it again.
func (a *app) enginePill(c *ui.Context, p palette) {
	label, light := a.engineLabel(p)
	b := ui.ButtonBase(c.Key("engine")).Height(28).Padding(0, 12).Gap(7).Radius(14).AlignItems(ui.Center).
		Background(p.hover).FontSize(13).FontWeight(500)
	if a.stats.State == engine.Broken {
		b.Tooltip(a.stats.Err)
	}
	b.Children(func() {
		dot(c, light)
		ui.Text(c, label).SingleLine()
	})
	if b.Clicked() && a.stats.State == engine.Broken {
		a.eng.Restart()
	}
}

func (a *app) setTurtle(mode string) {
	a.set.SpeedLimit.Turtle = mode
	a.saveSettings()
}
