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

// Store is the SQL store of incidents.
type Store struct{ db *gorm.DB }

var _ core.Store = (*Store)(nil)

// New returns the store over db.
func New(db *gorm.DB) *Store { return &Store{db: db} }

// conn is the connection to use: the transaction in ctx when there is one, else the database.
func (s *Store) conn(ctx context.Context) *gorm.DB {
	if tx, ok := database.TxFromContext(ctx); ok {
		return tx.WithContext(ctx)
	}

	return s.db.WithContext(ctx)
}

// visible restricts a query over the incidents table to what the viewer sees: the staff everything, a person the
// shared incidents they reported, wrote a report on or watch, anybody else nothing.
func visible(q *gorm.DB, table string, who core.Viewer) *gorm.DB {
	if who.Staff {
		return q
	}

	if who.UserID <= 0 {
		return q.Where("1 = 0")
	}

	return q.Where(fmt.Sprintf(`%[1]s.visibility = ? AND (
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
func (s *Store) List(ctx context.Context, page pagination.Page, who core.Viewer) (*pagination.PageResult[core.Incident], error) {
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
			"id", "public_code", "source", "status", "severity", "visibility", "title", "affected_module", "affected_route",
			"environment", "error_class", "error_code", "first_seen_at", "last_seen_at", "occurrence_count", "report_count",
			"comment_count", "reporter_user_id", "assigned_to_user_id", "resolved_at", "created_at", "updated_at",
		}).
		WithSearchableFields([]string{"public_code", "title", "error_message_sample", "affected_module", "affected_route"}).
		WithDefaultOrder("last_seen_at", "desc").
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

	return &pagination.PageResult[core.Incident]{
		Items: mapped.Data, Meta: mapped.Meta, Total: mapped.Total, PerPage: mapped.PerPage, Page: mapped.Page,
		LastPage: mapped.LastPage, From: mapped.From, To: mapped.To,
	}, nil
}

// Find implements core.Store.
func (s *Store) Find(ctx context.Context, id int, who core.Viewer) (core.Incident, error) {
	var row incidentModel

	err := visible(s.conn(ctx).Model(&incidentModel{}), "incidents", who).Where("incidents.id = ?", id).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return core.Incident{}, core.ErrNotFound
	}

	if err != nil {
		return core.Incident{}, fmt.Errorf("read incident %d: %w", id, err)
	}

	return row.incident(), nil
}

// Reports implements core.Store.
func (s *Store) Reports(ctx context.Context, incidentID int, who core.Viewer) ([]core.Report, error) {
	q := s.conn(ctx).Model(&reportModel{}).Where("incident_id = ?", incidentID).Order("created_at ASC, id ASC").Limit(maxList)
	if !who.Staff {
		q = q.Where("reported_by_user_id = ?", who.UserID)
	}

	var rows []reportModel
	if err := q.Find(&rows).Error; err != nil {
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
	q := s.conn(ctx).Model(&commentModel{}).Where("incident_id = ? AND deleted_at IS NULL", incidentID).
		Order("created_at ASC, id ASC").Limit(maxList)
	if !who.Staff {
		q = q.Where("visibility = ?", string(core.CommentPublic))
	}

	var rows []commentModel
	if err := q.Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list comments of incident %d: %w", incidentID, err)
	}

	out := make([]core.Comment, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.comment())
	}

	return out, nil
}

// Watching implements core.Store.
func (s *Store) Watching(ctx context.Context, incidentID, userID int) (bool, error) {
	var n int64

	err := s.conn(ctx).Model(&watcherModel{}).Where("incident_id = ? AND user_id = ?", incidentID, userID).Count(&n).Error
	if err != nil {
		return false, fmt.Errorf("read watcher of incident %d: %w", incidentID, err)
	}

	return n > 0, nil
}

// FindOpen implements core.Store.
func (s *Store) FindOpen(ctx context.Context, fingerprint string) (core.Incident, error) {
	return s.byFingerprint(ctx, s.conn(ctx).Where("open_fingerprint = ?", fingerprint))
}

// FindLatest implements core.Store.
func (s *Store) FindLatest(ctx context.Context, fingerprint string) (core.Incident, error) {
	return s.byFingerprint(ctx, s.conn(ctx).Where("fingerprint = ?", fingerprint).Order("last_seen_at DESC, id DESC"))
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

// isDuplicate recognises a unique-key violation on SQLite, MySQL and MariaDB.
func isDuplicate(err error) bool {
	lower := strings.ToLower(err.Error())

	return strings.Contains(lower, "unique constraint failed") || strings.Contains(lower, "duplicate entry")
}

// CreateIncident implements core.Store.
func (s *Store) CreateIncident(ctx context.Context, incident *core.Incident) error {
	row := modelOf(*incident)
	row.ID = 0

	if err := s.conn(ctx).Create(&row).Error; err != nil {
		if isDuplicate(err) {
			return fmt.Errorf("create incident: %w", core.ErrDuplicate)
		}

		return failed("create incident", err)
	}

	incident.ID = row.ID

	return nil
}

// UpdateIncident implements core.Store.
func (s *Store) UpdateIncident(ctx context.Context, incident core.Incident) error {
	row := modelOf(incident)

	err := s.conn(ctx).Model(&incidentModel{}).Where("id = ?", incident.ID).
		Select("*").Omit("id", "created_at").Updates(&row).Error
	if err != nil {
		return failed("update incident "+strconv.Itoa(incident.ID), err)
	}

	return nil
}

// CreateReport implements core.Store.
func (s *Store) CreateReport(ctx context.Context, report *core.Report) error {
	row := reportModelOf(*report)
	row.ID = 0

	if err := s.conn(ctx).Create(&row).Error; err != nil {
		return failed("create report", err)
	}

	report.ID = row.ID

	return nil
}

// CreateComment implements core.Store.
func (s *Store) CreateComment(ctx context.Context, comment *core.Comment) error {
	row := commentModel{
		IncidentID: comment.IncidentID, AuthorUserID: comment.AuthorUserID, Visibility: string(comment.Visibility),
		Body: comment.Body, EditedAt: wholePtr(comment.EditedAt), DeletedAt: wholePtr(comment.DeletedAt),
		CreatedAt: whole(comment.CreatedAt), UpdatedAt: whole(comment.UpdatedAt),
	}

	if err := s.conn(ctx).Create(&row).Error; err != nil {
		return failed("create comment", err)
	}

	comment.ID = row.ID

	return nil
}

// CreateOccurrence implements core.Store.
func (s *Store) CreateOccurrence(ctx context.Context, o *core.Occurrence) error {
	row := occurrenceModel{
		IncidentID: o.IncidentID, Fingerprint: o.Fingerprint, Environment: o.Environment,
		AffectedModule: text(o.AffectedModule), AffectedRoute: text(o.AffectedRoute), RequestMethod: text(o.RequestMethod),
		UserID: o.UserID, TenantID: o.TenantID, IPAddress: text(o.IPAddress), RequestID: text(o.RequestID),
		ErrorClass: text(o.ErrorClass), ErrorCode: text(o.ErrorCode), Message: o.Message,
		NormalizedMessage: text(o.NormalizedMessage), Stacktrace: text(o.Stacktrace), ContextJSON: text(o.ContextJSON),
		RequestJSON: text(o.RequestJSON), OccurredAt: whole(o.OccurredAt), CreatedAt: whole(o.CreatedAt),
	}

	if err := s.conn(ctx).Create(&row).Error; err != nil {
		return failed("create occurrence", err)
	}

	return nil
}

func (s *Store) bump(ctx context.Context, incidentID int, set map[string]any) error {
	err := s.conn(ctx).Model(&incidentModel{}).Where("id = ?", incidentID).Updates(set).Error
	if err != nil {
		return failed("update counts of incident "+strconv.Itoa(incidentID), err)
	}

	return nil
}

// CountComment implements core.Store.
func (s *Store) CountComment(ctx context.Context, incidentID int, at time.Time) error {
	return s.bump(ctx, incidentID, map[string]any{"comment_count": gorm.Expr("comment_count + 1"), "updated_at": whole(at)})
}

// CountReport implements core.Store.
func (s *Store) CountReport(
	ctx context.Context, incidentID int, source core.Source, visibility string, reporterID *int, at time.Time,
) error {
	set := map[string]any{
		"report_count": gorm.Expr("report_count + 1"), "last_seen_at": whole(at), "source": string(source),
		"visibility": visibility, "updated_at": whole(at),
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
		"occurrence_count": gorm.Expr("occurrence_count + 1"), "last_seen_at": whole(at), "source": string(source),
		"updated_at": whole(at),
	}

	if sample != nil {
		set["error_class"], set["error_code"] = text(sample.ErrorClass), text(sample.ErrorCode)
		set["error_message_sample"], set["stacktrace_sample"] = text(sample.Message), text(sample.Stacktrace)

		for column, value := range map[string]string{
			"affected_module": sample.AffectedModule, "affected_route": sample.AffectedRoute, "environment": sample.Environment,
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

	err := s.conn(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error
	if err != nil {
		return failed("watch incident "+strconv.Itoa(incidentID), err)
	}

	return nil
}

// PruneOccurrences implements core.Store. The ids are read first and deleted by id: MySQL refuses a subquery over the
// table a DELETE changes, and MariaDB 10.3 has no multi-table delete with a limit.
func (s *Store) PruneOccurrences(ctx context.Context, before time.Time, limit int) (int, error) {
	var ids []int

	err := s.conn(ctx).Model(&occurrenceModel{}).
		Where("occurred_at < ? AND id NOT IN (SELECT keep.id FROM (SELECT MAX(id) AS id FROM incident_occurrences GROUP BY incident_id) AS keep)",
			whole(before)).
		Order("id ASC").Limit(limit).Pluck("id", &ids).Error
	if err != nil {
		return 0, fmt.Errorf("select old occurrences: %w", err)
	}

	if len(ids) == 0 {
		return 0, nil
	}

	res := s.conn(ctx).Where("id IN ?", ids).Delete(&occurrenceModel{})
	if res.Error != nil {
		return 0, fmt.Errorf("delete old occurrences: %w", res.Error)
	}

	return int(res.RowsAffected), nil
}

func (m incidentModel) incident() core.Incident {
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

func modelOf(i core.Incident) incidentModel {
	return incidentModel{
		ID: i.ID, PublicCode: i.PublicCode, Fingerprint: i.Fingerprint, OpenFingerprint: i.OpenFingerprint,
		Source: string(i.Source), Status: string(i.Status), Severity: string(i.Severity), Visibility: i.Visibility,
		Title: i.Title, Description: text(i.Description), AffectedModule: text(i.AffectedModule),
		AffectedRoute: text(i.AffectedRoute), Environment: text(i.Environment), ErrorClass: text(i.ErrorClass),
		ErrorCode: text(i.ErrorCode), ErrorMessageSample: text(i.ErrorMessageSample),
		StacktraceSample: text(i.StacktraceSample), FirstSeenAt: whole(i.FirstSeenAt), LastSeenAt: whole(i.LastSeenAt),
		OccurrenceCount: i.OccurrenceCount, ReportCount: i.ReportCount, CommentCount: i.CommentCount,
		ReporterUserID: i.ReporterUserID, AssignedToUserID: i.AssignedToUserID,
		ExternalTicketProvider: i.ExternalTicketProvider, ExternalTicketID: text(i.ExternalTicketID),
		ExternalTicketURL: text(i.ExternalTicketURL), DuplicateOfIncidentID: i.DuplicateOfIncidentID,
		ResolutionSummary: text(i.ResolutionSummary), ResolvedAt: wholePtr(i.ResolvedAt), CreatedAt: whole(i.CreatedAt),
		UpdatedAt: whole(i.UpdatedAt),
	}
}

func reportModelOf(r core.Report) reportModel {
	return reportModel{
		ID: r.ID, IncidentID: r.IncidentID, ReportedByUserID: r.ReportedByUserID, ReportType: r.ReportType, Status: r.Status,
		Title: r.Title, Description: r.Description, StepsToReproduce: text(r.StepsToReproduce),
		ExpectedResult: text(r.ExpectedResult), ActualResult: text(r.ActualResult), Severity: r.Severity,
		AffectedModule: text(r.AffectedModule), PageURL: text(r.PageURL), BrowserInfo: text(r.BrowserInfo),
		AppVersion: text(r.AppVersion), FrontendErrorMessage: text(r.FrontendErrorMessage),
		FrontendStacktrace: text(r.FrontendStacktrace), AttachmentJSON: text(string(r.AttachmentJSON)),
		ExtraJSON: text(string(r.ExtraJSON)), CreatedAt: whole(r.CreatedAt), UpdatedAt: whole(r.UpdatedAt),
	}
}

func (m reportModel) report() core.Report {
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
	return core.Comment{
		ID: m.ID, IncidentID: m.IncidentID, AuthorUserID: m.AuthorUserID, Visibility: core.CommentVisibility(m.Visibility),
		Body: m.Body, EditedAt: utcPtr(m.EditedAt), DeletedAt: utcPtr(m.DeletedAt), CreatedAt: m.CreatedAt.UTC(),
		UpdatedAt: m.UpdatedAt.UTC(),
	}
}
