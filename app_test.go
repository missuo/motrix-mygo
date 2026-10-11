package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"motrix-mygo/engine"
)

// newTestApp makes an app over a real aria2c, with its data and its
// downloads in a temporary directory.
func newTestApp(t *testing.T) *app {
	t.Helper()
	resources, _ := filepath.Abs("resources")
	binary, template := findEngine(resources)
	if binary == "" {
		t.Skip("aria2c is not in resources: run `go run ./scripts/fetch-engine`")
	}
	// A short path: aria2 takes its database and session as arguments.
	root, err := os.MkdirTemp("", "motrix")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	downloads := filepath.Join(root, "Downloads")
	set := defaultSettings(downloads)
	set.App.Language, set.App.SidebarColor = "en", "blue"
	set.Engine.RPCPort, set.Engine.ListenPort, set.Engine.DHTListenPort = 27800, 27881, 27881
	set.Engine.DHTEnabled, set.Engine.BTEnableLPD = false, false
	set.Tracker.AutoSync = false
	a := newApp(set, appPaths{data: filepath.Join(root, "data"), binary: binary, template: template, downloads: downloads})
	t.Cleanup(a.eng.Close)
	// Pages take no clicks while they slide in.
	a.router.Transition = ui.TransitionNone
	a.applySettings()
	waitEngine(t, a)
	return a
}

// waitEngine waits until aria2 answers.
func waitEngine(t *testing.T, a *app) {
	t.Helper()
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		switch s := a.eng.Stats(); s.State {
		case engine.Ready:
			a.refresh()
			return
		case engine.Broken:
			t.Fatalf("the engine did not start: %s", s.Err)
		}
	}
	t.Fatal("the engine did not become ready")
}

// serveFile serves n bytes at /<name>, a chunk every delay.
func serveFile(t *testing.T, n int, delay time.Duration) *httptest.Server {
	t.Helper()
	data := bytes.Repeat([]byte("motrix"), n/6+1)[:n]
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "", time.Time{}, &slowReader{Reader: bytes.NewReader(data), delay: delay})
	}))
	t.Cleanup(srv.Close)
	return srv
}

type slowReader struct {
	*bytes.Reader
	delay time.Duration
}

func (s *slowReader) Read(p []byte) (int, error) {
	time.Sleep(s.delay)
	return s.Reader.Read(p[:min(len(p), 32<<10)])
}

// waitStatus waits for a task to reach a status, then refreshes
// the app as the engine's listener would.
func waitStatus(t *testing.T, a *app, want engine.Status) {
	t.Helper()
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		for _, task := range a.eng.Tasks() {
			if task.Status == want {
				a.refresh()
				return
			}
		}
	}
	t.Fatalf("the task did not become %s: %+v", want, a.eng.Tasks())
}

func click(t *testing.T, tt *ui.Tester, label string) {
	t.Helper()
	if err := tt.Click(label); err != nil {
		t.Fatalf("click %q: %v (texts %q)", label, err, tt.Texts())
	}
}

func TestNavigation(t *testing.T) {
	a := newTestApp(t)
	tt := ui.NewTester(a.view, 960, 680)
	if !tt.HasText("No downloads yet") {
		t.Fatalf("the downloads page is not the first: %q", tt.Texts())
	}
	for page, text := range map[string]string{
		"Dashboard":     "ACTIVE TASKS",
		"Trackers":      "ngosang/trackerslist (best)",
		"Plugins":       "Plugins are not available",
		"Notifications": "No notifications",
		"Settings":      "Appearance",
		"Downloads":     "All Downloads",
	} {
		click(t, tt, page)
		if !tt.HasText(text) {
			t.Errorf("%s does not show %q: %q", page, text, tt.Texts())
		}
	}
}

func TestAddDownload(t *testing.T) {
	a := newTestApp(t)
	srv := serveFile(t, 200<<10, 0)
	tt := ui.NewTester(a.view, 960, 680)

	click(t, tt, "New task")
	if !tt.HasText("New Task") {
		t.Fatalf("the dialog did not open: %q", tt.Texts())
	}
	click(t, tt, "Download")
	if !tt.HasText("Enter a link or choose a torrent file") {
		t.Errorf("no error for an empty dialog: %q", tt.Texts())
	}
	click(t, tt, "Links")
	tt.Type("not a link")
	click(t, tt, "Download")
	if !a.add.open || len(a.eng.Tasks()) != 0 {
		t.Fatalf("a text that is no link was added")
	}
	a.add.links = srv.URL + "/report.pdf"
	tt.Frame()
	click(t, tt, "Download")
	if a.add.open {
		t.Fatalf("the dialog stayed open: %q", a.add.err)
	}
	waitStatus(t, a, engine.Completed)
	tt.Frame()
	if !tt.HasText("report.pdf") || !tt.HasText("Completed") || !tt.HasText("100%") {
		t.Errorf("the list does not show the finished download: %q", tt.Texts())
	}
	if _, err := os.Stat(filepath.Join(a.set.App.DefaultSaveDir, "report.pdf")); err != nil {
		t.Error(err)
	}
	for deadline := time.Now().Add(3 * time.Second); len(a.notes) == 0 && time.Now().Before(deadline); {
		time.Sleep(20 * time.Millisecond)
	}
	if len(a.notes) != 1 || a.unread != 1 {
		t.Errorf("%d notifications, %d unread", len(a.notes), a.unread)
	}
	click(t, tt, "Notifications")
	if !tt.HasText("Download completed") || a.unread != 0 {
		t.Errorf("the notifications page: %q, %d unread", tt.Texts(), a.unread)
	}
}

func TestPauseResumeRemoveFromMenu(t *testing.T) {
	a := newTestApp(t)
	srv := serveFile(t, 8<<20, 30*time.Millisecond)
	if _, err := a.eng.Add(srv.URL+"/big.iso", "", ""); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, a, engine.Downloading)
	tt := ui.NewTester(a.view, 960, 680)

	choose := func(item string) {
		t.Helper()
		if err := tt.RightClick("big.iso"); err != nil {
			t.Fatal(err)
		}
		if err := tt.ChooseMenuItem(item); err != nil {
			t.Fatalf("choose %q: %v (menu %q)", item, err, tt.Menu())
		}
		tt.Frame()
	}
	choose("Pause")
	if s := a.eng.Tasks()[0].Status; s != engine.Paused {
		t.Fatalf("status %s after Pause", s)
	}
	if !tt.HasText("Paused") {
		t.Errorf("the row does not say Paused: %q", tt.Texts())
	}
	choose("Resume")
	if s := a.eng.Tasks()[0].Status; s != engine.Downloading && s != engine.Queued {
		t.Fatalf("status %s after Resume", s)
	}
	choose("Remove")
	if n := len(a.eng.Tasks()); n != 0 {
		t.Fatalf("%d tasks after Remove", n)
	}
	if !tt.HasText("No downloads yet") {
		t.Errorf("the page is not empty again: %q", tt.Texts())
	}
}

func TestSettings(t *testing.T) {
	a := newTestApp(t)
	a.paths.settings = filepath.Join(t.TempDir(), "settings.json")
	tt := ui.NewTester(a.view, 1024, 660)
	click(t, tt, "Settings")
	for _, section := range settingsSections {
		if !tt.HasText(section.title) {
			t.Errorf("the settings do not list %s", section.title)
		}
	}

	// Appearance applies at once.
	click(t, tt, "Appearance")
	click(t, tt, "violet")
	click(t, tt, "Dark")
	if got := loadSettings(a.paths.settings, "/tmp"); got.App.SidebarColor != "violet" || got.App.Theme != "dark" {
		t.Errorf("saved %+v", got.App)
	}

	// The engine's settings are a draft until saved.
	a.router.Push("/settings/downloads")
	tt.Frame()
	for _, text := range []string{"Performance", "Download performance", "Max connections", "Simultaneous downloads",
		"Speed limits", "Connections and retries", "User-Agent", "Disk and files", "Reserve disk space", "Magnet"} {
		if !tt.HasText(text) {
			t.Errorf("the downloads settings do not show %q", text)
		}
	}
	if tt.HasText("You have unsaved changes.") {
		t.Error("the page asks to save without a change")
	}
	a.draft.Engine.MaxConcurrentDownloads = 3
	a.draft.SpeedLimit.Turtle = "on"
	tt.Frame()
	if a.set.Engine.MaxConcurrentDownloads != 10 {
		t.Error("a draft changed the settings")
	}
	click(t, tt, "Save")
	if a.set.Engine.MaxConcurrentDownloads != 3 || a.limits.Download != 512<<10 || a.limits.Upload != 64<<10 {
		t.Errorf("saved %d downloads, limits %+v", a.set.Engine.MaxConcurrentDownloads, a.limits)
	}
	if cfg := a.eng.Config(); cfg.Settings.MaxConcurrentDownloads != 3 || cfg.Limits.Download != 512<<10 {
		t.Errorf("the engine's configuration: %+v", cfg.Limits)
	}
	if got := loadSettings(a.paths.settings, "/tmp"); got.Engine.MaxConcurrentDownloads != 3 || got.SpeedLimit.Turtle != "on" {
		t.Errorf("the file: %+v", got.SpeedLimit)
	}

	for section, text := range map[string]string{
		"bittorrent": "BT listen port", "network": "DNS lookup", "advanced": "RPC port", "about": "github.com/missuo/motrix-mygo",
	} {
		a.router.Push("/settings/" + section)
		tt.Frame()
		if !tt.HasText(text) {
			t.Errorf("%s does not show %q: %q", section, text, tt.Texts())
		}
	}

	// The dashboard's speed modes are the settings'.
	a.router.Push("/dashboard")
	tt.Frame()
	click(t, tt, "Standard")
	if a.set.SpeedLimit.Turtle != "off" || a.limits.Download != 0 {
		t.Errorf("mode %q, limits %+v", a.set.SpeedLimit.Turtle, a.limits)
	}
}

func TestSpeedLimits(t *testing.T) {
	l := defaultSettings("").SpeedLimit
	at := func(clock string) time.Time {
		tm, _ := time.ParseInLocation("2006-01-02 15:04", "2026-10-10 "+clock, time.Local) // a Saturday
		return tm
	}
	l.Base.Download = 2 << 20
	if got := l.effective(at("12:00")); got.Download != 2<<20 || got.Upload != 0 {
		t.Errorf("standard: %+v", got)
	}
	l.Turtle = "on"
	if got := l.effective(at("12:00")); got.Download != 512<<10 || got.Upload != 64<<10 {
		t.Errorf("low speed: %+v", got)
	}
	l.Turtle = "auto"
	if got := l.effective(at("23:30")); got.Download != 2<<20 {
		t.Errorf("automatic without rules: %+v", got)
	}
	l.Auto.Schedule.Enabled = true
	for clock, want := range map[string]int64{"23:30": 512 << 10, "06:59": 512 << 10, "07:00": 2 << 20, "12:00": 2 << 20} {
		if got := l.effective(at(clock)); got.Download != want {
			t.Errorf("scheduled at %s: %d, want %d", clock, got.Download, want)
		}
	}
	l.Auto.Schedule.Days = []int{1}
	if got := l.effective(at("23:30")); got.Download != 2<<20 {
		t.Errorf("scheduled for Mondays, on a Saturday: %+v", got)
	}
	l.Auto.Adaptive.Enabled, l.Auto.Adaptive.LinkDown = true, 1<<20
	if got := l.effective(at("12:00")); got.Download != (1<<20)*80/100 {
		t.Errorf("adaptive: %+v", got)
	}
}

func TestTrackerLists(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("udp://a.example:1337/announce\n\nhttp://b.example/announce\nnot a tracker\nudp://a.example:1337/announce\n"))
	}))
	defer srv.Close()
	sources := []trackerSource{
		{ID: "one", Label: "one", URL: srv.URL + "/one", Enabled: true},
		{ID: "off", Label: "off", URL: srv.URL + "/off"},
		{ID: "gone", Label: "gone", URL: "http://127.0.0.1:1/gone", Enabled: true},
	}
	list := fetchTrackers(t.Context(), http.DefaultClient, sources, 50)
	if len(list.Trackers) != 2 || list.Trackers[0] != "udp://a.example:1337/announce" || list.Errors["gone"] == "" {
		t.Errorf("list %+v", list)
	}
}

func TestChinese(t *testing.T) {
	a := newTestApp(t)
	a.set.App.Language = "zh"
	a.applySettings()
	defer setLanguage("en", "")
	tt := ui.NewTester(a.view, 960, 680)
	if !tt.HasText("全部下载") || !tt.HasText("暂无下载任务") {
		t.Errorf("texts %q", tt.Texts())
	}
	for en := range zhCN {
		if strings.TrimSpace(en) == "" {
			t.Error("an empty key")
		}
	}
}

func TestNarrowWindowAndHiddenSidebar(t *testing.T) {
	a := newTestApp(t)
	tt := ui.NewTester(a.view, 720, 480)
	click(t, tt, "Toggle sidebar")
	if !a.set.UI.SidebarHidden {
		t.Fatal("the sidebar is still shown")
	}
	// The buttons beside the window controls stay while it is hidden.
	click(t, tt, "Toggle sidebar")
	click(t, tt, "Dashboard")
	for _, text := range []string{"ENGINE", "UPLOAD", "DOWNLOADING", "ACTIVITY"} {
		if !tt.HasText(text) {
			t.Errorf("the narrow dashboard does not show %q", text)
		}
	}
}

// A download that failed is removed like any other, from its menu, with
// its files or without, while the details show it.
func TestRemoveFailed(t *testing.T) {
	a := newTestApp(t)
	for _, item := range []string{"Remove", "Remove and delete files"} {
		if _, err := a.eng.Add("http://127.0.0.1:1/nothing.bin", "", ""); err != nil {
			t.Fatal(err)
		}
		waitStatus(t, a, engine.Failed)
		tt := ui.NewTester(a.view, 1024, 660)
		click(t, tt, "nothing.bin")
		click(t, tt, "Details")
		if err := tt.RightClick("nothing.bin"); err != nil {
			t.Fatal(err)
		}
		if err := tt.ChooseMenuItem(item); err != nil {
			t.Fatalf("choose %q: %v (menu %q)", item, err, tt.Menu())
		}
		tt.Frame()
		if item != "Remove" {
			click(t, tt, "Delete")
			tt.Frame()
		}
		if n := len(a.eng.Tasks()); n != 0 {
			t.Fatalf("%d tasks after %q", n, item)
		}
		if !tt.HasText("No downloads yet") {
			t.Errorf("the page is not empty after %q: %q", item, tt.Texts())
		}
	}
}

// The New Task dialog opens with the links on the clipboard, and without
// what is no link.
func TestClipboardLinks(t *testing.T) {
	a := newTestApp(t)
	tt := ui.NewTester(a.view, 1024, 660)
	tt.SetClipboard("see https://example.com/page\n https://example.com/file.zip \nmagnet:?xt=urn:btih:c12fe1c06bba254a9dc9f519b335aa7c1367a88a\nnot a link")
	click(t, tt, "New task")
	if want := "https://example.com/file.zip\nmagnet:?xt=urn:btih:c12fe1c06bba254a9dc9f519b335aa7c1367a88a"; a.add.links != want {
		t.Errorf("the dialog opened with %q, want %q", a.add.links, want)
	}
	a.add.open = false
	tt.Frame()

	tt.SetClipboard("just some text")
	click(t, tt, "New task")
	if a.add.links != "" {
		t.Errorf("the dialog took %q from the clipboard", a.add.links)
	}
	a.add.open = false
	tt.Frame()

	a.set.App.AutofillClipboardLinks = false
	tt.SetClipboard("https://example.com/file.zip")
	click(t, tt, "New task")
	if a.add.links != "" {
		t.Errorf("the dialog took %q with the setting off", a.add.links)
	}
}
