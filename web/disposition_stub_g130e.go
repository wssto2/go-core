// Package web is the HTTP layer; this file is a temporary stand-in for lane G-D.
package web

import "strconv"

// Disposition is a stand-in for lane G-D's web/disposition.go, which defines the real one with this signature: the
// orchestrator deletes this file at merge.
func Disposition(kind, name string) string { return kind + "; filename=" + strconv.Quote(name) }
