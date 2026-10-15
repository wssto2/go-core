package repository

import (
	db "github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/tenancy"
)

func misuse() {
	_ = db.TxFromContext
	_ = tenancy.ScopeByTenant
}
