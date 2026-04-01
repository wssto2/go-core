package event

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/wssto2/go-core/database"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openTestDB(t *testing.T) *gorm.DB {
	// use unique in-memory database per test to avoid cross-test interference
	dsn := fmt.Sprintf("file:outbox_test_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	return db
}

func TestOutbox_InsertWithinTransaction(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureOutboxSchema(db); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	// create marker table
	if err := db.Exec("CREATE TABLE tx_marker (id INTEGER PRIMARY KEY AUTOINCREMENT, val TEXT)").Error; err != nil {
		t.Fatalf("create marker table: %v", err)
	}

	trans := database.NewTransactor(db)

	env, err := WrapEventWithMetadata(context.Background(), struct{ Msg string }{Msg: "hello"})
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}

	// within transaction: insert marker and outbox
	if err := trans.WithinTransaction(context.Background(), func(ctx context.Context) error {
		tx, ok := database.TxFromContext(ctx)
		if !ok {
			return nil
		}
		if err := tx.Exec("INSERT INTO tx_marker (val) VALUES (?)", "v1").Error; err != nil {
			return err
		}
		if err := InsertOutboxEvent(ctx, tx, env); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatalf("transaction failed: %v", err)
	}

	// verify marker present
	var cnt int64
	if err := db.Raw("SELECT COUNT(*) FROM tx_marker").Scan(&cnt).Error; err != nil {
		t.Fatalf("count marker: %v", err)
	}
	if cnt != 1 {
		t.Fatalf("expected 1 marker, got %d", cnt)
	}

	// verify outbox present
	var outs []OutboxEvent
	if err := db.Find(&outs).Error; err != nil {
		t.Fatalf("find outbox: %v", err)
	}
	if len(outs) != 1 {
		t.Fatalf("expected 1 outbox, got %d", len(outs))
	}

	// ensure envelope can be unmarshalled
	var got Envelope
	if err := json.Unmarshal(outs[0].Envelope, &got); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if got.RequestID == "" {
		t.Fatalf("envelope missing request id")
	}
}

func TestOutbox_WorkerPublishesAndMarksProcessed(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureOutboxSchema(db); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	// create and insert an envelope directly
	env, err := WrapEventWithMetadata(context.Background(), struct{ Msg string }{Msg: "world"})
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	if err := InsertOutboxEvent(context.Background(), db, env); err != nil {
		t.Fatalf("insert outbox: %v", err)
	}

	bus := NewInMemoryBus()
	ch := make(chan Envelope, 1)
	if err := bus.Subscribe(Envelope{}, func(ctx context.Context, ev any) error {
		ch <- ev.(Envelope)
		return nil
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	log := slog.Default()

	worker := NewOutboxWorker(db, bus, log, 50*time.Millisecond, 10)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// run worker in goroutine
	go func() {
		_ = worker.Run(ctx)
	}()

	select {
	case got := <-ch:
		if got.RequestID != env.RequestID {
			t.Fatalf("mismatched request id: %s != %s", got.RequestID, env.RequestID)
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("timeout waiting for published event")
	}

	// verify processed_at is set
	var out OutboxEvent
	if err := db.First(&out).Error; err != nil {
		t.Fatalf("fetch outbox: %v", err)
	}
	if out.ProcessedAt == nil {
		t.Fatalf("expected processed_at to be set")
	}
}
