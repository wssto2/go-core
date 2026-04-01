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

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/audit"
	coreauth "github.com/wssto2/go-core/auth"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/event"
	"github.com/wssto2/go-core/web"
	"go-core-example/internal/domain/auth"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestHandler_CreateAndShow(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	// Create a minimal audit_logs table so resource counts work without
	// requiring AutoMigrate on the core audit.AuditLog type (which uses
	// map[string]any for Metadata and can break GORM's AutoMigrate).
	if err := db.Exec("CREATE TABLE IF NOT EXISTS audit_logs (id INTEGER PRIMARY KEY, entity_type TEXT, entity_id INTEGER)").Error; err != nil {
		t.Fatalf("create audit_logs table: %v", err)
	}
	if err := database.SafeMigrate(db, &Product{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	repo := NewRepository(db)
	transactor := database.NewTransactor(db)
	auditRepo := audit.NewRepositoryWithHook(transactor, func(ctx context.Context, log audit.AuditLog) error { return nil })
	bus := event.NewInMemoryBus()
	svc := NewService(repo, transactor, auditRepo, bus, slog.Default())
	h := newHandler(svc)

	router := gin.New()
	// Inject an authenticated user so authorization middleware passes.
	router.Use(func(c *gin.Context) {
		coreauth.SetUser(c, auth.User{ID: 1, Username: "tester", Policies: []string{"products:create", "products:update", "products:delete"}})
		c.Next()
	})

	api := router.Group("/api/v1")
	h.registerRoutes(api.Group("/products"))

	// Create product
	w := httptest.NewRecorder()
	body := `{"name":"Widget","sku":"WGT-1","price":12.34,"stock":10}`
	req := httptest.NewRequest("POST", "/api/v1/products", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d: %s", w.Code, w.Body.String())
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
	sku, _ := data["sku"].(string)
	if sku != "WGT-1" {
		t.Fatalf("expected sku WGT-1, got %s", sku)
	}

	idFloat, ok := data["id"].(float64)
	if !ok {
		t.Fatalf("expected numeric id, got %T", data["id"])
	}
	id := int(idFloat)

	// Fetch the created product
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", fmt.Sprintf("/api/v1/products/%d", id), nil)
	router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d: %s", w2.Code, w2.Body.String())
	}
	var resp2 web.Response
	if err := json.Unmarshal(w2.Body.Bytes(), &resp2); err != nil {
		t.Fatalf("invalid response: %v", err)
	}
	data2, ok := resp2.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected data type: %T", resp2.Data)
	}
	if data2["sku"] != "WGT-1" {
		t.Fatalf("expected sku WGT-1, got %v", data2["sku"])
	}
}
