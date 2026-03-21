package frontend

import "github.com/gin-gonic/gin"

// AppStateBuilder is a function the application provides.
// It receives the current request context and returns any value
// that can be serialised to JSON. The result is injected into
// the HTML template as window.APP_STATE.
//
// Keep it cheap: this runs on every non-API page load.
type AppStateBuilder func(ctx *gin.Context) any
