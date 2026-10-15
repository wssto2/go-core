// Package core is the framework-free part of the incident module: what an incident is, how it is classified and
// fingerprinted, the port a store implements and the Service that records, lists and changes incidents. It imports no
// HTTP, GORM or SQL package; the module's facade (package incident) wires it to them.
//
// An incident is one problem, however often it is seen: a server error captured by the module, a browser error the
// app reports, or a bug a person reports. Repeats of the same problem (the same fingerprint, INC-FINGERPRINT-001) are
// counted on one open incident.
package core

import "time"

// Visibility of an incident: who besides the staff sees it.
const (
	// VisibilityShared shows the incident to the people who reported or watch it, besides the staff.
	VisibilityShared = "reporter_and_staff"
	// VisibilityInternal shows it to the staff only.
	VisibilityInternal = "staff_only"
)

// The external ticket providers an incident can point at. The module links, it does not integrate.
const (
	TicketProviderNone   = "none"
	TicketProviderGitHub = "github"
	TicketProviderJira   = "jira"
)

// Source says what produced or updated an incident.
type Source string

// The sources. SourceMixed is an incident that was seen from more than one of the others (see Source.Merge).
const (
	SourceUserReport   Source = "user_report"
	SourceRuntimeError Source = "runtime_error"
	SourceClientError  Source = "client_error"
	SourceMixed        Source = "mixed"
)

// Status is an incident's triage state.
type Status string

// The statuses. New, triaged and in progress are open (Status.IsOpen).
const (
	StatusNew        Status = "new"
	StatusTriaged    Status = "triaged"
	StatusInProgress Status = "in_progress"
	StatusResolved   Status = "resolved"
	StatusIgnored    Status = "ignored"
	StatusDuplicate  Status = "duplicate"
)

// Severity is an incident's assessed impact.
type Severity string

// The severities, lowest first.
const (
	SeverityLow      Severity = "low"
	SeverityMedium   Severity = "medium"
	SeverityHigh     Severity = "high"
	SeverityCritical Severity = "critical"
)

// CommentVisibility says who reads a comment.
type CommentVisibility string

// The comment visibilities. An internal comment is read by the staff only.
const (
	CommentPublic   CommentVisibility = "public"
	CommentInternal CommentVisibility = "internal"
)

// Viewer is who is looking: the incidents they may see follow from it. Staff sees every incident.
type Viewer struct {
	// UserID is the person's id; 0 for a caller that is not a person (a service account), who sees only what Staff
	// lets them.
	UserID int
	// Staff is true for a holder of the manage permission.
	Staff bool
}

// Person is a name for a person's id: what a response shows of a reporter, an assignee or an author.
type Person struct {
	ID    int
	Name  string
	Email string
}

// Incident is one problem and what is known of it.
type Incident struct {
	ID         int
	PublicCode string
	// Fingerprint groups repeats; OpenFingerprint is the same while the incident is open and nil after, so that only
	// one open incident per fingerprint exists (a unique index in the store).
	Fingerprint            string
	OpenFingerprint        *string
	Source                 Source
	Status                 Status
	Severity               Severity
	Visibility             string
	Title                  string
	Description            string
	AffectedModule         string
	AffectedRoute          string
	Environment            string
	ErrorClass             string
	ErrorCode              string
	ErrorMessageSample     string
	StacktraceSample       string
	FirstSeenAt            time.Time
	LastSeenAt             time.Time
	OccurrenceCount        int
	ReportCount            int
	CommentCount           int
	ReporterUserID         *int
	AssignedToUserID       *int
	ExternalTicketProvider string
	ExternalTicketID       string
	ExternalTicketURL      string
	DuplicateOfIncidentID  *int
	ResolutionSummary      string
	ResolvedAt             *time.Time
	CreatedAt              time.Time
	UpdatedAt              time.Time
	// Reporter and Assignee are filled by the Service from the people directory, not stored.
	Reporter *Person
	Assignee *Person
}

// Report is one person's report of a problem, behind an incident.
type Report struct {
	ID                   int
	IncidentID           *int
	ReportedByUserID     int
	ReportType           string
	Status               string
	Title                string
	Description          string
	StepsToReproduce     string
	ExpectedResult       string
	ActualResult         string
	Severity             string
	AffectedModule       string
	PageURL              string
	BrowserInfo          string
	AppVersion           string
	FrontendErrorMessage string
	FrontendStacktrace   string
	// AttachmentJSON and ExtraJSON are JSON documents the Service validated; they are bytes, not a wire type.
	AttachmentJSON []byte
	ExtraJSON      []byte
	CreatedAt      time.Time
	UpdatedAt      time.Time
	Reporter       *Person
}

// Comment is a remark on an incident.
type Comment struct {
	ID           int
	IncidentID   int
	AuthorUserID int
	Visibility   CommentVisibility
	Body         string
	EditedAt     *time.Time
	DeletedAt    *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
	Author       *Person
}

// Occurrence is one sighting of an incident: a captured error with the request it happened in.
type Occurrence struct {
	IncidentID        int
	Fingerprint       string
	Environment       string
	AffectedModule    string
	AffectedRoute     string
	RequestMethod     string
	UserID            *int
	TenantID          *int
	IPAddress         string
	RequestID         string
	ErrorClass        string
	ErrorCode         string
	Message           string
	NormalizedMessage string
	Stacktrace        string
	ContextJSON       string
	RequestJSON       string
	OccurredAt        time.Time
	CreatedAt         time.Time
}

// Detail is an incident with what hangs on it: the reports and comments the viewer may see, and whether they watch it.
type Detail struct {
	Incident Incident
	Reports  []Report
	Comments []Comment
	Watching bool
}
