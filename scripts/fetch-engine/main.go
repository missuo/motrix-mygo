// Command fetch-engine downloads the aria2c builds Motrix ships (the
// motrixapp/aria2 fork, with SQLite persistence) into resources/, where
// mygo copies each platform's into its app. Archives and binaries are
// checked against the digests of engine.lock.json, which is Motrix's own.
//
//	go run ./scripts/fetch-engine            # this machine's platform
//	go run ./scripts/fetch-engine -all       # every platform mygo builds
package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
)

//go:embed engine.lock.json
var lockJSON []byte

type lock struct {
	Repo   string `json:"repo"`
	Tag    string `json:"tag"`
	Assets map[string]struct {
		File          string `json:"file"`
		Bin           string `json:"bin"`
		ArchiveSha256 string `json:"archiveSha256"`
		BinarySha256  string `json:"binarySha256"`
	} `json:"assets"`
}

// platforms maps Go's platforms to the lockfile's.
var platforms = map[string]string{
	"darwin-arm64":  "darwin-arm64",
	"darwin-amd64":  "darwin-x64",
	"linux-amd64":   "linux-x64",
	"linux-arm64":   "linux-arm64",
	"windows-amd64": "win32-x64",
}

func main() {
	all := flag.Bool("all", false, "fetch every platform")
	out := flag.String("out", "resources", "the resources directory")
	flag.Parse()
	var l lock
	if err := json.Unmarshal(lockJSON, &l); err != nil {
		log.Fatal(err)
	}
	for goPlatform, key := range platforms {
		if !*all && goPlatform != runtime.GOOS+"-"+runtime.GOARCH {
			continue
		}
		asset, ok := l.Assets[key]
		if !ok {
			log.Fatalf("the lockfile has no %s", key)
		}
		dest := filepath.Join(*out, goPlatform, "engine", asset.Bin)
		if data, err := os.ReadFile(dest); err == nil && digest(data) == asset.BinarySha256 {
			fmt.Println("up to date:", dest)
			continue
		}
		url := fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", l.Repo, l.Tag, asset.File)
		fmt.Println("fetching", url)
		archive, err := download(url)
		if err != nil {
			log.Fatal(err)
		}
		if got := digest(archive); got != asset.ArchiveSha256 {
			log.Fatalf("%s: archive digest %s, the lockfile has %s", asset.File, got, asset.ArchiveSha256)
		}
		bin, err := extract(archive, asset.File, asset.Bin)
		if err != nil {
			log.Fatal(err)
		}
		if got := digest(bin); got != asset.BinarySha256 {
			log.Fatalf("%s: binary digest %s, the lockfile has %s", asset.Bin, got, asset.BinarySha256)
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			log.Fatal(err)
		}
		if err := os.WriteFile(dest, bin, 0o755); err != nil {
			log.Fatal(err)
		}
		fmt.Println("wrote", dest)
	}
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func download(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 256<<20))
}

// extract returns the file named bin of a .tar.gz or .zip archive.
func extract(archive []byte, name, bin string) ([]byte, error) {
	if filepath.Ext(name) == ".zip" {
		zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			return nil, err
		}
		for _, f := range zr.File {
			if filepath.Base(f.Name) == bin {
				r, err := f.Open()
				if err != nil {
					return nil, err
				}
				defer r.Close()
				return io.ReadAll(r)
			}
		}
		return nil, fmt.Errorf("%s has no %s", name, bin)
	}
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err != nil {
			return nil, fmt.Errorf("%s has no %s", name, bin)
		}
		if filepath.Base(h.Name) == bin {
			return io.ReadAll(tr)
		}
	}
}
