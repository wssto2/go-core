package savedview

import (
	"context"
	"encoding/json"
	"net/url"
	"time"

	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/route"
	"github.com/wssto2/go-core/savedview/core"
	"github.com/wssto2/go-core/validation"
	"github.com/wssto2/go-core/web"
)

const base = "/v1/saved-views"

// ViewPath names a saved view. A non-number answers 400 for the whole group.
type ViewPath struct {
	ID int `path:"id"`
}

// ListRequest is the list's query: `list` names the list whose views are asked for.
type ListRequest struct {
	Query url.Values `json:"-" query:"*"`
}

// listInput is the list asked for.
type listInput struct {
	ListKey string `form:"list" validation:"trim|required|max:100"`
}

// SaveInput saves the current list state under a name. Saving under a name the person already uses on that list
// replaces that view.
//
//nolint:tagliatelle // snake_case wire
type SaveInput struct {
	ListKey string `json:"list_key" validation:"trim|required|max:100"`
	Name    string `json:"name"     validation:"trim|required|max:60"`
	// State is the list state the screen applies back, as a JSON object.
	State string `json:"state" validation:"required|max:4000"`
}

// clean requires State to be a JSON object, so the screen never reads back something it cannot apply.
func (in *SaveInput) clean() error {
	// JSON null unmarshals into a nil map without an error, so nil is refused too.
	var object map[string]any

	err := json.Unmarshal([]byte(in.State), &object)
	if err != nil || object == nil {
		return apperr.BadRequest("state must be a JSON object")
	}

	return nil
}

// ViewResponse is one of the person's saved views. State is sent as the JSON object it was saved as, not as a string,
// so the screen applies it without a second parse.
//
//nolint:tagliatelle // snake_case wire
type ViewResponse struct {
	ID        int            `json:"id"`
	ListKey   string         `json:"list_key"`
	Name      string         `json:"name"`
	State     map[string]any `json:"state"`
	UpdatedAt time.Time      `json:"updated_at"`
}

func responseOf(view core.View) ViewResponse {
	// The state was checked to be a JSON object on the way in; anything unreadable reads back as an empty state
	// rather than failing the list.
	state := map[string]any{}

	err := json.Unmarshal([]byte(view.State), &state)
	if err != nil || state == nil {
		state = map[string]any{}
	}

	return ViewResponse{ID: view.ID, ListKey: view.ListKey, Name: view.Name, State: state, UpdatedAt: view.UpdatedAt}
}

// Routes is the declared HTTP contract of the saved-view module (paths under /v1, before the application's
// gocore.WithPrefix): what the TypeScript generator reads, without installing anything. Every route needs a signed-in
// person and no permission, and acts on that person's own views.
//
//nolint:gochecknoglobals // a contract is declared once, as a value
var Routes = Declare().Contract()

// Declared is the routes of the module as values.
type Declared struct {
	// Index is the person's views of the list named by ?list=, by name.
	Index route.Route[ListRequest, []ViewResponse]
	// Store saves the state under the name, replacing the person's view of that name; a new name beyond 20 is a 400.
	Store route.Route[SaveInput, ViewResponse]
	// Delete removes one of the person's views; another person's id is a 404.
	Delete route.Route[ViewPath, route.Empty]

	group *route.Contract
}

// Declare declares the routes.
func Declare() *Declared {
	//nolint:exhaustruct,exhaustruct_v5 // zero by design
	declared := &Declared{
		Index:  route.Get[ListRequest, []ViewResponse](base).Name("savedview.index").PersonOnly(),
		Store:  route.Post[SaveInput, ViewResponse](base).Name("savedview.store").PersonOnly(),
		Delete: route.Delete[ViewPath, route.Empty](base + "/:id").Name("savedview.delete").PersonOnly(),
	}

	declared.group = route.Group("savedview", declared.Index, declared.Store, declared.Delete)

	return declared
}

// Contract is the routes as a group: what contract.Generate reads.
func (d *Declared) Contract() *route.Contract { return d.group }

// To binds a handler to every route; hand the result to app.Routes.
func (d *Declared) To(service *core.Service) []route.Handled {
	h := &handlers{service: service}

	return []route.Handled{d.Index.To(h.index), d.Store.To(h.store), d.Delete.To(h.destroy)}
}

type handlers struct{ service *core.Service }

func (h *handlers) index(ctx context.Context, input ListRequest) ([]ViewResponse, error) {
	userID, err := person(ctx)
	if err != nil {
		return nil, err
	}

	list := listInput{ListKey: input.Query.Get("list")}

	err = validation.ValidateInput(&list)
	if err != nil {
		return nil, err //nolint:wrapcheck // validation's own error
	}

	views, err := h.service.List(ctx, userID, list.ListKey)
	if err != nil {
		return nil, err //nolint:wrapcheck // the service names the call
	}

	out := make([]ViewResponse, 0, len(views))
	for _, view := range views {
		out = append(out, responseOf(view))
	}

	return out, nil
}

func (h *handlers) store(ctx context.Context, input SaveInput) (ViewResponse, error) {
	userID, err := person(ctx)
	if err != nil {
		return ViewResponse{}, err
	}

	err = input.clean()
	if err != nil {
		return ViewResponse{}, err
	}

	view, err := h.service.Save(ctx, userID, input.ListKey, input.Name, input.State)
	if err != nil {
		return ViewResponse{}, err //nolint:wrapcheck // the service names the call
	}

	return responseOf(view), nil
}

// destroy answers a zero or negative id with 422 in the standard validation body.
func (h *handlers) destroy(ctx context.Context, input ViewPath) (route.Empty, error) {
	err := web.PositivePathInt("id", input.ID)
	if err != nil {
		return route.Empty{}, err //nolint:wrapcheck // the answer is the 422
	}

	userID, err := person(ctx)
	if err != nil {
		return route.Empty{}, err
	}

	return route.Empty{}, h.service.Delete(ctx, userID, input.ID) //nolint:wrapcheck // the service names the call
}

// person is the signed-in person: the id comes from the session, never from the request.
func person(ctx context.Context) (int, error) {
	principal, ok := authz.PrincipalFrom(ctx)
	if !ok {
		return 0, apperr.Unauthorized("not authenticated")
	}

	if principal.Kind != authz.KindUser {
		return 0, apperr.Forbidden("only people keep saved views")
	}

	return principal.ID, nil
}
