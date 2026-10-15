package http

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	nethttp "net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/identity/account"
	"github.com/wssto2/go-core/route"
)

// EmbedPath is where the embed page is served: the root of the site, outside the application's prefix.
const EmbedPath = "/embed"

// MinEmbedSecret is the shortest secret the embed token is signed with.
const MinEmbedSecret = 32

// Embed is GET /embed?token=...&redirect=...: the page an application embeds in an iframe of another host opens first.
// The host signs a token for the person (an HMAC-SHA256 JWT with sub, the account id, exp and jti) and the page
// exchanges it for a session of EmbedTTL with no refresh. iOS blocks third-party storage in cross-origin iframes,
// so the answer is a page that writes the session's access token to sessionStorage (embed_access_token,
// embed_expires_at) and navigates to redirect, which must be a path of this site (anything else is "/"); the client
// sends the token as a bearer token from there. A token works once: its jti is remembered until it expires.
//
// The answers are JSON {"error": ...} with the status: 400 no token, 401 a token that is not valid ("invalid token":
// a bad signature or format, expired, no jti, a subject that is no id), 401 "token already used", 401 "unauthorized"
// for a person who cannot sign in.
type Embed struct {
	signIn *account.SignIn
	secret []byte
	clock  interface{ Now() time.Time }
	used   *jtis
}

// NewEmbed returns the embed page's handler; the secret is shared with the host that signs the tokens.
func NewEmbed(signIn *account.SignIn, secret string, clock interface{ Now() time.Time }) *Embed {
	return &Embed{signIn: signIn, secret: []byte(secret), clock: clock, used: &jtis{seen: map[string]time.Time{}}}
}

// Route is GET /embed, public: the token is the credential.
func (e *Embed) Route() route.Bound {
	return route.Raw(nethttp.MethodGet, EmbedPath).Name("identity.embed").Public().To(e.handle)
}

func (e *Embed) handle(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		c.JSON(nethttp.StatusBadRequest, gin.H{"error": "token is required"})

		return
	}

	claims, err := e.verify(token)
	if err != nil {
		c.JSON(nethttp.StatusUnauthorized, gin.H{"error": "invalid token"})

		return
	}

	if !e.used.firstUse(claims.JTI, time.Unix(claims.Exp, 0), e.clock.Now()) {
		c.JSON(nethttp.StatusUnauthorized, gin.H{"error": "token already used"})

		return
	}

	id, err := strconv.Atoi(claims.Sub)
	if err != nil {
		c.JSON(nethttp.StatusUnauthorized, gin.H{"error": "invalid token"})

		return
	}

	signed, err := e.signIn.Embed(c.Request.Context(), account.EmbedInput{
		AccountID: id, Device: account.DeviceLabel(c.GetHeader("User-Agent")), IP: c.ClientIP(),
	})
	if err != nil {
		c.JSON(nethttp.StatusUnauthorized, gin.H{"error": "unauthorized"})

		return
	}

	target := c.Query("redirect")
	if !samePath(target) {
		target = "/"
	}

	// The three values are JSON strings, which a script can hold as they are: JSON escapes the characters that end one.
	accessJSON, _ := json.Marshal(signed.Credentials.Access)                          //nolint:errchkjson // a string cannot fail to marshal
	expiresJSON, _ := json.Marshal(signed.Credentials.ExpiresAt.Format(time.RFC3339)) //nolint:errchkjson // a string cannot fail to marshal
	targetJSON, _ := json.Marshal(target)                                             //nolint:errchkjson // a string cannot fail to marshal
	nonce := c.GetString("nonce")                                                     // the CSP middleware's; the script needs it to run under the policy

	page := fmt.Sprintf(`<!DOCTYPE html>
<html><head><script nonce="%s">(function(){try{
sessionStorage.setItem('embed_access_token',%s);
sessionStorage.setItem('embed_expires_at',%s);
}catch(e){}window.location.replace(%s);})();</script></head>
<body></body></html>`, nonce, accessJSON, expiresJSON, targetJSON)

	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Header("Cache-Control", "no-store") // the page holds a credential
	c.String(nethttp.StatusOK, page)
}

// samePath reports whether target is a path of this site: it starts with one slash, and has no scheme, host, backslash
// or control character that a browser would read as another place ("//host", "/\host", "https://host").
func samePath(target string) bool {
	if !strings.HasPrefix(target, "/") || strings.HasPrefix(target, "//") {
		return false
	}

	for _, r := range target {
		if r == '\\' || r < ' ' || r == 0x7f {
			return false
		}
	}

	return true
}

type embedClaims struct {
	Sub string `json:"sub"`
	Exp int64  `json:"exp"`
	JTI string `json:"jti"`
}

var errEmbedToken = errors.New("identity: the embed token is not valid")

// verify checks the signature (constant time), the lifetime and the jti.
func (e *Embed) verify(token string) (embedClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return embedClaims{}, errEmbedToken
	}

	mac := hmac.New(sha256.New, e.secret)
	mac.Write([]byte(parts[0] + "." + parts[1]))

	if !hmac.Equal([]byte(base64.RawURLEncoding.EncodeToString(mac.Sum(nil))), []byte(parts[2])) {
		return embedClaims{}, errEmbedToken
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return embedClaims{}, errEmbedToken
	}

	var claims embedClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return embedClaims{}, errEmbedToken
	}

	if e.clock.Now().Unix() > claims.Exp || claims.JTI == "" {
		return embedClaims{}, errEmbedToken
	}

	return claims, nil
}

// jtis remembers the tokens that were used, until they would have expired anyway.
//
// shortcut: one process only; a shared store is the upgrade when an app runs several (a token could then be
// used once on each).
type jtis struct {
	mu   sync.Mutex
	seen map[string]time.Time
}

// firstUse records the jti and reports whether it was the first use.
func (j *jtis) firstUse(jti string, exp, now time.Time) bool {
	j.mu.Lock()
	defer j.mu.Unlock()

	for k, until := range j.seen {
		if now.After(until) {
			delete(j.seen, k)
		}
	}

	if _, used := j.seen[jti]; used {
		return false
	}

	j.seen[jti] = exp

	return true
}
