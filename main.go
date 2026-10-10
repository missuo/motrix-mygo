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
	var a *app
	mygo.App.OnSecondInstance(func([]string, string) {
		if a != nil && a.win != nil {
			a.win.Show()
			a.win.Focus()
		}
	})
	mygo.App.WhenReady(func() {
		path := func(name mygo.PathName) string {
			dir, err := mygo.App.Path(name)
			if err != nil {
				log.Fatal(err)
			}
			return dir
		}
		// A directory of this app's own: Motrix itself keeps its aria2
		// session and database under "Motrix", which must stay its.
		userData := filepath.Join(path(mygo.PathAppData), "Motrix MyGo")
		mygo.App.SetPath(mygo.PathUserData, userData)
		paths := appPaths{
			settings:  filepath.Join(userData, "settings.json"),
			trackers:  filepath.Join(userData, "trackers.json"),
			data:      userData,
			downloads: path(mygo.PathDownloads),
		}
		paths.binary, paths.template = findEngine(path(mygo.PathResources))

		a = newApp(loadSettings(paths.settings, paths.downloads), paths)
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

// beforeQuit asks before quitting while downloads run, when the settings
// say to.
func (a *app) beforeQuit(e *mygo.QuitEvent) {
	stats := a.eng.Stats()
	if a.quitting || !a.set.App.WarnBeforeQuit || stats.Active+stats.Seeding == 0 {
		return
	}
	e.PreventDefault()
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
