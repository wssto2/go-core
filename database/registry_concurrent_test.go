package database

import (
	"fmt"
	"log/slog"
	"sync"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRegistry_ConcurrentRegisterRaw(t *testing.T) {
	log := slog.New(slog.Default().Handler())
	reg := NewRegistry(log, RegistryConfig{})
	t.Cleanup(func() { _ = reg.CloseAll() })

	const n = 20
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			conn, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
			if err != nil {
				t.Errorf("open sqlite: %v", err)
				return
			}
			name := fmt.Sprintf("db-%d", i)
			reg.AddConnection(name, conn)

			if !reg.Has(name) {
				t.Errorf("expected Has(%s) true", name)
			}
			c, err := reg.Get(name)
			if err != nil {
				t.Errorf("Get %s: %v", name, err)
			} else if c == nil {
				t.Errorf("Get returned nil for %s", name)
			}
		}(i)
	}
	wg.Wait()
	names := reg.Names()
	if len(names) != n {
		t.Fatalf("expected %d names, got %d", n, len(names))
	}
}

func TestRegistry_ConcurrentRegisterSameName(t *testing.T) {
	log := slog.New(slog.Default().Handler())
	reg := NewRegistry(log, RegistryConfig{})
	t.Cleanup(func() { _ = reg.CloseAll() })

	const n = 10
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			conn, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
			if err != nil {
				t.Errorf("open sqlite: %v", err)
				return
			}
			reg.AddConnection("shared", conn)
		}(i)
	}
	wg.Wait()
	names := reg.Names()
	if len(names) != 1 {
		t.Fatalf("expected 1 name, got %d", len(names))
	}
}
