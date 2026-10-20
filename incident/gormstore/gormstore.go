// Package gormstore is the SQL store of the incident module: GORM over the tables of incident/migrations, MariaDB 10.3
// and MySQL safe (no SKIP LOCKED, no FOR UPDATE OF, no FOR SHARE). It passes storetest.
package gormstore

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/datatable"
	"github.com/wssto2/go-core/incident/core"
	"github.com/wssto2/go-core/pagination"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// maxList bounds the reports and comments one incident shows.
const maxList = 1000

// The columns the store names more than once.
const (
	colModule      = "affected_module"
	colLastSeen    = "last_seen_at"
	colUpdated     = "updated_at"
	colRoute       = "affected_route"
	colEnvironment = "environment"
	colComments    = "comment_count"
	colCode        = "public_code"
	colSource      = "source"
	colTitle       = "title"
	colVisibility  = "visibility"
	colReports     = "report_count"
	colOccurrences = "occurrence_count"
)

// Store is the SQL store of incidents.
type Store struct{ db *gorm.DB }

var _ core.Store = (*Store)(nil)

// New returns the store over db.
func New(db *gorm.DB) *Store { return &Store{db: db} }

// visible restricts a query over the incidents table to what the viewer sees: the staff everything, a person the
// shared incidents they reported, wrote a report on or watch, anybody else nothing.
func visible(query *gorm.DB, table string, who core.Viewer) *gorm.DB {
	if who.Staff {
		return query
	}

	if who.UserID <= 0 {
		return query.Where("1 = 0")
	}

	return query.Where(fmt.Sprintf(`%[1]s.visibility = ? AND (
		%[1]s.reporter_user_id = ? OR
		EXISTS (SELECT 1 FROM incident_reports r WHERE r.incident_id = %[1]s.id AND r.reported_by_user_id = ?) OR
		EXISTS (SELECT 1 FROM incident_watchers w WHERE w.incident_id = %[1]s.id AND w.user_id = ?)
	)`, table), core.VisibilityShared, who.UserID, who.UserID, who.UserID)
}

// whole is a time as the DATETIME columns keep it: UTC, to the second.
func whole(t time.Time) time.Time { return t.UTC().Truncate(time.Second) }

func wholePtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}

	w := whole(*t)

	return &w
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}

	u := t.UTC()

	return &u
}

// text is the column's value for a string: NULL for an empty one.
func text(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}

	return &value
}

func plain(value *string) string {
	if value == nil {
		return ""
	}

	return *value
}

// List implements core.Store.
func (s *Store) List(
	ctx context.Context, page pagination.Page, who core.Viewer,
) (*pagination.PageResult[core.Incident], error) {
	params := datatable.QueryParams{
		View: page.View, Page: page.Number, PerPage: page.Size, Search: page.Search, OrderCol: page.OrderCol,
		OrderDir: page.OrderDir, Filters: page.Filters,
	}

	exact := func(key string) datatable.Filter {
		return datatable.NewFilter(key, func(q *gorm.DB, value, table string) *gorm.DB {
			return q.Where(table+"."+key+" = ?", value)
		})
	}

	result, err := datatable.New[incidentModel](s.conn(ctx), params).
		WithColumns([]string{
			"id", colCode, colSource, "status", "severity", colVisibility, colTitle, colModule, colRoute,
			colEnvironment, "error_class", "error_code", "first_seen_at", colLastSeen, colOccurrences, colReports,
			colComments, "reporter_user_id", "assigned_to_user_id", "resolved_at", "created_at", colUpdated,
		}).
		WithSearchableFields([]string{colCode, colTitle, "error_message_sample", colModule, colRoute}).
		WithDefaultOrder(colLastSeen, "desc").
		WithDateFilter("created_at").
		WithFilter(exact("status")).
		WithFilter(exact("severity")).
		WithFilter(exact("source")).
		WithScope(func(q *gorm.DB, table string) *gorm.DB { return visible(q, table, who) }).
		Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("list incidents: %w", err)
	}

	mapped := datatable.Map(result, func(m incidentModel) core.Incident { return m.incident() })

	//nolint:exhaustruct,exhaustruct_v5 // zero by design
	return &pagination.PageResult[core.Incident]{
		Items: mapped.Data, Meta: mapped.Meta, Total: mapped.Total, PerPage: mapped.PerPage, Page: mapped.Page,
		LastPage: mapped.LastPage, From: mapped.From, To: mapped.To,
	}, nil
}

// Find implements core.Store.
func (s *Store) Find(ctx context.Context, incidentID int, who core.Viewer) (core.Incident, error) {
	var row incidentModel

	err := visible(s.conn(ctx).Model(new(incidentModel)), "incidents", who).
		Where("incidents.id = ?", incidentID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return core.Incident{}, core.ErrNotFound
	}

	if err != nil {
		return core.Incident{}, fmt.Errorf("read incident %d: %w", incidentID, err)
	}

	return row.incident(), nil
}

// Reports implements core.Store.
func (s *Store) Reports(ctx context.Context, incidentID int, who core.Viewer) ([]core.Report, error) {
	query := s.conn(ctx).Model(new(reportModel)).Where("incident_id = ?", incidentID).
		Order("created_at ASC, id ASC").Limit(maxList)
	if !who.Staff {
		query = query.Where("reported_by_user_id = ?", who.UserID)
	}

	var rows []reportModel

	err := query.Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("list reports of incident %d: %w", incidentID, err)
	}

	out := make([]core.Report, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.report())
	}

	return out, nil
}

// Comments implements core.Store.
func (s *Store) Comments(ctx context.Context, incidentID int, who core.Viewer) ([]core.Comment, error) {
	query := s.conn(ctx).Model(new(commentModel)).Where("incident_id = ? AND deleted_at IS NULL", incidentID).
		Order("created_at ASC, id ASC").Limit(maxList)
	if !who.Staff {
		query = query.Where("visibility = ?", string(core.CommentPublic))
	}

	var rows []commentModel

	err := query.Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("list comments of incident %d: %w", incidentID, err)
	}

	out := make([]core.Comment, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.comment())
	}

	return out, nil
}

// Occurrences implements core.Store. The incident is read first with the viewer's rules, so an incident the viewer may
// not see answers ErrNotFound; the page is newest first by id, which follows the time and is unique.
func (s *Store) Occurrences(
	ctx context.Context, incidentID int, page pagination.Page, who core.Viewer,
) (*pagination.PageResult[core.Occurrence], error) {
	_, err := s.Find(ctx, incidentID, who)
	if err != nil {
		return nil, err
	}

	//nolint:exhaustruct,exhaustruct_v5 // zero by design
	params := datatable.QueryParams{Page: page.Number, PerPage: page.Size, OrderCol: "id", OrderDir: "desc"}

	result, err := datatable.New[occurrenceModel](s.conn(ctx), params).
		WithColumns([]string{
			"id", "incident_id", "environment", colModule, colRoute, "request_method", "user_id", "tenant_id", "ip_address",
			"request_id", "error_class", "error_code", "message", "stacktrace", "context_json", "request_json", "occurred_at",
			"created_at",
		}).
		WithScope(func(q *gorm.DB, table string) *gorm.DB { return q.Where(table+".incident_id = ?", incidentID) }).
		Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("list occurrences of incident %d: %w", incidentID, err)
	}

	mapped := datatable.Map(result, func(m occurrenceModel) core.Occurrence { return m.occurrence() })

	//nolint:exhaustruct,exhaustruct_v5 // zero by design
	return &pagination.PageResult[core.Occurrence]{
		Items: mapped.Data, Meta: mapped.Meta, Total: mapped.Total, PerPage: mapped.PerPage, Page: mapped.Page,
		LastPage: mapped.LastPage, From: mapped.From, To: mapped.To,
	}, nil
}

// Watching implements core.Store.
func (s *Store) Watching(ctx context.Context, incidentID, userID int) (bool, error) {
	var found int64

	err := s.conn(ctx).Model(new(watcherModel)).
		Where("incident_id = ? AND user_id = ?", incidentID, userID).Count(&found).Error
	if err != nil {
		return false, fmt.Errorf("read watcher of incident %d: %w", incidentID, err)
	}

	return found > 0, nil
}

// FindOpen implements core.Store.
func (s *Store) FindOpen(ctx context.Context, fingerprint string) (core.Incident, error) {
	return s.byFingerprint(ctx, s.conn(ctx).Where("open_fingerprint = ?", fingerprint))
}

// FindLatest implements core.Store.
func (s *Store) FindLatest(ctx context.Context, fingerprint string) (core.Incident, error) {
	return s.byFingerprint(ctx, s.conn(ctx).Where("fingerprint = ?", fingerprint).Order("last_seen_at DESC, id DESC"))
}

// isConflict recognises a write the database gave up on, because of a deadlock or a lock wait that ran out (MySQL and
// MariaDB: 1213 and 1205; SQLite: the database is locked).
func isConflict(err error) bool {
	lower := strings.ToLower(err.Error())

	return strings.Contains(lower, "deadlock found") || strings.Contains(lower, "lock wait timeout") ||
		strings.Contains(lower, "database is locked") || strings.Contains(lower, "database table is locked")
}

// failed is the error of a write: one that lost a race to the database's locks says so, for the Service to try again.
func failed(what string, err error) error {
	if isConflict(err) {
		return fmt.Errorf("%s: %w: %w", what, core.ErrConflict, err)
	}

	return fmt.Errorf("%s: %w", what, err)
}

// CreateIncident implements core.Store.
func (s *Store) CreateIncident(ctx context.Context, incident *core.Incident) error {
	row := modelOf(*incident)
	row.ID = 0

	err := s.conn(ctx).Create(&row).Error
	if err != nil {
		if database.IsDuplicateKey(err) {
			return fmt.Errorf("create incident: %w", core.ErrDuplicate)
		}

		return failed("create incident", err)
	}

	incident.ID = row.ID

	return nil
}

// UpdateIncident implements core.Store. Every column but the id and the creation time is written, a NULL included, so
// the update is a map and not a struct, which GORM would skip the zero values of.
func (s *Store) UpdateIncident(ctx context.Context, incident core.Incident) error {
	row := modelOf(incident)

	err := s.conn(ctx).Model(new(incidentModel)).Where("id = ?", incident.ID).Updates(map[string]any{
		colCode: row.PublicCode, "fingerprint": row.Fingerprint, "open_fingerprint": row.OpenFingerprint,
		colSource: row.Source, "status": row.Status, "severity": row.Severity, colVisibility: row.Visibility,
		colTitle: row.Title, "description": row.Description, colModule: row.AffectedModule,
		colRoute: row.AffectedRoute, colEnvironment: row.Environment, "error_class": row.ErrorClass,
		"error_code": row.ErrorCode, "error_message_sample": row.ErrorMessageSample,
		"stacktrace_sample": row.StacktraceSample, "first_seen_at": row.FirstSeenAt, colLastSeen: row.LastSeenAt,
		colOccurrences: row.OccurrenceCount, colReports: row.ReportCount, colComments: row.CommentCount,
		"reporter_user_id": row.ReporterUserID, "assigned_to_user_id": row.AssignedToUserID,
		"external_ticket_provider": row.ExternalTicketProvider, "external_ticket_id": row.ExternalTicketID,
		"external_ticket_url": row.ExternalTicketURL, "duplicate_of_incident_id": row.DuplicateOfIncidentID,
		"resolution_summary": row.ResolutionSummary, "resolved_at": row.ResolvedAt, colUpdated: row.UpdatedAt,
	}).Error
	if err != nil {
		return failed("update incident "+strconv.Itoa(incident.ID), err)
	}

	return nil
}

// CreateReport implements core.Store.
func (s *Store) CreateReport(ctx context.Context, report *core.Report) error {
	row := reportModelOf(*report)
	row.ID = 0

	err := s.conn(ctx).Create(&row).Error
	if err != nil {
		return failed("create report", err)
	}

	report.ID = row.ID

	return nil
}

// CreateComment implements core.Store.
func (s *Store) CreateComment(ctx context.Context, comment *core.Comment) error {
	//nolint:exhaustruct,exhaustruct_v5 // zero by design
	row := commentModel{
		IncidentID: comment.IncidentID, AuthorUserID: comment.AuthorUserID, Visibility: string(comment.Visibility),
		Body: comment.Body, EditedAt: wholePtr(comment.EditedAt), DeletedAt: wholePtr(comment.DeletedAt),
		CreatedAt: whole(comment.CreatedAt), UpdatedAt: whole(comment.UpdatedAt),
	}

	err := s.conn(ctx).Create(&row).Error
	if err != nil {
		return failed("create comment", err)
	}

	comment.ID = row.ID

	return nil
}

// CreateOccurrence implements core.Store.
func (s *Store) CreateOccurrence(ctx context.Context, sighting *core.Occurrence) error {
	//nolint:exhaustruct,exhaustruct_v5 // zero by design
	row := occurrenceModel{
		IncidentID: sighting.IncidentID, Fingerprint: sighting.Fingerprint, Environment: sighting.Environment,
		AffectedModule: text(sighting.AffectedModule), AffectedRoute: text(sighting.AffectedRoute),
		RequestMethod: text(sighting.RequestMethod),
		UserID:        sighting.UserID, TenantID: sighting.TenantID, IPAddress: text(sighting.IPAddress),
		RequestID:  text(sighting.RequestID),
		ErrorClass: text(sighting.ErrorClass), ErrorCode: text(sighting.ErrorCode), Message: sighting.Message,
		NormalizedMessage: text(sighting.NormalizedMessage), Stacktrace: text(sighting.Stacktrace),
		ContextJSON: text(sighting.ContextJSON),
		RequestJSON: text(sighting.RequestJSON), OccurredAt: whole(sighting.OccurredAt), CreatedAt: whole(sighting.CreatedAt),
	}

	err := s.conn(ctx).Create(&row).Error
	if err != nil {
		return failed("create occurrence", err)
	}

	return nil
}

func (m occurrenceModel) occurrence() core.Occurrence {
	return core.Occurrence{
		ID: m.ID, IncidentID: m.IncidentID, Fingerprint: m.Fingerprint, Environment: m.Environment,
		AffectedModule: plain(m.AffectedModule), AffectedRoute: plain(m.AffectedRoute), RequestMethod: plain(m.RequestMethod),
		UserID: m.UserID, TenantID: m.TenantID, IPAddress: plain(m.IPAddress), RequestID: plain(m.RequestID),
		ErrorClass: plain(m.ErrorClass), ErrorCode: plain(m.ErrorCode), Message: m.Message,
		NormalizedMessage: plain(m.NormalizedMessage), Stacktrace: plain(m.Stacktrace), ContextJSON: plain(m.ContextJSON),
		RequestJSON: plain(m.RequestJSON), OccurredAt: m.OccurredAt.UTC(), CreatedAt: m.CreatedAt.UTC(), User: nil,
	}
}

// CountComment implements core.Store.
func (s *Store) CountComment(ctx context.Context, incidentID int, at time.Time) error {
	return s.bump(ctx, incidentID, map[string]any{
		colComments: gorm.Expr(colComments + " + 1"), colUpdated: whole(at),
	})
}

// CountReport implements core.Store.
func (s *Store) CountReport(
	ctx context.Context, incidentID int, source core.Source, visibility string, reporterID *int, at time.Time,
) error {
	set := map[string]any{
		colReports: gorm.Expr(colReports + " + 1"), colLastSeen: whole(at), colSource: string(source),
		colVisibility: visibility, colUpdated: whole(at),
	}

	if reporterID != nil {
		set["reporter_user_id"] = gorm.Expr("COALESCE(reporter_user_id, ?)", *reporterID)
	}

	return s.bump(ctx, incidentID, set)
}

// CountOccurrence implements core.Store.
func (s *Store) CountOccurrence(
	ctx context.Context, incidentID int, source core.Source, at time.Time, sample *core.Occurrence,
) error {
	set := map[string]any{
		colOccurrences: gorm.Expr(colOccurrences + " + 1"), colLastSeen: whole(at), colSource: string(source),
		colUpdated: whole(at),
	}

	if sample != nil {
		set["error_class"], set["error_code"] = text(sample.ErrorClass), text(sample.ErrorCode)
		set["error_message_sample"], set["stacktrace_sample"] = text(sample.Message), text(sample.Stacktrace)

		for column, value := range map[string]string{
			colModule: sample.AffectedModule, colRoute: sample.AffectedRoute, colEnvironment: sample.Environment,
		} {
			if value != "" {
				set[column] = value
			}
		}
	}

	return s.bump(ctx, incidentID, set)
}

// Watch implements core.Store.
func (s *Store) Watch(ctx context.Context, incidentID, userID int, at time.Time) error {
	if userID <= 0 {
		return nil
	}

	row := watcherModel{IncidentID: incidentID, UserID: userID, CreatedAt: whole(at)}

	//nolint:exhaustruct,exhaustruct_v5 // zero by design
	err := s.conn(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error
	if err != nil {
		return failed("watch incident "+strconv.Itoa(incidentID), err)
	}

	return nil
}

// newest selects the newest occurrence of each incident, which the housekeeping keeps. The derived table keeps MySQL
// from refusing the subquery over the table it deletes from.
const newest = "(SELECT keep.id FROM (SELECT MAX(id) AS id FROM incident_occurrences GROUP BY incident_id) AS keep)"

// PruneOccurrences implements core.Store. The ids are read first and deleted by id: MySQL refuses a subquery over the
// table a DELETE changes, and MariaDB 10.3 has no multi-table delete with a limit.
func (s *Store) PruneOccurrences(ctx context.Context, before time.Time, limit int) (int, error) {
	var ids []int

	err := s.conn(ctx).Model(new(occurrenceModel)).
		Where("occurred_at < ? AND id NOT IN "+newest, whole(before)).
		Order("id ASC").Limit(limit).Pluck("id", &ids).Error
	if err != nil {
		return 0, fmt.Errorf("select old occurrences: %w", err)
	}

	if len(ids) == 0 {
		return 0, nil
	}

	res := s.conn(ctx).Where("id IN ?", ids).Delete(new(occurrenceModel))
	if res.Error != nil {
		return 0, fmt.Errorf("delete old occurrences: %w", res.Error)
	}

	return int(res.RowsAffected), nil
}

// conn is the connection to use: the transaction in ctx when there is one, else the database.
func (s *Store) conn(ctx context.Context) *gorm.DB {
	if tx, ok := database.TxFor(ctx, s.db); ok {
		return tx.WithContext(ctx)
	}

	return s.db.WithContext(ctx)
}

func (s *Store) byFingerprint(_ context.Context, q *gorm.DB) (core.Incident, error) {
	var row incidentModel

	err := q.Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return core.Incident{}, core.ErrNotFound
	}

	if err != nil {
		return core.Incident{}, fmt.Errorf("find incident by fingerprint: %w", err)
	}

	return row.incident(), nil
}

func (s *Store) bump(ctx context.Context, incidentID int, set map[string]any) error {
	err := s.conn(ctx).Model(new(incidentModel)).Where("id = ?", incidentID).Updates(set).Error
	if err != nil {
		return failed("update counts of incident "+strconv.Itoa(incidentID), err)
	}

	return nil
}

func (m incidentModel) incident() core.Incident {
	//nolint:exhaustruct,exhaustruct_v5 // zero by design
	return core.Incident{
		ID: m.ID, PublicCode: m.PublicCode, Fingerprint: m.Fingerprint, OpenFingerprint: m.OpenFingerprint,
		Source: core.Source(m.Source), Status: core.Status(m.Status), Severity: core.Severity(m.Severity),
		Visibility: m.Visibility, Title: m.Title, Description: plain(m.Description), AffectedModule: plain(m.AffectedModule),
		AffectedRoute: plain(m.AffectedRoute), Environment: plain(m.Environment), ErrorClass: plain(m.ErrorClass),
		ErrorCode: plain(m.ErrorCode), ErrorMessageSample: plain(m.ErrorMessageSample),
		StacktraceSample: plain(m.StacktraceSample), FirstSeenAt: m.FirstSeenAt.UTC(), LastSeenAt: m.LastSeenAt.UTC(),
		OccurrenceCount: m.OccurrenceCount, ReportCount: m.ReportCount, CommentCount: m.CommentCount,
		ReporterUserID: m.ReporterUserID, AssignedToUserID: m.AssignedToUserID,
		ExternalTicketProvider: m.ExternalTicketProvider, ExternalTicketID: plain(m.ExternalTicketID),
		ExternalTicketURL: plain(m.ExternalTicketURL), DuplicateOfIncidentID: m.DuplicateOfIncidentID,
		ResolutionSummary: plain(m.ResolutionSummary), ResolvedAt: utcPtr(m.ResolvedAt), CreatedAt: m.CreatedAt.UTC(),
		UpdatedAt: m.UpdatedAt.UTC(),
	}
}

func modelOf(incident core.Incident) incidentModel {
	return incidentModel{
		ID: incident.ID, PublicCode: incident.PublicCode, Fingerprint: incident.Fingerprint,
		OpenFingerprint: incident.OpenFingerprint,
		Source:          string(incident.Source), Status: string(incident.Status), Severity: string(incident.Severity),
		Visibility: incident.Visibility,
		Title:      incident.Title, Description: text(incident.Description), AffectedModule: text(incident.AffectedModule),
		AffectedRoute: text(incident.AffectedRoute), Environment: text(incident.Environment),
		ErrorClass: text(incident.ErrorClass),
		ErrorCode:  text(incident.ErrorCode), ErrorMessageSample: text(incident.ErrorMessageSample),
		StacktraceSample: text(incident.StacktraceSample), FirstSeenAt: whole(incident.FirstSeenAt),
		LastSeenAt:      whole(incident.LastSeenAt),
		OccurrenceCount: incident.OccurrenceCount, ReportCount: incident.ReportCount, CommentCount: incident.CommentCount,
		ReporterUserID: incident.ReporterUserID, AssignedToUserID: incident.AssignedToUserID,
		ExternalTicketProvider: incident.ExternalTicketProvider, ExternalTicketID: text(incident.ExternalTicketID),
		ExternalTicketURL: text(incident.ExternalTicketURL), DuplicateOfIncidentID: incident.DuplicateOfIncidentID,
		ResolutionSummary: text(incident.ResolutionSummary), ResolvedAt: wholePtr(incident.ResolvedAt),
		CreatedAt: whole(incident.CreatedAt),
		UpdatedAt: whole(incident.UpdatedAt),
	}
}

func reportModelOf(report core.Report) reportModel {
	return reportModel{
		ID: report.ID, IncidentID: report.IncidentID, ReportedByUserID: report.ReportedByUserID,
		ReportType: report.ReportType, Status: report.Status,
		Title: report.Title, Description: report.Description, StepsToReproduce: text(report.StepsToReproduce),
		ExpectedResult: text(report.ExpectedResult), ActualResult: text(report.ActualResult), Severity: report.Severity,
		AffectedModule: text(report.AffectedModule), PageURL: text(report.PageURL), BrowserInfo: text(report.BrowserInfo),
		AppVersion: text(report.AppVersion), FrontendErrorMessage: text(report.FrontendErrorMessage),
		FrontendStacktrace: text(report.FrontendStacktrace), AttachmentJSON: text(string(report.AttachmentJSON)),
		ExtraJSON: text(string(report.ExtraJSON)), CreatedAt: whole(report.CreatedAt), UpdatedAt: whole(report.UpdatedAt),
	}
}

func (m reportModel) report() core.Report {
	//nolint:exhaustruct,exhaustruct_v5 // zero by design
	return core.Report{
		ID: m.ID, IncidentID: m.IncidentID, ReportedByUserID: m.ReportedByUserID, ReportType: m.ReportType, Status: m.Status,
		Title: m.Title, Description: m.Description, StepsToReproduce: plain(m.StepsToReproduce),
		ExpectedResult: plain(m.ExpectedResult), ActualResult: plain(m.ActualResult), Severity: m.Severity,
		AffectedModule: plain(m.AffectedModule), PageURL: plain(m.PageURL), BrowserInfo: plain(m.BrowserInfo),
		AppVersion: plain(m.AppVersion), FrontendErrorMessage: plain(m.FrontendErrorMessage),
		FrontendStacktrace: plain(m.FrontendStacktrace), AttachmentJSON: jsonBytes(m.AttachmentJSON),
		ExtraJSON: jsonBytes(m.ExtraJSON), CreatedAt: m.CreatedAt.UTC(), UpdatedAt: m.UpdatedAt.UTC(),
	}
}

func jsonBytes(value *string) []byte {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil
	}

	return []byte(strings.TrimSpace(*value))
}

func (m commentModel) comment() core.Comment {
	//nolint:exhaustruct,exhaustruct_v5 // zero by design
	return core.Comment{
		ID: m.ID, IncidentID: m.IncidentID, AuthorUserID: m.AuthorUserID, Visibility: core.CommentVisibility(m.Visibility),
		Body: m.Body, EditedAt: utcPtr(m.EditedAt), DeletedAt: utcPtr(m.DeletedAt), CreatedAt: m.CreatedAt.UTC(),
		UpdatedAt: m.UpdatedAt.UTC(),
	}
}
