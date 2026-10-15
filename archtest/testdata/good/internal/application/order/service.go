package order

import (
	"context"
	"fmt"

	"example.com/app/internal/domain/order"
)

type CreateInput struct {
	Name string `validation:"required|max:50"`
}

func (CreateInput) Validate() error { return nil }

type AppService struct{ repo order.Repository }

func (s *AppService) Create(ctx context.Context, in CreateInput) error {
	if _, err := s.repo.Find(ctx, 1); err != nil {
		return fmt.Errorf("find %s: %w", in.Name, err)
	}

	return nil
}

func (s *AppService) helper() {}
