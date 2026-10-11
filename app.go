package main

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"motrix-mygo/engine"
)

// appPaths are the files the app runs with.
type appPaths struct {
	// settings and trackers are JSON files; empty keeps nothing.
	settings, trackers string
	// data is the directory of aria2's configuration, session, database
	// and log.
	data string
	// binary is aria2c, template the aria2.conf copied into data.
	binary, template string
	// downloads is the system's downloads directory.
	downloads string
}

// app is the state the window shows. Its view builds the interface from
// it, on the main thread; the engine's goroutines reach it through
// Window.Update.
type app struct {
	win    *mygo.Window // nil in tests
	eng    *engine.Manager
	set    settings
	paths  appPaths
	router *ui.Router

	// tasks and stats are the engine's, as of the last refresh.
	tasks []engine.Snapshot
	stats engine.Stats

	downloads downloadsState
	add       addTaskState
	notes     []note
	unread    int
	// transferAll is 1 while the dashboard shows the transfer of all time,
	// in place of today's.
	transferAll int

	// draft is the settings as a section's page edits them, until saved;
	// draftFor is that section.
	draft    settings
	draftFor string
	// bypass is the proxy's bypass list as the page's text area holds it.
	bypass string

	trackers  trackerList
	syncing   bool
	newSource string

	// flash is a message to show as a toast in the next frame.
	flash string
	// limits are the speed limits last given to the engine.
	limits engine.Limits
	// quitting is true once the user confirmed quitting with downloads
	// running.
	quitting bool
	// leaving says that a quit is under way, so that closing the window
	// closes it instead of hiding it.
	leaving bool
	tray    *mygo.Tray
}

// note is a notification: a download that finished or failed.
type note struct {
	task engine.Snapshot
	at   time.Time
}

func newApp(set settings, paths appPaths) *app {
	a := &app{set: set, paths: paths, router: ui.NewRouter("/downloads")}
	a.downloads.selected = -1
	a.trackers = loadTrackers(paths.trackers)
	a.limits = set.SpeedLimit.effective(time.Now())
	a.eng = engine.New(a.engineConfig())
	a.eng.OnChange(func() { a.update(a.refresh) })
	a.eng.OnEvent(a.onEvent)
	a.eng.OnPortChange(func(port int) {
		a.update(func() {
			a.set.Engine.RPCPort = port
			a.set.save(a.paths.settings)
		})
	})
	a.refresh()
	return a
}

// engineConfig is what the engine runs with, of the settings.
func (a *app) engineConfig() engine.Config {
	trackers := a.trackers.Trackers
	if len(trackers) > a.set.Tracker.MaxTrackerCount {
		trackers = trackers[:a.set.Tracker.MaxTrackerCount]
	}
	return engine.Config{
		Binary: a.paths.binary, Template: a.paths.template, DataDir: a.paths.data,
		SaveDir: a.set.App.DefaultSaveDir, Settings: a.set.Engine,
		Proxy: a.set.Proxy.engineProxy(), Limits: a.limits, Trackers: trackers,
		Delete: a.deleteFile,
	}
}

// deleteFile deletes a file of a task removed with its files: to the
// trash, or for good, as the settings say.
func (a *app) deleteFile(path string) error {
	if a.set.App.FileDeletionMode == "trash" && a.win != nil {
		if err := mygo.Shell.TrashItem(path); err == nil {
			return nil
		}
	}
	return os.Remove(path)
}

// refresh takes the tasks as the engine has them now.
func (a *app) refresh() {
	a.tasks = a.eng.Tasks()
	a.stats = a.eng.Stats()
}

// update runs fn on the main thread and draws a frame.
func (a *app) update(fn func()) {
	if a.win == nil {
		fn()
		return
	}
	a.win.Update(fn)
}

// try shows an error of an action on the engine, and takes its new state.
func (a *app) try(err error) {
	if err != nil {
		a.flash = err.Error()
	}
	a.refresh()
}

// saveSettings keeps the settings and has the engine and the window follow
// them.
func (a *app) saveSettings() {
	a.set.normalize(a.paths.downloads)
	a.set.save(a.paths.settings)
	a.limits = a.set.SpeedLimit.effective(time.Now())
	a.try(a.eng.Configure(a.engineConfig()))
	a.applySettings()
}

// applySettings has the app follow the language, the theme and the login
// item chosen.
func (a *app) applySettings() {
	if a.win == nil {
		setLanguage(a.set.App.Language, "")
		return
	}
	setLanguage(a.set.App.Language, mygo.App.Locale())
	if a.tray != nil {
		a.tray.SetMenu(a.trayMenu())
	}
	switch a.set.App.Theme {
	case "light":
		mygo.Theme.SetSource(mygo.ThemeLight)
	case "dark":
		mygo.Theme.SetSource(mygo.ThemeDark)
	default:
		mygo.Theme.SetSource(mygo.ThemeSystem)
	}
	if mygo.App.IsPackaged() && mygo.App.OpenAtLogin() != a.set.App.LaunchAtStartup {
		mygo.App.SetOpenAtLogin(a.set.App.LaunchAtStartup)
	}
}

// tick has the engine follow the limits of the hour, as the schedule of
// the automatic mode brings them, and updates the trackers when due. The
// app calls it every half minute.
func (a *app) tick() {
	if limits := a.set.SpeedLimit.effective(time.Now()); limits != a.limits {
		a.limits = limits
		a.try(a.eng.Configure(a.engineConfig()))
	}
	interval := time.Duration(a.set.Tracker.SyncIntervalHours) * time.Hour
	if a.set.Tracker.AutoSync && time.Since(a.trackers.SyncedAt) > interval {
		a.syncTrackers()
	}
}

// syncTrackers fetches the tracker lists of the sources, in the
// background, and gives the trackers to the engine.
func (a *app) syncTrackers() {
	if a.syncing {
		return
	}
	a.syncing = true
	sources, limit := a.set.Tracker.Sources, 200
	client := a.set.Proxy.trackerClient()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		list := fetchTrackers(ctx, client, sources, limit)
		a.update(func() {
			a.syncing = false
			if len(list.Trackers) == 0 && len(a.trackers.Trackers) > 0 {
				// Keep the last list when no source answered.
				list.Trackers = a.trackers.Trackers
			}
			a.trackers = list
			list.save(a.paths.trackers)
			a.try(a.eng.Configure(a.engineConfig()))
		})
	}()
}

// onEvent notes a download that finished or failed, from the engine's
// goroutine.
func (a *app) onEvent(ev engine.Event) {
	failed := ev.Task.Status == engine.Failed
	a.update(func() {
		inApp := a.set.App.NotifyInAppOnComplete
		if failed {
			inApp = a.set.App.NotifyInAppOnError
		}
		if !inApp {
			return
		}
		a.notes = append([]note{{task: ev.Task, at: ev.At}}, a.notes...)
		if len(a.notes) > 200 {
			a.notes = a.notes[:200]
		}
		if a.router.Path() != "/notifications" {
			a.unread++
		}
	})
	system := a.set.App.NotifyOnComplete
	if failed {
		system = a.set.App.NotifyOnError
	}
	if !system || a.win == nil || !mygo.NotificationsSupported() {
		return
	}
	title, body := T("Download completed"), ev.Task.Name
	if failed {
		title, body = T("Download failed"), ev.Task.Name+"\n"+ev.Task.Err
	}
	mygo.NewNotification(mygo.NotificationOptions{ID: ev.Task.ID, Title: title, Body: body}).Show()
}

func (a *app) view(c *ui.Context) {
	p := newPalette(c.Theme().Dark, findSidebarColor(a.set.App.SidebarColor))
	c.SetTheme(p.theme(c.Theme()))
	c.Root().Background(p.windowBottom).TextColor(p.text)
	if a.flash != "" {
		c.Toast(a.flash)
		a.flash = ""
	}

	ui.Row(c).Grow(1).AlignItems(ui.Stretch).Gradient(p.windowTop, p.windowBottom, 180).Children(func() {
		a.sidebar(c, p)
		a.content(c, p)
	})
	a.chrome(c, p)
	a.addTaskDialog(c, p)
	a.deleteDialog(c)

	if c.Shortcut(ui.Cmd, ui.KeyN) {
		a.openAddTask(c)
	}
}

// chromeLeft is where the buttons beside the window controls start.
func chromeLeft(c *ui.Context) float32 { return max(c.TitleBar().Left, 8) + 10 }

const titleBarHeight = 52

// trailingInset is the room a page's header leaves at its right: its
// padding, or on Windows and Linux the window controls, which sit over the
// top right corner of the window.
func trailingInset(c *ui.Context) float32 {
	if right := c.TitleBar().Right; right > 0 {
		return right + 12
	}
	return 24
}

// chrome builds the buttons that sit beside the window controls: the one
// showing and hiding the sidebar, and the one adding a download.
func (a *app) chrome(c *ui.Context, p palette) {
	ui.Row(c).Absolute().Top(0).Left(chromeLeft(c)).Height(titleBarHeight).Gap(4).AlignItems(ui.Center).Children(func() {
		if iconButton(c, p, "panel-left", T("Toggle sidebar")).Clicked() {
			a.set.UI.SidebarHidden = !a.set.UI.SidebarHidden
			a.set.save(a.paths.settings)
		}
		if iconButton(c, p, "plus", T("New task")).Clicked() {
			a.openAddTask(c)
		}
	})
}

const sidebarWidth = 200

// sidebarShown is the width the sidebar takes, none while it is hidden.
func (a *app) sidebarShown() float32 {
	if a.set.UI.SidebarHidden {
		return 0
	}
	return sidebarWidth
}

func (a *app) sidebar(c *ui.Context, p palette) {
	side := ui.Column(c).Clip()
	side.Width(side.Animate("width", a.sidebarShown(), 180*time.Millisecond))
	side.Children(func() {
		// As wide as the sidebar when shown, so that it slides out whole.
		ui.Column(c).Width(sidebarWidth).Grow(1).Padding(titleBarHeight+4, 10, 10, 10).Gap(4).DragWindow().Children(func() {
			a.navItem(c, p, "/dashboard", "layout-dashboard", T("Dashboard"), 0)
			a.navItem(c, p, "/downloads", "download", T("Downloads"), 0)
			a.navItem(c, p, "/trackers", "radio-tower", T("Trackers"), 0)
			a.navItem(c, p, "/plugins", "puzzle", T("Plugins"), 0)
			ui.Spacer(c)
			a.navItem(c, p, "/notifications", "bell", T("Notifications"), a.unread)
			ui.Divider(c).Background(p.border).Margin(4, 0)
			a.navItem(c, p, "/settings", "settings", T("Settings"), 0)
		})
	})
}

func (a *app) navItem(c *ui.Context, p palette, path, iconName, label string, badge int) {
	at := a.router.Path()
	active := at == path || strings.HasPrefix(at, path+"/")
	b := ui.ButtonBase(c.Key(path)).Height(38).Padding(0, 10).Gap(10).Radius(8).AlignItems(ui.Center).TextColor(p.text)
	switch {
	case active:
		b.Background(p.active)
	case b.Hovered():
		b.Background(p.hover)
	}
	b.Children(func() {
		ui.Icon(c, icon(iconName)).FontSize(18)
		ui.Text(c, label).FontSize(14.5).FontWeight(500).SingleLine().Grow(1)
		if badge > 0 {
			ui.Textf(c, "%d", badge).FontSize(11).FontWeight(600).Padding(1, 6).Radius(9).
				Background(p.accent).TextColor(ui.RGB(255, 255, 255))
		}
	})
	if b.Clicked() && at != path {
		a.router.Push(path)
	}
}

// content builds the page the router shows, on its panel.
func (a *app) content(c *ui.Context, p palette) {
	panel := ui.Column(c).Grow(1).MinWidth(0)
	if a.router.Path() != "/dashboard" {
		// The dashboard's tiles lie on the window itself.
		left := float32(0)
		if a.set.UI.SidebarHidden {
			left = 8
		}
		panel.Margin(8, 8, 8, left).Radius(14).Background(p.card).Clip().
			Shadow(0, 1, 3, 0, p.shadow)
	}
	panel.Children(func() {
		a.router.View(c, func(r *ui.Route) {
			r.Page().Grow(1)
			switch {
			case r.Match("/dashboard"):
				r.Title(T("Dashboard"))
				a.dashboardPage(c, p)
			case r.Match("/downloads"):
				r.Title(T("Downloads"))
				a.downloadsPage(c, p)
			case r.Match("/trackers"):
				r.Title(T("Trackers"))
				a.trackersPage(c, p)
			case r.Match("/plugins"):
				r.Title(T("Plugins"))
				a.pageHeader(c, T("Plugins"), nil)
				emptyState(c, p, "puzzle", T("Plugins are not available"),
					T("The plugin system of Motrix has not been ported to this build."))
			case r.Match("/notifications"):
				r.Title(T("Notifications"))
				a.notificationsPage(c, p)
			case r.Match("/settings"):
				r.Title(T("Settings"))
				a.settingsPage(c, p)
			case r.Match("/settings/{section}"):
				a.settingsSection(c, p, r.Param("section"))
			default:
				a.router.Replace("/downloads")
			}
		}).Grow(1)
	})
}

// pageHeader builds the band at the top of a page: its title, then what
// trailing builds. It drags the window, as a title bar, and keeps clear of
// the window controls while the sidebar is hidden.
func (a *app) pageHeader(c *ui.Context, title string, trailing func()) ui.Element {
	row := a.headerRow(c, 76, 24)
	return row.Gap(10).AlignItems(ui.Center).DragWindow().Children(func() {
		if title != "" {
			ui.Text(c, title).FontSize(24).Bold().LetterSpacing(-0.4).SingleLine()
		}
		if trailing != nil {
			trailing()
		}
	})
}

// headerRow is the row of a page's header, height tall and padded left
// beside the sidebar. While the sidebar is hidden it moves below the title
// bar, under the window controls and the buttons beside them, and starts at
// the window's edge.
func (a *app) headerRow(c *ui.Context, height, left float32) ui.Element {
	row := ui.Row(c)
	top, right := float32(8), trailingInset(c)
	if a.set.UI.SidebarHidden {
		top, right, left, height = titleBarHeight-8, 24, max(left, 22), height+20
	}
	const d = 180 * time.Millisecond
	return row.Height(row.Animate("height", height, d)).
		Padding(row.Animate("top", top, d), right, 0, row.Animate("left", left, d))
}
