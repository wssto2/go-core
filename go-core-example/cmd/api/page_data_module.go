package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/bootstrap"
)

// pageDataModule exposes route-scoped page shell data for client-side router
// navigations so Vue can hydrate new pages without a full document reload.
type pageDataModule struct {
	composer pageShellComposer
}

func newPageDataModule(composer pageShellComposer) *pageDataModule {
	return &pageDataModule{composer: composer}
}

func (m *pageDataModule) Name() string {
	return "page-data"
}

func (m *pageDataModule) Register(c *bootstrap.Container) error {
	eng, err := bootstrap.Resolve[*gin.Engine](c)
	if err != nil {
		return fmt.Errorf("page-data: resolve engine: %w", err)
	}

	eng.GET("/__page-data", func(ctx *gin.Context) {
		path := normalizePageDataPath(ctx.Query("path"))
		shell, err := m.composer.ComposePageShell(ctx.Request.Context(), pageShellRequest{
			Path:                path,
			Locale:              requestedLocale(ctx),
			AuthorizationHeader: ctx.GetHeader("Authorization"),
		})
		if err != nil {
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": "failed to compose page shell"})
			return
		}
		ctx.JSON(http.StatusOK, shell)
	})

	return nil
}

func (m *pageDataModule) Boot(context.Context) error {
	return nil
}

func (m *pageDataModule) Shutdown(context.Context) error {
	return nil
}

func normalizePageDataPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || !strings.HasPrefix(path, "/") {
		return "/"
	}
	return path
}
