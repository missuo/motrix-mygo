package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"motrix-mygo/engine"
)

// settings is what the user chose, kept in settings.json under Motrix's
// names.
type settings struct {
	Engine     engine.Settings    `json:"engine"`
	App        appSettings        `json:"app"`
	Proxy      proxySettings      `json:"proxy"`
	Tracker    trackerSettings    `json:"tracker"`
	SpeedLimit speedLimitSettings `json:"speedLimit"`
	UI         uiSettings         `json:"ui"`
}

type appSettings struct {
	LaunchAtStartup bool   `json:"launchAtStartup"`
	Theme           string `json:"theme"`        // "system", "light" or "dark"
	SidebarColor    string `json:"sidebarColor"` // one of sidebarColors
	Language        string `json:"language"`     // "system", "en" or "zh"
	DefaultSaveDir  string `json:"defaultSaveDir"`
	// FileDeletionMode is "trash" or "permanent".
	FileDeletionMode       string `json:"fileDeletionMode"`
	NotifyOnComplete       bool   `json:"notifyOnComplete"`
	NotifyOnError          bool   `json:"notifyOnError"`
	NotifyInAppOnComplete  bool   `json:"notifyInAppOnComplete"`
	NotifyInAppOnError     bool   `json:"notifyInAppOnError"`
	AutofillClipboardLinks bool   `json:"autofillClipboardLinks"`
	WarnBeforeQuit         bool   `json:"warnBeforeQuit"`
}

type proxySettings struct {
	Enabled  bool     `json:"enabled"`
	Protocol string   `json:"protocol"` // "http" or "https"
	Host     string   `json:"host"`
	Port     int      `json:"port"`
	User     string   `json:"user"`
	Password string   `json:"password"`
	Bypass   []string `json:"bypass"`
	Scopes   struct {
		Download       bool `json:"download"`
		UpdateTrackers bool `json:"updateTrackers"`
	} `json:"scopes"`
}

type trackerSource struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	URL     string `json:"url"`
	Builtin bool   `json:"builtin"`
	Enabled bool   `json:"enabled"`
	CDN     bool   `json:"cdn"`
}

type trackerSettings struct {
	AutoSync          bool            `json:"autoSync"`
	SyncIntervalHours int             `json:"syncIntervalHours"`
	Sources           []trackerSource `json:"sources"`
	MaxTrackerCount   int             `json:"maxTrackerCount"`
}

type speedLimitProfile struct {
	Download int64 `json:"download"` // bytes per second, 0 is unlimited
	Upload   int64 `json:"upload"`
}

type speedLimitSettings struct {
	Base speedLimitProfile `json:"base"`
	Alt  speedLimitProfile `json:"alt"`
	// Turtle is "off" for the standard limits, "on" for the low speed
	// ones, "auto" for what the rules of Auto decide.
	Turtle string `json:"turtle"`
	Auto   struct {
		Schedule struct {
			Enabled bool   `json:"enabled"`
			From    string `json:"from"` // "HH:MM"
			To      string `json:"to"`   // before From, it is the next day's
			Days    []int  `json:"days"` // 0 is Sunday; none is every day
		} `json:"schedule"`
		Adaptive struct {
			Enabled         bool  `json:"enabled"`
			LinkDown        int64 `json:"linkDown"` // bytes per second
			LinkUp          int64 `json:"linkUp"`
			HeadroomPercent int   `json:"headroomPercent"` // the share Motrix uses
		} `json:"adaptive"`
	} `json:"auto"`
}

// uiSettings is how the user left the window.
type uiSettings struct {
	HiddenColumns  []string `json:"hiddenColumns,omitempty"`
	SidebarHidden  bool     `json:"sidebarHidden,omitempty"`
	InspectorShown bool     `json:"inspectorShown,omitempty"`
}

var builtinTrackerSources = []trackerSource{
	{"ngosang-best", "ngosang/trackerslist (best)", "https://raw.githubusercontent.com/ngosang/trackerslist/master/trackers_best.txt", true, true, false},
	{"ngosang-best-cdn", "ngosang/trackerslist (best, CDN)", "https://cdn.jsdelivr.net/gh/ngosang/trackerslist/trackers_best.txt", true, false, true},
	{"xiu2-best", "XIU2/TrackersListCollection (best)", "https://raw.githubusercontent.com/XIU2/TrackersListCollection/master/best.txt", true, true, false},
	{"xiu2-best-cdn", "XIU2/TrackersListCollection (best, CDN)", "https://cdn.jsdelivr.net/gh/XIU2/TrackersListCollection/best.txt", true, false, true},
}

// defaultSettings are Motrix's defaults.
func defaultSettings(downloads string) settings {
	var s settings
	s.Engine = engine.DefaultSettings()
	s.App = appSettings{
		Theme: "system", SidebarColor: "cyan", Language: "system", DefaultSaveDir: downloads,
		FileDeletionMode: "trash", NotifyOnComplete: true, NotifyOnError: true,
		NotifyInAppOnComplete: true, NotifyInAppOnError: true,
		AutofillClipboardLinks: true, WarnBeforeQuit: true,
	}
	s.Proxy.Protocol, s.Proxy.Port = "http", 8080
	s.Tracker = trackerSettings{AutoSync: true, SyncIntervalHours: 12, Sources: slices.Clone(builtinTrackerSources), MaxTrackerCount: 50}
	s.SpeedLimit.Alt = speedLimitProfile{Download: 512 << 10, Upload: 64 << 10}
	s.SpeedLimit.Turtle = "off"
	s.SpeedLimit.Auto.Schedule.From, s.SpeedLimit.Auto.Schedule.To = "23:00", "07:00"
	s.SpeedLimit.Auto.Adaptive.HeadroomPercent = 80
	return s
}

var clockTime = regexp.MustCompile(`^(?:[01]\d|2[0-3]):[0-5]\d$`)

// normalize puts every value within the range Motrix accepts, a value
// outside it becoming the default.
func (s *settings) normalize(downloads string) {
	d := defaultSettings(downloads)
	s.Engine.Normalize()
	a := &s.App
	if !slices.Contains([]string{"system", "light", "dark"}, a.Theme) {
		a.Theme = d.App.Theme
	}
	if !slices.Contains([]string{"system", "en", "zh"}, a.Language) {
		a.Language = d.App.Language
	}
	a.SidebarColor = findSidebarColor(a.SidebarColor).name
	if a.DefaultSaveDir == "" {
		a.DefaultSaveDir = downloads
	}
	if a.FileDeletionMode != "permanent" {
		a.FileDeletionMode = "trash"
	}
	p := &s.Proxy
	if p.Protocol != "https" {
		p.Protocol = "http"
	}
	if p.Port < 1 || p.Port > 65535 {
		p.Port = 8080
	}
	t := &s.Tracker
	t.SyncIntervalHours = min(max(t.SyncIntervalHours, 1), 168)
	t.MaxTrackerCount = min(max(t.MaxTrackerCount, 5), 200)
	if t.Sources == nil {
		t.Sources = slices.Clone(builtinTrackerSources)
	}
	l := &s.SpeedLimit
	for _, v := range []*int64{&l.Base.Download, &l.Base.Upload, &l.Alt.Download, &l.Alt.Upload, &l.Auto.Adaptive.LinkDown, &l.Auto.Adaptive.LinkUp} {
		*v = max(*v, 0)
	}
	if !slices.Contains([]string{"off", "on", "auto"}, l.Turtle) {
		l.Turtle = "off"
	}
	if !clockTime.MatchString(l.Auto.Schedule.From) {
		l.Auto.Schedule.From = "23:00"
	}
	if !clockTime.MatchString(l.Auto.Schedule.To) {
		l.Auto.Schedule.To = "07:00"
	}
	l.Auto.Schedule.Days = slices.DeleteFunc(l.Auto.Schedule.Days, func(d int) bool { return d < 0 || d > 6 })
	// Empty lists are nil, so that settings compare equal however made.
	if len(l.Auto.Schedule.Days) == 0 {
		l.Auto.Schedule.Days = nil
	}
	if len(p.Bypass) == 0 {
		p.Bypass = nil
	}
	if h := l.Auto.Adaptive.HeadroomPercent; h < 1 || h > 100 {
		l.Auto.Adaptive.HeadroomPercent = 80
	}
}

// loadSettings reads path over the defaults: what the file lacks keeps its
// default.
func loadSettings(path, downloads string) settings {
	s := defaultSettings(downloads)
	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, &s)
	}
	s.normalize(downloads)
	return s
}

func (s settings) save(path string) error {
	if path == "" {
		return nil
	}
	data, err := json.MarshalIndent(s, "", " ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	// The file holds the proxy's password and the RPC secret.
	return os.WriteFile(path, data, 0o600)
}

// minCap is the lowest of the limits that are set, 0 when none is.
func minCap(values ...int64) int64 {
	var lowest int64
	for _, v := range values {
		if v > 0 && (lowest == 0 || v < lowest) {
			lowest = v
		}
	}
	return lowest
}

func clockMinutes(hhmm string) int {
	var h, m int
	fmt.Sscanf(hhmm, "%d:%d", &h, &m)
	return h*60 + m
}

// scheduled tells whether now is within the schedule of the low speed
// limits.
func (l speedLimitSettings) scheduled(now time.Time) bool {
	sch := l.Auto.Schedule
	if len(sch.Days) > 0 && !slices.Contains(sch.Days, int(now.Weekday())) {
		return false
	}
	cur, from, to := now.Hour()*60+now.Minute(), clockMinutes(sch.From), clockMinutes(sch.To)
	switch {
	case from == to:
		return false
	case from < to:
		return cur >= from && cur < to
	}
	return cur >= from || cur < to // past midnight
}

// effective are the limits in force now: the standard ones, the lowest of
// them and the low speed ones in low speed mode, and in automatic mode the
// lowest of those the rules bring in.
func (l speedLimitSettings) effective(now time.Time) engine.Limits {
	switch l.Turtle {
	case "off":
		return engine.Limits{Download: l.Base.Download, Upload: l.Base.Upload}
	case "on":
		return engine.Limits{Download: minCap(l.Base.Download, l.Alt.Download), Upload: minCap(l.Base.Upload, l.Alt.Upload)}
	}
	downs, ups := []int64{l.Base.Download}, []int64{l.Base.Upload}
	if l.Auto.Schedule.Enabled && l.scheduled(now) {
		downs, ups = append(downs, l.Alt.Download), append(ups, l.Alt.Upload)
	}
	if ad := l.Auto.Adaptive; ad.Enabled {
		downs = append(downs, ad.LinkDown*int64(ad.HeadroomPercent)/100)
		ups = append(ups, ad.LinkUp*int64(ad.HeadroomPercent)/100)
	}
	return engine.Limits{Download: minCap(downs...), Upload: minCap(ups...)}
}

// engineProxy is the proxy downloads go through, nil for none.
func (p proxySettings) engineProxy() *engine.Proxy {
	if !p.Enabled || !p.Scopes.Download || strings.TrimSpace(p.Host) == "" {
		return nil
	}
	var bypass []string
	for _, b := range p.Bypass {
		if b = strings.TrimSpace(b); b != "" {
			bypass = append(bypass, b)
		}
	}
	return &engine.Proxy{
		URL:  fmt.Sprintf("%s://%s:%d", p.Protocol, strings.TrimSpace(p.Host), p.Port),
		User: p.User, Password: p.Password, NoProxy: bypass,
	}
}
