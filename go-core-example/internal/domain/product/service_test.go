package product

import (
	"context"
	"fmt"
	"log/slog"
	"testing"

	"go-core-example/internal/domain/auth"

	"github.com/wssto2/go-core/audit"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/event"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestService_Create_WritesAuditAndPublishesEvent(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	if err := database.SafeMigrate(db, &Product{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	repo := NewRepository(db)
	transactor := database.NewTransactor(db)

	var wrote []audit.AuditLog
	hook := func(ctx context.Context, log audit.AuditLog) error {
		wrote = append(wrote, log)
		return nil
	}
	auditRepo := audit.NewRepositoryWithHook(transactor, hook)

	bus := event.NewInMemoryBus()
	var published []ProductCreatedEvent
	if err := bus.Subscribe(ProductCreatedEvent{}, func(ctx context.Context, ev any) error {
		if e, ok := ev.(ProductCreatedEvent); ok {
			published = append(published, e)
		}
		return nil
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	svc := NewService(repo, transactor, auditRepo, bus, slog.Default())
	actor := auth.User{ID: 1, Username: "tester", Policies: []string{"products:create"}}

	opts := CreateProductOptions{Name: "Widget", SKU: "WGT-001", Price: 9.99}
	created, err := svc.Create(context.Background(), opts, actor)
	if err != nil {
		t.Fatalf("create error: %v", err)
	}
	if created.ID == 0 {
		t.Fatalf("expected id set")
	}
	if len(wrote) != 1 {
		t.Fatalf("expected 1 audit write, got %d", len(wrote))
	}
	if len(published) != 1 {
		t.Fatalf("expected 1 published event, got %d", len(published))
	}
}

func TestService_Create_RollsBackOnAuditFailure(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	if err := database.SafeMigrate(db, &Product{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	repo := NewRepository(db)
	transactor := database.NewTransactor(db)

	hook := func(ctx context.Context, log audit.AuditLog) error {
		return fmt.Errorf("forced failure")
	}
	auditRepo := audit.NewRepositoryWithHook(transactor, hook)

	bus := event.NewInMemoryBus()
	svc := NewService(repo, transactor, auditRepo, bus, slog.Default())
	actor := auth.User{ID: 1, Username: "tester", Policies: []string{"products:create"}}

	opts := CreateProductOptions{Name: "Widget", SKU: "WGT-ROLL", Price: 1.23}
	if _, err := svc.Create(context.Background(), opts, actor); err == nil {
		t.Fatalf("expected error due to audit failure")
	}

	var count int64
	if err := db.Model(&Product{}).Count(&count).Error; err != nil {
		t.Fatalf("count err: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 rows after rollback, got %d", count)
	}
}
