package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"
)

// rpcClient calls aria2's JSON-RPC interface over HTTP on the loopback.
type rpcClient struct {
	url    string
	secret string
	http   *http.Client
	id     atomic.Int64
}

func newRPCClient(port int, secret string) *rpcClient {
	return &rpcClient{
		url:    fmt.Sprintf("http://127.0.0.1:%d/jsonrpc", port),
		secret: secret,
		// The loopback needs no proxy, whatever the environment says.
		http: &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{Proxy: nil}},
	}
}

// rpcError is an error aria2 answered with.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return "aria2: " + e.Message }

type rpcCall struct {
	Method string `json:"methodName"`
	Params []any  `json:"params"`
}

// params puts the secret before the parameters, as aria2 takes it.
func (c *rpcClient) params(params []any) []any {
	if c.secret == "" {
		if params == nil {
			return []any{}
		}
		return params
	}
	return append([]any{"token:" + c.secret}, params...)
}

// call calls a method and decodes its result into result, unless nil.
func (c *rpcClient) call(ctx context.Context, result any, method string, params ...any) error {
	if method != "system.multicall" {
		params = c.params(params)
	}
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": strconv.FormatInt(c.id.Add(1), 10), "method": method, "params": params,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var reply struct {
		Result json.RawMessage `json:"result"`
		Error  *rpcError       `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&reply); err != nil {
		return fmt.Errorf("aria2 answered %s: %w", resp.Status, err)
	}
	if reply.Error != nil {
		return reply.Error
	}
	if result == nil {
		return nil
	}
	return json.Unmarshal(reply.Result, result)
}

// multicall calls several methods in one request. It returns the result of
// each, or its error.
func (c *rpcClient) multicall(ctx context.Context, calls ...rpcCall) ([]json.RawMessage, []error, error) {
	for i := range calls {
		calls[i].Params = c.params(calls[i].Params)
	}
	var raw []json.RawMessage
	if err := c.call(ctx, &raw, "system.multicall", calls); err != nil {
		return nil, nil, err
	}
	if len(raw) != len(calls) {
		return nil, nil, fmt.Errorf("aria2 answered %d of %d calls", len(raw), len(calls))
	}
	results := make([]json.RawMessage, len(raw))
	errs := make([]error, len(raw))
	for i, r := range raw {
		// A result comes in an array of one, an error as an object.
		var one []json.RawMessage
		if json.Unmarshal(r, &one) == nil && len(one) == 1 {
			results[i] = one[0]
			continue
		}
		var e rpcError
		if json.Unmarshal(r, &e) == nil && e.Message != "" {
			errs[i] = &e
		} else {
			errs[i] = fmt.Errorf("aria2: unexpected answer %s", r)
		}
	}
	return results, errs, nil
}

// status is a download as aria2.tellStatus tells it. aria2 writes numbers
// as strings.
type status struct {
	GID             string   `json:"gid"`
	Status          string   `json:"status"`
	TotalLength     int64    `json:"totalLength,string"`
	CompletedLength int64    `json:"completedLength,string"`
	UploadLength    int64    `json:"uploadLength,string"`
	DownloadSpeed   int64    `json:"downloadSpeed,string"`
	UploadSpeed     int64    `json:"uploadSpeed,string"`
	Connections     int      `json:"connections,string"`
	NumSeeders      int      `json:"numSeeders,string"`
	Seeder          string   `json:"seeder"`
	ErrorCode       string   `json:"errorCode"`
	ErrorMessage    string   `json:"errorMessage"`
	Dir             string   `json:"dir"`
	InfoHash        string   `json:"infoHash"`
	FollowedBy      []string `json:"followedBy"`
	Following       string   `json:"following"`
	Files           []struct {
		Path string `json:"path"`
		URIs []struct {
			URI string `json:"uri"`
		} `json:"uris"`
	} `json:"files"`
	BitTorrent *struct {
		Info *struct {
			Name string `json:"name"`
		} `json:"info"`
	} `json:"bittorrent"`
}

// statusKeys are the keys of a status asked of aria2.
var statusKeys = []string{
	"gid", "status", "totalLength", "completedLength", "uploadLength", "downloadSpeed", "uploadSpeed",
	"connections", "numSeeders", "seeder", "errorCode", "errorMessage", "dir", "infoHash",
	"followedBy", "following", "files", "bittorrent",
}

type globalStat struct {
	DownloadSpeed int64 `json:"downloadSpeed,string"`
	UploadSpeed   int64 `json:"uploadSpeed,string"`
}
