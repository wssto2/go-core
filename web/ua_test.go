package web

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetUserAgentBrowser_EdgeUA_ReturnsEdge(t *testing.T) {
	// Real-world Edge 120 User-Agent string — contains both "Edg/" and "Chrome/".
	edgeUA := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 Edg/120.0.2210.91"

	browser, _ := GetUserAgentBrowser(edgeUA)
	assert.Equal(t, "Edge", browser, "Edge UA must be identified as Edge, not Chrome")
}

func TestGetUserAgentBrowser_ChromeUA_ReturnsChrome(t *testing.T) {
	// Pure Chrome UA (no Edg/ token).
	chromeUA := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

	browser, _ := GetUserAgentBrowser(chromeUA)
	assert.Equal(t, "Chrome", browser)
}
