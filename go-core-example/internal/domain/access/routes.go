package access

import (
	"context"

	"github.com/wssto2/go-core/route"
)

// LeadPath names the lead a request is about.
type LeadPath struct {
	ID int `path:"id"`
}

// RenameRequest is the body of PUT /v1/leads/:id.
type RenameRequest struct {
	LeadPath
	Title string `json:"title" validation:"required|max:200"`
}

// The lead routes. .Requires is the coarse gate ("holds it somewhere"); the service decides which leads.
var (
	List   = route.Get[route.None, []Lead]("/v1/leads").Name("leads.list").Requires(LeadView)
	Show   = route.Get[LeadPath, Lead]("/v1/leads/:id").Name("leads.show").Requires(LeadView)
	Rename = route.Put[RenameRequest, Lead]("/v1/leads/:id").Name("leads.rename").Requires(LeadUpdate)

	// Routes is the feature's contract.
	Routes = route.Group("leads", List, Show, Rename)
)

func (s *Service) list(ctx context.Context, _ route.None) ([]Lead, error) { return s.List(ctx) }

func (s *Service) show(ctx context.Context, in LeadPath) (Lead, error) { return s.Get(ctx, in.ID) }

func (s *Service) rename(ctx context.Context, in RenameRequest) (Lead, error) {
	return s.Rename(ctx, in.ID, in.Title)
}
