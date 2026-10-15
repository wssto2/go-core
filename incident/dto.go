package incident

import (
	"encoding/json"
	"time"

	"github.com/wssto2/go-core/incident/core"
	"github.com/wssto2/go-core/pagination"
	"github.com/wssto2/go-core/route"
	"github.com/wssto2/go-core/web/typed"
)

// Stamp is a time on the wire: RFC 3339, or null for a time that is not set.
type Stamp struct{ time.Time }

// MarshalJSON writes the time as RFC 3339.
func (s Stamp) MarshalJSON() ([]byte, error) {
	if s.IsZero() {
		return []byte("null"), nil
	}

	return json.Marshal(s.Format(time.RFC3339)) //nolint:wrapcheck // a string cannot fail to encode
}

func stamp(t time.Time) Stamp { return Stamp{Time: t} }

func stampPtr(t *time.Time) *Stamp {
	if t == nil {
		return nil
	}

	s := stamp(*t)

	return &s
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

// IncidentResponse is an incident on the wire. The fingerprints never cross it.
type IncidentResponse struct {
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
	FirstSeenAt            Stamp           `json:"first_seen_at"`
	LastSeenAt             Stamp           `json:"last_seen_at"`
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
	ResolvedAt             *Stamp          `json:"resolved_at,omitempty"`
	CreatedAt              Stamp           `json:"created_at"`
	UpdatedAt              Stamp           `json:"updated_at"`
	Reporter               *PersonResponse `json:"reporter,omitempty"`
	Assignee               *PersonResponse `json:"assignee,omitempty"`
}

func incidentOf(i core.Incident) IncidentResponse {
	return IncidentResponse{
		ID: i.ID, PublicCode: i.PublicCode, Source: string(i.Source), Status: string(i.Status), Severity: string(i.Severity),
		Visibility: i.Visibility, Title: i.Title, Description: i.Description, AffectedModule: i.AffectedModule,
		AffectedRoute: i.AffectedRoute, Environment: i.Environment, ErrorClass: i.ErrorClass, ErrorCode: i.ErrorCode,
		ErrorMessageSample: i.ErrorMessageSample, StacktraceSample: i.StacktraceSample, FirstSeenAt: stamp(i.FirstSeenAt),
		LastSeenAt: stamp(i.LastSeenAt), OccurrenceCount: i.OccurrenceCount, ReportCount: i.ReportCount,
		CommentCount: i.CommentCount, ReporterUserID: i.ReporterUserID, AssignedToUserID: i.AssignedToUserID,
		ExternalTicketProvider: i.ExternalTicketProvider, ExternalTicketID: i.ExternalTicketID,
		ExternalTicketURL: i.ExternalTicketURL, DuplicateOfIncidentID: i.DuplicateOfIncidentID,
		ResolutionSummary: i.ResolutionSummary, ResolvedAt: stampPtr(i.ResolvedAt), CreatedAt: stamp(i.CreatedAt),
		UpdatedAt: stamp(i.UpdatedAt), Reporter: personOf(i.Reporter), Assignee: personOf(i.Assignee),
	}
}

// CommentResponse is a comment on the wire.
type CommentResponse struct {
	ID           int             `json:"id"`
	IncidentID   int             `json:"incident_id"`
	AuthorUserID int             `json:"author_user_id"`
	Visibility   string          `json:"visibility"`
	Body         string          `json:"body"`
	EditedAt     *Stamp          `json:"edited_at,omitempty"`
	DeletedAt    *Stamp          `json:"deleted_at,omitempty"`
	CreatedAt    Stamp           `json:"created_at"`
	UpdatedAt    Stamp           `json:"updated_at"`
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
	AttachmentJSON       json.RawMessage `json:"attachment_json,omitempty"`
	ExtraJSON            json.RawMessage `json:"extra_json,omitempty"`
	CreatedAt            Stamp           `json:"created_at"`
	UpdatedAt            Stamp           `json:"updated_at"`
	Reporter             *PersonResponse `json:"reporter,omitempty"`
}

func reportOf(r core.Report) ReportResponse {
	return ReportResponse{
		ID: r.ID, IncidentID: r.IncidentID, ReportedByUserID: r.ReportedByUserID, ReportType: r.ReportType, Status: r.Status,
		Title: r.Title, Description: r.Description, StepsToReproduce: r.StepsToReproduce, ExpectedResult: r.ExpectedResult,
		ActualResult: r.ActualResult, Severity: r.Severity, AffectedModule: r.AffectedModule, PageURL: r.PageURL,
		BrowserInfo: r.BrowserInfo, AppVersion: r.AppVersion, FrontendErrorMessage: r.FrontendErrorMessage,
		FrontendStacktrace: r.FrontendStacktrace, AttachmentJSON: r.AttachmentJSON, ExtraJSON: r.ExtraJSON,
		CreatedAt: stamp(r.CreatedAt), UpdatedAt: stamp(r.UpdatedAt), Reporter: personOf(r.Reporter),
	}
}

// IncidentMeta is what an incident's answer carries beside it: the reports and comments the viewer may see and whether
// they watch the incident. The fields are in the order of the keys sorted, which is how the first application wrote them.
type IncidentMeta struct {
	Comments []CommentResponse `json:"comments"`
	Reports  []ReportResponse  `json:"reports"`
	Watching bool              `json:"watching"`
}

// IncidentRecord is one incident with its meta, the answer of the show and update routes.
type IncidentRecord = route.WithMeta[IncidentResponse, IncidentMeta]

// IncidentCreated is a newly filed report's incident: the whole record is the 201's data, a nested data/meta envelope.
type IncidentCreated struct {
	Data IncidentResponse `json:"data"`
	Meta IncidentMeta     `json:"meta"`
}

func metaOf(d core.Detail) IncidentMeta {
	meta := IncidentMeta{
		Comments: make([]CommentResponse, 0, len(d.Comments)), Reports: make([]ReportResponse, 0, len(d.Reports)),
		Watching: d.Watching,
	}

	for _, c := range d.Comments {
		meta.Comments = append(meta.Comments, commentOf(c))
	}

	for _, r := range d.Reports {
		meta.Reports = append(meta.Reports, reportOf(r))
	}

	return meta
}

func recordOf(d core.Detail) IncidentRecord {
	return IncidentRecord{Items: incidentOf(d.Incident), Info: metaOf(d)}
}

func pageOf(result *pagination.PageResult[core.Incident]) route.Page[IncidentResponse] {
	return typed.ToPage(pagination.MapResult(result, incidentOf))
}
