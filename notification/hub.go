package notification

import (
	"context"
	"sync"
)

// StreamKind says what changed in a person's inbox.
type StreamKind string

// The kinds of change the live stream carries.
const (
	// StreamCreated carries a new notification.
	StreamCreated StreamKind = "created"
	// StreamRead says notifications were read on some device: ReadIDs, or every
	// notification up to ReadUpToID when AllRead is set.
	StreamRead StreamKind = "read"
	// StreamUnread is a snapshot of the unread count, sent when a stream connects
	// so a reconnecting app catches up on what it missed.
	StreamUnread StreamKind = "unread"
)

// StreamEvent is one change pushed to every open app of one person. Every event
// carries the fresh unread count, so a client never has to count itself
// (NOTIF-READ-001).
type StreamEvent struct {
	Kind         StreamKind `json:"type"`
	Notification *Item      `json:"notification,omitempty"`
	ReadIDs      []int      `json:"read_ids,omitempty"`
	// AllRead with ReadUpToID: every notification with id <= ReadUpToID is read.
	AllRead     bool `json:"all_read,omitempty"`
	ReadUpToID  int  `json:"read_up_to_id,omitempty"`
	UnreadCount int  `json:"unread_count"`
}

// subscriberBuffer is how many events a subscriber may lag behind. An open
// stream drains at once, so a full buffer means a stuck connection.
const subscriberBuffer = 32

type subscription struct {
	ch   chan StreamEvent
	once sync.Once
}

func (s *subscription) close() { s.once.Do(func() { close(s.ch) }) }

// Hub fans inbox changes out to every app a person has open (NOTIF-READ-001).
//
// It lives in one process: ONE INSTANCE ONLY. With several instances of the
// application, a notification made on one is announced only to the apps open
// on that one. Delivery is best effort in any case: a slow or gone subscriber
// misses an event and resynchronises from the unread snapshot every stream starts
// with, which is why a client refetches the unread count on reconnect and on
// return to the foreground. A shared channel (polling the table, or a message
// broker) behind the same Subscribe and Publish is the way to several instances.
type Hub struct {
	mu   sync.Mutex
	subs map[int]map[*subscription]struct{}
}

// NewHub returns an empty hub.
func NewHub() *Hub { return &Hub{subs: map[int]map[*subscription]struct{}{}} }

// Subscribe registers one open app of the person. The channel closes when cancel
// is called, which is idempotent and must be called when the app goes away, or
// when the hub dropped it for lagging.
func (h *Hub) Subscribe(_ context.Context, userID int) (<-chan StreamEvent, func()) {
	sub := &subscription{ch: make(chan StreamEvent, subscriberBuffer)}

	h.mu.Lock()
	if h.subs[userID] == nil {
		h.subs[userID] = map[*subscription]struct{}{}
	}

	h.subs[userID][sub] = struct{}{}
	h.mu.Unlock()

	return sub.ch, func() { h.remove(userID, sub) }
}

// Publish sends the event to every open app of the person without blocking. A
// subscriber whose buffer is full is disconnected instead of silently missing the
// event: its client reconnects and resynchronises.
func (h *Hub) Publish(_ context.Context, userID int, ev StreamEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for sub := range h.subs[userID] {
		select {
		case sub.ch <- ev:
		default:
			h.removeLocked(userID, sub)
		}
	}
}

// Subscribers counts the open apps of the person.
func (h *Hub) Subscribers(userID int) int {
	h.mu.Lock()
	defer h.mu.Unlock()

	return len(h.subs[userID])
}

func (h *Hub) remove(userID int, sub *subscription) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.removeLocked(userID, sub)
}

func (h *Hub) removeLocked(userID int, sub *subscription) {
	if _, ok := h.subs[userID][sub]; ok {
		delete(h.subs[userID], sub)

		if len(h.subs[userID]) == 0 {
			delete(h.subs, userID)
		}
	}

	sub.close()
}
