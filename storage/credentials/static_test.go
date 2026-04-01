package credentials

import (
	"context"
	"errors"
	"testing"

	"github.com/wssto2/go-core/apperr"
)

func TestStaticResolver_Resolve(t *testing.T) {
	initial := map[string]Credentials{
		"default": {Provider: "s3", Key: "AKIA", Secret: "SECRET", Endpoint: "https://s3.example"},
	}
	r := NewStaticResolver(initial)
	ctx := context.Background()

	c, err := r.Resolve(ctx, "default")
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if c == nil || c.Key != "AKIA" || c.Provider != "s3" {
		t.Fatalf("unexpected credentials: %+v", c)
	}

	_, err = r.Resolve(ctx, "missing")
	if err == nil {
		t.Fatalf("expected error for missing credentials")
	}
	var aerr *apperr.AppError
	if !errors.As(err, &aerr) {
		t.Fatalf("expected apperr.AppError, got %T", err)
	}
	if aerr.Code != apperr.CodeNotFound {
		t.Fatalf("expected CodeNotFound, got %s", aerr.Code)
	}

	_, err = r.Resolve(ctx, "")
	if err == nil {
		t.Fatalf("expected error for empty name")
	}
	if !errors.As(err, &aerr) {
		t.Fatalf("expected apperr.AppError, got %T", err)
	}
	if aerr.Code != apperr.CodeBadRequest {
		t.Fatalf("expected CodeBadRequest, got %s", aerr.Code)
	}
}
