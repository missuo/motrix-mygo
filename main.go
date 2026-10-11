// Motrix is a download manager whose interface MyGo draws natively, over
// the aria2 engine Motrix ships: no webview, one Go binary beside aria2c.
package main

import (
	"log"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/plugins/updater/native"
	"github.com/egoist/mygo/ui"
)

// findEngine returns aria2c and the aria2.conf template among the
// resources: where mygo puts them in the app, or, under `go run`, in the
// project's directory of the platform.
func findEngine(resources string) (binary, template string) {
	name := "aria2c"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	for _, candidate := range []string{
		filepath.Join(resources, "engine", name),
		filepath.Join(resources, runtime.GOOS+"-"+runtime.GOARCH, "engine", name),
	} {
		if _, err := os.Stat(candidate); err == nil {
			binary = candidate
			break
		}
	}
	return binary, filepath.Join(resources, "engine", "aria2.conf")
}

// showPath shows a file in the file manager.
func showPath(path string) { mygo.Shell.ShowItemInFolder(path) }

func main() {
	// Two instances would run two engines over the same session.
	if !mygo.App.RequestSingleInstanceLock() {
		return
	}
	path := func(name mygo.PathName) string {
		dir, err := mygo.App.Path(name)
		if err != nil {
			log.Fatal(err)
		}
		return dir
	}
	// A directory of this app's own: Motrix itself keeps its aria2
	// session and database under "Motrix", which must stay its. Set before
	// the updater, which keeps its preferences there, starts.
	userData := filepath.Join(path(mygo.PathAppData), "Motrix MyGo")
	mygo.App.SetPath(mygo.PathUserData, userData)
	settingsPath := filepath.Join(userData, "settings.json")
	set := loadSettings(settingsPath, path(mygo.PathDownloads))

	// The update window, in native UI and in the language of the app. It
	// checks once a day, for the releases of the repository in mygo.json.
	mygo.Use(native.New(updater.Options{Language: updaterLanguage(set.App.Language)}))
	updates = true
	if menu := appMenu(); menu != nil {
		mygo.App.SetMenu(menu)
	}

	var a *app
	mygo.App.OnSecondInstance(func([]string, string) {
		if a != nil && a.win != nil {
			a.showWindow()
		}
	})
	mygo.App.WhenReady(func() {
		paths := appPaths{
			settings:  settingsPath,
			trackers:  filepath.Join(userData, "trackers.json"),
			data:      userData,
			downloads: path(mygo.PathDownloads),
		}
		paths.binary, paths.template = findEngine(path(mygo.PathResources))

		a = newApp(set, paths)
		mygo.App.OnBeforeQuit(a.beforeQuit)
		mygo.App.OnQuit(a.eng.Close)
		a.win = mygo.NewWindow(mygo.WindowOptions{
			Title: "Motrix",
			// Just wide enough for every column of the downloads and the
			// four tiles of the dashboard, whole.
			Width:     1024,
			Height:    660,
			MinWidth:  960,
			MinHeight: 620,
			StateKey:  "main-3",
			// The view draws the title bar, under the window controls.
			// macOS keeps its traffic lights at the left; Windows and
			// Linux draw their controls at the right, as high as the band.
			TitleBarStyle:        mygo.TitleBarHidden,
			TrafficLightPosition: &mygo.Point{X: 20, Y: 19},
			TitleBarHeight:       40,
			// No menu bar across the window on Windows and Linux.
			AutoHideMenuBar: true,
			Content:         ui.View(a.view),
		})
		a.keepInMenuBar()
		a.applySettings()
		go func() {
			for range time.Tick(30 * time.Second) {
				a.update(a.tick)
			}
		}()
		a.update(a.tick)
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// updates says that the updater plugin is in use, as it is but in tests.
var updates bool

// updaterLanguage is the language of the update window for the app's
// setting: the system's unless the user chose one.
func updaterLanguage(language string) string {
	switch language {
	case "en":
		return "en"
	case "zh":
		return "zh-Hans"
	}
	return ""
}

// appMenu is the menu bar of macOS, which hosts the standard shortcuts,
// with "Check for Updates…" under About. Windows and Linux show none: the
// About page of the settings checks there.
func appMenu() *mygo.Menu {
	if runtime.GOOS != "darwin" {
		return nil
	}
	return mygo.NewMenu([]*mygo.MenuItem{
		{Label: "Motrix", Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleAbout},
			{Label: updater.MenuItem().Label, Click: func(*mygo.MenuItem, *mygo.Window) { checkForUpdates() }},
			mygo.Separator(),
			{Role: mygo.RoleServices},
			mygo.Separator(),
			{Role: mygo.RoleHide},
			{Role: mygo.RoleHideOthers},
			{Role: mygo.RoleUnhide},
			mygo.Separator(),
			{Role: mygo.RoleQuit},
		}},
		{Role: mygo.RoleFileMenu},
		{Role: mygo.RoleEditMenu},
		{Role: mygo.RoleViewMenu},
		{Role: mygo.RoleWindowMenu},
	})
}

// keepInMenuBar keeps the app running, with an icon in the menu bar, or
// in the notification area on Windows and Linux, when its window is closed:
// downloads go on, and the icon's menu shows the window again or quits.
func (a *app) keepInMenuBar() {
	a.win.OnClose(func(e *mygo.CloseEvent) {
		// Closed by the quit under way: let it.
		if a.leaving {
			return
		}
		e.PreventDefault()
		a.win.Hide()
	})
	// A click on the Dock icon, as on the icon in the menu bar.
	mygo.App.OnActivate(func(bool) { a.showWindow() })

	png, template := trayIcon, true
	if runtime.GOOS != "darwin" {
		// The notification area shows icons in color.
		if dir, err := mygo.App.Path(mygo.PathResources); err == nil {
			if icon, err := os.ReadFile(filepath.Join(dir, "icon.png")); err == nil {
				png, template = icon, false
			}
		}
	}
	tray, err := mygo.NewTray(mygo.TrayOptions{Icon: png, IconIsTemplate: template, ToolTip: "Motrix", Menu: a.trayMenu()})
	if err != nil {
		// Without the icon nothing could show the window again.
		log.Printf("tray: %v", err)
		a.leaving = true
		return
	}
	a.tray = tray
}

// showWindow brings the window back to the front.
func (a *app) showWindow() {
	a.win.Show()
	a.win.Focus()
}

// trayMenu is the menu of the icon in the menu bar, in the app's language.
func (a *app) trayMenu() *mygo.Menu {
	items := []*mygo.MenuItem{
		{Label: T("Show Motrix"), Click: func(*mygo.MenuItem, *mygo.Window) { a.showWindow() }},
	}
	if updates {
		items = append(items, &mygo.MenuItem{Label: T("Check for Updates…"), Click: func(*mygo.MenuItem, *mygo.Window) { checkForUpdates() }})
	}
	items = append(items, mygo.Separator(),
		&mygo.MenuItem{Label: T("Quit Motrix"), Click: func(*mygo.MenuItem, *mygo.Window) { mygo.App.Quit() }})
	return mygo.NewMenu(items)
}

// beforeQuit asks before quitting while downloads run, when the settings
// say to.
func (a *app) beforeQuit(e *mygo.QuitEvent) {
	stats := a.eng.Stats()
	if a.quitting || !a.set.App.WarnBeforeQuit || stats.Active+stats.Seeding == 0 {
		// The quit goes on, and closes the window for good.
		a.leaving = true
		return
	}
	e.PreventDefault()
	// The question hangs on the window, which may be hidden.
	a.showWindow()
	go func() {
		res, err := mygo.Dialog.Message(mygo.MessageOptions{
			Parent:  a.win,
			Type:    mygo.MessageWarning,
			Message: T("Quit Motrix?"),
			Detail:  T("Downloads are still running. They continue the next time Motrix opens."),
			Buttons: []string{T("Quit"), T("Cancel")},
		})
		if err == nil && res.Button == 0 {
			a.quitting = true
			mygo.App.Quit()
		}
	}()
}
