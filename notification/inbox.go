package notification

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/gocore"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// The inbox page size: DefaultPageSize unless the caller asks for another, never more than MaxPageSize.
const (
	DefaultPageSize = 20
	MaxPageSize     = 50
)

// Item is one notification as its recipient sees it. Read state lives on the
// notification, so reading it on one device marks it read on every device
// (NOTIF-READ-001).
type Item struct {
	ID       int      `json:"id"`
	Category Category `json:"category"`
	Title    string   `json:"title"`
	Body     string   `json:"body"`
	// Link is an in-app path, empty when the notification opens nothing.
	Link string            `json:"link"`
	Data map[string]string `json:"data"`
	// ReadAt is when it was read on any device, nil while unread.
	ReadAt    *time.Time `json:"read_at"`
	CreatedAt time.Time  `json:"created_at"`
}

// ListQuery selects one page of a person's inbox, newest first. To load the
// next page pass the last item's id as before_id.
type ListQuery struct {
	// BeforeID returns only notifications older than this id; 0 starts at the newest.
	BeforeID int `query:"before_id" json:"before_id,omitempty"`
	// Limit is the page size: DefaultPageSize when 0, at most MaxPageSize.
	Limit int `query:"limit" json:"limit,omitempty"`
}

func (q ListQuery) normalized() ListQuery {
	if q.Limit <= 0 {
		q.Limit = DefaultPageSize
	}

	q.Limit = min(q.Limit, MaxPageSize)
	q.BeforeID = max(q.BeforeID, 0)

	return q
}

// Page is one page of an inbox, newest first.
type Page struct {
	Items   []Item `json:"items"`
	HasMore bool   `json:"has_more"`
}

// Inbox is what a person does with their own notifications: list them, count
// the unread ones and read them. Every method acts on one person's inbox, whose
// id the caller takes from the session, never from the request, so nobody
// reaches another person's notifications.
type Inbox struct {
	store *store
	clock gocore.Clock
	hub   *Hub
}

// List returns one page of the person's notifications, newest first.
func (i *Inbox) List(ctx context.Context, userID int, q ListQuery) (Page, error) {
	page, err := i.store.list(ctx, userID, q.normalized())
	if err != nil {
		return Page{}, apperr.Internal(err)
	}

	return page, nil
}

// Unread counts the person's unread notifications.
func (i *Inbox) Unread(ctx context.Context, userID int) (int, error) {
	n, err := i.store.countUnread(ctx, userID)
	if err != nil {
		return 0, apperr.Internal(err)
	}

	return n, nil
}

// MarkRead marks one of the person's notifications read, on every device
// (NOTIF-READ-001), tells the person's open apps, and returns how many are still
// unread. A notification that is not theirs is not found; one already read stays as it is.
func (i *Inbox) MarkRead(ctx context.Context, userID, notificationID int) (int, error) {
	changed, err := i.store.markRead(ctx, userID, notificationID, whole(i.clock.Now()))
	if errors.Is(err, errNotFound) {
		return 0, apperr.NotFound("notification not found")
	}

	if err != nil {
		return 0, apperr.Internal(err)
	}

	count, err := i.Unread(ctx, userID)
	if err != nil {
		return 0, err
	}

	if changed {
		i.hub.Publish(ctx, userID, StreamEvent{Kind: StreamRead, ReadIDs: []int{notificationID}, UnreadCount: count})
	}

	return count, nil
}

// MarkAllRead marks the person's notifications up to and including upToID read
// and returns how many are still unread. upToID is the newest notification the
// person has seen, so one that arrives while they click stays unread; 0 (nothing
// seen) marks nothing, never "all" (NOTIF-READ-001).
func (i *Inbox) MarkAllRead(ctx context.Context, userID, upToID int) (int, error) {
	if upToID <= 0 {
		return i.Unread(ctx, userID)
	}

	changed, err := i.store.markAllRead(ctx, userID, upToID, whole(i.clock.Now()))
	if err != nil {
		return 0, apperr.Internal(err)
	}

	count, err := i.Unread(ctx, userID)
	if err != nil {
		return 0, err
	}

	if changed > 0 {
		i.hub.Publish(ctx, userID, StreamEvent{Kind: StreamRead, AllRead: true, ReadUpToID: upToID, UnreadCount: count})
	}

	return count, nil
}

// whole is a time as the DATETIME columns keep it: to the second.
func whole(t time.Time) time.Time { return t.Truncate(time.Second) }

// row is a notifications row: the GORM model Migrate creates the table from on
// SQLite, and the shape the store reads and writes. The migration file is the
// table in production; migrations_test holds the two equal.
type row struct {
	ID        uint64     `gorm:"column:id;primaryKey;autoIncrement;index:notifications_user_id,priority:2"`
	UserID    uint32     `gorm:"column:user_id;not null;type:int unsigned;index:notifications_user_id,priority:1;index:notifications_user_read,priority:1"`
	Category  string     `gorm:"column:category;size:64;not null"`
	Title     string     `gorm:"column:title;size:160;not null"`
	Body      string     `gorm:"column:body;size:500;not null;default:''"`
	Link      string     `gorm:"column:link;size:255;not null;default:''"`
	Data      string     `gorm:"column:data;type:json;not null"`
	DedupeKey string     `gorm:"column:dedupe_key;size:128;not null;uniqueIndex:notifications_dedupe_key"`
	ReadAt    *time.Time `gorm:"column:read_at;type:datetime;index:notifications_user_read,priority:2"`
	CreatedAt time.Time  `gorm:"column:created_at;type:datetime;not null"`
}

func (row) TableName() string { return "notifications" }

// Migrate creates the notifications table from its GORM model, for tests on
// SQLite and as the reference the migration file is compared with.
func Migrate(db *gorm.DB) error { return db.AutoMigrate(&row{}) }

func newRow(eventID uint64, userID int, category Category, m Message, now time.Time) (row, error) {
	data := "{}"

	if len(m.Data) > 0 {
		raw, err := json.Marshal(m.Data)
		if err != nil {
			return row{}, fmt.Errorf("%w: the data cannot be encoded: %w", ErrInvalidMessage, err)
		}

		data = string(raw)
	}

	return row{
		UserID:   uint32(userID), //nolint:gosec // resolve keeps ids between 1 and MaxInt32
		Category: string(category), Title: m.Title, Body: m.Body, Link: m.Link, Data: data,
		DedupeKey: DedupeKey(eventID, userID, category), CreatedAt: whole(now),
	}, nil
}

func (r row) item() Item {
	data := map[string]string{}
	if r.Data != "" {
		_ = json.Unmarshal([]byte(r.Data), &data) // written by newRow: a flat string map
	}

	return Item{
		ID:       int(r.ID), //nolint:gosec // an auto-increment id of this table fits an int
		Category: Category(r.Category), Title: r.Title, Body: r.Body, Link: r.Link, Data: data,
		ReadAt: r.ReadAt, CreatedAt: r.CreatedAt,
	}
}

var errNotFound = errors.New("notification not found")

// store keeps notifications. Every query is keyed on user_id, so one person's
// inbox can never be read or changed through another's session. It is portable SQL:
// MariaDB 10.3 has no SKIP LOCKED, FOR UPDATE OF or FOR SHARE, and none is needed.
type store struct{ db *gorm.DB }

// conn is the connection to use: the transaction in ctx, or the store's own.
func (s *store) conn(ctx context.Context) *gorm.DB {
	if tx, ok := database.TxFromContext(ctx); ok {
		return tx.WithContext(ctx)
	}

	return s.db.WithContext(ctx)
}

// insertNew writes the rows that do not exist yet, in one transaction, and
// returns the ones it wrote with their ids. A row whose dedupe key exists is
// skipped (NOTIF-EVENT-001): the unique index refuses the second insert, so two
// deliveries racing on one event still make a single row.
func (s *store) insertNew(ctx context.Context, rows []row) ([]row, error) {
	var created []row

	err := s.conn(ctx).Transaction(func(tx *gorm.DB) error {
		for _, r := range rows {
			res := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&r)
			if res.Error != nil {
				return res.Error
			}

			if res.RowsAffected > 0 {
				created = append(created, r)
			}
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("notification: save the notifications: %w", err)
	}

	return created, nil
}

func (s *store) list(ctx context.Context, userID int, q ListQuery) (Page, error) {
	stmt := s.conn(ctx).Where("user_id = ?", userID)
	if q.BeforeID > 0 {
		stmt = stmt.Where("id < ?", q.BeforeID)
	}

	var rows []row

	// One extra row answers "is there an older page" without a count query.
	if err := stmt.Order("id DESC").Limit(q.Limit + 1).Find(&rows).Error; err != nil {
		return Page{}, err
	}

	page := Page{HasMore: len(rows) > q.Limit}
	if page.HasMore {
		rows = rows[:q.Limit]
	}

	page.Items = make([]Item, 0, len(rows))
	for _, r := range rows {
		page.Items = append(page.Items, r.item())
	}

	return page, nil
}

func (s *store) countUnread(ctx context.Context, userID int) (int, error) {
	var n int64

	err := s.conn(ctx).Model(&row{}).Where("user_id = ? AND read_at IS NULL", userID).Count(&n).Error

	return int(n), err
}

// markRead reports whether the notification changed: false when it was already
// read, errNotFound when the person has no such notification.
func (s *store) markRead(ctx context.Context, userID, id int, now time.Time) (bool, error) {
	res := s.conn(ctx).Model(&row{}).
		Where("id = ? AND user_id = ? AND read_at IS NULL", id, userID).
		Update("read_at", now)
	if res.Error != nil {
		return false, res.Error
	}

	if res.RowsAffected > 0 {
		return true, nil
	}

	// Nothing changed: already read, or not this person's.
	var exists int64
	if err := s.conn(ctx).Model(&row{}).Where("id = ? AND user_id = ?", id, userID).Count(&exists).Error; err != nil {
		return false, err
	}

	if exists == 0 {
		return false, errNotFound
	}

	return false, nil
}

// markAllRead marks the person's unread notifications with id <= upToID read
// and returns how many changed.
func (s *store) markAllRead(ctx context.Context, userID, upToID int, now time.Time) (int, error) {
	if upToID <= 0 {
		return 0, nil
	}

	res := s.conn(ctx).Model(&row{}).
		Where("user_id = ? AND read_at IS NULL AND id <= ?", userID, upToID).
		Update("read_at", now)

	return int(res.RowsAffected), res.Error
}
