package core

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wssto2/go-core/apperr"
)

const (
	// publicCodeBytes is the random bytes of a public code, which is twice as many hexadecimal digits.
	publicCodeBytes = 6
	// maxAttempts bounds the retry of a write that lost a race: to create the same incident, or to the database's locks.
	maxAttempts = 5
	// retryWait is the wait before the second attempt; it doubles with each.
	retryWait = 5 * time.Millisecond
)

// lostRace reports whether a write should be tried again in a transaction of its own.
func lostRace(err error) bool { return errors.Is(err, ErrDuplicate) || errors.Is(err, ErrConflict) }

// transact runs work in a transaction, again when it lost a race (a concurrent request created the same incident, or
// the database chose it to resolve a deadlock), up to maxAttempts times.
func (s *Service) transact(ctx context.Context, work func(ctx context.Context) error) error {
	var err error

	wait := retryWait

	for range maxAttempts {
		err = s.tx.WithinTransaction(ctx, work)
		if !lostRace(err) {
			if err != nil {
				return fmt.Errorf("incident: %w", err)
			}

			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("incident: %w", ctx.Err())
		case <-time.After(wait):
			wait *= 2
		}
	}

	return apperr.Internal(fmt.Errorf("incident: gave up after %d attempts: %w", maxAttempts, err))
}

// signedIn is the person's id, or 401 for a viewer who is not a person.
func signedIn(who Viewer) (int, error) {
	if who.UserID <= 0 {
		return 0, apperr.Unauthorized("not authenticated")
	}

	return who.UserID, nil
}

// Report files a person's bug report. A report of a problem already open (the same module, page, title and error
// message, INC-FINGERPRINT-001) joins that incident, and a closed one is reopened. The reporter watches the incident.
// The answer is the incident as the reporter sees it.
func (s *Service) Report(ctx context.Context, who Viewer, input ReportInput) (Detail, error) {
	reporter, err := signedIn(who)
	if err != nil {
		return Detail{}, err
	}

	err = input.validate()
	if err != nil {
		return Detail{}, err
	}

	input.sanitize()

	var incidentID int

	err = s.transact(ctx, func(ctx context.Context) error {
		var filed error

		incidentID, filed = s.fileReport(ctx, reporter, input)

		return filed
	})
	if err != nil {
		return Detail{}, err
	}

	return s.Show(ctx, incidentID, who)
}

func (s *Service) fileReport(ctx context.Context, reporter int, input ReportInput) (int, error) {
	now := s.now()
	key := Fingerprint("report", input.AffectedModule, input.PageURL, input.Title, input.FrontendErrorMessage)

	incident, created, err := s.reportedIncident(ctx, key, input, reporter, now)
	if err != nil {
		return 0, err
	}

	if created {
		err = s.opened(ctx, openedOf(*incident))
	} else {
		err = s.store.CountReport(ctx, incident.ID, incident.Source, incident.Visibility, &reporter, now)
	}

	if err != nil {
		return 0, apperr.Internal(err)
	}

	incidentID := incident.ID
	report := reportOf(incidentID, reporter, input, now)

	err = s.store.CreateReport(ctx, &report)
	if err != nil {
		return 0, apperr.Internal(err)
	}

	err = s.watch(ctx, incidentID, reporter, now)
	if err != nil {
		return 0, err
	}

	return incidentID, nil
}

// reportOf is the report row of an input.
func reportOf(incidentID, reporter int, input ReportInput, now time.Time) Report {
	//nolint:exhaustruct,exhaustruct_v5 // zero by design
	return Report{
		IncidentID: &incidentID, ReportedByUserID: reporter, ReportType: "manual", Status: "linked", Title: input.Title,
		Description: input.Description, StepsToReproduce: input.StepsToReproduce, ExpectedResult: input.ExpectedResult,
		ActualResult: input.ActualResult, Severity: string(Severity(input.Severity).OrDefault()),
		AffectedModule: input.AffectedModule, PageURL: input.PageURL, BrowserInfo: input.BrowserInfo,
		AppVersion: input.AppVersion, FrontendErrorMessage: input.FrontendErrorMessage,
		FrontendStacktrace: input.FrontendStacktrace, AttachmentJSON: []byte(input.AttachmentJSON),
		ExtraJSON: []byte(input.ExtraJSON), CreatedAt: now, UpdatedAt: now,
	}
}

// reportedIncident is the incident a report belongs to: the open one with its fingerprint, else the latest closed one,
// reopened, else a new one. It says whether the incident is new.
func (s *Service) reportedIncident(
	ctx context.Context, key string, input ReportInput, reporter int, now time.Time,
) (*Incident, bool, error) {
	open, err := s.store.FindOpen(ctx, key)

	switch {
	case err == nil:
		open.Source = open.Source.Merge(SourceUserReport)
		open.Visibility = VisibilityShared

		return &open, false, nil
	case !errors.Is(err, ErrNotFound):
		return nil, false, apperr.Internal(err)
	}

	latest, err := s.store.FindLatest(ctx, key)

	switch {
	case err == nil:
		latest.Source = latest.Source.Merge(SourceUserReport)
		latest.Visibility = VisibilityShared
		reopen(&latest, key, now)

		err = s.store.UpdateIncident(ctx, latest)
		if err != nil {
			return nil, false, apperr.Internal(err)
		}

		return &latest, false, nil
	case !errors.Is(err, ErrNotFound):
		return nil, false, apperr.Internal(err)
	}

	return s.reported(ctx, key, input, reporter, now)
}

// reported creates the incident of a report that has none.
func (s *Service) reported(
	ctx context.Context, key string, input ReportInput, reporter int, now time.Time,
) (*Incident, bool, error) {
	code, err := publicCode()
	if err != nil {
		return nil, false, apperr.Internal(err)
	}

	//nolint:exhaustruct,exhaustruct_v5 // zero by design
	incident := Incident{
		PublicCode: code, Fingerprint: key, OpenFingerprint: &key, Source: SourceUserReport, Status: StatusNew,
		Severity: Severity(input.Severity).OrDefault(), Visibility: VisibilityShared, Title: input.Title,
		Description: input.Description, AffectedModule: input.AffectedModule, AffectedRoute: input.PageURL,
		ErrorMessageSample: input.FrontendErrorMessage, StacktraceSample: input.FrontendStacktrace, FirstSeenAt: now,
		LastSeenAt: now, ReportCount: 1, ReporterUserID: &reporter, ExternalTicketProvider: TicketProviderNone,
		CreatedAt: now, UpdatedAt: now,
	}

	err = s.store.CreateIncident(ctx, &incident)
	if err != nil {
		return nil, false, createFailed(err)
	}

	return &incident, true, nil
}

// reopen makes a closed incident new again because the problem was seen again.
func reopen(incident *Incident, key string, now time.Time) {
	incident.Status = StatusNew
	incident.OpenFingerprint = &key
	incident.ResolvedAt = nil
	incident.LastSeenAt = now
	incident.UpdatedAt = now
}

// createFailed is a failed CreateIncident: a duplicate is kept as it is, for the caller to try again.
func createFailed(err error) error {
	if errors.Is(err, ErrDuplicate) {
		return err
	}

	return apperr.Internal(err)
}

func openedOf(incident Incident) Opened {
	return Opened{
		IncidentID: incident.ID, PublicCode: incident.PublicCode, Title: incident.Title, Severity: incident.Severity,
		Source: incident.Source,
	}
}

// publicCode is the code people quote, such as INC-3F9A0C41B2D7.
func publicCode() (string, error) {
	random := make([]byte, publicCodeBytes)

	_, err := rand.Read(random)
	if err != nil {
		return "", fmt.Errorf("generate public incident code: %w", err)
	}

	return "INC-" + strings.ToUpper(hex.EncodeToString(random)), nil
}

// Comment adds a comment to an incident the viewer may see. Anybody but the staff writes a public comment whatever
// they ask for. The writer watches the incident.
func (s *Service) Comment(ctx context.Context, incidentID int, who Viewer, input CommentInput) (*Comment, error) {
	author, err := signedIn(who)
	if err != nil {
		return nil, err
	}

	if incidentID <= 0 {
		return nil, apperr.BadRequest("invalid incident id")
	}

	err = input.validate()
	if err != nil {
		return nil, err
	}

	input.sanitize()

	visibility := CommentVisibility(input.Visibility)
	if !who.Staff {
		visibility = CommentPublic
	}

	//nolint:exhaustruct,exhaustruct_v5 // zero by design
	comment := Comment{IncidentID: incidentID, AuthorUserID: author, Visibility: visibility, Body: input.Body}

	err = s.transact(ctx, func(ctx context.Context) error { return s.addComment(ctx, who, &comment) })
	if errors.Is(err, ErrNotFound) {
		return nil, apperr.NotFound("incident not found")
	}

	if err != nil {
		return nil, err
	}

	names, err := s.names(ctx, []int{author})
	if err != nil {
		return nil, err
	}

	comment.Author = names[author]

	return &comment, nil
}

// addComment writes the comment, counts it on the incident the viewer may see, and makes its author a watcher.
func (s *Service) addComment(ctx context.Context, who Viewer, comment *Comment) error {
	_, err := s.store.Find(ctx, comment.IncidentID, who)
	if err != nil {
		return fmt.Errorf("find the incident: %w", err)
	}

	now := s.now()
	comment.CreatedAt, comment.UpdatedAt = now, now

	// Count first: the comment's foreign key takes a shared lock on the incident, and two comments that each hold one
	// and wait for the exclusive lock of the count deadlock.
	err = s.store.CountComment(ctx, comment.IncidentID, now)
	if err != nil {
		return apperr.Internal(err)
	}

	err = s.store.CreateComment(ctx, comment)
	if err != nil {
		return apperr.Internal(err)
	}

	return s.watch(ctx, comment.IncidentID, comment.AuthorUserID, now)
}

func (s *Service) watch(ctx context.Context, incidentID, person int, at time.Time) error {
	err := s.store.Watch(ctx, incidentID, person, at)
	if err != nil {
		return apperr.Internal(err)
	}

	return nil
}

// Update changes an incident the viewer may see: its triage state, severity, visibility, assignee, the incident it
// duplicates, the resolution and the external ticket it points at. A field left out stays. An open status keeps the
// incident open for repeats, a closing one stops grouping repeats under it, and resolving stamps the time.
func (s *Service) Update(ctx context.Context, incidentID int, who Viewer, input UpdateInput) (Detail, error) {
	_, err := signedIn(who)
	if err != nil {
		return Detail{}, err
	}

	if incidentID <= 0 {
		return Detail{}, apperr.BadRequest("invalid incident id")
	}

	err = input.validate()
	if err != nil {
		return Detail{}, err
	}

	input.sanitize()

	err = s.transact(ctx, func(ctx context.Context) error {
		incident, found := s.store.Find(ctx, incidentID, who)
		if found != nil {
			return fmt.Errorf("find the incident: %w", found)
		}

		apply(&incident, input, s.now())

		return s.store.UpdateIncident(ctx, incident)
	})
	if err != nil {
		return Detail{}, notFound(err)
	}

	return s.Show(ctx, incidentID, who)
}

// apply puts the fields of the input that are set on the incident.
func apply(incident *Incident, input UpdateInput, now time.Time) {
	if input.Status != "" {
		setStatus(incident, Status(input.Status), now)
	}

	if input.Severity != "" {
		incident.Severity = Severity(input.Severity)
	}

	if input.Visibility != "" {
		incident.Visibility = input.Visibility
	}

	incident.AssignedToUserID = positive(input.AssignedToUserID, incident.AssignedToUserID)
	incident.DuplicateOfIncidentID = positive(input.DuplicateOfIncidentID, incident.DuplicateOfIncidentID)

	if input.ResolutionSummary != "" {
		incident.ResolutionSummary = input.ResolutionSummary
	}

	setTicket(incident, input)

	incident.UpdatedAt = now
}

// setStatus moves the incident to a status: an open one keeps it open for repeats, any other stops grouping them, and
// resolving stamps the time.
func setStatus(incident *Incident, status Status, now time.Time) {
	incident.Status = status

	if status.IsOpen() {
		incident.OpenFingerprint = &incident.Fingerprint
		incident.ResolvedAt = nil

		return
	}

	incident.OpenFingerprint = nil

	if status == StatusResolved {
		incident.ResolvedAt = &now
	}
}

// setTicket points the incident at an external ticket; no provider clears the ticket.
func setTicket(incident *Incident, input UpdateInput) {
	if input.ExternalTicketProvider != "" {
		incident.ExternalTicketProvider = input.ExternalTicketProvider
	}

	if input.ExternalTicketProvider == TicketProviderNone {
		incident.ExternalTicketID, incident.ExternalTicketURL = "", ""

		return
	}

	if input.ExternalTicketID != "" {
		incident.ExternalTicketID = input.ExternalTicketID
	}

	if input.ExternalTicketURL != "" {
		incident.ExternalTicketURL = input.ExternalTicketURL
	}
}

// positive is the id given when it is set and positive, nil when it is set to zero or less (clears), else the old one.
func positive(given, old *int) *int {
	switch {
	case given == nil:
		return old
	case *given <= 0:
		return nil
	default:
		return given
	}
}
