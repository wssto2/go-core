package main

import (
	"errors"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	coreauth "github.com/wssto2/go-core/auth"
	"github.com/wssto2/go-core/bootstrap"
)

type appState struct {
	AppName     string          `json:"appName"`
	Env         string          `json:"env"`
	Path        string          `json:"path"`
	APIBase     string          `json:"apiBase"`
	Viewer      *appStateViewer `json:"viewer,omitempty"`
	ViewerError string          `json:"viewerError,omitempty"`
}

type appStateViewer struct {
	ID       int      `json:"id"`
	Username string   `json:"username"`
	Policies []string `json:"policies"`
}

type appStateViewerIdentity interface {
	coreauth.Identifiable
	GetEmail() string
	GetPolicies() []string
}

// appStateProvider demonstrates how to compose SPA state from explicit
// dependencies instead of passing the container into WithSPA.
type appStateProvider struct {
	appName        string
	env            string
	apiBase        string
	tokenCfg       coreauth.TokenConfig
	viewerResolver coreauth.IdentityResolver
}

func newAppStateProvider(cfg bootstrap.Config, tokenCfg coreauth.TokenConfig, viewerResolver coreauth.IdentityResolver) appStateProvider {
	return appStateProvider{
		appName:        cfg.App.Name,
		env:            cfg.App.Env,
		apiBase:        "/api/v1",
		tokenCfg:       tokenCfg,
		viewerResolver: viewerResolver,
	}
}

func (p appStateProvider) Build(ctx *gin.Context) any {
	state := appState{
		AppName: p.appName,
		Env:     p.env,
		Path:    ctx.Request.URL.Path,
		APIBase: p.apiBase,
	}

	viewer, err := p.resolveViewer(ctx)
	if err != nil {
		state.ViewerError = classifyViewerError(err)
		return state
	}
	if viewer != nil {
		state.Viewer = viewer
	}

	return state
}

func (p appStateProvider) resolveViewer(ctx *gin.Context) (*appStateViewer, error) {
	if p.viewerResolver == nil {
		return nil, nil
	}

	token := extractBearerToken(ctx.GetHeader("Authorization"))
	if token == "" {
		return nil, nil
	}

	claims, err := coreauth.ParseToken(token, p.tokenCfg)
	if err != nil {
		return nil, err
	}

	subject := claims.Subject
	if subject == "" && claims.UserID != 0 {
		subject = strconv.Itoa(claims.UserID)
	}
	if subject == "" {
		return nil, coreauth.ErrInvalidClaims
	}

	identity, err := p.viewerResolver(ctx.Request.Context(), subject)
	if err != nil {
		return nil, err
	}

	viewer := &appStateViewer{ID: identity.GetID()}
	if detailed, ok := identity.(appStateViewerIdentity); ok {
		viewer.Username = detailed.GetEmail()
		viewer.Policies = append([]string(nil), detailed.GetPolicies()...)
	}

	return viewer, nil
}

func extractBearerToken(header string) string {
	if header == "" || !strings.HasPrefix(header, "Bearer ") {
		return ""
	}
	return strings.TrimSpace(header[len("Bearer "):])
}

func classifyViewerError(err error) string {
	switch {
	case errors.Is(err, coreauth.ErrExpiredToken):
		return "viewer token has expired"
	case errors.Is(err, coreauth.ErrInvalidToken), errors.Is(err, coreauth.ErrInvalidClaims):
		return "viewer token is invalid"
	default:
		return "failed to resolve viewer"
	}
}
