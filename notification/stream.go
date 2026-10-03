package notification

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/authz"
	identityhttp "github.com/wssto2/go-core/identity/http"
	"github.com/wssto2/go-core/web"
)

// heartbeatInterval keeps proxies and the browser from closing an idle stream,
// and is how often an open stream re-checks its session. A client's liveness
// watchdog is sized from it.
const heartbeatInterval = 25 * time.Second

// Subscribe opens the person's live stream: every change to their inbox from
// now on. Call cancel when the app goes away.
func (i *Inbox) Subscribe(ctx context.Context, userID int) (<-chan StreamEvent, func()) {
	return i.hub.Subscribe(ctx, userID)
}

// announce tells the open apps of the people who got a notification, after the
// transaction that wrote it has committed. Best effort: failing to count is
// logged, never an error of the event.
func (n *Notices) announce(ctx context.Context, created []row) {
	for _, r := range created {
		userID := int(r.UserID)
		if n.Inbox.hub.Subscribers(userID) == 0 {
			continue
		}

		count, err := n.store.countUnread(ctx, userID)
		if err != nil {
			n.log.WarnContext(ctx, "notification: the stream was not told of a new notification", "user_id", userID, "error", err)

			continue
		}

		item := r.item()
		n.Inbox.hub.Publish(ctx, userID, StreamEvent{Kind: StreamCreated, Notification: &item, UnreadCount: count})
	}
}

// person is the id of the signed-in person. The inbox belongs to people: a
// service account has none.
func person(ctx context.Context) (int, error) {
	p, ok := authz.PrincipalFrom(ctx)
	if !ok {
		return 0, apperr.Unauthorized("not authenticated")
	}

	if p.Kind != authz.KindUser {
		return 0, apperr.Forbidden("only people have an inbox")
	}

	return p.ID, nil
}

// stream is the handler of the live stream: server-sent events of the person's
// inbox changes. It opens with the current unread count, so a reconnecting app
// catches up on anything it missed while disconnected (NOTIF-READ-001). It lifts
// the server's write deadline for this response only, sends a heartbeat comment
// every interval and, with each, re-checks the session it was opened with, so
// signing out, a password change or a revoked session ends it.
func (n *Notices) stream(interval time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, err := person(c.Request.Context())
		if err != nil {
			web.Fail(c, err)

			return
		}

		reqCtx := c.Request.Context()

		// Subscribe before reading the snapshot: an event published in between waits
		// in the subscription instead of being lost until the next one.
		events, cancel := n.Inbox.Subscribe(reqCtx, userID)
		defer cancel()

		count, err := n.Inbox.Unread(reqCtx, userID)
		if err != nil {
			web.Fail(c, err)

			return
		}

		// The server's write timeout would cut the stream; a stream has no natural
		// end, so it lifts the deadline for this response only.
		controller := http.NewResponseController(c.Writer)
		_ = controller.SetWriteDeadline(time.Time{})

		header := c.Writer.Header()
		header.Set("Content-Type", "text/event-stream")
		header.Set("Cache-Control", "no-cache, no-transform")
		header.Set("Connection", "keep-alive")
		header.Set("X-Accel-Buffering", "no")
		c.Status(http.StatusOK)

		if !writeEvent(c, controller, StreamEvent{Kind: StreamUnread, UnreadCount: count}) {
			return
		}

		heartbeat := time.NewTicker(interval)
		defer heartbeat.Stop()

		for {
			select {
			case <-reqCtx.Done():
				return
			case <-heartbeat.C:
				if !n.sessionStillLive(c, userID) {
					return
				}

				if _, err := fmt.Fprint(c.Writer, ": ping\n\n"); err != nil || controller.Flush() != nil {
					return
				}
			case ev, open := <-events:
				if !open {
					return // dropped as too slow: ending the response makes the client reconnect
				}

				if !writeEvent(c, controller, ev) {
					return
				}
			}
		}
	}
}

// sessionStillLive re-checks the identity session the stream was opened with. The
// authentication that opened the stream may not be identity's (a test's stand-in,
// the application's own): then there is no session to check, and the stream
// lives as long as the connection.
func (n *Notices) sessionStillLive(c *gin.Context, userID int) bool {
	ctx := c.Request.Context()

	who, ok := identityhttp.AuthenticatedFrom(ctx)
	if !ok {
		return true
	}

	sessions, err := n.people.Sessions(ctx, userID)
	if err != nil {
		return true // a failed check is not a revoked session; the next heartbeat tries again
	}

	for _, s := range sessions {
		if s.ID == who.Session.ID {
			return true
		}
	}

	return false
}

func writeEvent(c *gin.Context, controller *http.ResponseController, ev StreamEvent) bool {
	payload, err := json.Marshal(ev)
	if err != nil {
		return false
	}

	if _, err := fmt.Fprintf(c.Writer, "data: %s\n\n", payload); err != nil {
		return false
	}

	return controller.Flush() == nil
}
