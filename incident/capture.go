package incident

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"reflect"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/audit"
	"github.com/wssto2/go-core/auth"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/gocore"
	"github.com/wssto2/go-core/incident/core"
	"github.com/wssto2/go-core/middlewares"
	"github.com/wssto2/go-core/tenancy"
)

const (
	// captureTimeout bounds the writes of one capture, so a slow database cannot hold the answer of a failed request.
	captureTimeout = 10 * time.Second
	// panicSignatureFrames is how many frames of the stack tell one panic from another.
	panicSignatureFrames = 3
	redacted             = "<redacted>"
)

// capturer is the middleware Install puts before every route.
type capturer struct {
	service *core.Service
	trail   *trail
	prefix  string
	clock   gocore.Clock
	log     *slog.Logger
	module  func(path string) string
	env     string
}

func newCapturer(app *gocore.App, service *core.Service, cfg Capture) *capturer {
	c := &capturer{service: service, prefix: app.Prefix(), clock: app.Clock(), log: app.Logger(), module: cfg.Module, env: cfg.Environment}

	if cfg.TrailSize > 0 {
		c.trail = newTrail(cfg.TrailSize)
	}

	if c.module == nil {
		c.module = func(path string) string { return moduleOf(c.prefix, path) }
	}

	if c.env == "" {
		c.env = strings.TrimSpace(app.Config().App.Env)
	}

	if c.env == "" {
		c.env = strings.TrimSpace(os.Getenv("APP_ENV"))
	}

	return c
}

// caller is who made the request.
type caller struct {
	key  string // the trail key: "user:7", "service:3"
	kind authz.Kind
	id   int
}

// callerOf is the person or service account the request was authenticated as, once the routes' security has run.
func callerOf(ctx *gin.Context) (caller, bool) {
	if principal, ok := authz.PrincipalFrom(ctx.Request.Context()); ok {
		return caller{key: principal.Subject.String(), kind: principal.Kind, id: principal.ID}, true
	}

	if who, ok := auth.GetIdentifiable(ctx); ok && who.GetID() > 0 {
		return caller{key: authz.Subject{Kind: authz.KindUser, ID: who.GetID()}.String(), kind: authz.KindUser, id: who.GetID()}, true
	}

	return caller{}, false
}

// handle is the middleware: it times the request, records the failure a response of 500 or more comes from, records a
// panic and raises it again, and remembers the call in the caller's trail.
func (c *capturer) handle(ctx *gin.Context) {
	start := c.clock.Now()
	began := time.Now()

	defer func() {
		recovered := recover()
		if recovered == nil {
			return
		}

		// A client that went away stops the handler with this panic on purpose: it is not a failure.
		if err, ok := recovered.(error); !ok || !errors.Is(err, http.ErrAbortHandler) {
			c.capture(ctx, c.panicked(ctx, recovered))
		}

		panic(recovered)
	}()

	ctx.Next()

	status := ctx.Writer.Status()

	if failure, ok := c.failed(ctx); ok {
		c.capture(ctx, failure)

		status = max(status, http.StatusInternalServerError)
	}

	c.remember(ctx, start, time.Since(began), status)
}

// remember adds the call to the trail of the caller, for a request the application's routes answered.
func (c *capturer) remember(ctx *gin.Context, start time.Time, took time.Duration, status int) {
	if c.trail == nil || ctx.FullPath() == "" || !strings.HasPrefix(ctx.Request.URL.Path, c.prefix+"/") {
		return
	}

	who, ok := callerOf(ctx)
	if !ok {
		return
	}

	c.trail.add(who.key, TrailEntry{
		At: start, Method: ctx.Request.Method, Path: ctx.Request.URL.Path, Page: refererPath(ctx.Request.Referer()),
		Status: status, Ms: took.Milliseconds(),
	})
}

// failed is the failure of a request that ended in a server error: the last error the request collected, when it answers
// 500 or more (the status ErrorHandler will write).
func (c *capturer) failed(ctx *gin.Context) (core.Failure, bool) {
	last := ctx.Errors.Last()
	if last == nil || last.Err == nil {
		return core.Failure{}, false
	}

	status := apperr.GetHTTPStatus(last.Err)
	if status < http.StatusInternalServerError {
		return core.Failure{}, false
	}

	failure := c.describe(ctx, status)
	failure.Message = last.Err.Error()
	failure.ErrorClass = errorClass(last.Err)
	failure.ErrorCode = errorCode(last.Err)
	failure.Stacktrace = errorStack(last.Err)

	if appErr := asAppError(last.Err); appErr != nil && len(appErr.Fields) > 0 {
		failure.ContextJSON = marshal(map[string]any{
			"handler": routeOf(ctx), "error_fields": appErr.Fields, "principal": principalOf(ctx),
			"recent_requests": c.recent(ctx),
		})
	}

	return failure, true
}

// panicked is the failure of a request whose handler panicked.
func (c *capturer) panicked(ctx *gin.Context, recovered any) core.Failure {
	failure := c.describe(ctx, http.StatusInternalServerError)
	failure.Message = fmt.Sprint(recovered)
	failure.ErrorClass = fmt.Sprintf("%T", recovered)
	failure.ErrorCode = "panic"

	stack := debug.Stack()
	failure.Stacktrace = string(stack)
	failure.Signature = panicSignature(stack)

	return failure
}

// describe is what every captured request says about itself: where it was, who made it, and the calls before it.
func (c *capturer) describe(ctx *gin.Context, status int) core.Failure {
	request := ctx.Request
	route := routeOf(ctx)
	reqCtx := request.Context()

	failure := core.Failure{
		Source: core.SourceRuntimeError, AffectedModule: c.module(request.URL.Path), AffectedRoute: route,
		RequestMethod: request.Method, IPAddress: ctx.ClientIP(), RequestID: middlewares.GetRequestID(ctx), Environment: c.env,
	}

	failure.RequestJSON = marshal(map[string]any{
		"path": request.URL.Path, "query": audit.Mask(request.URL.Query()), "status_code": status, "request_path": route,
		"params": paramsOf(ctx), "headers": audit.Mask(maskedHeaders(request.Header)),
	})
	failure.ContextJSON = marshal(map[string]any{
		"handler": route, "principal": principalOf(ctx), "recent_requests": c.recent(ctx),
	})

	if who, ok := callerOf(ctx); ok && who.kind == authz.KindUser {
		failure.UserID = &who.id
	}

	if tenant, ok := tenancy.TenantIDFromContext(reqCtx); ok {
		failure.TenantID = &tenant
	}

	return failure
}

func (c *capturer) recent(ctx *gin.Context) []TrailEntry {
	if who, ok := callerOf(ctx); ok {
		return c.trail.recent(who.key)
	}

	return nil
}

// capture records the failure in a transaction of its own, never the request's: it outlives a cancelled request and
// is written even when the request's own transaction rolled back. It never fails the request.
func (c *capturer) capture(ctx *gin.Context, failure core.Failure) {
	defer func() {
		if recovered := recover(); recovered != nil {
			c.log.ErrorContext(ctx, "incident: capture panicked", "panic", recovered)
		}
	}()

	own, cancel := context.WithTimeout(context.WithoutCancel(ctx.Request.Context()), captureTimeout)
	defer cancel()

	err := c.service.Capture(own, failure)
	if err != nil {
		c.log.ErrorContext(ctx, "incident: failed to capture a server error", "error", err, "request_id", failure.RequestID)
	}
}

func routeOf(ctx *gin.Context) string {
	if route := ctx.FullPath(); route != "" {
		return route
	}

	return ctx.Request.URL.Path
}

// principalOf says who made the request, as a person or as a service account: a service account's id is not a user id.
func principalOf(ctx *gin.Context) map[string]any {
	who, ok := callerOf(ctx)
	if !ok {
		return nil
	}

	return map[string]any{"kind": string(who.kind), "id": who.id}
}

func paramsOf(ctx *gin.Context) map[string]string {
	params := make(map[string]string, len(ctx.Params))
	for _, p := range ctx.Params {
		params[p.Key] = p.Value
	}

	return params
}

// credentialHeaders are the request headers that hold a secret whatever their value; audit.Mask redacts by the names of
// JSON keys, which these are not among.
var credentialHeaders = map[string]bool{ //nolint:gochecknoglobals // a fixed set
	"authorization": true, "proxy-authorization": true, "cookie": true, "set-cookie": true, "x-api-key": true,
	"x-auth-token": true, "x-csrf-token": true, "x-xsrf-token": true,
}

// maskedHeaders are the request headers with the credentials redacted (the rest goes through audit.Mask).
func maskedHeaders(header http.Header) map[string]string {
	out := make(map[string]string, len(header))

	for key, values := range header {
		if credentialHeaders[strings.ToLower(key)] {
			out[key] = redacted

			continue
		}

		out[key] = strings.Join(values, ",")
	}

	return out
}

var versionSegment = regexp.MustCompile(`^v[0-9]+$`)

// moduleOf is the first segment of the path after the application's prefix and the version ("/api/v1/vehicles/12" is
// "vehicles").
func moduleOf(prefix, path string) string {
	segments := strings.Split(strings.Trim(strings.TrimPrefix(path, prefix), "/"), "/")

	for _, segment := range segments {
		if segment != "" && !versionSegment.MatchString(segment) {
			return segment
		}
	}

	return ""
}

// errorClass is the type of the innermost error: the layers of *apperr.AppError around it say nothing.
func errorClass(err error) string {
	current := err

	for {
		appErr := asAppError(current)
		if appErr == nil || appErr.Err == nil {
			break
		}

		current = appErr.Err
	}

	if kind := reflect.TypeOf(current); kind != nil {
		return kind.String()
	}

	return ""
}

func errorCode(err error) string {
	if appErr := asAppError(err); appErr != nil {
		return string(appErr.Code)
	}

	return ""
}

func errorStack(err error) string {
	appErr := asAppError(err)

	switch {
	case appErr == nil:
		return ""
	case len(appErr.Stack) > 0:
		return strings.Join(appErr.Stack, "\n")
	case appErr.File != "" && appErr.Line > 0:
		return appErr.File + ":" + strconv.Itoa(appErr.Line)
	default:
		return ""
	}
}

func asAppError(err error) *apperr.AppError {
	appErr, ok := errors.AsType[*apperr.AppError](err)
	if !ok {
		return nil
	}

	return appErr
}

// stackLine is a source line of a goroutine stack: a tab, the file and line, and the offset into the function.
var stackLine = regexp.MustCompile(`^\t(\S+\.go:[0-9]+)`)

// panicSignature names where a panic happened by the first source lines of its stack that are not the runtime's or this
// file's, without the addresses and offsets that differ from one panic to the next.
func panicSignature(stack []byte) string {
	var frames []string

	for _, line := range strings.Split(string(stack), "\n") {
		match := stackLine.FindStringSubmatch(line)
		if match == nil || strings.Contains(match[1], "/runtime/") || strings.Contains(match[1], "/incident/capture.go") {
			continue
		}

		frames = append(frames, match[1])

		if len(frames) == panicSignatureFrames {
			break
		}
	}

	return strings.Join(frames, "\n")
}

// marshal is the JSON of a diagnostic document; one that cannot be encoded is "{}", since a capture is best effort.
func marshal(v any) string {
	text, err := core.Marshal(v)
	if err != nil {
		return "{}"
	}

	return text
}
