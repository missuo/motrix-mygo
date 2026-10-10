package engine

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

var (
	seedTimeLine  = regexp.MustCompile(`(?m)^[\t ]*seed-time[\t ]*=.*(?:\r?\n|$)`)
	inputFileLine = regexp.MustCompile(`(?m)^\s*input-file\s*=`)
)

// paths are the files of a data directory.
type paths struct {
	userConf, runtimeConf string
	session, db           string
	dht, dht6, log        string
}

func pathsIn(dir string) paths {
	return paths{
		userConf:    filepath.Join(dir, "aria2.conf"),
		runtimeConf: filepath.Join(dir, "aria2.runtime.conf"),
		session:     filepath.Join(dir, "aria2.session"),
		db:          filepath.Join(dir, "aria2.db"),
		dht:         filepath.Join(dir, "dht.dat"),
		dht6:        filepath.Join(dir, "dht6.dat"),
		log:         filepath.Join(dir, "logs", "aria2.log"),
	}
}

// ensureUserConfig copies the template to the data directory on the first
// start, and returns the configuration aria2 reads and whether it names an
// input file. Seeding time is set for each task: a global one in the file
// could not be cleared, so aria2 reads a copy without it.
func ensureUserConfig(template, dir string) (conf string, inputFile bool, err error) {
	p := pathsIn(dir)
	if err := os.MkdirAll(filepath.Dir(p.log), 0o755); err != nil {
		return "", false, err
	}
	if _, err := os.Stat(p.userConf); err != nil {
		data, err := os.ReadFile(template)
		if err != nil {
			return "", false, fmt.Errorf("the aria2.conf template: %w", err)
		}
		if err := os.WriteFile(p.userConf, data, 0o644); err != nil {
			return "", false, err
		}
	}
	source, err := os.ReadFile(p.userConf)
	if err != nil {
		return "", false, err
	}
	inputFile = inputFileLine.Match(source)
	filtered := seedTimeLine.ReplaceAll(source, nil)
	if len(filtered) == len(source) {
		return p.userConf, inputFile, nil
	}
	if err := os.WriteFile(p.runtimeConf, filtered, 0o600); err != nil {
		return "", false, err
	}
	return p.runtimeConf, inputFile, nil
}

// argOptions are what buildArgs needs besides the Config.
type argOptions struct {
	conf string
	// hasSQLite tells whether the binary has SQLite persistence, as the
	// Motrix build does: others start without its flags.
	hasSQLite bool
	// loadTextSession loads aria2.session, the fallback while SQLite
	// persistence is off.
	loadTextSession bool
	// confHasInputFile tells whether the user's aria2.conf names one.
	confHasInputFile bool
	// stopWithPID makes aria2 quit when this process is gone.
	stopWithPID int
}

// buildArgs returns aria2c's arguments, in Motrix's layers: the user's
// aria2.conf, what binds the engine to the app, what the settings tune, the
// proxy, and last the invariants of the product. aria2 honors the last
// occurrence of an option, so nothing earlier, nor the user's file, undoes
// the invariants.
func buildArgs(cfg Config, o argOptions) ([]string, error) {
	s := cfg.Settings
	p := pathsIn(cfg.DataDir)
	args := []string{"--conf-path=" + o.conf}

	// What binds the engine to the app.
	args = append(args,
		"--enable-rpc=true",
		fmt.Sprintf("--rpc-allow-origin-all=%t", s.RPCSecret != ""),
		"--rpc-listen-all=false",
		fmt.Sprintf("--rpc-listen-port=%d", s.RPCPort),
	)
	if s.RPCSecret != "" {
		args = append(args, "--rpc-secret="+s.RPCSecret)
	}
	args = append(args,
		"--dir="+cfg.SaveDir,
		fmt.Sprintf("--listen-port=%d", s.ListenPort),
		fmt.Sprintf("--dht-listen-port=%d", s.DHTListenPort),
		fmt.Sprintf("--enable-dht=%t", s.DHTEnabled),
		fmt.Sprintf("--enable-dht6=%t", s.DHTEnabled),
		"--dht-file-path="+p.dht,
		"--dht-file-path6="+p.dht6,
		"--save-session="+p.session,
	)
	sqliteActive := o.hasSQLite && s.SQLite3Persistence
	if !sqliteActive {
		args = append(args, "--auto-save-interval=10")
	}
	if o.loadTextSession && !sqliteActive {
		args = append(args, "--input-file="+p.session)
	} else if o.confHasInputFile {
		args = append(args, "--input-file=")
	}
	if o.hasSQLite {
		db := strings.TrimSpace(s.SQLite3DBPath)
		if db == "" {
			db = p.db
		}
		args = append(args,
			fmt.Sprintf("--enable-sqlite3-persistence=%t", s.SQLite3Persistence),
			"--sqlite3-db-path="+db,
			fmt.Sprintf("--sqlite3-history-limit=%d", s.SQLite3HistoryLimit),
		)
	}

	// What the settings tune.
	args = appendOptions(args, tunable(cfg))
	args = append(args,
		fmt.Sprintf("--bt-enable-lpd=%t", s.BTEnableLPD),
		fmt.Sprintf("--disk-cache=%d", s.DiskCache),
		fmt.Sprintf("--save-session-interval=%d", s.SessionSaveInterval),
	)

	// The proxy: every other source of one, in aria2.conf or the
	// environment, is overridden, so the route is the one set here.
	args = appendOptions(args, proxyOptions(cfg.Proxy))
	args = append(args,
		"--http-proxy=", "--http-proxy-user=", "--http-proxy-passwd=",
		"--https-proxy=", "--https-proxy-user=", "--https-proxy-passwd=",
		"--ftp-proxy=", "--ftp-proxy-user=", "--ftp-proxy-passwd=",
		"--proxy-method=get",
	)

	// The invariants of the product.
	args = append(args,
		"--bt-save-metadata=true",
		"--bt-metadata-only=false",
		"--auto-file-renaming=false",
		"--allow-overwrite=false",
		"--rpc-save-upload-metadata=true",
		"--force-save=true",
		"--continue=false",
		"--pause=false",
		"--pause-metadata=false",
		"--bt-seed-unverified=false",
		"--http-accept-gzip=true",
		"--no-want-digest-header=false",
		"--bt-remove-unselected-file=true",
	)
	if o.stopWithPID > 0 {
		args = append(args, fmt.Sprintf("--stop-with-process=%d", o.stopWithPID))
	}
	for _, arg := range args {
		if strings.ContainsAny(arg, "\r\n") {
			return nil, fmt.Errorf("aria2 option values must not contain line breaks")
		}
	}
	return args, nil
}

// appendOptions appends options as arguments, in the order of their names.
func appendOptions(args []string, options map[string]string) []string {
	for _, name := range slices.Sorted(maps.Keys(options)) {
		args = append(args, "--"+name+"="+options[name])
	}
	return args
}

// tunable are the options of the settings that aria2 also takes while it
// runs, through aria2.changeGlobalOption.
func tunable(cfg Config) map[string]string {
	s := cfg.Settings
	return map[string]string{
		"max-concurrent-downloads":   fmt.Sprint(s.MaxConcurrentDownloads),
		"max-overall-download-limit": fmt.Sprint(cfg.Limits.Download),
		"max-overall-upload-limit":   fmt.Sprint(cfg.Limits.Upload),
		"max-connection-per-server":  fmt.Sprint(s.MaxConnectionPerServer),
		"split":                      fmt.Sprint(s.Split),
		"min-split-size":             fmt.Sprint(s.MinSplitSize),
		"user-agent":                 s.UserAgent,
		"connect-timeout":            fmt.Sprint(s.ConnectTimeout),
		"timeout":                    fmt.Sprint(s.SocketTimeout),
		"max-tries":                  fmt.Sprint(s.MaxTries),
		"retry-wait":                 fmt.Sprint(s.RetryWait),
		"lowest-speed-limit":         fmt.Sprint(s.LowestSpeedLimit),
		// "auto" starts with aria2's own resolver, as "engine" does.
		"async-dns":       fmt.Sprint(s.DNSMode != "system"),
		"bt-max-peers":    fmt.Sprint(s.BTMaxPeers),
		"seed-ratio":      fmt.Sprint(s.SeedRatio),
		"file-allocation": s.FileAllocation,
		"remote-time":     fmt.Sprint(s.RemoteTime),
	}
}

func proxyOptions(p *Proxy) map[string]string {
	if p == nil {
		p = &Proxy{}
	}
	return map[string]string{
		"all-proxy":        p.URL,
		"all-proxy-user":   p.User,
		"all-proxy-passwd": p.Password,
		"no-proxy":         strings.Join(p.NoProxy, ","),
	}
}

// needsRestart tells whether going from one configuration to another takes
// a new aria2 process: what changed is not an option aria2 takes while it
// runs.
func needsRestart(from, to Config) bool {
	a, b := from.Settings, to.Settings
	return from.Binary != to.Binary || from.DataDir != to.DataDir || from.SaveDir != to.SaveDir ||
		a.RPCPort != b.RPCPort || a.RPCSecret != b.RPCSecret ||
		a.ListenPort != b.ListenPort || a.DHTListenPort != b.DHTListenPort || a.DHTEnabled != b.DHTEnabled ||
		a.BTEnableLPD != b.BTEnableLPD || a.DiskCache != b.DiskCache || a.SessionSaveInterval != b.SessionSaveInterval ||
		a.SQLite3Persistence != b.SQLite3Persistence || a.SQLite3DBPath != b.SQLite3DBPath ||
		a.SQLite3HistoryLimit != b.SQLite3HistoryLimit
}
