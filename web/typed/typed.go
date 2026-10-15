// Package typed holds the helpers of a typed route's handler that need the route package, which web itself cannot
// import: the outputs that answer what web.WritePage and web.WriteResource write, and the session token of the
// exchange.
package typed

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"strings"

	"github.com/wssto2/go-core/datatable"
	"github.com/wssto2/go-core/pagination"
	"github.com/wssto2/go-core/route"
	"github.com/wssto2/go-core/web"
)

// ToPage is a pagination.PageResult as the typed route output that answers what web.WritePage writes: the rows as data
// and the paging numbers, the result's own meta and the views' counts as meta.
func ToPage[T any](result *pagination.PageResult[T]) route.Page[T] {
	meta := maps.Clone(result.Meta)
	if meta == nil {
		meta = map[string]any{}
	}

	if len(result.Views) > 0 {
		meta["views"] = web.ViewMeta(result.Views)
	}

	return route.Page[T]{Result: datatable.DatatableResult[T]{
		Data: result.Items, Meta: meta, Total: result.Total, PerPage: result.PerPage, Page: result.Page,
		LastPage: result.LastPage, From: result.From, To: result.To,
	}}
}

// RecordMeta is the meta of a record's answer: the people behind it (author, editor, ...) as the repository's resource
// reads them. It answers the map as it is; the type is what the generated route table names the meta by.
type RecordMeta struct {
	Values map[string]any `json:"-"`
}

// MarshalJSON writes the map the repository built.
func (m RecordMeta) MarshalJSON() ([]byte, error) {
	raw, err := json.Marshal(m.Values)
	if err != nil {
		return nil, fmt.Errorf("record meta: %w", err)
	}

	return raw, nil
}

// Record is a record with its meta as the typed route output that answers what web.WriteResource writes: the record as
// data and its meta beside it.
func Record[T any](data T, meta map[string]any) route.WithMeta[T, RecordMeta] {
	return route.WithMeta[T, RecordMeta]{Items: data, Info: RecordMeta{Values: meta}}
}

// AccessToken is the session's opaque token, from the cookie the SPA uses (access_token) or a bearer header. A JWT
// embed session yields a JWT here, which matches no stored token.
func AccessToken(ctx context.Context) string {
	exchange := route.ExchangeOf(ctx)

	if token := exchange.Cookie("access_token"); token != "" {
		return token
	}

	if token, found := strings.CutPrefix(exchange.Header("Authorization"), "Bearer "); found {
		return strings.TrimSpace(token)
	}

	return ""
}
