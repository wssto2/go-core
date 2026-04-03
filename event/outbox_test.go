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
	if err := db.Exec("CREATE TABLE tx_marker (id INTEGER PRIMARY KEY AUTOINCREMENT, val TEXT)").Error; err != nil {
		t.Fatalf("create marker table: %v", err)
	}

	trans := database.NewTransactor(db)

	type testMsg struct{ Msg string }

	if err := trans.WithinTransaction(context.Background(), func(ctx context.Context) error {
		tx, ok := database.TxFromContext(ctx)
		if !ok {
			return nil
		}
		if err := tx.Exec("INSERT INTO tx_marker (val) VALUES (?)", "v1").Error; err != nil {
			return err
		}
		return InsertOutboxEvent(ctx, tx, testMsg{Msg: "hello"})
	}); err != nil {
		t.Fatalf("transaction failed: %v", err)
	}

	var cnt int64
	if err := db.Raw("SELECT COUNT(*) FROM tx_marker").Scan(&cnt).Error; err != nil {
		t.Fatalf("count marker: %v", err)
	}
	if cnt != 1 {
		t.Fatalf("expected 1 marker, got %d", cnt)
	}

	var outs []OutboxEvent
	if err := db.Find(&outs).Error; err != nil {
		t.Fatalf("find outbox: %v", err)
	}
	if len(outs) != 1 {
		t.Fatalf("expected 1 outbox event, got %d", len(outs))
	}
	if outs[0].EventType == "" {
		t.Fatalf("expected EventType to be set")
	}

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

	type testMsg struct{ Msg string }
	if err := InsertOutboxEvent(context.Background(), db, testMsg{Msg: "world"}); err != nil {
		t.Fatalf("insert outbox: %v", err)
	}

	type delivery struct {
		subject string
		data    []byte
	}
	ch := make(chan delivery, 1)
	publish := func(ctx context.Context, subject string, data []byte) error {
		ch <- delivery{subject: subject, data: data}
		return nil
	}

	worker := NewOutboxWorker(db, publish, slog.Default(), 50*time.Millisecond, 10)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() { _ = worker.Run(ctx) }()

	select {
	case got := <-ch:
		if got.subject == "" {
			t.Fatalf("expected non-empty subject")
		}
		var env Envelope
		if err := json.Unmarshal(got.data, &env); err != nil {
			t.Fatalf("unmarshal envelope: %v", err)
		}
		if env.RequestID == "" {
			t.Fatalf("envelope missing request id")
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("timeout waiting for published event")
	}

	// give worker time to call markProcessed before reading
	time.Sleep(200 * time.Millisecond)

	var out OutboxEvent
	if err := db.First(&out).Error; err != nil {
		t.Fatalf("fetch outbox: %v", err)
	}
	if out.ProcessedAt == nil {
		t.Fatalf("expected processed_at to be set")
	}
}
