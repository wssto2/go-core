package core

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/pagination"
)

// Opened is what the Service tells its Config.Opened hook when a capture or a report creates a new incident, in the
// transaction that creates it. Repeats of a known incident, a reopened one included, are not announced.
type Opened struct {
	IncidentID int
	PublicCode string
	Title      string
	Severity   Severity
	Source     Source
}

// Config is what New needs.
type Config struct {
	Store      Store
	Transactor Transactor
	People     People
	Clock      Clock
	// Opened is called inside the transaction that creates an incident; an error rolls the incident back. The module
	// queues the alert in it. Nil does nothing.
	Opened func(ctx context.Context, opened Opened) error
	// Environment is what an occurrence says it happened in when the capture does not say ("production").
	Environment string
}

// Service lists, shows, reports, comments, changes and captures incidents. It is safe for concurrent use.
type Service struct {
	store  Store
	tx     Transactor
	people People
	clock  Clock
	opened func(ctx context.Context, opened Opened) error
	env    string
}

// New returns the service over the config.
func New(cfg Config) *Service {
	opened := cfg.Opened
	if opened == nil {
		opened = func(context.Context, Opened) error { return nil }
	}

	env := clean(cfg.Environment, 50)
	if env == "" {
		env = "unknown"
	}

	return &Service{store: cfg.Store, tx: cfg.Transactor, people: cfg.People, clock: cfg.Clock, opened: opened, env: env}
}

// List is a page of the incidents the viewer may see, with the people of each named.
func (s *Service) List(ctx context.Context, page pagination.Page, viewer Viewer) (*pagination.PageResult[Incident], error) {
	result, err := s.store.List(ctx, page, viewer)
	if err != nil {
		return nil, apperr.Internal(err)
	}

	var ids []int
	for _, incident := range result.Items {
		ids = append(ids, personIDs(incident)...)
	}

	names, err := s.names(ctx, ids)
	if err != nil {
		return nil, err
	}

	for i := range result.Items {
		named(&result.Items[i], names)
	}

	return result, nil
}

// Show is an incident with the reports and comments the viewer may see. An incident the viewer may not see is not found.
func (s *Service) Show(ctx context.Context, id int, viewer Viewer) (Detail, error) {
	if id <= 0 {
		return Detail{}, apperr.BadRequest("invalid incident id")
	}

	incident, err := s.store.Find(ctx, id, viewer)
	if err != nil {
		return Detail{}, notFound(err)
	}

	reports, err := s.store.Reports(ctx, id, viewer)
	if err != nil {
		return Detail{}, apperr.Internal(err)
	}

	comments, err := s.store.Comments(ctx, id, viewer)
	if err != nil {
		return Detail{}, apperr.Internal(err)
	}

	ids := personIDs(incident)
	for _, report := range reports {
		ids = append(ids, report.ReportedByUserID)
	}

	for _, comment := range comments {
		ids = append(ids, comment.AuthorUserID)
	}

	names, err := s.names(ctx, ids)
	if err != nil {
		return Detail{}, err
	}

	named(&incident, names)

	for i := range reports {
		reports[i].Reporter = names[reports[i].ReportedByUserID]
	}

	for i := range comments {
		comments[i].Author = names[comments[i].AuthorUserID]
	}

	detail := Detail{Incident: incident, Reports: reports, Comments: comments}

	if viewer.UserID > 0 {
		detail.Watching, err = s.store.Watching(ctx, id, viewer.UserID)
		if err != nil {
			return Detail{}, apperr.Internal(err)
		}
	}

	return detail, nil
}

// Comments are the comments on an incident the viewer may see, with their authors named.
func (s *Service) Comments(ctx context.Context, id int, viewer Viewer) ([]Comment, error) {
	if id <= 0 {
		return nil, apperr.BadRequest("invalid incident id")
	}

	if _, err := s.store.Find(ctx, id, viewer); err != nil {
		return nil, notFound(err)
	}

	comments, err := s.store.Comments(ctx, id, viewer)
	if err != nil {
		return nil, apperr.Internal(err)
	}

	ids := make([]int, 0, len(comments))
	for _, comment := range comments {
		ids = append(ids, comment.AuthorUserID)
	}

	names, err := s.names(ctx, ids)
	if err != nil {
		return nil, err
	}

	for i := range comments {
		comments[i].Author = names[comments[i].AuthorUserID]
	}

	return comments, nil
}

// names looks the people up in one query.
func (s *Service) names(ctx context.Context, ids []int) (map[int]*Person, error) {
	unique := make([]int, 0, len(ids))

	for _, id := range ids {
		if id > 0 && !slices.Contains(unique, id) {
			unique = append(unique, id)
		}
	}

	out := make(map[int]*Person, len(unique))
	if len(unique) == 0 {
		return out, nil
	}

	people, err := s.people.Find(ctx, unique)
	if err != nil {
		return nil, apperr.Internal(fmt.Errorf("incident: look up people: %w", err))
	}

	for _, p := range people {
		out[p.ID] = &p
	}

	return out, nil
}

// personIDs are the people an incident names.
func personIDs(incident Incident) []int {
	var ids []int
	if incident.ReporterUserID != nil {
		ids = append(ids, *incident.ReporterUserID)
	}

	if incident.AssignedToUserID != nil {
		ids = append(ids, *incident.AssignedToUserID)
	}

	return ids
}

// named fills the reporter and assignee of the incident from the people found.
func named(incident *Incident, people map[int]*Person) {
	if incident.ReporterUserID != nil {
		incident.Reporter = people[*incident.ReporterUserID]
	}

	if incident.AssignedToUserID != nil {
		incident.Assignee = people[*incident.AssignedToUserID]
	}
}

func notFound(err error) error {
	if errors.Is(err, ErrNotFound) {
		return apperr.NotFound("incident not found")
	}

	return apperr.Internal(err)
}

// now is the clock's time to the second, in UTC: what the DATETIME columns keep.
func (s *Service) now() time.Time { return s.clock.Now().UTC().Truncate(time.Second) }
