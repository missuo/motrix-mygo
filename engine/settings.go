// Package engine runs aria2c, the engine Motrix downloads with, and drives
// it over JSON-RPC: it builds aria2's arguments from Motrix's engine
// settings, starts and restarts the process, and turns what aria2 tells
// into tasks. It knows nothing about the UI.
package engine

import "strings"

const mb = 1 << 20

// MaxConnectionsPerServer is the most connections the Motrix build of
// aria2 opens to one server.
const MaxConnectionsPerServer = 64

// Settings are Motrix's engine settings, with its names in JSON.
type Settings struct {
	RPCPort       int    `json:"rpcPort"`
	RPCSecret     string `json:"rpcSecret"`
	ListenPort    int    `json:"listenPort"`
	DHTListenPort int    `json:"dhtListenPort"`
	DHTEnabled    bool   `json:"dhtEnabled"`

	// PerformanceProfile is "auto", "balanced", "high", "maximum" or
	// "custom"; all but custom set the four values after it.
	PerformanceProfile     string `json:"performanceProfile"`
	MaxConcurrentDownloads int    `json:"maxConcurrentDownloads"`
	MaxConnectionPerServer int    `json:"maxConnectionPerServer"`
	Split                  int    `json:"split"`
	MinSplitSize           int64  `json:"minSplitSize"` // bytes
	DiskCache              int64  `json:"diskCache"`    // bytes

	UserAgent        string `json:"userAgent"`
	ConnectTimeout   int    `json:"connectTimeout"`   // seconds
	SocketTimeout    int    `json:"socketTimeout"`    // seconds
	MaxTries         int    `json:"maxTries"`         // 0 is unlimited
	RetryWait        int    `json:"retryWait"`        // seconds
	LowestSpeedLimit int64  `json:"lowestSpeedLimit"` // bytes per second
	// DNSMode is "auto", "system" or "engine".
	DNSMode string `json:"dnsMode"`

	BTMaxPeers  int     `json:"btMaxPeers"`
	BTEnableLPD bool    `json:"btEnableLpd"`
	SeedRatio   float64 `json:"seedRatio"`
	SeedTime    int     `json:"seedTime"` // minutes, 0 is unlimited

	// FileAllocation is "none", "prealloc", "trunc" or "falloc".
	FileAllocation      string `json:"fileAllocation"`
	RemoteTime          bool   `json:"remoteTime"`
	SessionSaveInterval int    `json:"sessionSaveInterval"` // seconds

	SQLite3Persistence  bool   `json:"sqlite3Persistence"`
	SQLite3DBPath       string `json:"sqlite3DbPath"`
	SQLite3HistoryLimit int    `json:"sqlite3HistoryLimit"` // -1 keeps all

	MagnetResolveTimeout int `json:"magnetResolveTimeout"` // seconds
}

// Tuning is what a performance profile sets.
type Tuning struct {
	MaxConnectionPerServer int
	Split                  int
	MinSplitSize           int64
	DiskCache              int64
}

// Profiles are Motrix's performance profiles, in the order it lists them.
var Profiles = []struct {
	ID string
	Tuning
}{
	{"auto", Tuning{MaxConnectionsPerServer, 16, 4 * mb, 32 * mb}},
	{"balanced", Tuning{16, 16, 10 * mb, 32 * mb}},
	{"high", Tuning{32, 32, 4 * mb, 64 * mb}},
	{"maximum", Tuning{MaxConnectionsPerServer, 64, 1 * mb, 64 * mb}},
}

// DefaultSettings are Motrix's defaults.
func DefaultSettings() Settings {
	s := Settings{
		RPCPort: 16800, ListenPort: 6881, DHTListenPort: 6881, DHTEnabled: true,
		PerformanceProfile: "auto", MaxConcurrentDownloads: 10,
		UserAgent: "Motrix/2.0", ConnectTimeout: 30, SocketTimeout: 30, MaxTries: 5, RetryWait: 10,
		DNSMode:    "auto",
		BTMaxPeers: 128, BTEnableLPD: true, SeedRatio: 1, SeedTime: 60,
		FileAllocation: "none", SessionSaveInterval: 15,
		SQLite3Persistence: true, SQLite3HistoryLimit: -1,
		MagnetResolveTimeout: 600,
	}
	s.Normalize()
	return s
}

func clamp[T int | int64 | float64](v, lo, hi, fallback T) T {
	if v < lo || v > hi {
		return fallback
	}
	return v
}

func oneOf(v, fallback string, allowed ...string) string {
	for _, a := range allowed {
		if v == a {
			return v
		}
	}
	return fallback
}

// Normalize puts every value within the range Motrix accepts, a value
// outside it becoming the default, and applies the performance profile.
func (s *Settings) Normalize() {
	auto := Profiles[0].Tuning
	s.RPCPort = clamp(s.RPCPort, 1024, 65535, 16800)
	s.RPCSecret = stripControl(s.RPCSecret)
	s.ListenPort = clamp(s.ListenPort, 1024, 65535, 6881)
	s.DHTListenPort = clamp(s.DHTListenPort, 1024, 65535, 6881)
	s.PerformanceProfile = oneOf(s.PerformanceProfile, "auto", "auto", "balanced", "high", "maximum", "custom")
	s.MaxConcurrentDownloads = clamp(s.MaxConcurrentDownloads, 1, 100, 10)
	s.MaxConnectionPerServer = clamp(s.MaxConnectionPerServer, 1, MaxConnectionsPerServer, auto.MaxConnectionPerServer)
	s.Split = clamp(s.Split, 1, 128, auto.Split)
	s.MinSplitSize = clamp(s.MinSplitSize, 1*mb, 1024*mb, auto.MinSplitSize)
	s.DiskCache = clamp(s.DiskCache, 0, 128*mb, auto.DiskCache)
	if s.UserAgent = stripControl(s.UserAgent); s.UserAgent == "" {
		s.UserAgent = "Motrix/2.0"
	}
	s.ConnectTimeout = clamp(s.ConnectTimeout, 1, 600, 30)
	s.SocketTimeout = clamp(s.SocketTimeout, 1, 600, 30)
	s.MaxTries = clamp(s.MaxTries, 0, 100, 5)
	s.RetryWait = clamp(s.RetryWait, 0, 300, 10)
	s.LowestSpeedLimit = max(s.LowestSpeedLimit, 0)
	s.DNSMode = oneOf(s.DNSMode, "auto", "auto", "system", "engine")
	s.BTMaxPeers = clamp(s.BTMaxPeers, 1, 1000, 128)
	s.SeedRatio = clamp(s.SeedRatio, 0, 100, 1)
	s.SeedTime = clamp(s.SeedTime, 0, 525600, 60)
	s.FileAllocation = oneOf(s.FileAllocation, "none", "none", "prealloc", "trunc", "falloc")
	s.SessionSaveInterval = clamp(s.SessionSaveInterval, 10, 3600, 15)
	s.SQLite3DBPath = strings.ReplaceAll(s.SQLite3DBPath, "\x00", "")
	s.SQLite3HistoryLimit = clamp(s.SQLite3HistoryLimit, -1, 1_000_000, -1)
	s.MagnetResolveTimeout = clamp(s.MagnetResolveTimeout, 30, 900, 600)
	for _, p := range Profiles {
		if p.ID == s.PerformanceProfile {
			s.MaxConnectionPerServer, s.Split = p.MaxConnectionPerServer, p.Split
			s.MinSplitSize, s.DiskCache = p.MinSplitSize, p.DiskCache
		}
	}
}

// stripControl takes control characters out of a value: aria2 reads its
// options as lines, which a line break in one would split in two.
func stripControl(s string) string {
	return strings.Map(func(r rune) rune {
		if r <= 0x1f || r == 0x7f {
			return -1
		}
		return r
	}, s)
}

// Proxy is the proxy aria2 downloads through.
type Proxy struct {
	// URL is "http://host:port" or "https://host:port".
	URL            string
	User, Password string
	// NoProxy are the hosts, domains and CIDRs connected to directly.
	NoProxy []string
}

// Limits cap the speed of all tasks together, in bytes per second; 0 is
// unlimited.
type Limits struct{ Download, Upload int64 }

// Config is all a Manager needs to run aria2.
type Config struct {
	// Binary is the aria2c to run, Template the aria2.conf copied into
	// DataDir on the first start, which the user may edit there.
	Binary, Template string
	// DataDir holds aria2.conf, the session, the database and the log.
	DataDir string
	// SaveDir is where downloads go that name no directory.
	SaveDir  string
	Settings Settings
	Proxy    *Proxy
	Limits   Limits
	// Trackers are added to every BitTorrent download.
	Trackers []string
	// Delete removes a file of a task removed with its files; os.Remove
	// when nil.
	Delete func(path string) error
}
