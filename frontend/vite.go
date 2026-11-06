package frontend

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// ViteConfig holds Vite dev server and production asset resolution settings.
type ViteConfig struct {
	// DevServerURL is the base URL of the Vite dev server.
	// Defaults to "http://localhost:5173".
	DevServerURL string

	// Entry is the Vite entry module used by the SPA.
	// Defaults to "src/main.ts".
	Entry string

	// ManifestPath is the path to Vite's production manifest file.
	// Defaults to "./frontend/dist/.vite/manifest.json".
	ManifestPath string

	// AssetsURLPrefix is the public URL prefix under which built assets are served.
	// Defaults to "/frontend/dist".
	AssetsURLPrefix string
}

func (c ViteConfig) WithDefaults() ViteConfig {
	if c.DevServerURL == "" {
		c.DevServerURL = "http://localhost:5173"
	}
	if c.Entry == "" {
		c.Entry = "src/main.ts"
	}
	if c.ManifestPath == "" {
		c.ManifestPath = "./frontend/dist/.vite/manifest.json"
	}
	if c.AssetsURLPrefix == "" {
		c.AssetsURLPrefix = "/frontend/dist"
	}
	return c
}

// IsViteRunning probes the Vite dev server. Uses a 2s timeout.
func IsViteRunning(cfg ViteConfig) bool {
	cfg = cfg.WithDefaults()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	url := strings.TrimRight(cfg.DevServerURL, "/") + "/@vite/client"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()

	return resp.StatusCode == http.StatusOK
}

// Assets holds the resolved CSS and JS paths for the current environment.
type Assets struct {
	CSSPaths      []string
	JSPath        string
	IsDev         bool
	ViteClientURL string
}

type viteManifestEntry struct {
	File    string   `json:"file"`
	CSS     []string `json:"css"`
	Assets  []string `json:"assets"`
	IsEntry bool     `json:"isEntry"`
}

// ResolveAssets detects whether Vite is running and returns the correct asset
// paths for the current environment.
//
// In dev mode it returns Vite dev server URLs.
// In prod mode it resolves the configured entry from Vite's manifest.
func ResolveAssets(cfg ViteConfig) Assets {
	return resolveAssets(cfg, nil)
}

// resolveAssets is ResolveAssets with the manifest read from fsys, or from disk when fsys is nil.
func resolveAssets(cfg ViteConfig, fsys fs.FS) Assets {
	cfg = cfg.WithDefaults()

	if IsViteRunning(cfg) {
		base := strings.TrimRight(cfg.DevServerURL, "/")
		return Assets{
			JSPath:        base + "/" + strings.TrimLeft(cfg.Entry, "/"),
			IsDev:         true,
			ViteClientURL: base + "/@vite/client",
		}
	}

	manifest, err := readManifest(fsys, cfg.ManifestPath)
	if err != nil {
		return Assets{}
	}

	entry, ok := manifest[cfg.Entry]
	if !ok {
		return Assets{}
	}

	prefix := strings.TrimRight(cfg.AssetsURLPrefix, "/")
	assets := Assets{
		JSPath: prefix + "/" + strings.TrimLeft(filepath.ToSlash(entry.File), "/"),
	}

	for _, css := range entry.CSS {
		assets.CSSPaths = append(assets.CSSPaths, prefix+"/"+strings.TrimLeft(filepath.ToSlash(css), "/"))
	}

	return assets
}

func readManifest(fsys fs.FS, name string) (map[string]viteManifestEntry, error) {
	var data []byte

	var err error
	if fsys != nil {
		data, err = fs.ReadFile(fsys, path.Clean(filepath.ToSlash(name)))
	} else {
		data, err = os.ReadFile(name) //nolint:gosec // the configured manifest path
	}

	if err != nil {
		return nil, err
	}

	var manifest map[string]viteManifestEntry
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, err
	}

	return manifest, nil
}
