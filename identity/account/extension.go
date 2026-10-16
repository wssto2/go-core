package account

import (
	"context"
	"encoding/json"
)

// UserExtension is the application's own data on a person (their dealer, their roles, a mobile phone;
// IDENTITY-ADMIN-003): it adds an "ext" to the list rows and the record of the users routes, takes one in the
// body of create, update and deactivate, and writes it in the transaction of go-core's own write,
// so an error from it undoes both. What "ext" holds is the application's to define: identity
// carries it as JSON and reads nothing in it. identity.WithUserExtension installs it.
//
// The methods run for the users routes and for Admin's methods that take an Ext (the services trust
// their caller, as for permissions). Return an *apperr.AppError whose reason the client can explain
// to refuse; any other error answers 500.
type UserExtension interface {
	// Ext returns the extension of each of the people with the ids, for a page of the list or for
	// one record; a person it has nothing for is absent. A read: it runs outside any transaction.
	Ext(ctx context.Context, ids []int) (map[int]any, error)
	// Create runs after the account is created, in its transaction, with the "ext" of the body
	// (nil when there is none). What it returns is the "ext" of the response.
	Create(ctx context.Context, id int, ext json.RawMessage, actorID int) (any, error)
	// Update runs in the transaction of the update, also when none of the account's own fields
	// differ, with the "ext" of the body (nil when there is none). The response carries Ext's
	// view of the person afterwards.
	Update(ctx context.Context, id int, ext json.RawMessage, actorID int) error
	// Deactivate runs in the transaction of the deactivation, after the deactivation hooks and
	// before the account is made inactive, with the "details" of the body (nil when there is
	// none), for example the people to hand the account's records to. An error refuses the
	// deactivation. What it returns is the "ext" of the response.
	Deactivate(ctx context.Context, a Account, details json.RawMessage, actorID int) (any, error)
}
