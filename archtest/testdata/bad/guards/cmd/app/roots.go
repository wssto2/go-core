package main

import (
	"context"
	"log/slog"

	"example.com/framework/tenancy"
)

func roots(log *slog.Logger) {
	_ = tenancy.WithAllTenants(context.Background())
	log.InfoContext(context.Background(), "x")
	_ = context.Background()
	_ = context.TODO()
	_ = tenancy.WithTenantID(context.TODO(), 1)
}
