package incident

import (
	"encoding/json"
	"time"

	"github.com/wssto2/go-core/incident/core"
	"github.com/wssto2/go-core/pagination"
	"github.com/wssto2/go-core/route"
	"github.com/wssto2/go-core/web/typed"
)

// DateTime is a time on the wire: RFC 3339, or null for a time that is not set. The generator writes it as a string.
type DateTime struct{ time.Time }

// MarshalJSON writes the time as RFC 3339.
func (d DateTime) MarshalJSON() ([]byte, error) {
	if d.IsZero() {
		return []byte("null"), nil
	}

	return json.Marshal(d.Format(time.RFC3339)) //nolint:wrapcheck // a string cannot fail to encode
}

func stamp(t time.Time) DateTime { return DateTime{Time: t} }

func stampPtr(t *time.Time) *DateTime {
	if t == nil {
		return nil
	}

	d := stamp(*t)

	return &d
}

// PersonResponse is a person named on an incident: the reporter, the assignee, an author.
type PersonResponse struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

func personOf(p *core.Person) *PersonResponse {
	if p == nil {
		return nil
	}

	return &PersonResponse{ID: p.ID, Name: p.Name, Email: p.Email}
}

// Incident is an incident on the wire. The fingerprints never cross it.
//
//nolint:tagliatelle // snake_case wire
type Incident struct {
	ID                     int             `json:"id"`
	PublicCode             string          `json:"public_code"`
	Source                 string          `json:"source"`
	Status                 string          `json:"status"`
	Severity               string          `json:"severity"`
	Visibility             string          `json:"visibility"`
	Title                  string          `json:"title"`
	Description            string          `json:"description,omitempty"`
	AffectedModule         string          `json:"affected_module,omitempty"`
	AffectedRoute          string          `json:"affected_route,omitempty"`
	Environment            string          `json:"environment,omitempty"`
	ErrorClass             string          `json:"error_class,omitempty"`
	ErrorCode              string          `json:"error_code,omitempty"`
	ErrorMessageSample     string          `json:"error_message_sample,omitempty"`
	StacktraceSample       string          `json:"stacktrace_sample,omitempty"`
	FirstSeenAt            DateTime        `json:"first_seen_at"`
	LastSeenAt             DateTime        `json:"last_seen_at"`
	OccurrenceCount        int             `json:"occurrence_count"`
	ReportCount            int             `json:"report_count"`
	CommentCount           int             `json:"comment_count"`
	ReporterUserID         *int            `json:"reporter_user_id,omitempty"`
	AssignedToUserID       *int            `json:"assigned_to_user_id,omitempty"`
	ExternalTicketProvider string          `json:"external_ticket_provider"`
	ExternalTicketID       string          `json:"external_ticket_id,omitempty"`
	ExternalTicketURL      string          `json:"external_ticket_url,omitempty"`
	DuplicateOfIncidentID  *int            `json:"duplicate_of_incident_id,omitempty"`
	ResolutionSummary      string          `json:"resolution_summary,omitempty"`
	ResolvedAt             *DateTime       `json:"resolved_at,omitempty"`
	CreatedAt              DateTime        `json:"created_at"`
	UpdatedAt              DateTime        `json:"updated_at"`
	Reporter               *PersonResponse `json:"reporter,omitempty"`
	Assignee               *PersonResponse `json:"assignee,omitempty"`
}

func incidentOf(incident core.Incident) Incident {
	return Incident{
		ID: incident.ID, PublicCode: incident.PublicCode, Source: string(incident.Source),
		Status: string(incident.Status), Severity: string(incident.Severity), Visibility: incident.Visibility,
		Title: incident.Title, Description: incident.Description, AffectedModule: incident.AffectedModule,
		AffectedRoute: incident.AffectedRoute, Environment: incident.Environment, ErrorClass: incident.ErrorClass,
		ErrorCode: incident.ErrorCode, ErrorMessageSample: incident.ErrorMessageSample,
		StacktraceSample: incident.StacktraceSample, FirstSeenAt: stamp(incident.FirstSeenAt),
		LastSeenAt: stamp(incident.LastSeenAt), OccurrenceCount: incident.OccurrenceCount,
		ReportCount: incident.ReportCount, CommentCount: incident.CommentCount, ReporterUserID: incident.ReporterUserID,
		AssignedToUserID: incident.AssignedToUserID, ExternalTicketProvider: incident.ExternalTicketProvider,
		ExternalTicketID: incident.ExternalTicketID, ExternalTicketURL: incident.ExternalTicketURL,
		DuplicateOfIncidentID: incident.DuplicateOfIncidentID, ResolutionSummary: incident.ResolutionSummary,
		ResolvedAt: stampPtr(incident.ResolvedAt), CreatedAt: stamp(incident.CreatedAt),
		UpdatedAt: stamp(incident.UpdatedAt), Reporter: personOf(incident.Reporter), Assignee: personOf(incident.Assignee),
	}
}

// CommentResponse is a comment on the wire.
//
//nolint:tagliatelle // snake_case wire
type CommentResponse struct {
	ID           int             `json:"id"`
	IncidentID   int             `json:"incident_id"`
	AuthorUserID int             `json:"author_user_id"`
	Visibility   string          `json:"visibility"`
	Body         string          `json:"body"`
	EditedAt     *DateTime       `json:"edited_at,omitempty"`
	DeletedAt    *DateTime       `json:"deleted_at,omitempty"`
	CreatedAt    DateTime        `json:"created_at"`
	UpdatedAt    DateTime        `json:"updated_at"`
	Author       *PersonResponse `json:"author,omitempty"`
}

func commentOf(c core.Comment) CommentResponse {
	return CommentResponse{
		ID: c.ID, IncidentID: c.IncidentID, AuthorUserID: c.AuthorUserID, Visibility: string(c.Visibility), Body: c.Body,
		EditedAt: stampPtr(c.EditedAt), DeletedAt: stampPtr(c.DeletedAt), CreatedAt: stamp(c.CreatedAt),
		UpdatedAt: stamp(c.UpdatedAt), Author: personOf(c.Author),
	}
}

// ReportResponse is one person's report behind an incident.
//
//nolint:tagliatelle // snake_case wire
type ReportResponse struct {
	ID                   int             `json:"id"`
	IncidentID           *int            `json:"incident_id,omitempty"`
	ReportedByUserID     int             `json:"reported_by_user_id"`
	ReportType           string          `json:"report_type"`
	Status               string          `json:"status"`
	Title                string          `json:"title"`
	Description          string          `json:"description"`
	StepsToReproduce     string          `json:"steps_to_reproduce,omitempty"`
	ExpectedResult       string          `json:"expected_result,omitempty"`
	ActualResult         string          `json:"actual_result,omitempty"`
	Severity             string          `json:"severity"`
	AffectedModule       string          `json:"affected_module,omitempty"`
	PageURL              string          `json:"page_url,omitempty"`
	BrowserInfo          string          `json:"browser_info,omitempty"`
	AppVersion           string          `json:"app_version,omitempty"`
	FrontendErrorMessage string          `json:"frontend_error_message,omitempty"`
	FrontendStacktrace   string          `json:"frontend_stacktrace,omitempty"`
	AttachmentJSON       any             `json:"attachment_json,omitempty"` // a JSON document (json.RawMessage)
	ExtraJSON            any             `json:"extra_json,omitempty"`      // a JSON document (json.RawMessage)
	CreatedAt            DateTime        `json:"created_at"`
	UpdatedAt            DateTime        `json:"updated_at"`
	Reporter             *PersonResponse `json:"reporter,omitempty"`
}

// document is a stored JSON document as the response carries it: raw, or absent.
func document(raw []byte) any {
	if len(raw) == 0 {
		return nil
	}

	return json.RawMessage(raw)
}

func reportOf(report core.Report) ReportResponse {
	return ReportResponse{
		ID: report.ID, IncidentID: report.IncidentID, ReportedByUserID: report.ReportedByUserID,
		ReportType: report.ReportType, Status: report.Status,
		Title: report.Title, Description: report.Description, StepsToReproduce: report.StepsToReproduce,
		ExpectedResult: report.ExpectedResult,
		ActualResult:   report.ActualResult, Severity: report.Severity, AffectedModule: report.AffectedModule,
		PageURL:     report.PageURL,
		BrowserInfo: report.BrowserInfo, AppVersion: report.AppVersion, FrontendErrorMessage: report.FrontendErrorMessage,
		FrontendStacktrace: report.FrontendStacktrace, AttachmentJSON: document(report.AttachmentJSON),
		ExtraJSON: document(report.ExtraJSON),
		CreatedAt: stamp(report.CreatedAt), UpdatedAt: stamp(report.UpdatedAt), Reporter: personOf(report.Reporter),
	}
}

// OccurrenceResponse is one sighting of an incident: where and when a failure happened, who met it, and the request
// and context captured with it (credentials already redacted). Context and Request are JSON documents.
//
//nolint:tagliatelle // snake_case wire
type OccurrenceResponse struct {
	ID          int             `json:"id"`
	IncidentID  int             `json:"incident_id"`
	OccurredAt  DateTime        `json:"occurred_at"`
	Environment string          `json:"environment,omitempty"`
	Module      string          `json:"module,omitempty"`
	Route       string          `json:"route,omitempty"`
	Method      string          `json:"method,omitempty"`
	UserID      *int            `json:"user_id,omitempty"`
	User        *PersonResponse `json:"user,omitempty"`
	IPAddress   string          `json:"ip_address,omitempty"`
	RequestID   string          `json:"request_id,omitempty"`
	ErrorClass  string          `json:"error_class,omitempty"`
	ErrorCode   string          `json:"error_code,omitempty"`
	Message     string          `json:"message"`
	Stacktrace  string          `json:"stacktrace,omitempty"`
	Context     any             `json:"context,omitempty"` // a JSON document (json.RawMessage)
	Request     any             `json:"request,omitempty"` // a JSON document (json.RawMessage)
}

func occurrenceOf(o core.Occurrence) OccurrenceResponse {
	return OccurrenceResponse{
		ID: o.ID, IncidentID: o.IncidentID, OccurredAt: stamp(o.OccurredAt), Environment: o.Environment,
		Module: o.AffectedModule, Route: o.AffectedRoute, Method: o.RequestMethod, UserID: o.UserID, User: personOf(o.User),
		IPAddress: o.IPAddress, RequestID: o.RequestID, ErrorClass: o.ErrorClass, ErrorCode: o.ErrorCode,
		Message: o.Message, Stacktrace: o.Stacktrace, Context: document([]byte(o.ContextJSON)),
		Request: document([]byte(o.RequestJSON)),
	}
}

// Meta is what an incident's answer carries beside it: the reports and comments the viewer may see and whether
// they watch the incident. The fields are in the order of the keys sorted, which is how the first application wrote
// them.
type Meta struct {
	Comments []CommentResponse `json:"comments"`
	Reports  []ReportResponse  `json:"reports"`
	Watching bool              `json:"watching"`
}

// Shown is one incident with its meta, the answer of the show and update routes.
type Shown = route.WithMeta[Incident, Meta]

// Created is a newly filed report's incident: the whole record is the 201's data, a nested data/meta envelope.
type Created struct {
	Data Incident `json:"data"`
	Meta Meta     `json:"meta"`
}

func metaOf(detail core.Detail) Meta {
	meta := Meta{
		Comments: make([]CommentResponse, 0, len(detail.Comments)), Reports: make([]ReportResponse, 0, len(detail.Reports)),
		Watching: detail.Watching,
	}

	for _, c := range detail.Comments {
		meta.Comments = append(meta.Comments, commentOf(c))
	}

	for _, r := range detail.Reports {
		meta.Reports = append(meta.Reports, reportOf(r))
	}

	return meta
}

func recordOf(detail core.Detail) Shown {
	return Shown{Items: incidentOf(detail.Incident), Info: metaOf(detail)}
}

func pageOf(result *pagination.PageResult[core.Incident]) route.Page[Incident] {
	return typed.ToPage(pagination.MapResult(result, incidentOf))
}

func occurrencePageOf(result *pagination.PageResult[core.Occurrence]) route.Page[OccurrenceResponse] {
	return typed.ToPage(pagination.MapResult(result, occurrenceOf))
}
