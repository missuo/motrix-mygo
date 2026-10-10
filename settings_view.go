package main

import (
	"encoding/json"
	"fmt"
	"runtime"
	"slices"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"motrix-mygo/engine"
)

type settingsSection struct {
	id, art, title, detail string
	// draft tells whether the section's page edits a draft, which Save
	// applies: what it holds restarts aria2, or is typed.
	draft bool
}

var settingsSections = []settingsSection{
	{"general", "general", "General", "Startup, default save folder, system integration", false},
	{"appearance", "appearance", "Appearance", "Theme, language, sidebar color", false},
	{"downloads", "download", "Downloads", "Concurrency, bandwidth limits, network reliability", true},
	{"bittorrent", "bittorrent", "BitTorrent", "DHT, listen ports, trackers", true},
	{"network", "network", "Network", "Proxy and DNS", true},
	{"advanced", "advanced", "Advanced", "RPC, SQLite persistence", true},
	{"about", "about", "About", "Version, engine and source code", false},
}

func (a *app) settingsPage(c *ui.Context, p palette) {
	a.draftFor = ""
	a.pageHeader(c, T("Settings"), nil)
	ui.Divider(c).Background(p.border)
	ui.Scroll(c).Grow(1).Padding(8, 24, 24, 24).Children(func() {
		const columns = 3
		lastRow := (len(settingsSections) - 1) / columns
		ui.Grid(c).Columns(columns).Children(func() {
			for i, s := range settingsSections {
				b := ui.ButtonBase(c.Key(s.id)).Column().AlignItems(ui.Start).Justify(ui.Start).Padding(22, 22, 24, 22).Gap(6)
				if i/columns < lastRow {
					b.BorderWidth(0, 0, 1, 0).BorderColor(p.border)
				}
				if b.Hovered() {
					b.Background(p.hover)
				}
				b.Children(func() {
					ui.Image(c, art(s.art)).Size(56, 56).Margin(0, 0, 10, 0)
					ui.Text(c, T(s.title)).FontSize(15.5).FontWeight(600)
					ui.Text(c, T(s.detail)).FontSize(13.5).TextColor(p.muted).LineHeight(1.4)
				})
				if b.Clicked() {
					a.router.Push("/settings/" + s.id)
				}
			}
		})
	})
}

// cloneSettings copies settings with their slices.
func cloneSettings(s settings) settings {
	var out settings
	data, _ := json.Marshal(s)
	json.Unmarshal(data, &out)
	return out
}

func sameSettings(x, y settings) bool {
	a, _ := json.Marshal(x)
	b, _ := json.Marshal(y)
	return string(a) == string(b)
}

func (a *app) settingsSection(c *ui.Context, p palette, id string) {
	i := slices.IndexFunc(settingsSections, func(s settingsSection) bool { return s.id == id })
	if i < 0 {
		a.router.Replace("/settings")
		return
	}
	section := settingsSections[i]
	if a.draftFor != id {
		a.draftFor = id
		a.draft = cloneSettings(a.set)
		a.bypass = strings.Join(a.set.Proxy.Bypass, "\n")
	}
	a.pageHeader(c, "", func() {
		back := ui.ButtonBase(c.Key("back")).Size(32, 32).Radius(16).Center().Margin(0, 0, 0, -8).Label(T("Settings")).Tooltip(T("Settings"))
		if back.Hovered() {
			back.Background(p.hover)
		}
		back.Children(func() { ui.Icon(c, icon("chevron-left")).FontSize(20) })
		if back.Clicked() {
			a.router.Push("/settings")
		}
		ui.Text(c, T(section.title)).FontSize(24).Bold().LetterSpacing(-0.4).SingleLine()
	})
	ui.Divider(c).Background(p.border)
	ui.Scroll(c).Grow(1).Padding(0, 24, 24, 24).Children(func() {
		ui.Column(c).MaxWidth(760).Children(func() {
			switch id {
			case "general":
				a.generalSettings(c, p)
			case "appearance":
				a.appearanceSettings(c, p)
			case "downloads":
				a.downloadSettings(c, p, &a.draft)
			case "bittorrent":
				a.bitTorrentSettings(c, p, &a.draft)
			case "network":
				a.networkSettings(c, p, &a.draft)
			case "advanced":
				a.advancedSettings(c, p, &a.draft)
			case "about":
				a.aboutSettings(c, p)
			}
		})
	})
	if !section.draft {
		return
	}
	a.draft.Proxy.Bypass = nil
	if fields := strings.Fields(strings.ReplaceAll(a.bypass, ",", " ")); len(fields) > 0 {
		a.draft.Proxy.Bypass = fields
	}
	a.draft.UI = a.set.UI
	if sameSettings(a.draft, a.set) {
		return
	}
	ui.Row(c).Padding(12, 24).Gap(8).Justify(ui.End).AlignItems(ui.Center).BorderWidth(1, 0, 0, 0).BorderColor(p.border).Children(func() {
		ui.Text(c, T("You have unsaved changes.")).FontSize(13).TextColor(p.muted).Grow(1)
		if ui.Button(c, T("Discard")).Clicked() {
			a.draftFor = ""
		}
		if ui.PrimaryButton(c, T("Save")).Clicked() || c.Shortcut(ui.Cmd, ui.KeyS) {
			a.set = cloneSettings(a.draft)
			a.saveSettings()
			a.draftFor = ""
			c.Toast(T("Settings saved"))
		}
	})
}

// sectionTitle heads a group of settings.
func sectionTitle(c *ui.Context, p palette, title string) {
	ui.Text(c, title).FontSize(12).FontWeight(600).LetterSpacing(0.4).TextColor(p.muted).Padding(22, 0, 4, 0).SingleLine()
}

// settingRow is a setting: its name over what it does, and its control.
func settingRow(c *ui.Context, p palette, title, detail string, control func()) {
	ui.Row(c.Key(title)).Padding(14, 0).Gap(24).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(p.border).Children(func() {
		ui.Column(c).Grow(1).Basis(0).Gap(3).Children(func() {
			ui.Text(c, title).FontSize(14).FontWeight(500)
			if detail != "" {
				ui.Text(c, detail).FontSize(12.5).TextColor(p.muted).LineHeight(1.4)
			}
		})
		control()
	})
}

// switchRow is a setting turned on and off. It reports a change.
func switchRow(c *ui.Context, p palette, title, detail string, on *bool) (changed bool) {
	settingRow(c, p, title, detail, func() {
		changed = ui.Switch(c, on).Label(title).Changed()
	})
	return changed
}

// numberRow is a setting that is a whole number of units, as megabytes of
// a value in bytes.
func numberRow[T int | int64](c *ui.Context, p palette, title, detail string, value *T, unit T, lo, hi float64) {
	settingRow(c, p, title, detail, func() {
		v := float64(*value) / float64(unit)
		if ui.NumberInput(c, &v, lo, hi, 1).Label(title).Width(130).Changed() {
			*value = T(v) * unit
		}
	})
}

// selectRow is a setting that is one of ids, shown by their labels. It
// reports a change.
func selectRow(c *ui.Context, p palette, title, detail string, value *string, ids, labels []string) (changed bool) {
	settingRow(c, p, title, detail, func() {
		label := labels[max(slices.Index(ids, *value), 0)]
		if ui.Select(c, &label, labels).Label(title).Width(200).Changed() {
			*value = ids[max(slices.Index(labels, label), 0)]
			changed = true
		}
	})
	return changed
}

func textRow(c *ui.Context, p palette, title, detail string, value *string, placeholder string) {
	settingRow(c, p, title, detail, func() {
		ui.TextInput(c, value).Label(title).Placeholder(placeholder).Width(280)
	})
}

func (a *app) generalSettings(c *ui.Context, p palette) {
	app := &a.set.App
	sectionTitle(c, p, T("Startup and quitting"))
	if switchRow(c, p, T("Open at login"), T("Start Motrix automatically when you log in."), &app.LaunchAtStartup) {
		a.saveSettings()
	}
	if switchRow(c, p, T("Confirm before quitting"), T("Ask before quitting if downloads are still running."), &app.WarnBeforeQuit) {
		a.saveSettings()
	}
	sectionTitle(c, p, T("System notifications"))
	if switchRow(c, p, T("Notify when download completes"), T("Show a system notification when a task finishes."), &app.NotifyOnComplete) {
		a.saveSettings()
	}
	if switchRow(c, p, T("Notify on failure"), T("Show a system notification when a download fails."), &app.NotifyOnError) {
		a.saveSettings()
	}
	sectionTitle(c, p, T("In-app notifications"))
	if switchRow(c, p, T("Show completed downloads"), "", &app.NotifyInAppOnComplete) {
		a.saveSettings()
	}
	if switchRow(c, p, T("Show failed downloads"), "", &app.NotifyInAppOnError) {
		a.saveSettings()
	}
}

func (a *app) appearanceSettings(c *ui.Context, p palette) {
	app := &a.set.App
	settingRow(c, p, T("Theme"), "", func() {
		themes := []string{"system", "light", "dark"}
		i := max(slices.Index(themes, app.Theme), 0)
		if ui.Segmented(c, &i, T("System"), T("Light"), T("Dark")).Label(T("Theme")).Changed() {
			app.Theme = themes[i]
			a.saveSettings()
		}
	})
	settingRow(c, p, T("Sidebar color"), T("Tints the sidebar and the window."), func() {
		ui.Row(c).Gap(10).Children(func() {
			for _, sc := range sidebarColors {
				b := ui.ButtonBase(c.Key(sc.name)).Size(24, 24).Radius(12).Label(sc.name).Background(sc.swatch())
				if app.SidebarColor == sc.name {
					b.Border(2, p.text)
				}
				if b.Clicked() {
					app.SidebarColor = sc.name
					a.saveSettings()
				}
			}
		})
	})
	if selectRow(c, p, T("Language"), "", &app.Language, []string{"system", "en", "zh"}, []string{T("System"), "English", "简体中文"}) {
		a.saveSettings()
	}
}

func (a *app) downloadSettings(c *ui.Context, p palette, d *settings) {
	e := &d.Engine
	sectionTitle(c, p, T("New tasks"))
	settingRow(c, p, T("Default download folder"), T("New downloads land here unless overridden per-task."), func() {
		ui.Text(c, d.App.DefaultSaveDir).FontSize(12.5).TextColor(p.muted).SingleLine().MaxWidth(280).Selectable()
		if ui.Button(c, T("Choose…")).Clicked() {
			a.chooseFolder(d.App.DefaultSaveDir, func(dir string) { a.draft.App.DefaultSaveDir = dir })
		}
	})
	switchRow(c, p, T("Autofill links from clipboard"), T("Paste copied links when opening a new task."), &d.App.AutofillClipboardLinks)

	sectionTitle(c, p, T("Performance"))
	profiles := []string{"auto", "balanced", "high", "maximum", "custom"}
	labels := []string{T("Automatic"), T("Balanced"), T("High speed"), T("Maximum"), T("Custom")}
	details := map[string]string{
		"auto":     "Adjusts to your drive and file size. Values below are defaults.",
		"balanced": "Uses fewer resources for everyday downloads.",
		"high":     "Uses more connections for faster downloads.",
		"maximum":  "Uses more connections. Some servers may limit them.",
		"custom":   "Set connections, segment size, and cache.",
	}
	if selectRow(c, p, T("Download performance"), T(details[e.PerformanceProfile]), &e.PerformanceProfile, profiles, labels) {
		e.Normalize()
	}
	if e.PerformanceProfile == "custom" {
		numberRow(c, p, T("Max connections per server"), T("Per-file connection limit for one server."), &e.MaxConnectionPerServer, 1, 1, engine.MaxConnectionsPerServer)
		numberRow(c, p, T("Max connections per file"), T("Connections used to download one file."), &e.Split, 1, 1, 128)
		numberRow(c, p, T("Min segment size (MB)"), T("Segments below 2× this size won’t split."), &e.MinSplitSize, 1<<20, 1, 1024)
		numberRow(c, p, T("Disk cache (MB)"), T("Memory used to buffer files before writing to disk."), &e.DiskCache, 1<<20, 0, 128)
	} else {
		ui.Grid(c).Columns(4).Gap(12).Padding(14, 0).BorderWidth(0, 0, 1, 0).BorderColor(p.border).Children(func() {
			metric := func(label, value string) {
				ui.Column(c).Gap(3).Padding(10, 12).Radius(10).Background(p.hover).Children(func() {
					ui.Text(c, label).FontSize(11.5).TextColor(p.muted).SingleLine()
					ui.Text(c, value).FontSize(15).FontWeight(600).FontFeatures("tnum").SingleLine()
				})
			}
			metric(T("Max connections"), fmt.Sprint(e.Split))
			metric(T("Per-server cap"), fmt.Sprint(e.MaxConnectionPerServer))
			metric(T("Min segment"), formatBytes(e.MinSplitSize))
			metric(T("Disk cache"), formatBytes(e.DiskCache))
		})
	}
	numberRow(c, p, T("Simultaneous downloads"), T("Additional tasks wait in the queue."), &e.MaxConcurrentDownloads, 1, 1, 100)

	a.speedLimitSettings(c, p, &d.SpeedLimit)

	sectionTitle(c, p, T("Connections and retries"))
	textRow(c, p, "User-Agent", T("Identifies the app to websites. Change it if a site requires a different one."), &e.UserAgent, "Motrix/2.0")
	numberRow(c, p, T("Connection timeout (s)"), T("How long to wait for a connection."), &e.ConnectTimeout, 1, 1, 600)
	numberRow(c, p, T("No-data timeout (s)"), T("How long to wait when no data arrives."), &e.SocketTimeout, 1, 1, 600)
	numberRow(c, p, T("Maximum attempts"), T("Includes the first attempt. 0 = unlimited."), &e.MaxTries, 1, 0, 100)
	numberRow(c, p, T("Retry delay (s)"), T("Wait this long before trying again."), &e.RetryWait, 1, 0, 300)
	numberRow(c, p, T("Minimum connection speed (KB/s)"), T("Disconnect below this speed. 0 = disabled."), &e.LowestSpeedLimit, 1<<10, 0, 1<<20)

	sectionTitle(c, p, T("Disk and files"))
	trashDetail := T("Space is freed when you empty the trash.")
	if d.App.FileDeletionMode == "permanent" {
		trashDetail = T("Deleted files cannot be restored from the trash.")
	}
	selectRow(c, p, T("When deleting task files"), trashDetail, &d.App.FileDeletionMode,
		[]string{"trash", "permanent"}, []string{T("Move to trash"), T("Delete permanently")})
	selectRow(c, p, T("Reserve disk space"), T("Reserve space before downloading a file."), &e.FileAllocation,
		[]string{"none", "prealloc", "trunc", "falloc"},
		[]string{T("Do not reserve"), T("Preallocate"), T("Set file size"), T("System allocation")})
	remote := "local"
	if e.RemoteTime {
		remote = "server"
	}
	if selectRow(c, p, T("File modification time"), T("For completed HTTP and FTP downloads."), &remote,
		[]string{"local", "server"}, []string{T("Download time"), T("Time from server")}) {
		e.RemoteTime = remote == "server"
	}
	numberRow(c, p, T("Progress save interval (s)"), T("Save more often to reduce progress lost after a crash."), &e.SessionSaveInterval, 1, 10, 3600)

	sectionTitle(c, p, T("Magnet"))
	numberRow(c, p, T("Magnet loading timeout (s)"), T("How long to wait for the file list."), &e.MagnetResolveTimeout, 1, 30, 900)
}

func (a *app) speedLimitSettings(c *ui.Context, p palette, l *speedLimitSettings) {
	sectionTitle(c, p, T("Speed limits"))
	ids := make([]string, len(turtleModes))
	labels := make([]string, len(turtleModes))
	for i, tm := range turtleModes {
		ids[i], labels[i] = tm.id, T(tm.label)
	}
	detail := map[string]string{
		"off":  "Use the standard speed limits.",
		"on":   "Use the low-speed limits now to leave room for other apps.",
		"auto": "Adjust speed automatically based on time and connection bandwidth.",
	}[l.Turtle]
	selectRow(c, p, T("Speed mode"), T(detail), &l.Turtle, ids, labels)

	unlimited := T("0 = unlimited.")
	numberRow(c, p, T("Standard download limit (KB/s)"), T("The maximum everyday download speed.")+" "+unlimited, &l.Base.Download, 1<<10, 0, 1<<30)
	numberRow(c, p, T("Standard upload limit (KB/s)"), T("The maximum everyday upload speed.")+" "+unlimited, &l.Base.Upload, 1<<10, 0, 1<<30)
	numberRow(c, p, T("Low speed download limit (KB/s)"), T("The maximum download speed while Low speed mode is active."), &l.Alt.Download, 1<<10, 0, 1<<30)
	numberRow(c, p, T("Low speed upload limit (KB/s)"), T("The maximum upload speed while Low speed mode is active."), &l.Alt.Upload, 1<<10, 0, 1<<30)

	if l.Turtle != "auto" {
		return
	}
	sch := &l.Auto.Schedule
	switchRow(c, p, T("Schedule low speed"), T("Switch to the low-speed limits on selected days and times."), &sch.Enabled)
	if sch.Enabled {
		settingRow(c, p, T("Start")+" – "+T("End"), T("24-hour format (HH:mm)")+". "+T("An end time before the start continues into the next day."), func() {
			ui.TextInput(c.Key("from"), &sch.From).Label(T("Start")).Width(76)
			ui.Text(c, "–").TextColor(p.muted)
			ui.TextInput(c.Key("to"), &sch.To).Label(T("End")).Width(76)
		})
		settingRow(c, p, T("Days"), T("None selected means every day."), func() {
			ui.Row(c).Gap(4).Children(func() {
				for day, name := range []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"} {
					on := slices.Contains(sch.Days, day)
					b := ui.ButtonBase(c.Key(day)).Height(28).MinWidth(40).Padding(0, 8).Radius(8).Center().FontSize(12.5).Label(T(name))
					if on {
						b.Background(p.accent).TextColor(ui.RGB(255, 255, 255))
					} else {
						b.Background(p.hover)
					}
					b.Children(func() { ui.Text(c, T(name)).SingleLine() })
					if b.Clicked() {
						if on {
							sch.Days = slices.DeleteFunc(sch.Days, func(d int) bool { return d == day })
						} else {
							sch.Days = append(sch.Days, day)
							slices.Sort(sch.Days)
						}
					}
				}
			})
		})
	}
	ad := &l.Auto.Adaptive
	switchRow(c, p, T("Reserve bandwidth for other apps"), T("Enter your connection speeds below to calculate the limit."), &ad.Enabled)
	if ad.Enabled {
		numberRow(c, p, T("Connection download speed (KB/s)"), T("The download speed your internet connection can reach."), &ad.LinkDown, 1<<10, 0, 1<<30)
		numberRow(c, p, T("Connection upload speed (KB/s)"), T("The upload speed your internet connection can reach."), &ad.LinkUp, 1<<10, 0, 1<<30)
		numberRow(c, p, T("Share Motrix may use (%)"), fmt.Sprintf(T("Reserving %d%% for other apps."), 100-ad.HeadroomPercent), &ad.HeadroomPercent, 1, 1, 100)
	}
}

func (a *app) bitTorrentSettings(c *ui.Context, p palette, d *settings) {
	e := &d.Engine
	sectionTitle(c, p, T("Incoming connections"))
	numberRow(c, p, T("BT listen port"), T("Accept incoming peer connections on this port."), &e.ListenPort, 1, 1024, 65535)
	numberRow(c, p, T("DHT listen port"), T("Find peers through DHT on this port."), &e.DHTListenPort, 1, 1024, 65535)
	switchRow(c, p, T("Enable DHT"), T("Find peers without relying on a tracker."), &e.DHTEnabled)

	sectionTitle(c, p, T("Peers"))
	numberRow(c, p, T("Max peers per torrent"), T("The most peers one task can connect to at once."), &e.BTMaxPeers, 1, 1, 1000)
	switchRow(c, p, T("Local peer discovery"), T("Find peers sharing files on your local network."), &e.BTEnableLPD)

	sectionTitle(c, p, T("Seeding"))
	settingRow(c, p, T("Share ratio limit"), T("Upload ÷ download. Seeding stops at this ratio. 0 = unlimited."), func() {
		ui.NumberInput(c, &e.SeedRatio, 0, 100, 0.1).Label(T("Share ratio limit")).Width(130)
	})
	numberRow(c, p, T("Seed time limit (minutes)"), T("0 = unlimited. Applies to new tasks and when seeding is restarted."), &e.SeedTime, 1, 0, 525600)

	sectionTitle(c, p, T("Trackers"))
	switchRow(c, p, T("Update tracker list automatically"), T("Keep the list up to date using your sources."), &d.Tracker.AutoSync)
	numberRow(c, p, T("Update interval (hours)"), T("From 1 to 168 hours."), &d.Tracker.SyncIntervalHours, 1, 1, 168)
	numberRow(c, p, T("Trackers per task"), T("Use up to this many trackers. 5–200."), &d.Tracker.MaxTrackerCount, 1, 5, 200)
	ui.Text(c, T("Manage sources in the Trackers page.")).FontSize(12.5).TextColor(p.muted).Padding(12, 0)
}

func (a *app) networkSettings(c *ui.Context, p palette, d *settings) {
	sectionTitle(c, p, "DNS")
	selectRow(c, p, T("DNS lookup"), T("How downloads look up server addresses."), &d.Engine.DNSMode,
		[]string{"auto", "system", "engine"}, []string{T("Auto"), T("System DNS"), T("Built-in DNS")})

	px := &d.Proxy
	sectionTitle(c, p, T("Proxy"))
	switchRow(c, p, T("Enable proxy"), T("Use a proxy for the connections selected below."), &px.Enabled)
	if !px.Enabled {
		return
	}
	selectRow(c, p, T("Protocol"), "", &px.Protocol, []string{"http", "https"}, []string{"HTTP", "HTTPS"})
	textRow(c, p, T("Server address"), "", &px.Host, "proxy.example.com")
	numberRow(c, p, T("Port"), "", &px.Port, 1, 1, 65535)
	textRow(c, p, T("Username"), T("Fill in if your proxy requires a username and password."), &px.User, "")
	settingRow(c, p, T("Password"), "", func() {
		ui.TextInput(c, &px.Password).Password().Label(T("Password")).Width(280)
	})
	settingRow(c, p, T("Connect directly to"), T("One domain, IP or CIDR per line. Start with . to match subdomains only."), func() {
		ui.TextArea(c, &a.bypass).Label(T("Connect directly to")).Placeholder("localhost\n127.0.0.1\n.local").Lines(3, 6).Width(280)
	})
	sectionTitle(c, p, T("Use proxy for"))
	switchRow(c, p, T("File downloads"), T("For BT, only HTTP(S) uses the proxy. Peers, DHT and UDP trackers connect directly."), &px.Scopes.Download)
	switchRow(c, p, T("Tracker list updates"), T("Download tracker lists through the proxy; BT traffic is unaffected."), &px.Scopes.UpdateTrackers)
}

func (a *app) advancedSettings(c *ui.Context, p palette, d *settings) {
	e := &d.Engine
	sectionTitle(c, p, "RPC")
	numberRow(c, p, T("RPC port"), T("The engine restarts after changing this."), &e.RPCPort, 1, 1024, 65535)
	settingRow(c, p, T("RPC secret"), T("Required for RPC connections."), func() {
		ui.TextInput(c, &e.RPCSecret).Password().Label(T("RPC secret")).Width(280)
	})

	sectionTitle(c, p, T("Progress and recovery"))
	switchRow(c, p, T("Save recovery data"), T("Keep data used to resume downloads after restarting."), &e.SQLite3Persistence)
	textRow(c, p, T("Recovery data file"), T("Leave empty to use the default location."), &e.SQLite3DBPath, T("Default location"))
	history := "recent"
	switch e.SQLite3HistoryLimit {
	case -1:
		history = "all"
	case 0:
		history = "none"
	}
	if selectRow(c, p, T("Completed recovery records"), T("The download list and downloaded files are unaffected."), &history,
		[]string{"all", "recent", "none"}, []string{T("All records"), T("Latest records…"), T("Do not retain")}) {
		e.SQLite3HistoryLimit = map[string]int{"all": -1, "recent": 1000, "none": 0}[history]
	}
	if history == "recent" {
		numberRow(c, p, T("Records to keep"), T("Older recovery records are removed."), &e.SQLite3HistoryLimit, 1, 1, 1_000_000)
	}

	sectionTitle(c, p, T("Engine"))
	settingRow(c, p, "aria2.conf", T("Options here apply before the settings; the app's own always win."), func() {
		if ui.Button(c, T("Show in folder")).Clicked() {
			showPath(a.paths.data + "/aria2.conf")
		}
	})
	settingRow(c, p, T("Restart engine"), T("Running downloads continue afterwards."), func() {
		if ui.Button(c, T("Restart")).Clicked() {
			a.eng.Restart()
		}
	})
}

func (a *app) aboutSettings(c *ui.Context, p palette) {
	value := func(s string) { ui.Text(c, s).FontSize(13).TextColor(p.muted).Selectable() }
	ui.Column(c).Padding(20, 0).Gap(4).BorderWidth(0, 0, 1, 0).BorderColor(p.border).Children(func() {
		ui.Text(c, "Motrix").FontSize(20).Bold()
		ui.Text(c, T("Motrix with a native interface in Go: no Electron, no webview.")).FontSize(13.5).TextColor(p.muted)
	})
	settingRow(c, p, T("Version"), "", func() {
		// The version of mygo.json, which the build stamps into the app.
		version := mygo.App.Version()
		if version == "" {
			version = "dev"
		}
		value(version)
	})
	settingRow(c, p, T("Engine"), "", func() {
		version := a.stats.Version
		if version == "" {
			version = "—"
		}
		value("aria2 " + version)
	})
	settingRow(c, p, T("Built with"), "", func() {
		value(runtime.Version() + " · " + runtime.GOOS + "/" + runtime.GOARCH)
		ui.Link(c, "MyGo", "https://github.com/egoist/mygo").FontSize(13)
	})
	settingRow(c, p, T("Source code"), "", func() {
		ui.Link(c, "github.com/missuo/motrix-mygo", "https://github.com/missuo/motrix-mygo").FontSize(13)
	})
	settingRow(c, p, T("Acknowledgements"), T("The design, the settings and the aria2 build are Motrix's."), func() {
		ui.Link(c, "Motrix", "https://github.com/agalwood/Motrix").FontSize(13)
		ui.Link(c, "aria2", "https://github.com/motrixapp/aria2").FontSize(13)
	})
}
