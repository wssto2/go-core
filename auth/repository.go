package auth

import "context"

type UserRepository[T any] interface {
	FindByID(ctx context.Context, id int) (*User[T], error)
	FindByEmail(ctx context.Context, email string) (*User[T], error)
	Save(ctx context.Context, user *User[T]) error
	Create(ctx context.Context, user *User[T]) error

	GetActiveTokens(ctx context.Context, userID int) ([]Token, error)
	SaveToken(ctx context.Context, token *Token) error
	RevokeToken(ctx context.Context, tokenID int) error
}
