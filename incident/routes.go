package incident

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/incident/core"
	"github.com/wssto2/go-core/ratelimit"
	"github.com/wssto2/go-core/route"
	"github.com/wssto2/go-core/tenancy"
	"github.com/wssto2/go-core/web"
)

// The permissions of the routes: reading is ViewIncidents, filing a report and commenting CreateIncidents, changing an
// incident UpdateIncidents. A holder of ManageIncidents is the staff: they see every person's incidents, not only
// the ones they reported, wrote a report on or watch, they write internal comments, and they are the ones an alert
// goes to. The ids are fixed.
const (
	ViewIncidents   = "incident:view"
	CreateIncidents = "incident:create"
	UpdateIncidents = "incident:update"
	ManageIncidents = "incident:manage"
)

// DefinePermissions adds the four permissions to the catalogue when it lacks them, so an application that already
// defined them (with its own labels) keeps its definitions. Call it on the application's catalogue before access.Install
// builds the engine: start-up checks that every route's permission is defined.
func DefinePermissions(c *authz.Catalogue) error {
	for _, def := range []struct {
		id   string
		opts []authz.DefineOption
	}{
		{ViewIncidents, nil},
		{CreateIncidents, []authz.DefineOption{authz.Requires(ViewIncidents)}},
		{UpdateIncidents, []authz.DefineOption{authz.Requires(ViewIncidents)}},
		{ManageIncidents, []authz.DefineOption{authz.Requires(ViewIncidents)}},
	} {
		if _, ok := c.Lookup(def.id); ok {
			continue
		}

		err := c.Define(def.id, def.opts...)
		if err != nil {
			return err //nolint:wrapcheck // the catalogue names the permission
		}
	}

	return nil
}

const (
	base   = "/v1/incidents"
	record = base + "/:incidentID"
)

// IncidentPath names an incident. A non-number answers 400 for the whole group.
type IncidentPath struct {
	IncidentID int `path:"incidentID"`
}

// ListQuery is the list's query: the paging, search, view and order, and every other parameter is a filter (status,
// severity, source, created_at).
type ListQuery struct {
	Query url.Values `json:"-" query:"*"`
}

// CommentRequest is the incident and the new comment.
type CommentRequest struct {
	IncidentPath
	core.CommentInput
}

// UpdateRequest is the incident and its new state.
type UpdateRequest struct {
	IncidentPath
	core.UpdateInput
}

// Routes is the declared HTTP contract of the incident module (paths under /v1, before the application's
// gocore.WithPrefix): what the TypeScript generator reads, without installing anything.
//
//nolint:gochecknoglobals // a contract is declared once, as a value
var Routes = Declare().Contract()

// Declared is the routes of the module as values.
type Declared struct {
	// List is a page of the incidents the caller may see, last seen first.
	List route.Route[ListQuery, route.Page[IncidentResponse]]
	// Show is one incident with the reports and comments the caller may see.
	Show route.Route[IncidentPath, IncidentRecord]
	// Comments are the comments on an incident.
	Comments route.Route[IncidentPath, []CommentResponse]
	// Report files a person's bug report (201): a report of an open problem joins its incident.
	Report route.Route[core.ReportInput, IncidentCreated]
	// Comment adds a comment (201).
	Comment route.Route[CommentRequest, CommentResponse]
	// Update changes an incident.
	Update route.Route[UpdateRequest, IncidentRecord]
	// ClientError records an error the browser reports (204): any signed-in person may, a person's reports are rate
	// limited.
	ClientError route.Route[core.ClientErrorInput, route.Empty]

	group *route.Contract
}

// Declare declares the routes.
func Declare() *Declared {
	d := &Declared{
		List: route.Get[ListQuery, route.Page[IncidentResponse]](base).Name("incident.index").Requires(ViewIncidents),
		Show: route.Get[IncidentPath, IncidentRecord](record).Name("incident.show").Requires(ViewIncidents),
		Comments: route.Get[IncidentPath, []CommentResponse](record + "/comments").
			Name("incident.comments").Requires(ViewIncidents),
		Report: route.Post[core.ReportInput, IncidentCreated](base + "/reports").
			Name("incident.report").Requires(CreateIncidents).PersonOnly().Status(http.StatusCreated),
		Comment: route.Post[CommentRequest, CommentResponse](record + "/comments").
			Name("incident.comment").Requires(CreateIncidents).PersonOnly().Status(http.StatusCreated),
		Update: route.Patch[UpdateRequest, IncidentRecord](record).
			Name("incident.update").Requires(UpdateIncidents).PersonOnly(),
		ClientError: route.Post[core.ClientErrorInput, route.Empty](base + "/client-errors").
			Name("incident.client-error").Authenticated().PersonOnly().Status(http.StatusNoContent),
	}

	d.group = route.Group("incident", d.List, d.Show, d.Comments, d.Report, d.Comment, d.Update, d.ClientError).
		MalformedPath(http.StatusBadRequest)

	return d
}

// Contract is the routes as a group: what contract.Generate reads.
func (d *Declared) Contract() *route.Contract { return d.group }

// To binds a handler to every route; hand the result to app.Routes.
func (d *Declared) To(h *Handlers) []route.Handled {
	return []route.Handled{
		d.List.To(h.list), d.Show.To(h.show), d.Comments.To(h.comments), d.Report.To(h.report), d.Comment.To(h.comment),
		d.Update.To(h.update), d.ClientError.To(h.clientError),
	}
}

// Handlers are the handlers of the routes over the service.
type Handlers struct {
	service    *core.Service
	authorizer func() authz.Authorizer
	limiter    ratelimit.Limiter
	log        interface {
		DebugContext(ctx context.Context, msg string, args ...any)
	}
}

// viewer is who is asking: a person by their id, the staff when they hold ManageIncidents. A service account is no
// person (UserID 0).
func (h *Handlers) viewer(ctx context.Context) (core.Viewer, error) {
	principal, ok := authz.PrincipalFrom(ctx)
	if !ok {
		return core.Viewer{}, apperr.Unauthorized("not authenticated")
	}

	viewer := core.Viewer{}
	if principal.Kind == authz.KindUser {
		viewer.UserID = principal.ID
	}

	authorizer := h.authorizer()
	if authorizer == nil {
		return viewer, nil
	}

	err := authorizer.Require(ctx, ManageIncidents)

	switch {
	case err == nil:
		viewer.Staff = true
	case errors.Is(err, authz.ErrForbidden) || apperr.HasCode(err, apperr.CodePermissionDenied):
	default:
		return core.Viewer{}, err //nolint:wrapcheck // the authorizer's own answer
	}

	return viewer, nil
}

func (h *Handlers) list(ctx context.Context, in ListQuery) (route.Page[IncidentResponse], error) {
	who, err := h.viewer(ctx)
	if err != nil {
		return route.Page[IncidentResponse]{}, err
	}

	result, err := h.service.List(ctx, web.PageFromValues(in.Query), who)
	if err != nil {
		return route.Page[IncidentResponse]{}, err //nolint:wrapcheck // the service's own error
	}

	return pageOf(result), nil
}

func (h *Handlers) show(ctx context.Context, in IncidentPath) (IncidentRecord, error) {
	return h.detail(ctx, in.IncidentID, func(who core.Viewer) (core.Detail, error) {
		return h.service.Show(ctx, in.IncidentID, who)
	})
}

func (h *Handlers) update(ctx context.Context, in UpdateRequest) (IncidentRecord, error) {
	return h.detail(ctx, in.IncidentID, func(who core.Viewer) (core.Detail, error) {
		return h.service.Update(ctx, in.IncidentID, who, in.UpdateInput)
	})
}

// detail answers an incident record from what the service returns for the viewer.
func (h *Handlers) detail(
	ctx context.Context, incidentID int, answer func(core.Viewer) (core.Detail, error),
) (IncidentRecord, error) {
	err := web.PositiveID("incidentID", incidentID)
	if err != nil {
		return IncidentRecord{}, err //nolint:wrapcheck // web's own answer
	}

	who, err := h.viewer(ctx)
	if err != nil {
		return IncidentRecord{}, err
	}

	detail, err := answer(who)
	if err != nil {
		return IncidentRecord{}, err
	}

	return recordOf(detail), nil
}

func (h *Handlers) comments(ctx context.Context, in IncidentPath) ([]CommentResponse, error) {
	err := web.PositiveID("incidentID", in.IncidentID)
	if err != nil {
		return nil, err //nolint:wrapcheck // web's own answer
	}

	who, err := h.viewer(ctx)
	if err != nil {
		return nil, err
	}

	comments, err := h.service.Comments(ctx, in.IncidentID, who)
	if err != nil {
		return nil, err //nolint:wrapcheck // the service's own error
	}

	out := make([]CommentResponse, 0, len(comments))
	for _, comment := range comments {
		out = append(out, commentOf(comment))
	}

	return out, nil
}

func (h *Handlers) report(ctx context.Context, in core.ReportInput) (IncidentCreated, error) {
	who, err := h.viewer(ctx)
	if err != nil {
		return IncidentCreated{}, err
	}

	detail, err := h.service.Report(ctx, who, in)
	if err != nil {
		return IncidentCreated{}, err //nolint:wrapcheck // the service's own error
	}

	return IncidentCreated{Data: incidentOf(detail.Incident), Meta: metaOf(detail)}, nil
}

func (h *Handlers) comment(ctx context.Context, in CommentRequest) (CommentResponse, error) {
	err := web.PositiveID("incidentID", in.IncidentID)
	if err != nil {
		return CommentResponse{}, err //nolint:wrapcheck // web's own answer
	}

	who, err := h.viewer(ctx)
	if err != nil {
		return CommentResponse{}, err
	}

	comment, err := h.service.Comment(ctx, in.IncidentID, who, in.CommentInput)
	if err != nil {
		return CommentResponse{}, err //nolint:wrapcheck // the service's own error
	}

	return commentOf(*comment), nil
}

// clientError records an error the browser reports. The person's reports are counted; the ones over the limit are
// dropped and answered like the others, since the browser can do nothing about them.
func (h *Handlers) clientError(ctx context.Context, in core.ClientErrorInput) (route.Empty, error) {
	principal, ok := authz.PrincipalFrom(ctx)
	if !ok || principal.Kind != authz.KindUser {
		return route.Empty{}, apperr.Unauthorized("not authenticated")
	}

	allowed, err := h.limiter.Allow(ctx, "incident.client-error:"+principal.Subject.String())
	if err != nil {
		return route.Empty{}, apperr.Internal(err)
	}

	if !allowed {
		h.log.DebugContext(ctx, "incident: a client error report was dropped by the limit", "user_id", principal.ID)

		return route.Empty{}, nil
	}

	var tenantID *int
	if tenant, found := tenancy.TenantIDFromContext(ctx); found {
		tenantID = &tenant
	}

	err = h.service.ClientError(ctx, core.Viewer{UserID: principal.ID}, tenantID, in)

	return route.Empty{}, err //nolint:wrapcheck // the service's own error
}
