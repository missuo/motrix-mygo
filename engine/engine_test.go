package engine

import (
	"bytes"
	"crypto/sha256"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// binary is the aria2c that `go run ./scripts/fetch-engine` put in the
// resources, or "" when it was not run.
func binary() string {
	name := "aria2c"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	p, _ := filepath.Abs(filepath.Join("..", "resources", runtime.GOOS+"-"+runtime.GOARCH, "engine", name))
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}

func testConfig(t *testing.T) Config {
	t.Helper()
	bin := binary()
	if bin == "" {
		t.Skip("aria2c is not in resources: run `go run ./scripts/fetch-engine`")
	}
	// A short path: aria2's database and session paths are arguments.
	root, err := os.MkdirTemp("", "motrix")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	template, _ := filepath.Abs(filepath.Join("..", "resources", "engine", "aria2.conf"))
	s := DefaultSettings()
	s.RPCPort = 26800
	s.ListenPort, s.DHTListenPort = 26881, 26881
	s.DHTEnabled, s.BTEnableLPD = false, false
	return Config{
		Binary: bin, Template: template,
		DataDir: filepath.Join(root, "data"), SaveDir: filepath.Join(root, "downloads"),
		Settings: s,
	}
}

func startManager(t *testing.T, cfg Config) *Manager {
	t.Helper()
	m := New(cfg)
	t.Cleanup(m.Close)
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		switch s := m.Stats(); s.State {
		case Ready:
			return m
		case Broken:
			t.Fatalf("the engine did not start: %s", s.Err)
		}
	}
	t.Fatal("the engine did not become ready")
	return nil
}

func payload(n int) []byte {
	b := make([]byte, n)
	rand.New(rand.NewSource(1)).Read(b)
	return b
}

type slowReader struct {
	*bytes.Reader
	delay time.Duration
}

func (s *slowReader) Read(p []byte) (int, error) {
	time.Sleep(s.delay)
	return s.Reader.Read(p[:min(len(p), 16<<10)])
}

// serve serves data at any path, a chunk every delay.
func serve(t *testing.T, data []byte, delay time.Duration) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/missing") {
			http.NotFound(w, r)
			return
		}
		http.ServeContent(w, r, "", time.Time{}, &slowReader{Reader: bytes.NewReader(data), delay: delay})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func waitFor(t *testing.T, m *Manager, id string, want ...Status) Snapshot {
	t.Helper()
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		for _, s := range m.Tasks() {
			if s.ID == id && slices.Contains(want, s.Status) {
				return s
			}
			if s.ID == id && s.Status == Failed {
				t.Fatalf("the task failed: %s", s.Err)
			}
		}
	}
	t.Fatalf("the task did not become %v: %+v", want, m.Tasks())
	return Snapshot{}
}

func sameFile(t *testing.T, path string, data []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if sha256.Sum256(got) != sha256.Sum256(data) {
		t.Fatalf("%s differs from what was served (%d bytes, want %d)", path, len(got), len(data))
	}
}

func TestArgs(t *testing.T) {
	cfg := Config{DataDir: "/data", SaveDir: "/downloads", Settings: DefaultSettings(),
		Limits: Limits{Download: 1024}, Proxy: &Proxy{URL: "http://127.0.0.1:7890", NoProxy: []string{"localhost", ".local"}}}
	args, err := buildArgs(cfg, argOptions{conf: "/data/aria2.conf", hasSQLite: true, loadTextSession: true})
	if err != nil {
		t.Fatal(err)
	}
	if args[0] != "--conf-path=/data/aria2.conf" {
		t.Errorf("the configuration is not first: %q", args[0])
	}
	for _, want := range []string{
		"--rpc-listen-port=16800", "--rpc-allow-origin-all=false", "--dir=/downloads",
		"--enable-sqlite3-persistence=true", "--sqlite3-db-path=/data/aria2.db", "--sqlite3-history-limit=-1",
		"--max-concurrent-downloads=10", "--max-connection-per-server=64", "--split=16", "--min-split-size=4194304",
		"--disk-cache=33554432", "--max-overall-download-limit=1024", "--max-overall-upload-limit=0",
		"--user-agent=Motrix/2.0", "--async-dns=true", "--seed-ratio=1", "--file-allocation=none",
		"--all-proxy=http://127.0.0.1:7890", "--no-proxy=localhost,.local", "--http-proxy=",
		"--allow-overwrite=false", "--force-save=true", "--bt-remove-unselected-file=true",
	} {
		if !slices.Contains(args, want) {
			t.Errorf("no %s in %q", want, args)
		}
	}
	// SQLite persistence replaces the text session.
	for _, arg := range args {
		if strings.HasPrefix(arg, "--input-file") || strings.HasPrefix(arg, "--auto-save-interval") || strings.HasPrefix(arg, "--rpc-secret") {
			t.Errorf("unexpected %s", arg)
		}
	}
	if last := args[len(args)-1]; last != "--bt-remove-unselected-file=true" {
		t.Errorf("the invariants are not last: %q", last)
	}

	cfg.Settings.SQLite3Persistence = false
	cfg.Settings.RPCSecret = "s3cret"
	args, _ = buildArgs(cfg, argOptions{conf: "c", hasSQLite: true, loadTextSession: true})
	for _, want := range []string{"--input-file=/data/aria2.session", "--auto-save-interval=10", "--rpc-secret=s3cret", "--rpc-allow-origin-all=true"} {
		if !slices.Contains(args, want) {
			t.Errorf("no %s without SQLite persistence", want)
		}
	}
	cfg.Settings.UserAgent = "a\nb"
	if _, err := buildArgs(cfg, argOptions{}); err == nil {
		t.Error("a line break in an option was accepted")
	}
}

func TestSettings(t *testing.T) {
	s := DefaultSettings()
	if s.Split != 16 || s.MaxConnectionPerServer != 64 || s.MinSplitSize != 4*mb || s.DiskCache != 32*mb {
		t.Errorf("the automatic profile: %+v", s)
	}
	s.PerformanceProfile = "maximum"
	s.Normalize()
	if s.Split != 64 || s.MinSplitSize != 1*mb {
		t.Errorf("the maximum profile: %+v", s)
	}
	s.PerformanceProfile, s.Split, s.RPCPort, s.FileAllocation = "custom", 5, 80, "bogus"
	s.Normalize()
	if s.Split != 5 || s.RPCPort != 16800 || s.FileAllocation != "none" {
		t.Errorf("custom values: %+v", s)
	}
	if needsRestart(Config{Settings: s}, Config{Settings: s, Limits: Limits{Download: 5}}) {
		t.Error("a speed limit restarts the engine")
	}
	changed := s
	changed.ListenPort++
	if !needsRestart(Config{Settings: s}, Config{Settings: changed}) {
		t.Error("a listen port does not restart the engine")
	}
}

func TestDownload(t *testing.T) {
	cfg := testConfig(t)
	data := payload(3<<20 + 77)
	srv := serve(t, data, 0)
	m := startManager(t, cfg)
	if s := m.Stats(); !strings.HasPrefix(s.Version, "1.37.0") {
		t.Errorf("version %q", s.Version)
	}
	events := make(chan Event, 4)
	m.OnEvent(func(ev Event) { events <- ev })

	id, err := m.Add(srv.URL+"/file.bin", "", "")
	if err != nil {
		t.Fatal(err)
	}
	s := waitFor(t, m, id, Completed)
	if s.Name != "file.bin" || s.Size != int64(len(data)) || s.FinishedAt.IsZero() || s.URL != srv.URL+"/file.bin" {
		t.Errorf("snapshot %+v", s)
	}
	sameFile(t, filepath.Join(cfg.SaveDir, "file.bin"), data)
	select {
	case ev := <-events:
		if ev.Task.ID != id || ev.Task.Status != Completed {
			t.Errorf("event %+v", ev)
		}
	case <-time.After(3 * time.Second):
		t.Error("no event for the finished download")
	}

	// A second download of the name gets a number, and one that is named
	// its name.
	second, _ := m.Add(srv.URL+"/file.bin", "", "")
	named, _ := m.Add(srv.URL+"/file.bin", "", "renamed.bin")
	if got := waitFor(t, m, second, Completed); got.Name != "file.1.bin" {
		t.Errorf("the second download is named %q", got.Name)
	}
	sameFile(t, waitFor(t, m, named, Completed).Path(), data)
	if st := m.Stats(); st.Completed != 3 || st.AllTime == 0 || st.Today == 0 {
		t.Errorf("stats %+v", st)
	}

	// Removing with the files deletes them; clearing keeps them.
	if err := m.Remove(named, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(cfg.SaveDir, "renamed.bin")); err == nil {
		t.Error("the file of a task removed with its files is still there")
	}
	if err := m.ClearCompleted(); err != nil {
		t.Fatal(err)
	}
	if n := len(m.Tasks()); n != 0 {
		t.Errorf("%d tasks after clearing", n)
	}
	sameFile(t, filepath.Join(cfg.SaveDir, "file.1.bin"), data)
}

func TestPauseResumeAcrossRestart(t *testing.T) {
	cfg := testConfig(t)
	data := payload(6 << 20)
	srv := serve(t, data, 15*time.Millisecond)
	m := startManager(t, cfg)
	id, err := m.Add(srv.URL+"/big.iso", "", "")
	if err != nil {
		t.Fatal(err)
	}
	for waitFor(t, m, id, Downloading).Done < 256<<10 {
		time.Sleep(20 * time.Millisecond)
	}
	if err := m.Pause(id); err != nil {
		t.Fatal(err)
	}
	paused := waitFor(t, m, id, Paused)
	if paused.Done == 0 || paused.Done >= int64(len(data)) {
		t.Fatalf("paused at %d of %d", paused.Done, len(data))
	}
	m.Close()

	// The database brings the task back, where it was.
	m = startManager(t, cfg)
	loaded := waitFor(t, m, id, Paused)
	if loaded.Name != "big.iso" || loaded.CreatedAt.IsZero() || !loaded.CreatedAt.Equal(paused.CreatedAt) {
		t.Errorf("loaded %+v, paused %+v", loaded, paused)
	}
	if err := m.Resume(id); err != nil {
		t.Fatal(err)
	}
	s := waitFor(t, m, id, Completed)
	sameFile(t, s.Path(), data)
}

func TestFailureRetryAndLimits(t *testing.T) {
	cfg := testConfig(t)
	cfg.Settings.MaxTries, cfg.Settings.RetryWait = 1, 0
	data := payload(1 << 20)
	srv := serve(t, data, 0)
	m := startManager(t, cfg)
	if _, err := m.Add("not a link", "", ""); err == nil {
		t.Error("a text that is no link was accepted")
	}
	id, err := m.Add(srv.URL+"/missing", "", "")
	if err != nil {
		t.Fatal(err)
	}
	var failed Snapshot
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline) && failed.Status != Failed; time.Sleep(20 * time.Millisecond) {
		for _, s := range m.Tasks() {
			if s.ID == id {
				failed = s
			}
		}
	}
	if failed.Status != Failed || failed.Err == "" {
		t.Fatalf("the missing file: %+v", failed)
	}
	if err := m.Remove(id, false); err != nil {
		t.Fatal(err)
	}

	// A limit set while aria2 runs applies without a restart.
	cfg.Limits.Download = 256 << 10
	if err := m.Configure(cfg); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	id, _ = m.Add(srv.URL+"/slow.bin", "", "")
	waitFor(t, m, id, Completed)
	if took := time.Since(start); took < 2*time.Second {
		t.Errorf("1 MiB at 256 KiB/s took %v", took)
	}
	if m.Stats().State != Ready {
		t.Error("the engine restarted for a speed limit")
	}
}

func TestMagnetIsQueued(t *testing.T) {
	cfg := testConfig(t)
	m := startManager(t, cfg)
	id, err := m.Add("magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=Example", "", "")
	if err != nil {
		t.Fatal(err)
	}
	s := waitFor(t, m, id, Metadata, Queued)
	if s.Name != "Example" || !s.BitTorrent {
		t.Errorf("snapshot %+v", s)
	}
	if err := m.Remove(id, false); err != nil {
		t.Fatal(err)
	}
	if n := len(m.Tasks()); n != 0 {
		t.Errorf("%d tasks after removing the magnet link", n)
	}
}
