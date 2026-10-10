package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"motrix-mygo/engine"
)

// addTaskState is the dialog that adds downloads.
type addTaskState struct {
	open  bool
	links string
	dir   string
	name  string
	err   string
	// torrents are the .torrent files chosen, to add with the links.
	torrents []string
}

// openAddTask opens the dialog, with the links the clipboard holds, if any.
func (a *app) openAddTask(c *ui.Context) {
	a.add = addTaskState{open: true, dir: a.set.App.DefaultSaveDir}
	if !a.set.App.AutofillClipboardLinks {
		return
	}
	if links := parseLinks(c.ReadClipboard()); len(links) > 0 && len(links) <= 20 {
		a.add.links = strings.Join(links, "\n")
	}
}

// parseLinks returns the links aria2 downloads of a text, one a line.
func parseLinks(text string) []string {
	var links []string
	for line := range strings.Lines(text) {
		if line = strings.TrimSpace(line); engine.ValidLink(line) && !strings.ContainsAny(line, " \t") {
			links = append(links, line)
		}
	}
	return links
}

// submitAddTask queues the links of the dialog. It stays open, saying why,
// when a line is no link.
func (a *app) submitAddTask() {
	var lines []string
	for line := range strings.Lines(a.add.links) {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 && len(a.add.torrents) == 0 {
		a.add.err = T("Enter a link or choose a torrent file")
		return
	}
	name := ""
	if len(lines) == 1 {
		name = strings.TrimSpace(a.add.name)
	}
	var failed []string
	for _, line := range lines {
		if _, err := a.eng.Add(line, strings.TrimSpace(a.add.dir), name); err != nil {
			failed = append(failed, line)
			a.add.err = err.Error()
		}
	}
	var failedTorrents []string
	for _, file := range a.add.torrents {
		data, err := os.ReadFile(file)
		if err == nil {
			_, err = a.eng.AddTorrent(data, strings.TrimSpace(a.add.dir))
		}
		if err != nil {
			failedTorrents = append(failedTorrents, file)
			a.add.err = filepath.Base(file) + ": " + err.Error()
		}
	}
	a.refresh()
	if len(failed) > 0 || len(failedTorrents) > 0 {
		// Keep what was not added, to correct.
		a.add.links, a.add.torrents = strings.Join(failed, "\n"), failedTorrents
		return
	}
	a.add.open = false
	if a.router.Path() != "/downloads" {
		a.router.Push("/downloads")
	}
}

// chooseFolder asks for a folder in the system's dialog, and gives done the
// one chosen, on the main thread.
func (a *app) chooseFolder(start string, done func(dir string)) {
	if a.win == nil {
		return
	}
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent: a.win, Title: T("Choose a folder"), DefaultPath: start,
			Directory: true, CreateDirectories: true,
		})
		if err != nil || len(paths) == 0 {
			return
		}
		a.update(func() { done(paths[0]) })
	}()
}

// chooseTorrents asks for .torrent files in the system's dialog.
func (a *app) chooseTorrents() {
	if a.win == nil {
		return
	}
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent: a.win, Multiple: true,
			Filters: []mygo.FileFilter{{Name: "BitTorrent", Extensions: []string{"torrent"}}},
		})
		if err != nil || len(paths) == 0 {
			return
		}
		a.update(func() {
			for _, file := range paths {
				if !slices.Contains(a.add.torrents, file) {
					a.add.torrents = append(a.add.torrents, file)
				}
			}
			a.add.err = ""
		})
	}()
}

func (a *app) addTaskDialog(c *ui.Context, p palette) {
	add := &a.add
	ui.DialogBase(c, &add.open, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, 0.32))
		solid := ui.RGB(255, 255, 255)
		if p.dark {
			solid = ui.RGB(28, 28, 38)
		}
		panel.Width(520).Padding(24).Gap(14).Radius(16).Background(solid).Border(1, p.border).
			Shadow(0, 24, 60, -12, ui.RGBA(0, 0, 0, 0.35))
		ui.Text(c, T("New Task")).FontSize(18).Bold()

		label := func(s string) { ui.Text(c, s).FontSize(12.5).FontWeight(500).TextColor(p.muted) }
		ui.Column(c).Gap(6).Children(func() {
			label(T("Links"))
			if ui.TextArea(c.Key("links"), &add.links).Lines(4, 8).AutoFocus().Label(T("Links")).
				Placeholder(T("One link per line: HTTP, HTTPS, FTP or magnet")).Changed() {
				add.err = ""
			}
			ui.Row(c).Gap(8).AlignItems(ui.Center).Wrap().Children(func() {
				if ui.Button(c, T("Add torrent file…")).Clicked() {
					a.chooseTorrents()
				}
				for i, file := range add.torrents {
					chip := ui.Row(c.Key(file)).Gap(4).Padding(3, 4, 3, 10).Radius(12).AlignItems(ui.Center).Background(p.hover)
					chip.Children(func() {
						ui.Text(c, filepath.Base(file)).FontSize(12.5).SingleLine().MaxWidth(220)
						x := ui.ButtonBase(c).Size(18, 18).Radius(9).Center().Label(T("Remove"))
						x.Children(func() { ui.Icon(c, icon("x")).FontSize(12) })
						if x.Clicked() {
							removed := i
							add.torrents = append(add.torrents[:removed:removed], add.torrents[removed+1:]...)
						}
					})
				}
			})
		})
		ui.Column(c).Gap(6).Children(func() {
			label(T("Save to"))
			ui.Row(c).Gap(8).Children(func() {
				ui.TextInput(c.Key("dir"), &add.dir).Label(T("Save to")).Grow(1)
				if ui.Button(c, T("Choose…")).Clicked() {
					a.chooseFolder(add.dir, func(dir string) { a.add.dir = dir })
				}
			})
		})
		ui.Column(c).Gap(6).Children(func() {
			label(T("Rename"))
			ui.TextInput(c.Key("name"), &add.name).Label(T("Rename")).Placeholder(T("Optional")).
				Disabled(len(parseLinks(add.links)) != 1)
		})
		if add.err != "" {
			ui.Text(c, add.err).FontSize(12.5).TextColor(c.Theme().Danger)
		}
		ui.Row(c).Gap(8).Justify(ui.End).Margin(4, 0, 0, 0).Children(func() {
			if ui.Button(c, T("Cancel")).Clicked() {
				add.open = false
			}
			if ui.PrimaryButton(c, T("Download")).Clicked() || c.Shortcut(ui.Cmd, ui.KeyEnter) {
				a.submitAddTask()
			}
		})
	})
}
