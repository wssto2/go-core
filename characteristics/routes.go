//nolint:exhaustruct,exhaustruct_v5,varnamelen,tagliatelle,gochecknoglobals // snake_case wire; zero by design
package characteristics

import (
	"context"
	"encoding/json"

	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/characteristics/core"
	"github.com/wssto2/go-core/route"
)

const base = "/v1/characteristics"

// LevelInput addresses a level (1 is the root).
type LevelInput struct {
	Level int `path:"level"`
}

// ItemInput addresses an item.
type ItemInput struct {
	Level int `path:"level"`
	ID    int `path:"id"`
}

// WriteInput is what a person writes of an item. Translations by language (nil keeps them on update, an empty text
// clears a language), Meta replaces the meta object (nil keeps it), Values are the extra columns by column, and the
// columns not named keep their value.
type WriteInput struct {
	Name         string            `json:"name"                   validation:"trim|required|max:255"`
	Translations map[string]string `json:"translations,omitempty"`
	Meta         json.RawMessage   `json:"meta,omitempty"`
	Values       json.RawMessage   `json:"values,omitempty"`
}

// CreateInput creates an item under a parent (omitted at the root level), switched on or off.
type CreateInput struct {
	WriteInput

	Level    int  `path:"level"`
	ParentID int  `json:"parent_id,omitempty"`
	Active   bool `json:"active"`
}

// UpdateInput changes an item.
type UpdateInput struct {
	WriteInput

	Level int `path:"level"`
	ID    int `path:"id"`
}

// ActiveInput switches an item on or off.
type ActiveInput struct {
	Level  int  `path:"level"`
	ID     int  `path:"id"`
	Active bool `json:"active"`
}

// PositionInput moves an item to a position among its siblings, from 1.
type PositionInput struct {
	Level    int `path:"level"`
	ID       int `path:"id"`
	Position int `json:"position" validation:"required|min:1"`
}

// DeleteInput deletes an item; force=true deletes it although it is used or has items below it.
type DeleteInput struct {
	Level int  `path:"level"`
	ID    int  `path:"id"`
	Force bool `json:"force,omitempty" query:"force"`
}

// OptionsInput asks for the choices of a form field: the active items of one branch (the children of parent on
// level; no parent at level 1). Value is the id (empty) or a described column of the level; Lang the language of
// the labels (the first language of the description when empty).
type OptionsInput struct {
	Level  int    `json:"level"            query:"level"`
	Parent int    `json:"parent,omitempty" query:"parent"`
	Value  string `json:"value,omitempty"  query:"value"  validation:"max:64"`
	Lang   string `json:"lang,omitempty"   query:"lang"   validation:"max:8"`
}

// ItemResponse is one item. Values holds the described extra columns that apply to it: string, number, boolean or
// null.
type ItemResponse struct {
	Level        int               `json:"level"`
	ID           int               `json:"id"`
	ParentID     int               `json:"parent_id"`
	Name         string            `json:"name"`
	Translations map[string]string `json:"translations"`
	Order        int               `json:"order"`
	Active       bool              `json:"active"`
	Meta         map[string]any    `json:"meta"`
	Values       map[string]any    `json:"values"`
}

// TreeResponse is items depth first: a parent comes before its children, siblings by their order.
type TreeResponse struct {
	Items []ItemResponse `json:"items"`
}

// LabelResponse is a text in both languages of the administration.
type LabelResponse struct {
	HR string `json:"hr"`
	EN string `json:"en"`
}

// ChoiceResponse is one value of a choice field.
type ChoiceResponse struct {
	Value string        `json:"value"`
	Label LabelResponse `json:"label"`
}

// ScopeResponse is the item a field is limited to the branch of: the item with this id on this level.
type ScopeResponse struct {
	Level int `json:"level"`
	ID    int `json:"id"`
}

// UseResponse is one kind of place an item is used: "2 pages".
type UseResponse struct {
	What  string `json:"what"`
	Count int    `json:"count"`
}

// FieldResponse is an extra column as the form shows it.
type FieldResponse struct {
	Column   string           `json:"column"`
	Label    LabelResponse    `json:"label"`
	Type     string           `json:"type"`
	Choices  []ChoiceResponse `json:"choices"`
	Max      int              `json:"max"`
	InList   bool             `json:"in_list"`
	ReadOnly bool             `json:"read_only"`
	// Under limits the field to the items below one item of a higher level; null is the whole level.
	Under *ScopeResponse `json:"under"`
}

// LevelResponse is a level as the screen builds it.
type LevelResponse struct {
	Level           int             `json:"level"`
	Label           LabelResponse   `json:"label"`
	NameMax         int             `json:"name_max"`
	HasTranslations bool            `json:"has_translations"`
	HasOrder        bool            `json:"has_order"`
	HasActive       bool            `json:"has_active"`
	HasMeta         bool            `json:"has_meta"`
	Fields          []FieldResponse `json:"fields"`
}

// DescriptionResponse is the project's description of its tree, without table or column names beyond the extra
// columns' keys.
type DescriptionResponse struct {
	Languages []string        `json:"languages"`
	Levels    []LevelResponse `json:"levels"`
}

// UsageResponse says where an item, and what is below it, is used. Known is false when the application cannot tell
// ("usage unknown"): Uses is then empty and means nothing.
type UsageResponse struct {
	Known    bool          `json:"known"`
	Uses     []UseResponse `json:"uses"`
	Children int           `json:"children"`
}

// DeleteResponse is what a delete did. UsageKnown is false when the application cannot say where items are used, so
// the delete could not be checked against it.
type DeleteResponse struct {
	Deleted    int  `json:"deleted"`
	UsageKnown bool `json:"usage_known"`
}

// OptionResponse is one choice of a form field.
type OptionResponse struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// Routes is the declared HTTP contract (paths under /v1, before the application's gocore.WithPrefix): what the
// TypeScript generator reads, without installing anything.
var Routes = Declare().Contract()

// Declared is the routes of the module as values.
type Declared struct {
	// Tree is every item, depth first. Needs characteristics:view.
	Tree route.Route[route.None, TreeResponse]
	// Description is the levels and extra columns the screen builds its forms from. Needs characteristics:view.
	Description route.Route[route.None, DescriptionResponse]
	// Show is one item. Needs characteristics:view.
	Show route.Route[ItemInput, ItemResponse]
	// Branch is an item with everything below it, as Tree. Needs characteristics:view.
	Branch route.Route[ItemInput, TreeResponse]
	// Usage is where an item is used. Needs characteristics:view.
	Usage route.Route[ItemInput, UsageResponse]
	// Create adds an item last among its siblings. Needs characteristics:edit.
	Create route.Route[CreateInput, ItemResponse]
	// Update changes the name, translations, meta and extra columns of an item. Needs characteristics:edit.
	Update route.Route[UpdateInput, ItemResponse]
	// SetActive switches an item on or off. Needs characteristics:edit.
	SetActive route.Route[ActiveInput, ItemResponse]
	// Move puts an item at a position among its siblings. Needs characteristics:edit.
	Move route.Route[PositionInput, route.Empty]
	// Delete removes an item; 409 while it is used or has items below it unless force=true. Needs characteristics:edit.
	Delete route.Route[DeleteInput, DeleteResponse]
	// Options is the choices of a form field, for any signed-in caller: editors pick characteristics in forms.
	Options route.Route[OptionsInput, []OptionResponse]

	group *route.Contract
}

// Declare declares the routes.
func Declare() *Declared {
	item := base + "/items/:level/:id"

	//nolint:exhaustruct,exhaustruct_v5 // zero by design
	d := &Declared{
		Tree: route.Get[route.None, TreeResponse](base).Name("characteristics.tree").Requires(View),
		Description: route.Get[route.None, DescriptionResponse](base + "/description").
			Name("characteristics.description").Requires(View),
		Show:   route.Get[ItemInput, ItemResponse](item).Name("characteristics.show").Requires(View),
		Branch: route.Get[ItemInput, TreeResponse](item + "/branch").Name("characteristics.branch").Requires(View),
		Usage:  route.Get[ItemInput, UsageResponse](item + "/usage").Name("characteristics.usage").Requires(View),
		Create: route.Post[CreateInput, ItemResponse](base + "/items/:level").
			Name("characteristics.create").Requires(Edit),
		Update:    route.Put[UpdateInput, ItemResponse](item).Name("characteristics.update").Requires(Edit),
		SetActive: route.Put[ActiveInput, ItemResponse](item + "/active").Name("characteristics.set-active").Requires(Edit),
		Move:      route.Put[PositionInput, route.Empty](item + "/position").Name("characteristics.move").Requires(Edit),
		Delete:    route.Delete[DeleteInput, DeleteResponse](item).Name("characteristics.delete").Requires(Edit),
		Options: route.Get[OptionsInput, []OptionResponse](base + "/options").
			Name("characteristics.options").Authenticated(),
	}

	d.group = route.Group("characteristics",
		d.Tree, d.Description, d.Show, d.Branch, d.Usage, d.Create, d.Update, d.SetActive, d.Move, d.Delete, d.Options)

	return d
}

// Contract is the routes as a group: what contract.Generate reads.
func (d *Declared) Contract() *route.Contract { return d.group }

// To binds a handler to every route; hand the result to app.Routes.
func (d *Declared) To(service *core.Service) []route.Handled {
	h := &handlers{service: service}

	return []route.Handled{
		d.Tree.To(h.tree), d.Description.To(h.description), d.Show.To(h.show), d.Branch.To(h.branch), d.Usage.To(h.usage),
		d.Create.To(h.create), d.Update.To(h.update), d.SetActive.To(h.setActive), d.Move.To(h.move), d.Delete.To(h.destroy),
		d.Options.To(h.options),
	}
}

type handlers struct{ service *core.Service }

func itemResponse(item core.Item) ItemResponse {
	translations, values := item.Translations, item.Values
	if translations == nil {
		translations = map[string]string{}
	}

	if values == nil {
		values = map[string]any{}
	}

	return ItemResponse{
		Level: item.Level, ID: item.ID, ParentID: item.ParentID, Name: item.Name, Translations: translations,
		Order: item.Order, Active: item.Active, Meta: item.Meta, Values: values,
	}
}

func flatten(nodes []core.Node, out []ItemResponse) []ItemResponse {
	for _, node := range nodes {
		out = append(out, itemResponse(node.Item))
		out = flatten(node.Children, out)
	}

	return out
}

func (h *handlers) tree(ctx context.Context, _ route.None) (TreeResponse, error) {
	nodes, err := h.service.Tree(ctx)
	if err != nil {
		return TreeResponse{}, err //nolint:wrapcheck // the service names the call
	}

	return TreeResponse{Items: flatten(nodes, []ItemResponse{})}, nil
}

func (h *handlers) branch(ctx context.Context, in ItemInput) (TreeResponse, error) {
	node, err := h.service.Branch(ctx, in.Level, in.ID)
	if err != nil {
		return TreeResponse{}, err //nolint:wrapcheck // the service names the call
	}

	return TreeResponse{Items: flatten([]core.Node{node}, []ItemResponse{})}, nil
}

func (h *handlers) description(_ context.Context, _ route.None) (DescriptionResponse, error) {
	tree := h.service.Description()
	out := DescriptionResponse{Languages: tree.Languages, Levels: make([]LevelResponse, len(tree.Levels))}

	for i, level := range tree.Levels {
		fields := make([]FieldResponse, len(level.Fields))

		for j, f := range level.Fields {
			choices := make([]ChoiceResponse, len(f.Choices))
			for k, c := range f.Choices {
				choices[k] = ChoiceResponse{Value: c.Value, Label: labelResponse(c.Label)}
			}

			fields[j] = FieldResponse{
				Column: f.Column, Label: labelResponse(f.Label), Type: string(f.Type), Choices: choices, Max: f.Max,
				InList: f.InList, ReadOnly: f.ReadOnly, Under: nil,
			}
			if f.Under != nil {
				fields[j].Under = &ScopeResponse{Level: f.Under.Level, ID: f.Under.ID}
			}
		}

		out.Levels[i] = LevelResponse{
			Level: i + 1, Label: labelResponse(level.Label), NameMax: level.NameMax,
			HasTranslations: level.Translations.JSON != "" || len(level.Translations.Columns) > 0,
			HasOrder:        level.Order != "", HasActive: level.Active != "", HasMeta: level.Meta != "", Fields: fields,
		}
	}

	return out, nil
}

func labelResponse(t core.Text) LabelResponse { return LabelResponse{HR: t.HR, EN: t.EN} }

func (h *handlers) show(ctx context.Context, in ItemInput) (ItemResponse, error) {
	item, err := h.service.Get(ctx, in.Level, in.ID)
	if err != nil {
		return ItemResponse{}, err //nolint:wrapcheck // the service names the call
	}

	return itemResponse(item), nil
}

func (h *handlers) usage(ctx context.Context, in ItemInput) (UsageResponse, error) {
	report, err := h.service.Usage(ctx, in.Level, in.ID)
	if err != nil {
		return UsageResponse{}, err //nolint:wrapcheck // the service names the call
	}

	uses := make([]UseResponse, len(report.Uses))
	for i, use := range report.Uses {
		uses[i] = UseResponse{What: use.What, Count: use.Count}
	}

	return UsageResponse{Known: report.Known, Uses: uses, Children: report.Children}, nil
}

// core decodes the two JSON objects; a body that is not an object is a 400.
func (in WriteInput) core() (core.Input, error) {
	out := core.Input{Name: in.Name, Translations: in.Translations}

	for field, raw := range map[string]*struct {
		raw json.RawMessage
		to  *map[string]any
	}{"meta": {in.Meta, &out.Meta}, "values": {in.Values, &out.Values}} {
		if len(raw.raw) == 0 {
			continue
		}

		err := json.Unmarshal(raw.raw, raw.to)
		if err != nil {
			return core.Input{}, apperr.BadRequest(field + " must be a JSON object")
		}
	}

	return out, nil
}

func (h *handlers) create(ctx context.Context, in CreateInput) (ItemResponse, error) {
	actor, err := actorOf(ctx)
	if err != nil {
		return ItemResponse{}, err
	}

	input, err := in.core()
	if err != nil {
		return ItemResponse{}, err
	}

	input.Active = in.Active

	item, err := h.service.Create(ctx, actor, in.Level, in.ParentID, input)
	if err != nil {
		return ItemResponse{}, err //nolint:wrapcheck // the service names the call
	}

	return itemResponse(item), nil
}

func (h *handlers) update(ctx context.Context, in UpdateInput) (ItemResponse, error) {
	actor, err := actorOf(ctx)
	if err != nil {
		return ItemResponse{}, err
	}

	input, err := in.core()
	if err != nil {
		return ItemResponse{}, err
	}

	item, err := h.service.Update(ctx, actor, in.Level, in.ID, input)
	if err != nil {
		return ItemResponse{}, err //nolint:wrapcheck // the service names the call
	}

	return itemResponse(item), nil
}

func (h *handlers) setActive(ctx context.Context, in ActiveInput) (ItemResponse, error) {
	actor, err := actorOf(ctx)
	if err != nil {
		return ItemResponse{}, err
	}

	item, err := h.service.SetActive(ctx, actor, in.Level, in.ID, in.Active)
	if err != nil {
		return ItemResponse{}, err //nolint:wrapcheck // the service names the call
	}

	return itemResponse(item), nil
}

func (h *handlers) move(ctx context.Context, in PositionInput) (route.Empty, error) {
	err := h.service.Move(ctx, in.Level, in.ID, in.Position)

	return route.Empty{}, err //nolint:wrapcheck // the service names the call
}

func (h *handlers) destroy(ctx context.Context, in DeleteInput) (DeleteResponse, error) {
	result, err := h.service.Delete(ctx, in.Level, in.ID, in.Force)
	if err != nil {
		return DeleteResponse{}, err //nolint:wrapcheck // the service names the call
	}

	return DeleteResponse{Deleted: result.Deleted, UsageKnown: result.UsageKnown}, nil
}

func (h *handlers) options(ctx context.Context, in OptionsInput) ([]OptionResponse, error) {
	query := core.OptionsQuery{Level: in.Level, Parent: in.Parent, Value: in.Value, Lang: in.Lang}

	options, err := h.service.Options(ctx, query)
	if err != nil {
		return nil, err //nolint:wrapcheck // the service names the call
	}

	out := make([]OptionResponse, len(options))
	for i, option := range options {
		out[i] = OptionResponse{Value: option.Value, Label: option.Label}
	}

	return out, nil
}

// actorOf is the signed-in caller's id for the audit columns: it comes from the session, never from the request.
func actorOf(ctx context.Context) (int, error) {
	principal, ok := authz.PrincipalFrom(ctx)
	if !ok {
		return 0, apperr.Unauthorized("not authenticated")
	}

	return principal.ID, nil
}
