package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wssto2/go-core/apperr"
)

// Failure is something that went wrong in the application, to be recorded as an occurrence of an incident: a server
// error the module captured, or an error the browser reported.
type Failure struct {
	// Source is SourceRuntimeError (the default) or SourceClientError.
	Source Source
	// Severity is the severity of the incident this failure opens; the default is high for a server error and low for
	// a browser error. An incident that exists keeps the severity it has.
	Severity Severity
	// Message is what went wrong; a failure without one is not recorded.
	Message        string
	ErrorClass     string
	ErrorCode      string
	Stacktrace     string
	AffectedModule string
	AffectedRoute  string
	RequestMethod  string
	UserID         *int
	TenantID       *int
	IPAddress      string
	RequestID      string
	Environment    string
	// Signature, when set, stands for the stacktrace in the fingerprint: where the failure happened, without the
	// addresses and goroutine numbers a stack holds, which differ every time. The stacktrace is still stored.
	Signature string
	// ContextJSON and RequestJSON are JSON documents describing the surroundings and the request; anything else is
	// stored as "{}".
	ContextJSON string
	RequestJSON string
}

// Capture records a failure: it finds the open incident with the failure's fingerprint (INC-FINGERPRINT-001) and counts
// another occurrence on it, reopens the latest closed one, or opens a new incident, in a transaction of its own. A new
// incident is announced to Config.Opened.
//
// A server error is a high severity incident only the staff sees. A browser error is a low one, grouped by its message
// and top stack frame. Two sources seen on one incident make it SourceMixed.
func (s *Service) Capture(ctx context.Context, f Failure) error {
	if strings.TrimSpace(f.Message) == "" {
		return nil
	}

	f = s.normalized(f)

	return s.transact(ctx, func(ctx context.Context) error { return s.record(ctx, f) })
}

// normalized cuts the failure to what the columns hold and fills what it leaves out.
func (s *Service) normalized(f Failure) Failure {
	if f.Source == "" {
		f.Source = SourceRuntimeError
	}

	f.Message = clean(f.Message, StackMax)
	f.ErrorClass = clean(f.ErrorClass, 255)
	f.ErrorCode = clean(f.ErrorCode, 100)
	f.Stacktrace = clean(f.Stacktrace, StackMax)
	f.Signature = clean(f.Signature, StackMax)
	f.AffectedModule = clean(f.AffectedModule, ModuleMax)
	f.AffectedRoute = clean(f.AffectedRoute, RouteMax)
	f.RequestMethod = clean(f.RequestMethod, 10)
	f.IPAddress = clean(f.IPAddress, 45)
	f.RequestID = clean(f.RequestID, requestIDMax)
	f.Environment = clean(f.Environment, 50)

	if f.Environment == "" {
		f.Environment = s.env
	}

	f.ContextJSON = jsonOr(f.ContextJSON, "{}")
	f.RequestJSON = jsonOr(f.RequestJSON, "{}")

	return f
}

// fingerprint groups a failure with its repeats: a server error by where it happened and what it was, a browser error
// by its message and the frame it was thrown from.
func (f Failure) fingerprint() string {
	if f.Source == SourceClientError {
		return Fingerprint("client", f.Message, topFrame(f.Stacktrace))
	}

	where := f.Stacktrace
	if f.Signature != "" {
		where = f.Signature
	}

	return Fingerprint("runtime", f.AffectedModule, f.AffectedRoute, f.ErrorClass, f.Message, where)
}

// topFrame is the first stack frame of a browser stack: "at fn (file:1:2)" in Chrome, "fn@file:1:2" in Firefox and
// Safari. The message line Chrome starts its stack with is not one.
func topFrame(stack string) string {
	for _, line := range strings.Split(stack, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "at ") || strings.Contains(line, "@") {
			return line
		}
	}

	return ""
}

func (f Failure) title() string {
	switch {
	case f.Source == SourceClientError:
		return "Browser error: " + clip(firstLine(f.Message), 120)
	case f.ErrorClass != "" && f.AffectedModule != "":
		return clip(fmt.Sprintf("%s error in %s", f.ErrorClass, f.AffectedModule), TitleMax)
	case f.ErrorClass != "":
		return clip(f.ErrorClass+" error", TitleMax)
	case f.AffectedModule != "":
		return clip("Unhandled error in "+f.AffectedModule, TitleMax)
	default:
		return "Unhandled server error"
	}
}

func (f Failure) severity() Severity {
	if f.Severity.Valid() {
		return f.Severity
	}

	if f.Source == SourceClientError {
		return SeverityLow
	}

	return SeverityHigh
}

func firstLine(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")

	return line
}

// record writes one occurrence in the transaction of ctx.
func (s *Service) record(ctx context.Context, f Failure) error {
	now := s.now()
	fp := f.fingerprint()

	incident, created, err := s.sighted(ctx, fp, f, now)
	if err != nil {
		return err
	}

	occurrence := Occurrence{
		IncidentID: incident.ID, Fingerprint: fp, Environment: f.Environment, AffectedModule: f.AffectedModule,
		AffectedRoute: f.AffectedRoute, RequestMethod: f.RequestMethod, UserID: f.UserID, TenantID: f.TenantID,
		IPAddress: f.IPAddress, RequestID: f.RequestID, ErrorClass: f.ErrorClass, ErrorCode: f.ErrorCode, Message: f.Message,
		NormalizedMessage: clip(NormalizeText(f.Message), StackMax), Stacktrace: f.Stacktrace, ContextJSON: f.ContextJSON,
		RequestJSON: f.RequestJSON, OccurredAt: now, CreatedAt: now,
	}

	// The incident is written before the occurrence: the occurrence's foreign key takes a shared lock on the incident, and
	// concurrent captures of one error that each hold one and wait for the exclusive lock of the count deadlock.
	if !created {
		if err := s.store.CountOccurrence(ctx, incident.ID, incident.Source, now, &occurrence); err != nil {
			return apperr.Internal(err)
		}
	}

	if err := s.store.CreateOccurrence(ctx, &occurrence); err != nil {
		return apperr.Internal(err)
	}

	if created {
		if err := s.opened(ctx, openedOf(*incident)); err != nil {
			return apperr.Internal(err)
		}
	}

	return nil
}

// sighted is the incident a failure is an occurrence of: the open one with its fingerprint, else the latest closed one,
// reopened, else a new one. It says whether the incident is new.
func (s *Service) sighted(ctx context.Context, fp string, f Failure, now time.Time) (*Incident, bool, error) {
	open, err := s.store.FindOpen(ctx, fp)

	switch {
	case err == nil:
		open.Source = open.Source.Merge(f.Source)

		return &open, false, nil
	case !errors.Is(err, ErrNotFound):
		return nil, false, apperr.Internal(err)
	}

	latest, err := s.store.FindLatest(ctx, fp)

	switch {
	case err == nil:
		latest.Source = latest.Source.Merge(f.Source)
		reopen(&latest, fp, now)

		if err := s.store.UpdateIncident(ctx, latest); err != nil {
			return nil, false, apperr.Internal(err)
		}

		return &latest, false, nil
	case !errors.Is(err, ErrNotFound):
		return nil, false, apperr.Internal(err)
	}

	code, err := publicCode()
	if err != nil {
		return nil, false, apperr.Internal(err)
	}

	incident := Incident{
		PublicCode: code, Fingerprint: fp, OpenFingerprint: &fp, Source: f.Source, Status: StatusNew, Severity: f.severity(),
		Visibility: VisibilityInternal, Title: f.title(), Description: f.Message, AffectedModule: f.AffectedModule,
		AffectedRoute: f.AffectedRoute, Environment: f.Environment, ErrorClass: f.ErrorClass, ErrorCode: f.ErrorCode,
		ErrorMessageSample: f.Message, StacktraceSample: f.Stacktrace, FirstSeenAt: now, LastSeenAt: now, OccurrenceCount: 1,
		ExternalTicketProvider: TicketProviderNone, CreatedAt: now, UpdatedAt: now,
	}

	if err := s.store.CreateIncident(ctx, &incident); err != nil {
		return nil, false, createFailed(err)
	}

	return &incident, true, nil
}

// ClientError records an error the browser reported, as a low severity incident of SourceClientError. who is the
// signed-in person; the request ids are cut to ClientRequests of at most requestIDMax characters. The module's route
// rate-limits the person before it calls this.
func (s *Service) ClientError(ctx context.Context, who Viewer, tenantID *int, in ClientErrorInput) error {
	person, err := signedIn(who)
	if err != nil {
		return err
	}

	if strings.TrimSpace(in.Message) == "" {
		return apperr.BadRequest("message is required")
	}

	ids := make([]string, 0, min(len(in.RequestIDs), ClientRequests))

	for _, id := range in.RequestIDs {
		if id = clean(id, requestIDMax); id != "" && len(ids) < ClientRequests {
			ids = append(ids, id)
		}
	}

	ctxJSON, err := Marshal(map[string]any{
		"route": clean(in.Route, RouteMax), "browser": clean(in.Browser, 500), "app_version": clean(in.AppVersion, 100),
		"request_ids": ids,
	})
	if err != nil {
		return apperr.Internal(err)
	}

	return s.Capture(ctx, Failure{
		Source: SourceClientError, Message: in.Message, ErrorClass: "client", Stacktrace: in.Stack,
		AffectedModule: "client", AffectedRoute: in.Route, UserID: &person, TenantID: tenantID, ContextJSON: ctxJSON,
	})
}
