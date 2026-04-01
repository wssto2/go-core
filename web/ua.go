package web

import (
	"regexp"
	"strings"
)

type browserPattern struct {
	name string
	re   *regexp.Regexp
}

var compiledBrowserPatterns = []browserPattern{
	{name: "Chrome", re: regexp.MustCompile(`Chrome/([\d.]+)`)},
	{name: "Firefox", re: regexp.MustCompile(`Firefox/([\d.]+)`)},
	{name: "Safari", re: regexp.MustCompile(`Version/([\d.]+) Safari/`)},
	{name: "Edge", re: regexp.MustCompile(`Edg/([\d.]+)`)},
	{name: "Opera", re: regexp.MustCompile(`OPR/([\d.]+)`)},
}

func GetUserAgentDeviceType(ua string) string {
	if strings.Contains(ua, "Mobi") {
		return "Mobile"
	} else if strings.Contains(ua, "Tablet") {
		return "Tablet"
	}
	return "Desktop"
}

func GetUserAgentOS(ua string) string {
	type osPattern struct {
		pattern string
		name    string
	}

	osPatterns := []osPattern{
		{pattern: "Windows NT 10.0", name: "Windows 10"},
		{pattern: "Windows NT 6.1", name: "Windows 7"},
		{pattern: "Mac OS X 10", name: "macOS"},
		{pattern: "Macintosh", name: "macOS"},
		{pattern: "Android", name: "Android"},
		{pattern: "iPhone", name: "iOS"},
		{pattern: "iPad", name: "iOS"},
		{pattern: "Linux", name: "Linux"},
	}

	for _, os := range osPatterns {
		if strings.Contains(ua, os.pattern) {
			return os.name
		}
	}
	return "Unknown OS"
}

// GetUserAgentBrowser returns the browser name and version string parsed from
// the User-Agent header. Returns ("Unknown Browser", "") if no pattern matches.
func GetUserAgentBrowser(userAgent string) (string, string) {
	for _, b := range compiledBrowserPatterns {
		match := b.re.FindStringSubmatch(userAgent)
		if len(match) > 1 {
			return b.name, match[1]
		}
	}
	return "Unknown Browser", ""
}
