package main

import (
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"motrix-mygo/engine"
)

// TestLive downloads real files from the internet through aria2, a
// torrent among them, and writes pictures of the pages meanwhile. It runs
// only when asked to:
//
//	MOTRIX_LIVE=/tmp/motrix-live MOTRIX_SHOTS=docs/screenshots go test -run TestLive -v -timeout 20m .
func TestLive(t *testing.T) {
	root := os.Getenv("MOTRIX_LIVE")
	if root == "" {
		t.Skip("MOTRIX_LIVE is not set")
	}
	shots := os.Getenv("MOTRIX_SHOTS")
	resources, _ := filepath.Abs("resources")
	binary, template := findEngine(resources)
	if binary == "" {
		t.Fatal("aria2c is not in resources: run `go run ./scripts/fetch-engine`")
	}
	os.RemoveAll(root)
	downloads := filepath.Join(root, "Downloads")
	set := defaultSettings(downloads)
	set.App.Language, set.App.SidebarColor = "en", "blue"
	set.Engine.RPCPort, set.Engine.ListenPort, set.Engine.DHTListenPort = 27900, 27981, 27981
	if home, err := os.UserHomeDir(); err == nil && shots != "" {
		// The pictures show the folder as a user's would read.
		downloads = filepath.Join(home, "Downloads", "Motrix")
		if _, err := os.Stat(downloads); err == nil {
			t.Fatalf("%s exists: the test would delete it", downloads)
		}
		defer os.RemoveAll(downloads)
		set.App.DefaultSaveDir = downloads
	}
	set.Tracker.AutoSync = false
	a := newApp(set, appPaths{
		data: filepath.Join(root, "data"), binary: binary, template: template, downloads: downloads,
		trackers: filepath.Join(root, "data", "trackers.json"),
	})
	defer a.eng.Close()
	a.router.Transition = ui.TransitionNone
	a.applySettings()
	waitEngine(t, a)
	t.Logf("engine: aria2 %s", a.stats.Version)

	tt := ui.NewTester(a.view, 1200, 780)
	tt.SetScale(2)
	tt.SetTitleBar(ui.TitleBar{Height: 52, Left: 78})
	shot := func(name string) {
		t.Helper()
		a.refresh()
		tt.Frame()
		tt.Frame()
		if shots == "" {
			return
		}
		os.MkdirAll(shots, 0o755)
		f, err := os.Create(filepath.Join(shots, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
	}
	find := func(id string) engine.Snapshot {
		for _, s := range a.eng.Tasks() {
			if s.ID == id {
				return s
			}
		}
		return engine.Snapshot{}
	}
	shot("downloads-empty")

	// The tracker lists, from their real sources.
	a.syncTrackers()
	for deadline := time.Now().Add(90 * time.Second); a.syncing && time.Now().Before(deadline); {
		time.Sleep(100 * time.Millisecond)
	}
	t.Logf("trackers: %d, errors %v", len(a.trackers.Trackers), a.trackers.Errors)

	// A standard limit keeps the downloads going long enough to look at,
	// and to pause one.
	a.set.SpeedLimit.Base.Download = 8 << 20
	a.saveSettings()
	if cfg := a.eng.Config(); cfg.Limits.Download != 8<<20 {
		t.Errorf("the engine's limits: %+v", cfg.Limits)
	}

	files := []string{
		"https://github.com/aria2/aria2/releases/download/release-1.37.0/aria2-1.37.0.tar.xz",
		"https://www.python.org/ftp/python/3.12.0/Python-3.12.0.tar.xz",
		"https://dl.google.com/go/go1.22.0.src.tar.gz",
	}
	var ids []string
	for _, link := range files {
		id, err := a.eng.Add(link, "", "")
		if err != nil {
			t.Fatalf("%s: %v", link, err)
		}
		ids = append(ids, id)
	}
	big, err := a.eng.Add("https://dl.google.com/go/go1.22.0.linux-amd64.tar.gz", "", "")
	if err != nil {
		t.Fatal(err)
	}
	var torrent string
	resp, err := http.Get("https://releases.ubuntu.com/24.04/ubuntu-24.04.3-live-server-amd64.iso.torrent")
	if err == nil && resp.StatusCode == http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if torrent, err = a.eng.AddTorrent(data, ""); err != nil {
			t.Errorf("the torrent: %v", err)
		}
	} else {
		t.Logf("the torrent file could not be fetched: %v", err)
	}
	magnet, err := a.eng.Add("magnet:?xt=urn:btih:08ada5a7a6183aae1e09d831df6748d566095a10&dn=Sintel", "", "")
	if err != nil {
		t.Errorf("the magnet link: %v", err)
	}

	// Pictures while they download.
	time.Sleep(5 * time.Second)
	if speed := a.eng.Stats().Speed; speed > 10<<20 {
		t.Errorf("%s with a limit of 8 MB/s", formatSpeed(speed))
	}
	a.router.Push("/downloads")
	shot("downloads")
	a.router.Push("/dashboard")
	shot("dashboard")
	tt.SetDark(true)
	shot("dashboard-dark")
	a.router.Push("/downloads")
	shot("downloads-dark")
	tt.SetDark(false)

	// Pause and resume the large file.
	if err := a.eng.Pause(big); err != nil {
		t.Error(err)
	}
	paused := find(big)
	t.Logf("paused: %s at %d of %d bytes (%s)", paused.Name, paused.Done, paused.Size, paused.Status)
	if paused.Status != engine.Paused || paused.Done == 0 || paused.Done >= paused.Size {
		t.Errorf("the large file after Pause: %+v", paused)
	}
	a.set.UI.InspectorShown = true
	a.router.Push("/downloads")
	tt.Frame()
	if err := tt.Click(paused.Name); err != nil {
		t.Error(err)
	}
	shot("downloads-details")
	a.set.UI.InspectorShown = false
	if err := a.eng.Resume(big); err != nil {
		t.Error(err)
	}
	time.Sleep(3 * time.Second)
	if s := find(big); (s.Status != engine.Downloading && s.Status != engine.Completed) || s.Done < paused.Done {
		t.Errorf("the large file after Resume: %s at %d, paused at %d", s.Status, s.Done, paused.Done)
	} else {
		t.Logf("resumed: %s at %d bytes, %d connections, %s", s.Name, s.Done, s.Conns, formatSpeed(s.Speed))
	}
	for i, id := range ids {
		for deadline := time.Now().Add(6 * time.Minute); ; time.Sleep(200 * time.Millisecond) {
			s := find(id)
			if s.Status == engine.Completed {
				info, err := os.Stat(s.Path())
				if err != nil || info.Size() != s.Size {
					t.Errorf("%s: %v, task size %d", s.Path(), err, s.Size)
				}
				t.Logf("completed: %s, %d bytes, %s", s.Name, s.Size, s.FinishedAt.Sub(s.CreatedAt).Round(time.Millisecond))
				break
			}
			if s.Status == engine.Failed || time.Now().After(deadline) {
				t.Errorf("%s: %s %s", files[i], s.Status, s.Err)
				break
			}
		}
	}

	for _, s := range a.eng.Tasks() {
		if s.BitTorrent {
			t.Logf("bittorrent: %q %s, %d of %d bytes, %d seeders, %d connections", s.Name, s.Status, s.Done, s.Size, s.Seeders, s.Conns)
		}
	}

	a.router.Push("/trackers")
	shot("trackers")
	a.router.Push("/settings")
	shot("settings")
	a.router.Push("/settings/downloads")
	shot("settings-downloads")
	a.router.Push("/downloads")
	tt.Frame()
	if err := tt.Click("New task"); err != nil {
		t.Error(err)
	}
	a.add.links = "https://www.python.org/ftp/python/3.12.0/Python-3.12.0.tar.xz\nmagnet:?xt=urn:btih:08ada5a7a6183aae1e09d831df6748d566095a10&dn=Sintel"
	shot("new-task")
	a.add.open = false

	// Removing with the files takes the unfinished ones off the disk.
	for _, id := range []string{big, torrent, magnet} {
		if id == "" {
			continue
		}
		// The download a magnet link became has another ID.
		for _, s := range a.eng.Tasks() {
			if s.ID == id || (id == magnet && s.Name == "Sintel") {
				path := s.Path()
				if err := a.eng.Remove(s.ID, true); err != nil {
					t.Errorf("remove %s: %v", s.Name, err)
				}
				if _, err := os.Stat(path); err == nil && s.Name != "" && path != downloads {
					t.Errorf("%s is still there after removing the task with its files", path)
				}
			}
		}
	}
	a.refresh()
	if left, _ := filepath.Glob(filepath.Join(downloads, "*.torrent")); len(left) != 0 {
		t.Errorf("torrent files left behind: %v", left)
	}
	if len(a.tasks) != len(files) {
		t.Errorf("%d tasks left, want the %d finished ones", len(a.tasks), len(files))
	}
	st := a.eng.Stats()
	t.Logf("transferred today: %s down, %s up", formatBytes(st.Today), formatBytes(st.TodayUp))
}
