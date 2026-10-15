package core

import (
	"encoding/json"
	"strings"

	"github.com/wssto2/go-core/apperr"
)

// The bounds of the stored text; a longer value is refused by the inputs and cut by Capture.
const (
	TitleMax       = 255
	MessageMax     = 2000
	StackMax       = 20000
	RouteMax       = 255
	ModuleMax      = 100
	ClientRequests = 20
	requestIDMax   = 100
)

// ReportInput is a person's bug report.
type ReportInput struct {
	Title                string `json:"title" validation:"max:255"`
	Description          string `json:"description" validation:"max:20000"`
	StepsToReproduce     string `json:"steps_to_reproduce" validation:"max:20000"`
	ExpectedResult       string `json:"expected_result" validation:"max:20000"`
	ActualResult         string `json:"actual_result" validation:"max:20000"`
	Severity             string `json:"severity" validation:"max:8"`
	AffectedModule       string `json:"affected_module" validation:"max:100"`
	PageURL              string `json:"page_url" validation:"max:1000"`
	BrowserInfo          string `json:"browser_info" validation:"max:500"`
	AppVersion           string `json:"app_version" validation:"max:100"`
	FrontendErrorMessage string `json:"frontend_error_message" validation:"max:2000"`
	FrontendStacktrace   string `json:"frontend_stacktrace" validation:"max:20000"`
	AttachmentJSON       string `json:"attachment_json" validation:"max:20000"`
	ExtraJSON            string `json:"extra_json" validation:"max:20000"`
}

// validate refuses a report without a title or a description, with a severity that is not one, or with JSON that is not.
func (in *ReportInput) validate() error {
	switch {
	case strings.TrimSpace(in.Title) == "":
		return apperr.BadRequest("title is required")
	case strings.TrimSpace(in.Description) == "":
		return apperr.BadRequest("description is required")
	case in.Severity != "" && !Severity(in.Severity).Valid():
		return apperr.BadRequest("invalid severity")
	case !validJSON(in.AttachmentJSON):
		return apperr.BadRequest("invalid attachment_json payload")
	case !validJSON(in.ExtraJSON):
		return apperr.BadRequest("invalid extra_json payload")
	}

	return nil
}

// sanitize trims the text, defaults the severity and the JSON documents.
func (in *ReportInput) sanitize() {
	in.Title = clean(in.Title, TitleMax)
	in.Description = clean(in.Description, StackMax)
	in.StepsToReproduce = clean(in.StepsToReproduce, StackMax)
	in.ExpectedResult = clean(in.ExpectedResult, StackMax)
	in.ActualResult = clean(in.ActualResult, StackMax)
	in.Severity = string(Severity(strings.TrimSpace(in.Severity)).OrDefault())
	in.AffectedModule = clean(in.AffectedModule, ModuleMax)
	in.PageURL = clean(in.PageURL, 1000)
	in.BrowserInfo = clean(in.BrowserInfo, 500)
	in.AppVersion = clean(in.AppVersion, 100)
	in.FrontendErrorMessage = clean(in.FrontendErrorMessage, MessageMax)
	in.FrontendStacktrace = clean(in.FrontendStacktrace, StackMax)
	in.AttachmentJSON = jsonOr(in.AttachmentJSON, "[]")
	in.ExtraJSON = jsonOr(in.ExtraJSON, "{}")
}

// CommentInput is a comment on an incident. Visibility is "public" (the default) or "internal"; only the staff can
// write an internal one.
type CommentInput struct {
	Body       string `json:"body" validation:"max:20000"`
	Visibility string `json:"visibility" validation:"max:8"`
}

func (in *CommentInput) validate() error {
	switch {
	case strings.TrimSpace(in.Body) == "":
		return apperr.BadRequest("body is required")
	case in.Visibility != "" && in.Visibility != string(CommentPublic) && in.Visibility != string(CommentInternal):
		return apperr.BadRequest("invalid comment visibility")
	}

	return nil
}

func (in *CommentInput) sanitize() {
	in.Body = clean(in.Body, StackMax)
	if strings.TrimSpace(in.Visibility) == "" {
		in.Visibility = string(CommentPublic)
	}
}

// UpdateInput is a change of an incident; a field left out is left as it is.
type UpdateInput struct {
	Status                 string `json:"status" validation:"max:11"`
	Severity               string `json:"severity" validation:"max:8"`
	Visibility             string `json:"visibility" validation:"max:18"`
	AssignedToUserID       *int   `json:"assigned_to_user_id"`
	DuplicateOfIncidentID  *int   `json:"duplicate_of_incident_id"`
	ResolutionSummary      string `json:"resolution_summary" validation:"max:2000"`
	ExternalTicketProvider string `json:"external_ticket_provider" validation:"max:6"`
	ExternalTicketID       string `json:"external_ticket_id" validation:"max:100"`
	ExternalTicketURL      string `json:"external_ticket_url" validation:"max:500"`
}

func (in *UpdateInput) validate() error {
	switch {
	case in.Status != "" && !Status(in.Status).Valid():
		return apperr.BadRequest("invalid status")
	case in.Severity != "" && !Severity(in.Severity).Valid():
		return apperr.BadRequest("invalid severity")
	case in.Visibility != "" && in.Visibility != VisibilityShared && in.Visibility != VisibilityInternal:
		return apperr.BadRequest("invalid visibility")
	case in.ExternalTicketProvider != "" && !validProvider(in.ExternalTicketProvider):
		return apperr.BadRequest("invalid external_ticket_provider")
	}

	return nil
}

func (in *UpdateInput) sanitize() {
	in.Status = strings.TrimSpace(in.Status)
	in.Severity = strings.TrimSpace(in.Severity)
	in.Visibility = strings.TrimSpace(in.Visibility)
	in.ResolutionSummary = clean(in.ResolutionSummary, 2000)
	in.ExternalTicketProvider = strings.TrimSpace(in.ExternalTicketProvider)
	in.ExternalTicketID = clean(in.ExternalTicketID, 100)
	in.ExternalTicketURL = clean(in.ExternalTicketURL, 500)
}

func validProvider(p string) bool {
	return p == TicketProviderNone || p == TicketProviderGitHub || p == TicketProviderJira
}

// ClientErrorInput is an error the browser reports: what the script threw and where the person was. Every field is
// bounded; the request ids are those of the last API calls the page made, which link the browser error to the server
// incident of a call that failed.
type ClientErrorInput struct {
	Message    string   `json:"message" validation:"required|max:2000"`
	Stack      string   `json:"stack" validation:"max:20000"`
	Route      string   `json:"route" validation:"max:255"`
	Browser    string   `json:"browser" validation:"max:500"`
	AppVersion string   `json:"app_version" validation:"max:100"`
	RequestIDs []string `json:"request_ids" validation:"max:20"`
}

// validJSON reports whether the text is empty or a JSON document.
func validJSON(text string) bool {
	trimmed := strings.TrimSpace(text)

	return trimmed == "" || json.Valid([]byte(trimmed))
}

// jsonOr is the text trimmed when it is JSON, else the fallback.
func jsonOr(text, fallback string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || !json.Valid([]byte(trimmed)) {
		return fallback
	}

	return trimmed
}
