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

// maxAttempts bounds the retry of a write that lost a race to create the same incident.
const maxAttempts = 3

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
func (s *Service) Report(ctx context.Context, who Viewer, in ReportInput) (Detail, error) {
	reporter, err := signedIn(who)
	if err != nil {
		return Detail{}, err
	}

	if err := in.validate(); err != nil {
		return Detail{}, err
	}

	in.sanitize()

	var id int

	for attempt := 1; ; attempt++ {
		err = s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
			var err error

			id, err = s.fileReport(ctx, reporter, in)

			return err
		})
		if !errors.Is(err, ErrDuplicate) || attempt == maxAttempts {
			break
		}
	}

	if err != nil {
		return Detail{}, err
	}

	return s.Show(ctx, id, who)
}

func (s *Service) fileReport(ctx context.Context, reporter int, in ReportInput) (int, error) {
	now := s.now()
	fp := Fingerprint("report", in.AffectedModule, in.PageURL, in.Title, in.FrontendErrorMessage)

	incident, created, err := s.reportedIncident(ctx, fp, in, reporter, now)
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

	id := incident.ID
	report := Report{
		IncidentID: &id, ReportedByUserID: reporter, ReportType: "manual", Status: "linked", Title: in.Title,
		Description: in.Description, StepsToReproduce: in.StepsToReproduce, ExpectedResult: in.ExpectedResult,
		ActualResult: in.ActualResult, Severity: string(Severity(in.Severity).OrDefault()), AffectedModule: in.AffectedModule,
		PageURL: in.PageURL, BrowserInfo: in.BrowserInfo, AppVersion: in.AppVersion,
		FrontendErrorMessage: in.FrontendErrorMessage, FrontendStacktrace: in.FrontendStacktrace,
		AttachmentJSON: []byte(in.AttachmentJSON), ExtraJSON: []byte(in.ExtraJSON), CreatedAt: now, UpdatedAt: now,
	}

	if err := s.store.CreateReport(ctx, &report); err != nil {
		return 0, apperr.Internal(err)
	}

	if err := s.watch(ctx, id, reporter, now); err != nil {
		return 0, err
	}

	return id, nil
}

// reportedIncident is the incident a report belongs to: the open one with its fingerprint, else the latest closed one,
// reopened, else a new one. It says whether the incident is new.
func (s *Service) reportedIncident(
	ctx context.Context, fp string, in ReportInput, reporter int, now time.Time,
) (*Incident, bool, error) {
	open, err := s.store.FindOpen(ctx, fp)

	switch {
	case err == nil:
		open.Source = open.Source.Merge(SourceUserReport)
		open.Visibility = VisibilityShared

		return &open, false, nil
	case !errors.Is(err, ErrNotFound):
		return nil, false, apperr.Internal(err)
	}

	latest, err := s.store.FindLatest(ctx, fp)

	switch {
	case err == nil:
		latest.Source = latest.Source.Merge(SourceUserReport)
		latest.Visibility = VisibilityShared
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
		PublicCode: code, Fingerprint: fp, OpenFingerprint: &fp, Source: SourceUserReport, Status: StatusNew,
		Severity: Severity(in.Severity).OrDefault(), Visibility: VisibilityShared, Title: in.Title,
		Description: in.Description, AffectedModule: in.AffectedModule, AffectedRoute: in.PageURL,
		ErrorMessageSample: in.FrontendErrorMessage, StacktraceSample: in.FrontendStacktrace, FirstSeenAt: now,
		LastSeenAt: now, ReportCount: 1, ReporterUserID: &reporter, ExternalTicketProvider: TicketProviderNone,
		CreatedAt: now, UpdatedAt: now,
	}

	if err := s.store.CreateIncident(ctx, &incident); err != nil {
		return nil, false, createFailed(err)
	}

	return &incident, true, nil
}

// reopen makes a closed incident new again because the problem was seen again.
func reopen(incident *Incident, fp string, now time.Time) {
	incident.Status = StatusNew
	incident.OpenFingerprint = &fp
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
	random := make([]byte, 6)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate public incident code: %w", err)
	}

	return "INC-" + strings.ToUpper(hex.EncodeToString(random)), nil
}

// Comment adds a comment to an incident the viewer may see. Anybody but the staff writes a public comment whatever
// they ask for. The writer watches the incident.
func (s *Service) Comment(ctx context.Context, incidentID int, who Viewer, in CommentInput) (*Comment, error) {
	author, err := signedIn(who)
	if err != nil {
		return nil, err
	}

	if incidentID <= 0 {
		return nil, apperr.BadRequest("invalid incident id")
	}

	if err := in.validate(); err != nil {
		return nil, err
	}

	in.sanitize()

	visibility := CommentVisibility(in.Visibility)
	if !who.Staff {
		visibility = CommentPublic
	}

	var created Comment

	err = s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if _, err := s.store.Find(ctx, incidentID, who); err != nil {
			return err
		}

		now := s.now()
		created = Comment{
			IncidentID: incidentID, AuthorUserID: author, Visibility: visibility, Body: in.Body, CreatedAt: now, UpdatedAt: now,
		}

		if err := s.store.CreateComment(ctx, &created); err != nil {
			return apperr.Internal(err)
		}

		if err := s.store.CountComment(ctx, incidentID, now); err != nil {
			return apperr.Internal(err)
		}

		return s.watch(ctx, incidentID, author, now)
	})
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, apperr.NotFound("incident not found")
		}

		return nil, err
	}

	names, err := s.names(ctx, []int{author})
	if err != nil {
		return nil, err
	}

	created.Author = names[author]

	return &created, nil
}

func (s *Service) watch(ctx context.Context, incidentID, person int, at time.Time) error {
	if err := s.store.Watch(ctx, incidentID, person, at); err != nil {
		return apperr.Internal(err)
	}

	return nil
}

// Update changes an incident the viewer may see: its triage state, severity, visibility, assignee, the incident it
// duplicates, the resolution and the external ticket it points at. A field left out stays. An open status keeps the
// incident open for repeats, a closing one stops grouping repeats under it, and resolving stamps the time.
func (s *Service) Update(ctx context.Context, incidentID int, who Viewer, in UpdateInput) (Detail, error) {
	if _, err := signedIn(who); err != nil {
		return Detail{}, err
	}

	if incidentID <= 0 {
		return Detail{}, apperr.BadRequest("invalid incident id")
	}

	if err := in.validate(); err != nil {
		return Detail{}, err
	}

	in.sanitize()

	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		incident, err := s.store.Find(ctx, incidentID, who)
		if err != nil {
			return err
		}

		apply(&incident, in, s.now())

		return s.store.UpdateIncident(ctx, incident)
	})
	if err != nil {
		return Detail{}, notFound(err)
	}

	return s.Show(ctx, incidentID, who)
}

// apply puts the fields of the input that are set on the incident.
func apply(incident *Incident, in UpdateInput, now time.Time) {
	if in.Status != "" {
		incident.Status = Status(in.Status)

		if incident.Status.IsOpen() {
			incident.OpenFingerprint = &incident.Fingerprint
			incident.ResolvedAt = nil
		} else {
			incident.OpenFingerprint = nil

			if incident.Status == StatusResolved {
				incident.ResolvedAt = &now
			}
		}
	}

	if in.Severity != "" {
		incident.Severity = Severity(in.Severity)
	}

	if in.Visibility != "" {
		incident.Visibility = in.Visibility
	}

	incident.AssignedToUserID = positive(in.AssignedToUserID, incident.AssignedToUserID)
	incident.DuplicateOfIncidentID = positive(in.DuplicateOfIncidentID, incident.DuplicateOfIncidentID)

	if in.ResolutionSummary != "" {
		incident.ResolutionSummary = in.ResolutionSummary
	}

	if in.ExternalTicketProvider != "" {
		incident.ExternalTicketProvider = in.ExternalTicketProvider
	}

	switch {
	case in.ExternalTicketProvider == TicketProviderNone:
		incident.ExternalTicketID, incident.ExternalTicketURL = "", ""
	default:
		if in.ExternalTicketID != "" {
			incident.ExternalTicketID = in.ExternalTicketID
		}

		if in.ExternalTicketURL != "" {
			incident.ExternalTicketURL = in.ExternalTicketURL
		}
	}

	incident.UpdatedAt = now
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
