package account

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"time"
)

// Session is one sign-in of an account on one device.
type Session struct {
	ID        int
	AccountID int
	// ActorID is who opened the session by signing in as the account, zero for
	// a session the person opened themselves.
	ActorID int
	// Device is the User-Agent the session was opened (or last refreshed) with.
	Device     string
	IP         string
	LastUsedAt time.Time
	ExpiresAt  time.Time
	CreatedAt  time.Time
}

// Label names the device for a list: "Chrome · macOS".
func (s Session) Label() string { return DeviceLabel(s.Device) }

// NewSession is what a store needs to open a session.
type NewSession struct {
	AccountID int
	ActorID   int
	Device    string
	IP        string
	At        time.Time
	ExpiresAt time.Time
}

// Credentials are the tokens of a session. The store hands them out once, when
// it creates or rotates the session; afterwards only a hash of the refresh
// token is kept.
type Credentials struct {
	Access    string
	Refresh   string
	ExpiresAt time.Time
}

// Rotation is a request to swap the tokens of the session a refresh token belongs to.
type Rotation struct {
	Refresh   string
	At        time.Time
	ExpiresAt time.Time
	// Device, when not empty, replaces the session's device.
	Device string
	IP     string
}

// ErrSessionNotFound is what a SessionStore returns for a session that is
// unknown, revoked or expired.
var ErrSessionNotFound = errors.New("identity: session not found")

// NewToken returns 32 random bytes as a URL-safe string, for stores to use as
// access and refresh tokens.
func NewToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(b), nil
}

const deviceLabelMax = 80

// DeviceLabel names the device a session came from, "Browser · OS", from its
// User-Agent (IAM-USER-004): Chrome, Safari, Edge or Firefox on macOS, Windows,
// iPhone, iPad, Android or Linux. Anything else is the raw string, trimmed.
//
// ponytail: no User-Agent library; the names above, read off the substrings
// every such browser sends.
func DeviceLabel(userAgent string) string {
	ua := strings.TrimSpace(userAgent)
	if ua == "" {
		return ""
	}

	browser, system := browserOf(ua), systemOf(ua)
	if browser == "" || system == "" {
		if runes := []rune(ua); len(runes) > deviceLabelMax {
			return string(runes[:deviceLabelMax])
		}

		return ua
	}

	return browser + " · " + system
}

// browserOf checks the tokens in the order that tells them apart: Edge and
// Chrome on iOS say Safari too, and Chrome says Safari as well.
func browserOf(ua string) string {
	switch {
	case strings.Contains(ua, "Edg/"), strings.Contains(ua, "EdgA/"), strings.Contains(ua, "EdgiOS/"):
		return "Edge"
	case strings.Contains(ua, "Firefox/"), strings.Contains(ua, "FxiOS/"):
		return "Firefox"
	case strings.Contains(ua, "Chrome/"), strings.Contains(ua, "CriOS/"):
		return "Chrome"
	case strings.Contains(ua, "Safari/"), strings.Contains(ua, "iPhone"), strings.Contains(ua, "iPad"):
		// the installed iPhone app (a home-screen web app) sends no Safari/ token
		return "Safari"
	default:
		return ""
	}
}

func systemOf(ua string) string {
	switch {
	case strings.Contains(ua, "iPhone"):
		return "iPhone"
	case strings.Contains(ua, "iPad"):
		return "iPad"
	case strings.Contains(ua, "Android"):
		return "Android"
	case strings.Contains(ua, "Windows"):
		return "Windows"
	case strings.Contains(ua, "Mac OS X"), strings.Contains(ua, "Macintosh"):
		return "macOS"
	case strings.Contains(ua, "Linux"), strings.Contains(ua, "X11"):
		return "Linux"
	default:
		return ""
	}
}
