package frontend

import (
	"context"
	"net/http"
	"os"
	"strings"
	"time"
)

// ViteConfig holds Vite dev server configuration.
type ViteConfig struct {
	// Port is the Vite dev server port. Defaults to 5173.
	Port string
	// EntryPoint is the file Vite serves as the main entry.
	// Used only for the health check probe. Defaults to "main.ts".
	EntryPoint string
	// DistDir is the directory containing production build output.
	// Defaults to "./static/dist".
	DistDir string
}

func (c ViteConfig) withDefaults() ViteConfig {
	if c.Port == "" {
		c.Port = "5173"
	}
	if c.EntryPoint == "" {
		c.EntryPoint = "main.ts"
	}
	if c.DistDir == "" {
		c.DistDir = "./static/dist"
	}
	return c
}

// IsViteRunning probes the Vite dev server. Uses a 2s timeout.
func IsViteRunning(cfg ViteConfig) bool {
	cfg = cfg.withDefaults()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	url := "http://localhost:" + cfg.Port + "/" + cfg.EntryPoint
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}

// Assets holds the resolved CSS and JS paths for the current environment.
type Assets struct {
	CSSPath string
	JSPath  string
	IsDev   bool
}

// ResolveAssets detects whether Vite is running and returns the
// correct asset paths for the current environment.
//
// In dev mode: returns Vite server URLs.
// In prod mode: scans DistDir for hashed main.*.js / main.*.css files.
func ResolveAssets(cfg ViteConfig) Assets {
	cfg = cfg.withDefaults()

	if IsViteRunning(cfg) {
		base := "http://localhost:" + cfg.Port
		return Assets{
			CSSPath: base + "/css/main.css",
			JSPath:  base + "/" + cfg.EntryPoint,
			IsDev:   true,
		}
	}

	files, err := os.ReadDir(cfg.DistDir)
	if err != nil {
		return Assets{}
	}

	const staticPrefix = "/static/dist/"
	var a Assets
	for _, f := range files {
		if f.IsDir() {
			continue
		}
		name := f.Name()
		if strings.HasPrefix(name, "main") && strings.HasSuffix(name, ".js") {
			a.JSPath = staticPrefix + name
		}
		if strings.HasPrefix(name, "main") && strings.HasSuffix(name, ".css") {
			a.CSSPath = staticPrefix + name
		}
	}
	return a
}
