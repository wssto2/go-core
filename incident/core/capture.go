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
func (s *Service) Capture(ctx context.Context, failure Failure) error {
	if strings.TrimSpace(failure.Message) == "" {
		return nil
	}

	failure = s.normalized(failure)

	return s.transact(ctx, func(ctx context.Context) error { return s.record(ctx, failure) })
}

// normalized cuts the failure to what the columns hold and fills what it leaves out.
func (s *Service) normalized(failure Failure) Failure {
	if failure.Source == "" {
		failure.Source = SourceRuntimeError
	}

	failure.Message = clean(failure.Message, StackMax)
	failure.ErrorClass = clean(failure.ErrorClass, classMax)
	failure.ErrorCode = clean(failure.ErrorCode, codeMax)
	failure.Stacktrace = clean(failure.Stacktrace, StackMax)
	failure.Signature = clean(failure.Signature, StackMax)
	failure.AffectedModule = clean(failure.AffectedModule, ModuleMax)
	failure.AffectedRoute = clean(failure.AffectedRoute, RouteMax)
	failure.RequestMethod = clean(failure.RequestMethod, methodMax)
	failure.IPAddress = clean(failure.IPAddress, addressMax)
	failure.RequestID = clean(failure.RequestID, requestIDMax)
	failure.Environment = clean(failure.Environment, envMax)

	if failure.Environment == "" {
		failure.Environment = s.env
	}

	failure.ContextJSON = jsonOr(failure.ContextJSON, "{}")
	failure.RequestJSON = jsonOr(failure.RequestJSON, "{}")

	return failure
}

// fingerprint groups a failure with its repeats: a server error by where it happened and what it was, a browser error
// by its message and the frame it was thrown from.
func (failure Failure) fingerprint() string {
	if failure.Source == SourceClientError {
		return Fingerprint("client", failure.Message, topFrame(failure.Stacktrace))
	}

	where := failure.Stacktrace
	if failure.Signature != "" {
		where = failure.Signature
	}

	return Fingerprint(
		"runtime", failure.AffectedModule, failure.AffectedRoute, failure.ErrorClass, failure.Message, where,
	)
}

// topFrame is the first stack frame of a browser stack: "at fn (file:1:2)" in Chrome, "fn@file:1:2" in Firefox and
// Safari. The message line Chrome starts its stack with is not one.
func topFrame(stack string) string {
	for line := range strings.SplitSeq(stack, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "at ") || strings.Contains(line, "@") {
			return line
		}
	}

	return ""
}

func (failure Failure) title() string {
	switch {
	case failure.Source == SourceClientError:
		return "Browser error: " + clip(firstLine(failure.Message), snippetMax)
	case failure.ErrorClass != "" && failure.AffectedModule != "":
		return clip(fmt.Sprintf("%s error in %s", failure.ErrorClass, failure.AffectedModule), TitleMax)
	case failure.ErrorClass != "":
		return clip(failure.ErrorClass+" error", TitleMax)
	case failure.AffectedModule != "":
		return clip("Unhandled error in "+failure.AffectedModule, TitleMax)
	default:
		return "Unhandled server error"
	}
}

func (failure Failure) severity() Severity {
	switch {
	case failure.Severity.Valid():
		return failure.Severity
	case failure.Source == SourceClientError:
		return SeverityLow
	default:
		return SeverityHigh
	}
}

func firstLine(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")

	return line
}

// record writes one occurrence in the transaction of ctx.
func (s *Service) record(ctx context.Context, failure Failure) error {
	now := s.now()
	key := failure.fingerprint()

	incident, created, err := s.sighted(ctx, key, failure, now)
	if err != nil {
		return err
	}

	occurrence := Occurrence{
		IncidentID: incident.ID, Fingerprint: key, Environment: failure.Environment, AffectedModule: failure.AffectedModule,
		AffectedRoute: failure.AffectedRoute, RequestMethod: failure.RequestMethod, UserID: failure.UserID,
		TenantID: failure.TenantID, IPAddress: failure.IPAddress, RequestID: failure.RequestID,
		ErrorClass: failure.ErrorClass, ErrorCode: failure.ErrorCode, Message: failure.Message,
		NormalizedMessage: clip(NormalizeText(failure.Message), StackMax), Stacktrace: failure.Stacktrace,
		ContextJSON: failure.ContextJSON, RequestJSON: failure.RequestJSON, OccurredAt: now, CreatedAt: now,
	}

	// The incident is written before the occurrence: the occurrence's foreign key takes a shared lock on the incident, and
	// concurrent captures of one error that each hold one and wait for the exclusive lock of the count deadlock.
	if !created {
		err = s.store.CountOccurrence(ctx, incident.ID, incident.Source, now, &occurrence)
		if err != nil {
			return apperr.Internal(err)
		}
	}

	err = s.store.CreateOccurrence(ctx, &occurrence)
	if err != nil {
		return apperr.Internal(err)
	}

	if created {
		err = s.opened(ctx, openedOf(*incident))
		if err != nil {
			return apperr.Internal(err)
		}
	}

	return nil
}

// sighted is the incident a failure is an occurrence of: the open one with its fingerprint, else the latest closed one,
// reopened, else a new one. It says whether the incident is new.
func (s *Service) sighted(ctx context.Context, key string, failure Failure, now time.Time) (*Incident, bool, error) {
	open, err := s.store.FindOpen(ctx, key)

	switch {
	case err == nil:
		open.Source = open.Source.Merge(failure.Source)

		return &open, false, nil
	case !errors.Is(err, ErrNotFound):
		return nil, false, apperr.Internal(err)
	}

	latest, err := s.store.FindLatest(ctx, key)

	switch {
	case err == nil:
		latest.Source = latest.Source.Merge(failure.Source)
		reopen(&latest, key, now)

		err = s.store.UpdateIncident(ctx, latest)
		if err != nil {
			return nil, false, apperr.Internal(err)
		}

		return &latest, false, nil
	case !errors.Is(err, ErrNotFound):
		return nil, false, apperr.Internal(err)
	}

	return s.opening(ctx, key, failure, now)
}

// opening creates the incident of a failure that has none.
func (s *Service) opening(ctx context.Context, key string, failure Failure, now time.Time) (*Incident, bool, error) {
	code, err := publicCode()
	if err != nil {
		return nil, false, apperr.Internal(err)
	}

	//nolint:exhaustruct,exhaustruct_v5 // zero by design
	incident := Incident{
		PublicCode: code, Fingerprint: key, OpenFingerprint: &key, Source: failure.Source, Status: StatusNew,
		Severity: failure.severity(), Visibility: VisibilityInternal, Title: failure.title(), Description: failure.Message,
		AffectedModule: failure.AffectedModule, AffectedRoute: failure.AffectedRoute, Environment: failure.Environment,
		ErrorClass: failure.ErrorClass, ErrorCode: failure.ErrorCode, ErrorMessageSample: failure.Message,
		StacktraceSample: failure.Stacktrace, FirstSeenAt: now, LastSeenAt: now, OccurrenceCount: 1,
		ExternalTicketProvider: TicketProviderNone, CreatedAt: now, UpdatedAt: now,
	}

	err = s.store.CreateIncident(ctx, &incident)
	if err != nil {
		return nil, false, createFailed(err)
	}

	return &incident, true, nil
}

// ClientError records an error the browser reported, as a low severity incident of SourceClientError. who is the
// signed-in person; the request ids are cut to ClientRequests of at most requestIDMax characters. The module's route
// rate-limits the person before it calls this.
func (s *Service) ClientError(ctx context.Context, who Viewer, tenantID *int, input ClientErrorInput) error {
	person, err := signedIn(who)
	if err != nil {
		return err
	}

	if strings.TrimSpace(input.Message) == "" {
		return apperr.BadRequest("message is required")
	}

	requests := make([]string, 0, min(len(input.RequestIDs), ClientRequests))

	for _, requestID := range input.RequestIDs {
		if requestID = clean(requestID, requestIDMax); requestID != "" && len(requests) < ClientRequests {
			requests = append(requests, requestID)
		}
	}

	details, err := Marshal(map[string]any{
		"route": clean(input.Route, RouteMax), "browser": clean(input.Browser, browserMax),
		"app_version": clean(input.AppVersion, versionMax), "request_ids": requests,
	})
	if err != nil {
		return apperr.Internal(err)
	}

	//nolint:exhaustruct,exhaustruct_v5 // zero by design
	return s.Capture(ctx, Failure{
		Source: SourceClientError, Message: input.Message, ErrorClass: "client", Stacktrace: input.Stack,
		AffectedModule: "client", AffectedRoute: input.Route, UserID: &person, TenantID: tenantID, ContextJSON: details,
	})
}
