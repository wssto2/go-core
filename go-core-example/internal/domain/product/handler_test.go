package product

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go-core-example/internal/domain/auth"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/audit"
	coreauth "github.com/wssto2/go-core/auth"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/event"
	"github.com/wssto2/go-core/middlewares"
	storememory "github.com/wssto2/go-core/storage/memory"
	"github.com/wssto2/go-core/web"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// newTestDB opens an in-memory SQLite DB with all required tables migrated.
func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Exec("CREATE TABLE IF NOT EXISTS audit_logs (id INTEGER PRIMARY KEY, entity_type TEXT, entity_id INTEGER)").Error; err != nil {
		t.Fatalf("create audit_logs: %v", err)
	}
	if err := database.SafeMigrate(db, &Product{}, &event.OutboxEvent{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// newTestService builds a service wired to the provided DB.
func newTestService(t *testing.T, db *gorm.DB) Service {
	t.Helper()
	repo := NewRepository(db)
	transactor := database.NewTransactor(db)
	auditRepo := audit.NewRepositoryWithHook(transactor, func(ctx context.Context, log audit.AuditLog) error { return nil })
	memStore, _ := storememory.New()
	return NewService(repo, transactor, auditRepo, memStore, slog.Default())
}

// newTestRouter builds a gin router with error handling and auth injection.
// policies controls which permissions the test actor has.
func newTestRouter(h *handler, policies []string) *gin.Engine {
	r := gin.New()
	r.Use(middlewares.ErrorHandler(slog.Default(), nil, true))
	r.Use(func(c *gin.Context) {
		coreauth.SetUser(c, auth.User{ID: 1, Username: "tester", Policies: policies})
		c.Next()
	})
	h.registerRoutes(r.Group("/products"))
	return r
}

func TestHandler_CreateAndShow(t *testing.T) {
	db := newTestDB(t)
	h := newHandler(newTestService(t, db), middlewares.NewInMemoryIdempotencyStore(24*time.Hour))
	router := newTestRouter(h, []string{"products:create", "products:update", "products:delete"})

	// Create product.
	w := httptest.NewRecorder()
	body := `{"name":"Widget","sku":"WGT-1","price":12.34,"stock":10}`
	req := httptest.NewRequest("POST", "/products", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 got %d: %s", w.Code, w.Body.String())
	}

	var resp web.Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid response: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success true")
	}
	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected data type: %T", resp.Data)
	}
	if data["sku"] != "WGT-1" {
		t.Fatalf("expected sku WGT-1, got %v", data["sku"])
	}
	id := int(data["id"].(float64))

	// Fetch the created product.
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", fmt.Sprintf("/products/%d", id), nil)
	router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d: %s", w2.Code, w2.Body.String())
	}
	var resp2 web.Response
	if err := json.Unmarshal(w2.Body.Bytes(), &resp2); err != nil {
		t.Fatalf("invalid response: %v", err)
	}
	data2, _ := resp2.Data.(map[string]interface{})
	if data2["sku"] != "WGT-1" {
		t.Fatalf("expected sku WGT-1, got %v", data2["sku"])
	}
}

func TestHandler_Create_ValidationError(t *testing.T) {
	db := newTestDB(t)
	h := newHandler(newTestService(t, db), middlewares.NewInMemoryIdempotencyStore(24*time.Hour))
	router := newTestRouter(h, []string{"products:create"})

	// Missing required "name" and "sku" fields — should fail validation.
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/products", strings.NewReader(`{"price":5.00}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code == http.StatusCreated {
		t.Fatalf("expected validation error, got 201")
	}
}

func TestHandler_Delete_SoftDeletes(t *testing.T) {
	db := newTestDB(t)
	h := newHandler(newTestService(t, db), middlewares.NewInMemoryIdempotencyStore(24*time.Hour))
	router := newTestRouter(h, []string{"products:create", "products:delete"})

	// Create a product to delete.
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/products", strings.NewReader(`{"name":"ToDelete","sku":"DEL-1","price":1.00}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create failed: %d %s", w.Code, w.Body.String())
	}
	var resp web.Response
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	data, _ := resp.Data.(map[string]interface{})
	id := int(data["id"].(float64))

	// Delete the product.
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, httptest.NewRequest("DELETE", fmt.Sprintf("/products/%d", id), nil))
	if w2.Code != http.StatusNoContent {
		t.Fatalf("expected 204 got %d: %s", w2.Code, w2.Body.String())
	}

	// Verify it returns 404 after soft delete.
	w3 := httptest.NewRecorder()
	router.ServeHTTP(w3, httptest.NewRequest("GET", fmt.Sprintf("/products/%d", id), nil))
	if w3.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", w3.Code)
	}
}
