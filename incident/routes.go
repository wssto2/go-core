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
// defined them (with its own labels) keeps its definitions. Call it on the application's catalogue before
// access.Install // builds the engine: start-up checks that every route's permission is defined.
func DefinePermissions(catalogue *authz.Catalogue) error {
	for _, def := range []struct {
		id   string
		opts []authz.DefineOption
	}{
		{ViewIncidents, nil},
		{CreateIncidents, []authz.DefineOption{authz.Requires(ViewIncidents)}},
		{UpdateIncidents, []authz.DefineOption{authz.Requires(ViewIncidents)}},
		{ManageIncidents, []authz.DefineOption{authz.Requires(ViewIncidents)}},
	} {
		if _, ok := catalogue.Lookup(def.id); ok {
			continue
		}

		err := catalogue.Define(def.id, def.opts...)
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

// ByID names an incident. A non-number answers 400 for the whole group.
type ByID struct {
	IncidentID int `path:"incidentID"`
}

// ListQuery is the list's query: the paging, search, view and order, and every other parameter is a filter (status,
// severity, source, created_at).
type ListQuery struct {
	Query url.Values `json:"-" query:"*"`
}

// CommentRequest is the incident and the new comment.
type CommentRequest struct {
	ByID
	core.CommentInput
}

// UpdateRequest is the incident and its new state.
type UpdateRequest struct {
	ByID
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
	List route.Route[ListQuery, route.Page[Incident]]
	// Show is one incident with the reports and comments the caller may see.
	Show route.Route[ByID, Shown]
	// Comments are the comments on an incident.
	Comments route.Route[ByID, []CommentResponse]
	// Report files a person's bug report (201): a report of an open problem joins its incident.
	Report route.Route[core.ReportInput, Created]
	// Comment adds a comment (201).
	Comment route.Route[CommentRequest, CommentResponse]
	// Update changes an incident.
	Update route.Route[UpdateRequest, Shown]
	// ClientError records an error the browser reports (204): any signed-in person may, a person's reports are rate
	// limited.
	ClientError route.Route[core.ClientErrorInput, route.Empty]

	group *route.Contract
}

// Declare declares the routes.
func Declare() *Declared {
	//nolint:exhaustruct,exhaustruct_v5 // zero by design
	declared := &Declared{
		List: route.Get[ListQuery, route.Page[Incident]](base).Name("incident.index").Requires(ViewIncidents),
		Show: route.Get[ByID, Shown](record).Name("incident.show").Requires(ViewIncidents),
		Comments: route.Get[ByID, []CommentResponse](record + "/comments").
			Name("incident.comments").Requires(ViewIncidents),
		Report: route.Post[core.ReportInput, Created](base + "/reports").
			Name("incident.report").Requires(CreateIncidents).PersonOnly().Status(http.StatusCreated),
		Comment: route.Post[CommentRequest, CommentResponse](record + "/comments").
			Name("incident.comment").Requires(CreateIncidents).PersonOnly().Status(http.StatusCreated),
		Update: route.Patch[UpdateRequest, Shown](record).
			Name("incident.update").Requires(UpdateIncidents).PersonOnly(),
		ClientError: route.Post[core.ClientErrorInput, route.Empty](base + "/client-errors").
			Name("incident.client-error").Authenticated().PersonOnly().Status(http.StatusNoContent),
	}

	declared.group = route.Group(
		"incident", declared.List, declared.Show, declared.Comments, declared.Report, declared.Comment, declared.Update,
		declared.ClientError,
	).
		MalformedPath(http.StatusBadRequest)

	return declared
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

	//nolint:exhaustruct,exhaustruct_v5 // zero by design
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

func (h *Handlers) list(ctx context.Context, input ListQuery) (route.Page[Incident], error) {
	who, err := h.viewer(ctx)
	if err != nil {
		return route.Page[Incident]{}, err
	}

	result, err := h.service.List(ctx, web.PageFromValues(input.Query), who)
	if err != nil {
		return route.Page[Incident]{}, err //nolint:wrapcheck // the service's own error
	}

	return pageOf(result), nil
}

func (h *Handlers) show(ctx context.Context, input ByID) (Shown, error) {
	return h.detail(ctx, input.IncidentID, func(who core.Viewer) (core.Detail, error) {
		return h.service.Show(ctx, input.IncidentID, who)
	})
}

func (h *Handlers) update(ctx context.Context, input UpdateRequest) (Shown, error) {
	return h.detail(ctx, input.IncidentID, func(who core.Viewer) (core.Detail, error) {
		return h.service.Update(ctx, input.IncidentID, who, input.UpdateInput)
	})
}

// detail answers an incident record from what the service returns for the viewer.
func (h *Handlers) detail(
	ctx context.Context, incidentID int, answer func(core.Viewer) (core.Detail, error),
) (Shown, error) {
	err := web.PositiveID("incidentID", incidentID)
	if err != nil {
		return Shown{}, err //nolint:wrapcheck // web's own answer
	}

	who, err := h.viewer(ctx)
	if err != nil {
		return Shown{}, err
	}

	detail, err := answer(who)
	if err != nil {
		return Shown{}, err
	}

	return recordOf(detail), nil
}

func (h *Handlers) comments(ctx context.Context, input ByID) ([]CommentResponse, error) {
	err := web.PositiveID("incidentID", input.IncidentID)
	if err != nil {
		return nil, err //nolint:wrapcheck // web's own answer
	}

	who, err := h.viewer(ctx)
	if err != nil {
		return nil, err
	}

	comments, err := h.service.Comments(ctx, input.IncidentID, who)
	if err != nil {
		return nil, err //nolint:wrapcheck // the service's own error
	}

	out := make([]CommentResponse, 0, len(comments))
	for _, comment := range comments {
		out = append(out, commentOf(comment))
	}

	return out, nil
}

func (h *Handlers) report(ctx context.Context, input core.ReportInput) (Created, error) {
	who, err := h.viewer(ctx)
	if err != nil {
		return Created{}, err
	}

	detail, err := h.service.Report(ctx, who, input)
	if err != nil {
		return Created{}, err //nolint:wrapcheck // the service's own error
	}

	return Created{Data: incidentOf(detail.Incident), Meta: metaOf(detail)}, nil
}

func (h *Handlers) comment(ctx context.Context, input CommentRequest) (CommentResponse, error) {
	err := web.PositiveID("incidentID", input.IncidentID)
	if err != nil {
		return CommentResponse{}, err //nolint:wrapcheck // web's own answer
	}

	who, err := h.viewer(ctx)
	if err != nil {
		return CommentResponse{}, err
	}

	comment, err := h.service.Comment(ctx, input.IncidentID, who, input.CommentInput)
	if err != nil {
		return CommentResponse{}, err //nolint:wrapcheck // the service's own error
	}

	return commentOf(*comment), nil
}

// clientError records an error the browser reports. The person's reports are counted; the ones over the limit are
// dropped and answered like the others, since the browser can do nothing about them.
func (h *Handlers) clientError(ctx context.Context, input core.ClientErrorInput) (route.Empty, error) {
	principal, ok := authz.PrincipalFrom(ctx)
	if !ok || principal.Kind != authz.KindUser {
		return route.Empty{}, apperr.Unauthorized("not authenticated")
	}

	allowed, err := h.limiter.Allow(ctx, "incident.client-error:"+principal.String())
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

	//nolint:exhaustruct,exhaustruct_v5 // zero by design
	err = h.service.ClientError(ctx, core.Viewer{UserID: principal.ID}, tenantID, input)

	return route.Empty{}, err //nolint:wrapcheck // the service's own error
}
