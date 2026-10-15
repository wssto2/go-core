package web

import (
	"errors"

	"github.com/wssto2/go-core/apperr"
)

var errNoRecord = errors.New("the service answered no record")

// Mapped turns a service's answer into a route's output: the zero value and the error when the call failed, else the
// answer through from. It reads as one line over a service call:
//
//	return web.Mapped(dto.FromTicket)(service.Get(ctx, id))
func Mapped[T, R any](from func(T) R) func(T, error) (R, error) {
	return func(res T, err error) (R, error) {
		if err != nil {
			var zero R

			return zero, err
		}

		return from(res), nil
	}
}

// Value is the record a service answered, for a route whose output is the record itself. An answer with no record is
// the service's mistake (500), not a null for the client to handle.
func Value[T any](record *T) (T, error) { //nolint:ireturn // a generic accessor returns its type parameter
	if record == nil {
		var zero T

		return zero, apperr.Internal(errNoRecord)
	}

	return *record, nil
}
