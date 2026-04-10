package scaffold_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wssto2/go-core/cmd/go-core/scaffold"
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

func TestGenerateModule_BasicFiles(t *testing.T) {
	dir := t.TempDir()
	data := scaffold.ModuleData{
		GoModule: "acme/shop",
		Package:  "order",
		Pascal:   "Order",
	}

	written, err := scaffold.GenerateModule(dir, data)
	if err != nil {
		t.Fatalf("GenerateModule: %v", err)
	}

	want := []string{"model.go", "repository.go", "service.go", "handler.go", "requests.go", "module.go"}
	if len(written) != len(want) {
		t.Errorf("wrote %d files, want %d", len(written), len(want))
	}
	for _, name := range want {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("missing file: %s", name)
		}
	}
}

func TestGenerateModule_WithFeatures(t *testing.T) {
	dir := t.TempDir()
	data := scaffold.ModuleData{
		GoModule: "acme/shop",
		Package:  "invoice",
		Pascal:   "Invoice",
		Features: scaffold.Features{Events: true, Audit: true, Worker: true},
	}

	written, err := scaffold.GenerateModule(dir, data)
	if err != nil {
		t.Fatalf("GenerateModule: %v", err)
	}

	// Should include events.go and worker.go
	names := make(map[string]bool)
	for _, p := range written {
		names[filepath.Base(p)] = true
	}
	for _, f := range []string{"events.go", "worker.go"} {
		if !names[f] {
			t.Errorf("expected %s in output", f)
		}
	}
}

func TestGenerateModule_PackageNameInFiles(t *testing.T) {
	dir := t.TempDir()
	data := scaffold.ModuleData{
		GoModule: "acme/shop",
		Package:  "product",
		Pascal:   "Product",
		Features: scaffold.Features{Audit: true},
	}

	if _, err := scaffold.GenerateModule(dir, data); err != nil {
		t.Fatalf("GenerateModule: %v", err)
	}

	for _, name := range []string{"model.go", "service.go", "module.go"} {
		content, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if !strings.HasPrefix(strings.TrimSpace(string(content)), "package product") {
			t.Errorf("%s does not start with 'package product'", name)
		}
	}
}

func TestGenerateModule_NoDuplicateFiles(t *testing.T) {
	dir := t.TempDir()
	data := scaffold.ModuleData{GoModule: "acme/shop", Package: "order", Pascal: "Order"}

	if _, err := scaffold.GenerateModule(dir, data); err != nil {
		t.Fatalf("first generate: %v", err)
	}
	if _, err := scaffold.GenerateModule(dir, data); err == nil {
		t.Error("expected error on duplicate generate, got nil")
	}
}

func TestGenerateMigration(t *testing.T) {
	dir := t.TempDir()
	path, err := scaffold.GenerateMigration(dir, "add_status_to_orders", "acme/shop")
	if err != nil {
		t.Fatalf("GenerateMigration: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("migration file not created: %v", err)
	}
	content, _ := os.ReadFile(path)
	if !strings.Contains(string(content), "MigrateAddStatusToOrders") {
		t.Errorf("migration file missing expected function name")
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
