package main

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/gocore"
	"github.com/wssto2/go-core/route"
)

// pageData exposes route-scoped page shell data for client-side router
// navigations so Vue can hydrate new pages without a full document reload.
type pageData struct {
	composer pageShellComposer
}

// pageDataRoute is a raw route: it answers the page shell as plain JSON, outside the API's envelope and prefix, to
// anyone, like the SPA shell itself.
var pageDataRoute = route.Raw(http.MethodGet, "/__page-data").Name("pagedata.show").Public()

// installPageData mounts GET /__page-data at the site root.
func installPageData(app *gocore.App, composer pageShellComposer) {
	h := pageData{composer: composer}

	app.Root(pageDataRoute.To(h.show))
}

func (h pageData) show(ctx *gin.Context) {
	path := normalizePageDataPath(ctx.Query("path"))

	shell, err := h.composer.ComposePageShell(ctx.Request.Context(), pageShellRequest{
		Path:   path,
		Locale: requestedLocale(ctx),
	})
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "failed to compose page shell"})
		return
	}

	ctx.JSON(http.StatusOK, shell)
}

func normalizePageDataPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || !strings.HasPrefix(path, "/") {
		return "/"
	}
	return path
}
