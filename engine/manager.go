package engine

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// Status is where a task is in its life.
type Status string

const (
	Queued      Status = "queued"
	Metadata    Status = "metadata" // a magnet link loading its file list
	Downloading Status = "downloading"
	Seeding     Status = "seeding"
	Paused      Status = "paused"
	Completed   Status = "completed"
	Failed      Status = "error"
)

// State is what the aria2 process is doing.
type State string

const (
	Starting State = "starting"
	Ready    State = "ready"
	Stopped  State = "stopped"
	Broken   State = "error"
)

// Snapshot is a task as it was at the last poll.
type Snapshot struct {
	ID   string // aria2's GID
	URL  string
	Name string
	Dir  string
	// File is the task's first file, of FileCount.
	File      string
	FileCount int

	Size        int64 // -1 while unknown
	Done        int64
	Uploaded    int64
	Speed       int64 // bytes per second
	UploadSpeed int64
	Status      Status
	Err         string
	Conns       int
	Seeders     int
	BitTorrent  bool
	InfoHash    string
	CreatedAt   time.Time
	FinishedAt  time.Time
}

// Path is where the task's file is, or its directory for several files.
func (s Snapshot) Path() string {
	if s.FileCount <= 1 && s.File != "" {
		return s.File
	}
	return filepath.Join(s.Dir, s.Name)
}

// Progress is the share downloaded, from 0 to 1, or -1 while the size is
// unknown.
func (s Snapshot) Progress() float64 {
	if s.Status == Completed || s.Status == Seeding {
		return 1
	}
	if s.Size <= 0 {
		return -1
	}
	return min(1, float64(s.Done)/float64(s.Size))
}

// ETA is the time left at the current speed, or -1 when it cannot be told.
func (s Snapshot) ETA() time.Duration {
	if s.Status != Downloading || s.Speed <= 0 || s.Size <= 0 {
		return -1
	}
	return time.Duration(float64(s.Size-s.Done)/float64(s.Speed)) * time.Second
}

// Stats sums the engine and its tasks up.
type Stats struct {
	State   State
	Version string
	// Err is why the engine is Broken.
	Err string

	Speed, UploadSpeed int64
	Conns              int
	Active             int // downloading, or loading a magnet link
	Seeding            int
	Waiting            int
	Paused             int
	Completed          int
	Failed             int
	Total              int
	// History and UploadHistory are the speeds of the last minute, a
	// sample a second, oldest first.
	History, UploadHistory []int64
	// Today and AllTime are the bytes downloaded, TodayUp and AllTimeUp
	// uploaded; Daily those downloaded each day, by DayKey.
	Today, AllTime, TodayUp, AllTimeUp int64
	Daily                              map[string]int64
}

// Event tells of a task that finished or failed.
type Event struct {
	Task Snapshot
	At   time.Time
}

// DayKey is the key of a day in Stats.Daily.
func DayKey(t time.Time) string { return t.Format("2006-01-02") }

// taskMeta is what the app keeps about a task that aria2 does not.
type taskMeta struct {
	URL        string    `json:"url,omitempty"`
	AddedAt    time.Time `json:"addedAt"`
	FinishedAt time.Time `json:"finishedAt,omitzero"`
	// Torrent is the copy aria2 saved of a .torrent file added, which
	// goes with the task's files.
	Torrent string `json:"torrent,omitempty"`
	// TimedOut marks a magnet link given up on, which aria2 only knows as
	// removed.
	TimedOut bool `json:"timedOut,omitempty"`
	// metadataSince is when the task was first seen loading its file list.
	metadataSince time.Time
}

// saved is what the state file holds.
type saved struct {
	Tasks   map[string]*taskMeta `json:"tasks"`
	Daily   map[string][2]int64  `json:"daily"` // downloaded, uploaded
	AllTime [2]int64             `json:"allTime"`
}

// Manager runs aria2 and owns the tasks. Its methods may be called from any
// goroutine.
type Manager struct {
	mu        sync.Mutex
	cfg       Config
	state     State
	err       string
	version   string
	hasSQLite bool
	probed    bool
	rpc       *rpcClient

	tasks    []Snapshot
	raw      map[string]status
	prev     map[string]Status
	primed   bool
	meta     map[string]*taskMeta
	lastDone map[string][2]int64
	daily    map[string][2]int64
	allTime  [2]int64
	speed    globalStat
	history  [2][]int64
	dirty    bool
	savedAt  time.Time
	sig      string

	onChange func()
	onEvent  func(Event)
	onPort   func(port int)

	pollMu  sync.Mutex
	restart chan struct{}
	stop    chan struct{}
	done    chan struct{}
	closing sync.Once
}

// New makes a Manager and starts aria2 in the background: State tells when
// it is Ready.
func New(cfg Config) *Manager {
	cfg.Settings.Normalize()
	m := &Manager{
		cfg: cfg, state: Starting,
		raw: map[string]status{}, prev: map[string]Status{}, meta: map[string]*taskMeta{},
		lastDone: map[string][2]int64{}, daily: map[string][2]int64{},
		restart: make(chan struct{}, 1), stop: make(chan struct{}), done: make(chan struct{}),
	}
	m.loadState()
	go m.run()
	return m
}

// OnChange sets a function called when the tasks or the engine's state
// change, and every second while anything transfers. It runs on the
// engine's goroutines.
func (m *Manager) OnChange(fn func()) {
	m.mu.Lock()
	m.onChange = fn
	m.mu.Unlock()
}

// OnEvent sets a function called when a task finishes or fails.
func (m *Manager) OnEvent(fn func(Event)) {
	m.mu.Lock()
	m.onEvent = fn
	m.mu.Unlock()
}

// OnPortChange sets a function called when the RPC port of the settings
// was taken and aria2 listens on another, to save with the settings.
func (m *Manager) OnPortChange(fn func(port int)) {
	m.mu.Lock()
	m.onPort = fn
	m.mu.Unlock()
}

// Config returns the configuration in use.
func (m *Manager) Config() Config {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg
}

// Configure changes the configuration: aria2 takes what it can while it
// runs, and is restarted for the rest, its downloads going on after.
func (m *Manager) Configure(cfg Config) error {
	cfg.Settings.Normalize()
	m.mu.Lock()
	old := m.cfg
	m.cfg = cfg
	rpc, ready := m.rpc, m.state == Ready
	m.mu.Unlock()
	if needsRestart(old, cfg) || !ready {
		m.Restart()
		return nil
	}
	options := tunable(cfg)
	for name, value := range proxyOptions(cfg.Proxy) {
		options[name] = value
	}
	options["bt-tracker"] = strings.Join(cfg.Trackers, ",")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return rpc.call(ctx, nil, "aria2.changeGlobalOption", options)
}

// Restart stops aria2 and starts it again.
func (m *Manager) Restart() {
	select {
	case m.restart <- struct{}{}:
	default:
	}
}

// Close asks aria2 to save its session and quit, and waits for it.
func (m *Manager) Close() {
	m.closing.Do(func() { close(m.stop) })
	select {
	case <-m.done:
	case <-time.After(10 * time.Second):
	}
}

// Tasks returns the tasks, newest first.
func (m *Manager) Tasks() []Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.tasks)
}

// Stats sums the engine and its tasks up.
func (m *Manager) Stats() Stats {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := Stats{
		State: m.state, Version: m.version, Err: m.err, Total: len(m.tasks),
		Speed: m.speed.DownloadSpeed, UploadSpeed: m.speed.UploadSpeed,
		History: slices.Clone(m.history[0]), UploadHistory: slices.Clone(m.history[1]),
		AllTime: m.allTime[0], AllTimeUp: m.allTime[1],
		Daily: make(map[string]int64, len(m.daily)),
	}
	for _, t := range m.tasks {
		s.Conns += t.Conns
		switch t.Status {
		case Downloading, Metadata:
			s.Active++
		case Seeding:
			s.Seeding++
		case Queued:
			s.Waiting++
		case Paused:
			s.Paused++
		case Completed:
			s.Completed++
		case Failed:
			s.Failed++
		}
	}
	for day, n := range m.daily {
		s.Daily[day] = n[0]
	}
	today := m.daily[DayKey(time.Now())]
	s.Today, s.TodayUp = today[0], today[1]
	return s
}

// run keeps aria2 running until Close: it starts the process, polls it,
// and starts it again when asked to or when it died.
func (m *Manager) run() {
	defer close(m.done)
	crashes := 0
	for {
		proc, err := m.start()
		if err != nil {
			m.setState(Broken, err.Error())
			select {
			case <-m.stop:
				return
			case <-m.restart:
				continue
			}
		}
		started := time.Now()
		m.setState(Ready, "")
		m.poll()

		ticker := time.NewTicker(time.Second)
		reason := ""
		for reason == "" {
			select {
			case <-ticker.C:
				m.poll()
			case <-proc.exited:
				reason = "exited"
			case <-m.restart:
				reason = "restart"
			case <-m.stop:
				reason = "stop"
			}
		}
		ticker.Stop()
		m.shutdown(proc)
		switch reason {
		case "stop":
			m.setState(Stopped, "")
			return
		case "restart":
			m.setState(Starting, "")
		case "exited":
			// One that ran for a while may run again; one that keeps dying
			// waits to be asked.
			if time.Since(started) > time.Minute {
				crashes = 0
			}
			crashes++
			if crashes > 3 {
				m.setState(Broken, "aria2 keeps quitting: "+tail(pathsIn(m.Config().DataDir).log, 400))
				select {
				case <-m.stop:
					return
				case <-m.restart:
					crashes = 0
				}
				continue
			}
			m.setState(Starting, "")
			select {
			case <-m.stop:
				return
			case <-time.After(time.Second):
			}
		}
	}
}

func (m *Manager) setState(state State, err string) {
	m.mu.Lock()
	m.state, m.err = state, err
	if state != Ready {
		m.rpc = nil
		m.speed = globalStat{}
		for i := range m.tasks {
			m.tasks[i].Speed, m.tasks[i].UploadSpeed, m.tasks[i].Conns = 0, 0, 0
		}
	}
	m.saveState()
	fn := m.onChange
	m.mu.Unlock()
	if fn != nil {
		fn()
	}
}

type process struct {
	cmd    *exec.Cmd
	rpc    *rpcClient
	exited chan struct{}
	log    *os.File
}

// start starts aria2 and waits until it answers.
func (m *Manager) start() (*process, error) {
	cfg := m.Config()
	if cfg.Binary == "" {
		return nil, errors.New("aria2c was not found: run `go run ./scripts/fetch-engine`")
	}
	if !m.probed {
		out, err := exec.Command(cfg.Binary, "--version").Output()
		if err != nil {
			return nil, fmt.Errorf("aria2c does not run: %w", err)
		}
		version, hasSQLite := parseVersion(string(out))
		m.mu.Lock()
		m.version, m.hasSQLite, m.probed = version, hasSQLite, true
		m.mu.Unlock()
	}
	conf, inputFile, err := ensureUserConfig(cfg.Template, cfg.DataDir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cfg.SaveDir, 0o755); err != nil {
		return nil, err
	}
	// Another program on the port, as another Motrix: take the next free
	// one, and keep it.
	if port := freePort(cfg.Settings.RPCPort); port != cfg.Settings.RPCPort {
		if port == 0 {
			return nil, fmt.Errorf("no free port for aria2's RPC from %d on", cfg.Settings.RPCPort)
		}
		cfg.Settings.RPCPort = port
		m.mu.Lock()
		m.cfg.Settings.RPCPort = port
		fn := m.onPort
		m.mu.Unlock()
		if fn != nil {
			fn(port)
		}
	}
	p := pathsIn(cfg.DataDir)
	_, sessionErr := os.Stat(p.session)
	args, err := buildArgs(cfg, argOptions{
		conf: conf, hasSQLite: m.hasSQLite, confHasInputFile: inputFile,
		loadTextSession: sessionErr == nil, stopWithPID: os.Getpid(),
	})
	if err != nil {
		return nil, err
	}
	if info, err := os.Stat(p.log); err == nil && info.Size() > 4<<20 {
		os.Rename(p.log, p.log+".1")
	}
	logFile, err := os.OpenFile(p.log, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(cfg.Binary, args...)
	cmd.Stdout, cmd.Stderr = logFile, logFile
	hideWindow(cmd)
	if err := cmd.Start(); err != nil {
		logFile.Close()
		return nil, fmt.Errorf("aria2c did not start: %w", err)
	}
	proc := &process{cmd: cmd, exited: make(chan struct{}), log: logFile,
		rpc: newRPCClient(cfg.Settings.RPCPort, cfg.Settings.RPCSecret)}
	go func() {
		cmd.Wait()
		close(proc.exited)
	}()

	deadline := time.After(20 * time.Second)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		err := proc.rpc.call(ctx, nil, "aria2.getVersion")
		cancel()
		if err == nil {
			break
		}
		select {
		case <-proc.exited:
			logFile.Close()
			return nil, fmt.Errorf("aria2 quit as it started: %s", tail(p.log, 400))
		case <-deadline:
			m.shutdown(proc)
			return nil, fmt.Errorf("aria2 did not answer: %w", err)
		case <-m.stop:
			m.shutdown(proc)
			return nil, errors.New("stopped")
		case <-time.After(100 * time.Millisecond):
		}
	}
	if len(cfg.Trackers) > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		proc.rpc.call(ctx, nil, "aria2.changeGlobalOption", map[string]string{"bt-tracker": strings.Join(cfg.Trackers, ",")})
		cancel()
	}
	m.mu.Lock()
	m.rpc = proc.rpc
	m.primed = false
	m.mu.Unlock()
	return proc, nil
}

// shutdown asks aria2 to save and quit, and kills it when it does not.
func (m *Manager) shutdown(proc *process) {
	m.mu.Lock()
	m.rpc = nil
	m.mu.Unlock()
	select {
	case <-proc.exited:
	default:
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		proc.rpc.call(ctx, nil, "aria2.shutdown")
		cancel()
		select {
		case <-proc.exited:
		case <-time.After(6 * time.Second):
			proc.cmd.Process.Kill()
			<-proc.exited
		}
	}
	proc.log.Close()
}

// parseVersion reads the version and whether SQLite persistence is among
// the features of `aria2c --version`.
func parseVersion(out string) (version string, hasSQLite bool) {
	for line := range strings.Lines(out) {
		if v, ok := strings.CutPrefix(line, "aria2 version "); ok && version == "" {
			version = strings.TrimSpace(v)
		}
		if strings.HasPrefix(line, "Enabled Features:") {
			hasSQLite = strings.Contains(line, "SQLite3-Persistence")
		}
	}
	return version, hasSQLite
}

// freePort returns port if nothing listens on it on the loopback, else the
// next free one of the hundred after it, or 0.
func freePort(port int) int {
	for p := port; p < port+100 && p <= 65535; p++ {
		l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
		if err == nil {
			l.Close()
			return p
		}
	}
	return 0
}

// tail returns the end of a file, for an error to quote.
func tail(file string, n int) string {
	data, err := os.ReadFile(file)
	if err != nil {
		return ""
	}
	if len(data) > n {
		data = data[len(data)-n:]
	}
	return strings.TrimSpace(string(data))
}

// client returns the RPC client, or an error while aria2 is not running.
func (m *Manager) client() (*rpcClient, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.rpc == nil {
		if m.err != "" {
			return nil, errors.New(m.err)
		}
		return nil, errors.New("the engine is not running yet")
	}
	return m.rpc, nil
}

func rpcContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 5*time.Second)
}

// poll asks aria2 for its downloads and makes the tasks of them.
func (m *Manager) poll() {
	m.pollMu.Lock()
	defer m.pollMu.Unlock()
	rpc, err := m.client()
	if err != nil {
		return
	}
	ctx, cancel := rpcContext()
	defer cancel()
	results, errs, err := rpc.multicall(ctx,
		rpcCall{"aria2.tellActive", []any{statusKeys}},
		rpcCall{"aria2.tellWaiting", []any{0, 1000, statusKeys}},
		rpcCall{"aria2.tellStopped", []any{0, 1000, statusKeys}},
		rpcCall{"aria2.getGlobalStat", nil},
	)
	if err != nil {
		return
	}
	var all []status
	for i := range 3 {
		var list []status
		if errs[i] != nil || json.Unmarshal(results[i], &list) != nil {
			return
		}
		all = append(all, list...)
	}
	var speed globalStat
	if errs[3] == nil {
		json.Unmarshal(results[3], &speed)
	}

	now := time.Now()
	m.mu.Lock()
	timeout := time.Duration(m.cfg.Settings.MagnetResolveTimeout) * time.Second
	var events []Event
	var expired []string
	seen := make(map[string]bool, len(all))
	raw := make(map[string]status, len(all))
	tasks := make([]Snapshot, 0, len(all))
	var transferred [2]int64
	// The download of a magnet link's file list is done once the download
	// of the files follows it.
	followed := map[string]bool{}
	for _, st := range all {
		if st.Following != "" {
			followed[st.Following] = true
		}
	}
	for _, st := range all {
		// The Motrix build lists a stopped download both from memory and
		// from its database.
		if seen[st.GID] {
			continue
		}
		seen[st.GID] = true
		meta := m.meta[st.GID]
		if meta == nil {
			meta = &taskMeta{AddedAt: now}
			// The download a magnet link or a torrent file became takes
			// its place in the list.
			if parent := m.meta[st.Following]; parent != nil {
				meta.AddedAt, meta.URL = parent.AddedAt, parent.URL
			}
			m.meta[st.GID] = meta
			m.dirty = true
		}
		if len(st.FollowedBy) > 0 || followed[st.GID] || (st.Status == "removed" && !meta.TimedOut) {
			continue
		}
		t := translate(st, meta)
		raw[st.GID] = st

		if t.Status == Metadata {
			if meta.metadataSince.IsZero() {
				meta.metadataSince = now
			} else if now.Sub(meta.metadataSince) > timeout {
				meta.TimedOut = true
				expired = append(expired, st.GID)
			}
		} else {
			meta.metadataSince = time.Time{}
		}

		if last, ok := m.lastDone[st.GID]; ok {
			transferred[0] += max(0, st.CompletedLength-last[0])
			transferred[1] += max(0, st.UploadLength-last[1])
		}
		m.lastDone[st.GID] = [2]int64{st.CompletedLength, st.UploadLength}

		finished := t.Status == Completed || t.Status == Seeding
		if finished && meta.FinishedAt.IsZero() {
			meta.FinishedAt = now
			t.FinishedAt = now
			m.dirty = true
			if m.primed {
				events = append(events, Event{Task: t, At: now})
			}
		}
		if t.Status == Failed && m.primed && m.prev[st.GID] != Failed {
			events = append(events, Event{Task: t, At: now})
		}
		m.prev[st.GID] = t.Status
		tasks = append(tasks, t)
	}
	for gid := range m.meta {
		if !seen[gid] {
			delete(m.meta, gid)
			delete(m.prev, gid)
			delete(m.lastDone, gid)
			m.dirty = true
		}
	}
	slices.SortStableFunc(tasks, func(a, b Snapshot) int {
		if c := b.CreatedAt.Compare(a.CreatedAt); c != 0 {
			return c
		}
		return strings.Compare(b.ID, a.ID)
	})
	if transferred != [2]int64{} {
		day := m.daily[DayKey(now)]
		for i := range 2 {
			day[i] += transferred[i]
			m.allTime[i] += transferred[i]
		}
		m.daily[DayKey(now)] = day
		m.dirty = true
	}
	busy := speed.DownloadSpeed > 0 || speed.UploadSpeed > 0
	wasBusy := m.speed.DownloadSpeed > 0 || m.speed.UploadSpeed > 0
	for i, v := range []int64{speed.DownloadSpeed, speed.UploadSpeed} {
		m.history[i] = append(m.history[i], v)
		if len(m.history[i]) > 60 {
			m.history[i] = m.history[i][len(m.history[i])-60:]
		}
	}
	sig := signature(tasks)
	changed := sig != m.sig || busy || wasBusy || !m.primed
	m.tasks, m.raw, m.speed, m.sig, m.primed = tasks, raw, speed, sig, true
	if m.dirty && now.Sub(m.savedAt) > 5*time.Second {
		m.saveState()
	}
	onChange, onEvent := m.onChange, m.onEvent
	m.mu.Unlock()

	for _, gid := range expired {
		ctx, cancel := rpcContext()
		rpc.call(ctx, nil, "aria2.forceRemove", gid)
		cancel()
	}
	if onEvent != nil {
		for _, ev := range events {
			onEvent(ev)
		}
	}
	if changed && onChange != nil {
		onChange()
	}
}

// signature changes when what the list shows of the tasks does.
func signature(tasks []Snapshot) string {
	var b strings.Builder
	for _, t := range tasks {
		fmt.Fprintf(&b, "%s %s %d %d %s|", t.ID, t.Status, t.Done, t.Size, t.Name)
	}
	return b.String()
}

// translate makes a task of a download as aria2 tells it.
func translate(st status, meta *taskMeta) Snapshot {
	t := Snapshot{
		ID: st.GID, URL: meta.URL, Dir: st.Dir, FileCount: len(st.Files),
		Size: st.TotalLength, Done: st.CompletedLength, Uploaded: st.UploadLength,
		Speed: st.DownloadSpeed, UploadSpeed: st.UploadSpeed,
		Conns: st.Connections, Seeders: st.NumSeeders,
		BitTorrent: st.BitTorrent != nil, InfoHash: st.InfoHash,
		CreatedAt: meta.AddedAt, FinishedAt: meta.FinishedAt,
	}
	if len(st.Files) > 0 {
		t.File = st.Files[0].Path
		if t.URL == "" && len(st.Files[0].URIs) > 0 {
			t.URL = st.Files[0].URIs[0].URI
		}
	}
	if t.URL == "" && st.InfoHash != "" {
		t.URL = "magnet:?xt=urn:btih:" + st.InfoHash
	}
	metadataOnly := st.BitTorrent != nil && st.BitTorrent.Info == nil
	switch {
	case st.BitTorrent != nil && st.BitTorrent.Info != nil && st.BitTorrent.Info.Name != "":
		t.Name = st.BitTorrent.Info.Name
	case t.File != "" && !metadataOnly:
		t.Name = filepath.Base(t.File)
	default:
		t.Name = nameFromURL(t.URL)
	}
	if t.Size == 0 && st.Status != "complete" {
		t.Size = -1
	}
	switch st.Status {
	case "active":
		switch {
		case metadataOnly:
			t.Status = Metadata
		case st.Seeder == "true" && st.BitTorrent != nil && st.TotalLength > 0 && st.CompletedLength >= st.TotalLength:
			t.Status = Seeding
		default:
			t.Status = Downloading
		}
	case "waiting":
		t.Status = Queued
	case "paused":
		t.Status = Paused
	case "complete":
		t.Status = Completed
	default:
		t.Status = Failed
		t.Err = st.ErrorMessage
		if meta.TimedOut {
			t.Err = "The magnet link did not load its file list in time."
		}
		if t.Err == "" {
			t.Err = "aria2 error " + st.ErrorCode
		}
	}
	return t
}

// nameFromURL names a task before aria2 knows its file: by the dn of a
// magnet link, else by the last part of the URL's path.
func nameFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if u.Scheme == "magnet" {
		if dn := u.Query().Get("dn"); dn != "" {
			return dn
		}
		return raw
	}
	if base := path.Base(u.Path); base != "" && base != "/" && base != "." {
		return base
	}
	return raw
}

// taskOptions are the options every new task takes.
func (m *Manager) taskOptions(dir string) map[string]string {
	cfg := m.Config()
	options := map[string]string{}
	if dir = strings.TrimSpace(dir); dir != "" {
		options["dir"] = dir
	}
	// Motrix sets the seeding time for each task, in minutes.
	if cfg.Settings.SeedTime > 0 {
		options["seed-time"] = fmt.Sprint(cfg.Settings.SeedTime)
	}
	if len(cfg.Trackers) > 0 {
		options["bt-tracker"] = strings.Join(cfg.Trackers, ",")
	}
	return options
}

// schemes are the links aria2 downloads.
var schemes = []string{"http", "https", "ftp", "sftp", "magnet"}

// ValidLink tells whether aria2 downloads a link.
func ValidLink(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !slices.Contains(schemes, strings.ToLower(u.Scheme)) {
		return false
	}
	if strings.EqualFold(u.Scheme, "magnet") {
		return strings.Contains(u.RawQuery, "xt=urn:bt")
	}
	return u.Host != ""
}

// Add queues a download of a link into dir (the default directory when
// empty), named name, or by the server when empty.
func (m *Manager) Add(link, dir, name string) (string, error) {
	link = strings.TrimSpace(link)
	if !ValidLink(link) {
		return "", fmt.Errorf("not a link aria2 downloads: %q", link)
	}
	rpc, err := m.client()
	if err != nil {
		return "", err
	}
	options := m.taskOptions(dir)
	if !strings.HasPrefix(strings.ToLower(link), "magnet:") {
		// aria2 neither overwrites nor renames (the product's invariants):
		// a name already taken gets a number here, as the app above aria2
		// names files in Motrix.
		if name = strings.TrimSpace(name); name == "" {
			name = nameFromURL(link)
		}
		target := dir
		if strings.TrimSpace(target) == "" {
			target = m.Config().SaveDir
		}
		if unique := m.uniqueName(target, name); unique != name || options["out"] != "" || name != nameFromURL(link) {
			options["out"] = unique
		}
	}
	ctx, cancel := rpcContext()
	defer cancel()
	var gid string
	if err := rpc.call(ctx, &gid, "aria2.addUri", []string{link}, options); err != nil {
		return "", err
	}
	m.remember(gid, link)
	m.poll()
	return gid, nil
}

// AddTorrent queues the download of a .torrent file's content into dir.
func (m *Manager) AddTorrent(torrent []byte, dir string) (string, error) {
	rpc, err := m.client()
	if err != nil {
		return "", err
	}
	ctx, cancel := rpcContext()
	defer cancel()
	var gid string
	err = rpc.call(ctx, &gid, "aria2.addTorrent", base64.StdEncoding.EncodeToString(torrent), []string{}, m.taskOptions(dir))
	if err != nil {
		return "", err
	}
	m.remember(gid, "")
	// aria2 names its copy by the SHA-1 of the file.
	sum := sha1.Sum(torrent)
	target := strings.TrimSpace(dir)
	if target == "" {
		target = m.Config().SaveDir
	}
	m.mu.Lock()
	m.meta[gid].Torrent = filepath.Join(target, hex.EncodeToString(sum[:])+".torrent")
	m.mu.Unlock()
	m.poll()
	return gid, nil
}

// uniqueName returns name, or name with a number before its extension as
// aria2 writes it, that no file in dir and no unfinished task has.
func (m *Manager) uniqueName(dir, name string) string {
	if name == "" || strings.ContainsAny(name, "/\\") {
		return name
	}
	taken := map[string]bool{}
	for _, t := range m.Tasks() {
		if t.Status != Completed && t.Status != Failed && filepath.Dir(t.Path()) == filepath.Clean(dir) {
			taken[filepath.Base(t.Path())] = true
		}
	}
	free := func(n string) bool {
		if taken[n] {
			return false
		}
		_, err := os.Stat(filepath.Join(dir, n))
		return err != nil
	}
	if free(name) {
		return name
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 1; ; i++ {
		if n := fmt.Sprintf("%s.%d%s", base, i, ext); free(n) {
			return n
		}
	}
}

func (m *Manager) remember(gid, link string) {
	m.mu.Lock()
	m.lastDone[gid] = [2]int64{}
	m.meta[gid] = &taskMeta{URL: link, AddedAt: time.Now()}
	m.dirty = true
	m.mu.Unlock()
}

// Pause stops a task that downloads, seeds or waits, keeping what it has.
func (m *Manager) Pause(id string) error {
	return m.simple("aria2.forcePause", id)
}

// Resume starts a paused task again, and a failed one anew.
func (m *Manager) Resume(id string) error {
	m.mu.Lock()
	st, ok := m.raw[id]
	m.mu.Unlock()
	if ok && st.Status == "paused" {
		return m.simple("aria2.unpause", id)
	}
	return m.retry(id)
}

// retry adds a stopped task again, from its link, and takes the old one
// off the list.
func (m *Manager) retry(id string) error {
	m.mu.Lock()
	var link, dir string
	for _, t := range m.tasks {
		if t.ID == id {
			link, dir = t.URL, t.Dir
		}
	}
	m.mu.Unlock()
	if link == "" {
		return errors.New("the task has no link to try again")
	}
	if _, err := m.Add(link, dir, ""); err != nil {
		return err
	}
	m.simple("aria2.removeDownloadResult", id)
	return nil
}

func (m *Manager) simple(method string, params ...any) error {
	rpc, err := m.client()
	if err != nil {
		return err
	}
	ctx, cancel := rpcContext()
	defer cancel()
	err = rpc.call(ctx, nil, method, params...)
	m.poll()
	return err
}

// PauseAll pauses every task that downloads, seeds or waits.
func (m *Manager) PauseAll() error { return m.simple("aria2.forcePauseAll") }

// ResumeAll starts every paused task again.
func (m *Manager) ResumeAll() error { return m.simple("aria2.unpauseAll") }

// Remove takes a task off the list, stopping it first. Its files stay
// unless deleteFiles.
func (m *Manager) Remove(id string, deleteFiles bool) error {
	rpc, err := m.client()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var st status
	if err := rpc.call(ctx, &st, "aria2.tellStatus", id, []string{"gid", "status", "dir", "files", "bittorrent", "infoHash", "following"}); err != nil {
		return err
	}
	if st.Status == "active" || st.Status == "waiting" || st.Status == "paused" {
		if err := rpc.call(ctx, nil, "aria2.forceRemove", id); err != nil {
			return err
		}
		// aria2 acknowledges before the download has stopped.
		for {
			var now status
			if rpc.call(ctx, &now, "aria2.tellStatus", id, []string{"status"}) != nil || (now.Status != "active" && now.Status != "waiting" && now.Status != "paused") {
				break
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(50 * time.Millisecond):
			}
		}
	}
	rpc.call(ctx, nil, "aria2.removeDownloadResult", id)
	// The download of a magnet link's file list, which this one followed,
	// goes with it.
	if st.Following != "" {
		rpc.call(ctx, nil, "aria2.removeDownloadResult", st.Following)
	}
	m.mu.Lock()
	saved := ""
	if meta := m.meta[id]; meta != nil {
		saved = meta.Torrent
	}
	m.mu.Unlock()
	if deleteFiles {
		m.deleteFiles(st)
		if saved != "" {
			os.Remove(saved)
		}
	}
	m.mu.Lock()
	delete(m.meta, st.Following)
	delete(m.meta, id)
	m.dirty = true
	m.mu.Unlock()
	m.poll()
	return nil
}

// deleteFiles deletes the files of a download, and the directories of a
// torrent they leave empty. Only what lies in the download's directory
// goes.
func (m *Manager) deleteFiles(st status) {
	remove := m.Config().Delete
	if remove == nil {
		remove = os.Remove
	}
	inside := func(file string) bool {
		rel, err := filepath.Rel(st.Dir, file)
		return err == nil && rel != "." && !strings.HasPrefix(rel, "..")
	}
	dirs := map[string]bool{}
	for _, f := range st.Files {
		if f.Path == "" || !inside(f.Path) {
			continue
		}
		if _, err := os.Stat(f.Path); err == nil {
			remove(f.Path)
		}
		os.Remove(f.Path + ".aria2")
		for dir := filepath.Dir(f.Path); inside(dir); dir = filepath.Dir(dir) {
			dirs[dir] = true
		}
	}
	if st.BitTorrent != nil && st.BitTorrent.Info != nil && st.BitTorrent.Info.Name != "" {
		os.Remove(filepath.Join(st.Dir, st.BitTorrent.Info.Name+".aria2"))
	}
	// The torrent aria2 saved beside the files, by its info hash.
	if st.InfoHash != "" {
		os.Remove(filepath.Join(st.Dir, st.InfoHash+".torrent"))
	}
	// Deepest first; a directory that still holds something stays.
	sorted := slices.SortedFunc(func(yield func(string) bool) {
		for dir := range dirs {
			if !yield(dir) {
				return
			}
		}
	}, func(a, b string) int { return len(b) - len(a) })
	for _, dir := range sorted {
		os.Remove(dir)
	}
}

// ClearCompleted takes the finished tasks off the list, keeping their
// files.
func (m *Manager) ClearCompleted() error {
	rpc, err := m.client()
	if err != nil {
		return err
	}
	var calls []rpcCall
	for _, t := range m.Tasks() {
		if t.Status == Completed {
			calls = append(calls, rpcCall{"aria2.removeDownloadResult", []any{t.ID}})
		}
	}
	if len(calls) == 0 {
		return nil
	}
	ctx, cancel := rpcContext()
	defer cancel()
	_, _, err = rpc.multicall(ctx, calls...)
	m.poll()
	return err
}

func (m *Manager) statePath() string { return filepath.Join(m.cfg.DataDir, "motrix-state.json") }

func (m *Manager) loadState() {
	data, err := os.ReadFile(m.statePath())
	if err != nil {
		return
	}
	var s saved
	if json.Unmarshal(data, &s) != nil {
		return
	}
	if s.Tasks != nil {
		m.meta = s.Tasks
	}
	if s.Daily != nil {
		m.daily = s.Daily
	}
	m.allTime = s.AllTime
}

// saveState writes the state file. The caller holds the lock.
func (m *Manager) saveState() {
	m.dirty, m.savedAt = false, time.Now()
	data, err := json.Marshal(saved{Tasks: m.meta, Daily: m.daily, AllTime: m.allTime})
	if err != nil {
		return
	}
	os.MkdirAll(m.cfg.DataDir, 0o755)
	tmp := m.statePath() + ".tmp"
	if os.WriteFile(tmp, data, 0o644) == nil {
		os.Rename(tmp, m.statePath())
	}
}
