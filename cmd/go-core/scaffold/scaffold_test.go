package scaffold_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wssto2/go-core/cmd/go-core/scaffold"
	"time"
)

func TestToPascal(t *testing.T) {
	cases := []struct{ in, want string }{
		{"product", "Product"},
		{"product_image", "ProductImage"},
		{"product-image", "ProductImage"},
		{"order line item", "OrderLineItem"},
		{"", ""},
	}
	for _, c := range cases {
		if got := scaffold.ToPascal(c.in); got != c.want {
			t.Errorf("ToPascal(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestToSnake(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Product", "product"},
		{"ProductImage", "product_image"},
		{"product-image", "product_image"},
		{"order_line", "order_line"},
	}
	for _, c := range cases {
		if got := scaffold.ToSnake(c.in); got != c.want {
			t.Errorf("ToSnake(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestToCamel(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Product", "product"},
		{"ProductImage", "productImage"},
		{"product_image", "productImage"},
	}
	for _, c := range cases {
		if got := scaffold.ToCamel(c.in); got != c.want {
			t.Errorf("ToCamel(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestActionName(t *testing.T) {
	cases := []struct{ pkg, pascal, want string }{
		{"order", "OrderShipped", "shipped"},
		{"order", "Shipped", "shipped"},
		{"product", "ProductCreated", "created"},
		{"product", "ProductImageUploaded", "image_uploaded"},
		{"invoice", "InvoiceLineAdded", "line_added"},
	}
	for _, c := range cases {
		if got := scaffold.ActionName(c.pkg, c.pascal); got != c.want {
			t.Errorf("ActionName(%q, %q) = %q, want %q", c.pkg, c.pascal, got, c.want)
		}
	}
}

func TestGenerateMigration(t *testing.T) {
	dir := t.TempDir()
	path, err := scaffold.GenerateMigration(dir, "add_status_to_orders", time.Date(2026, 9, 30, 16, 5, 9, 0, time.UTC))
	if err != nil {
		t.Fatalf("GenerateMigration: %v", err)
	}
	if want := filepath.Join(dir, "20260930160509_add_status_to_orders.sql"); path != want {
		t.Errorf("path = %s, want %s", path, want)
	}
	content, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(content), "-- +goose Up\n") {
		t.Errorf("migration does not start with the goose annotation:\n%s", content)
	}
}

func TestGenerateEvent(t *testing.T) {
	dir := t.TempDir()
	path, err := scaffold.GenerateEvent(dir, "OrderShipped", "acme/shop", "order")
	if err != nil {
		t.Fatalf("GenerateEvent: %v", err)
	}
	content, _ := os.ReadFile(path)
	src := string(content)
	if !strings.Contains(src, `"order.shipped"`) {
		t.Errorf("event file has wrong EventName, got:\n%s", src)
	}
	if !strings.Contains(src, "OrderShippedEvent") {
		t.Errorf("event file missing type name")
	}
}
