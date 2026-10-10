package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// trackerList is the trackers fetched from the sources, kept in
// trackers.json.
type trackerList struct {
	Trackers []string  `json:"trackers"`
	SyncedAt time.Time `json:"syncedAt"`
	// Errors are the sources that could not be fetched, by their label.
	Errors map[string]string `json:"errors,omitempty"`
}

func loadTrackers(path string) trackerList {
	var l trackerList
	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, &l)
	}
	return l
}

func (l trackerList) save(path string) {
	if path == "" {
		return
	}
	if data, err := json.Marshal(l); err == nil {
		os.WriteFile(path, data, 0o644)
	}
}

// parseTrackers returns the tracker URLs of a list, one a line.
func parseTrackers(text string) []string {
	var out []string
	sc := bufio.NewScanner(strings.NewReader(text))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		u, err := url.Parse(line)
		if err != nil || u.Host == "" {
			continue
		}
		switch u.Scheme {
		case "http", "https", "udp", "ws", "wss":
			out = append(out, line)
		}
	}
	return out
}

// fetchTrackers fetches the enabled sources and returns their trackers,
// without duplicates, the first limit of them.
func fetchTrackers(ctx context.Context, client *http.Client, sources []trackerSource, limit int) trackerList {
	list := trackerList{SyncedAt: time.Now(), Errors: map[string]string{}}
	seen := map[string]bool{}
	for _, src := range sources {
		if !src.Enabled {
			continue
		}
		text, err := fetchText(ctx, client, src.URL)
		if err != nil {
			list.Errors[src.Label] = err.Error()
			continue
		}
		for _, tracker := range parseTrackers(text) {
			if !seen[tracker] && len(list.Trackers) < limit {
				seen[tracker] = true
				list.Trackers = append(list.Trackers, tracker)
			}
		}
	}
	return list
}

func fetchText(ctx context.Context, client *http.Client, link string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s", resp.Status)
	}
	var b strings.Builder
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() && b.Len() < 1<<20 {
		b.WriteString(sc.Text())
		b.WriteByte('\n')
	}
	return b.String(), sc.Err()
}

// trackerClient is the HTTP client tracker lists are fetched with: through
// the proxy when it is for them.
func (p proxySettings) trackerClient() *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if p.Enabled && p.Scopes.UpdateTrackers && strings.TrimSpace(p.Host) != "" {
		u := &url.URL{Scheme: p.Protocol, Host: fmt.Sprintf("%s:%d", strings.TrimSpace(p.Host), p.Port)}
		if p.User != "" {
			u.User = url.UserPassword(p.User, p.Password)
		}
		tr.Proxy = http.ProxyURL(u)
	}
	return &http.Client{Transport: tr, Timeout: 20 * time.Second}
}
