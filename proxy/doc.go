// Package proxy forwards requests to an upstream web server that keeps serving
// what the application does not own yet (the strangler pattern: a Go binary in
// front of a legacy site).
//
// The upstream is reached at a fixed address while the request keeps the Host
// it came with. Over HTTPS the Host is also the TLS server name and the
// certificate is verified against it, so an upstream that answers correctly
// only for its own hostname on one IP (a shared Apache) works, and one process
// can front many hostnames:
//
//	h, err := proxy.New(proxy.Config{Addr: "85.10.250.115:7081", HTTPS: true}, app.Log())
//	...
//	gocore.WithFallthrough(h)
//
// Redirects, cookies and every other response header reach the client as the
// upstream wrote them. X-Forwarded-For is appended to, never overwritten, and
// X-Forwarded-For, X-Forwarded-Proto and X-Real-IP from the peer are believed
// only when the peer is a trusted proxy (gocore tells the handler; a handler
// used outside gocore trusts nobody). Upgrade requests (WebSocket) pass through.
// A failing upstream answers 502 and is logged; under gocore it is also an
// error of the request, so the incident log counts it.
package proxy
