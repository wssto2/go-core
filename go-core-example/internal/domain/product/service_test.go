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
	storememory "github.com/wssto2/go-core/storage/memory"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestService_Create_WritesAuditAndOutboxEvent(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	if err := database.SafeMigrate(db, &Product{}, &event.OutboxEvent{}); err != nil {
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

	memStore, _ := storememory.New()
	svc := NewService(repo, transactor, auditRepo, memStore, slog.Default())
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

	var outboxCount int64
	if err := db.Model(&event.OutboxEvent{}).Count(&outboxCount).Error; err != nil {
		t.Fatalf("count outbox: %v", err)
	}
	if outboxCount != 1 {
		t.Fatalf("expected 1 outbox event, got %d", outboxCount)
	}
}

func TestService_Create_RollsBackOnAuditFailure(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	if err := database.SafeMigrate(db, &Product{}, &event.OutboxEvent{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	repo := NewRepository(db)
	transactor := database.NewTransactor(db)

	hook := func(ctx context.Context, log audit.AuditLog) error {
		return fmt.Errorf("forced failure")
	}
	auditRepo := audit.NewRepositoryWithHook(transactor, hook)

	memStore2, _ := storememory.New()
	svc := NewService(repo, transactor, auditRepo, memStore2, slog.Default())
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

func TestService_Update_ChangesFields(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := database.SafeMigrate(db, &Product{}, &event.OutboxEvent{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	repo := NewRepository(db)
	transactor := database.NewTransactor(db)
	auditRepo := audit.NewRepositoryWithHook(transactor, func(ctx context.Context, log audit.AuditLog) error { return nil })
	memStore, _ := storememory.New()
	svc := NewService(repo, transactor, auditRepo, memStore, slog.Default())
	actor := auth.User{ID: 1, Username: "tester", Policies: []string{"products:create", "products:update"}}

	created, err := svc.Create(context.Background(), CreateProductOptions{Name: "Old Name", SKU: "UPD-1", Price: 5.00}, actor)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	updated, err := svc.Update(context.Background(), created.ID, UpdateProductOptions{Name: "New Name", Price: 9.99}, actor)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Name != "New Name" {
		t.Fatalf("expected name 'New Name', got %q", updated.Name)
	}
	if updated.Price.Get() != 9.99 {
		t.Fatalf("expected price 9.99, got %v", updated.Price)
	}
	// SKU not in opts, should be unchanged.
	if updated.SKU != "UPD-1" {
		t.Fatalf("expected unchanged sku, got %q", updated.SKU)
	}
}

func TestService_Delete_SoftDeletes(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := database.SafeMigrate(db, &Product{}, &event.OutboxEvent{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	repo := NewRepository(db)
	transactor := database.NewTransactor(db)
	auditRepo := audit.NewRepositoryWithHook(transactor, func(ctx context.Context, log audit.AuditLog) error { return nil })
	memStore, _ := storememory.New()
	svc := NewService(repo, transactor, auditRepo, memStore, slog.Default())
	actor := auth.User{ID: 1, Username: "tester", Policies: []string{"products:create", "products:delete"}}

	created, err := svc.Create(context.Background(), CreateProductOptions{Name: "Del Me", SKU: "DEL-SVC-1", Price: 1.00}, actor)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := svc.Delete(context.Background(), created.ID, actor); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Should not be found after soft delete.
	_, err = svc.GetByID(context.Background(), created.ID)
	if err == nil {
		t.Fatalf("expected not found after delete")
	}

	// Row must still exist in DB (soft delete, not hard delete).
	var count int64
	if err := db.Unscoped().Model(&Product{}).Where("id = ?", created.ID).Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected row to still exist (soft delete), got count=%d", count)
	}
}
