# Logic IDs: NOTIF-EVENT-001, NOTIF-CATEGORY-001, NOTIF-RECIPIENT-001, NOTIF-CONTENT-001, NOTIF-READ-001

Title: In-app notifications come from events, to active people, in their own language, and are read everywhere at once

Status: Approved (the rules moved from arv-next, `documentation/business-rules/notification/notifications.md`; owner decisions 2026-10-03 for the API)
Last updated: 2026-10-03
Module: `notification` (inbox, live stream, routes), over the event queue in `event`; dead-letter routes are `notification`'s too, over `event.DeadLetters`

## Summary

A feature never writes a notification. It publishes an event, in the transaction of the write it reports, and a
consumer of that event calls `Notices.Send` with the category, the recipients and a function that renders the
message for each of them. The module keeps the inbox (read state per notification, so it is the same on every
device) and tells the apps a person has open. Delivery by push and e-mail, preferences and quiet hours are a later
phase (P6) and are not part of these rules.

## NOTIF-EVENT-001 — Notifications are made from events, once per event

1. A module that wants a notification publishes an event (`event.Define[T]("name").Publish`) inside the
   transaction of the write it reports. A rolled-back write never notifies; a committed one always does, even if the
   process dies right after: the event is queued with the write.
2. The event carries **ids and facts**, never rendered text. The text is rendered per recipient when the notification
   is made (NOTIF-CONTENT-001).
3. **`Send` runs only inside an event consumer** (`event.ID(ctx)` is the key it dedupes on). Called anywhere else it fails
   with `ErrNotInConsumer` and an error that says "publish an event and send from its consumer". It also fails inside a
   transaction: it commits its own, and tells the open apps only after the commit.
4. **The dedupe key is `<event id>:<user id>:<category>`.** The notifications table has a unique index on it, and a
   second delivery inserts nothing: an event handled twice (a retry after the consumer failed, a lease that ran out) makes
   one notification per person, whoever races. Only the notifications that were written are announced to the stream.
5. **A failing event is retried, not lost.** A `Send` that fails because the database or the directory is away returns the
   error, and the queue retries the event with the consumer's backoff (default 5 attempts, 5 seconds doubling up to 30
   minutes; `.Retry(event.Attempts(n), event.Backoff(first, longest))` changes them). After its attempts the event is set
   aside as a **dead letter**, with the last error. What can never be handled does not wait for retries: an unreadable
   payload, a category that was not registered, a title that is empty, a link that leaves the app, all `event.ErrMalformed`,
   are dead letters at once. Everyone's notifications of one `Send` are written in one transaction: all of them or, on an
   error, none.
6. **Dead letters are visible and can be retried**, for every consumer of the application's queue, not only the
   notification ones: `GET /v1/events/dead-letters` (page, `consumer` filter; newest first, with the event name, when the
   event was queued, attempts, last error and when it was given up on) behind `events.deadletter:view`;
   `POST /v1/events/dead-letters/:event/:consumer/retry` and `POST /v1/events/dead-letters/retry` (all of one consumer)
   behind `events.deadletter:retry`, which needs the view permission. The permission ids are fixed. A retry puts the
   letter back in the queue in one transaction, only while it is still dead: its attempts start over and its event is
   unprocessed again. A second retry of the same letter is 404; a letter whose event was pruned cannot be retried (409).
7. **Housekeeping.** An event every consumer finished is deleted with its attempts 30 days after it was processed
   (`event.Retention`), by a worker `App.Events` adds. **Dead letters are never deleted by it**: their events are kept for
   as long as the letters are, because a retry needs them.

## NOTIF-CATEGORY-001 — Categories are declared in code

1. A category is a value, `notification.Category("tickets.assigned")`, declared once by the application and registered
   with `notification.Install`. The code is lower-case words joined by dots or dashes, at most 64 characters. A code that
   is not, one registered twice, or the module's own `system.test` is a start-up problem that names the fix.
2. `Send` with a category that was not registered fails with an error naming the fix ("pass it to
   notification.Install") and the event is a dead letter at once (NOTIF-EVENT-001, rule 5).
3. `system.test` is the module's own. `POST /v1/notifications/test` publishes the module's own event for the signed-in
   person, its consumer sends the test notification, and the person is told: unlike a feature's notification it does not
   leave the actor out.
4. Priority, push and e-mail defaults per category come with the delivery channels (P6); a category is only its code now.

## NOTIF-RECIPIENT-001 — Only active people, each once, and not the actor

1. `notification.To(ids...)` names the people. **Invalid ids** (zero, negative), **duplicates** and **people who are
   not accounts** are dropped.
2. **Only active people.** An inactive account is dropped. The accounts of all the ids are looked up in **one** call per
   `Send`, and none when nobody is left to look up.
3. **Never notify the actor about their own action**: `.Except(actor)` takes them out, so assigning a ticket to yourself
   notifies nobody. It is the caller's choice, not a flag of the category: the test notification does not use it.
4. The render function is called **once per recipient**, in the order the ids were named, with the person's id, name
   (their login when they have none) and locale.

## NOTIF-CONTENT-001 — The text is rendered per recipient and always fits

1. **Rendered per recipient, in their locale**, when the notification is made: the module has no translations and no
   dependency on an i18n package; the application's render function translates for `Recipient.Locale`.
2. **The title is required**, and the title and body are cut to their columns (160 and 500 characters, ending in an
   ellipsis) rather than failing the event. Short and actionable; never a customer's phone number or e-mail address, and
   never a secret in `Data`.
3. **A link is a path inside the app**, or empty: it starts with one `/`, has no backslash or line break and is at most
   255 characters. An absolute URL, `//host` or anything else is refused, so a notification can never send a person
   off-site (an open redirect from a tapped push). A message that breaks 2 or 3 for any recipient fails the whole `Send`,
   so nobody gets a notification the others did not (NOTIF-EVENT-001, rule 5).

## NOTIF-READ-001 — Read state is per notification, everywhere at once

1. **Read and unread live on the notification** (per person), so reading it on one device marks it read on every device.
   Every inbox call acts on the signed-in person's own notifications: the id comes from the session, never from the
   request; another person's notification is "not found". Reading one that is already read changes nothing and keeps its
   first time.
2. **"Mark all as read" marks only what the person has seen**: the notifications up to `up_to_id`, the newest one the app
   showed, so one that arrives while they click stays unread. With nothing shown, `up_to_id` is 0 or missing and nothing
   is marked, never "all".
3. **The unread count comes with every answer** of the inbox's mutations, and the live stream tells every open app of the
   person about a new notification and about a reading, each with the fresh count, **after the transaction has
   committed**. A stream opens with the current count, so an app that reconnects catches up. A subscriber too slow to
   keep up is dropped and reconnects. The hub is **process-local: one instance only**; with several instances an app hears
   only what its own instance made, and the client's refetch of the unread count on reconnect and on return to the
   foreground keeps it correct.
4. The stream re-checks its session at each heartbeat (25 seconds) and ends when the identity session was revoked
   (sign-out, a password change, ending all sessions), so the app reconnects through authentication, which refuses it.
   It lifts the server's write deadline for its own response only.

## Differences from arv-next

- The dedupe reference is the queue's event id (`<event id>:<user>:<category>`); arv-next's was `outbox:<id>`. A
  notification row arv-next already has keeps its key; its table is adopted as it is (migration
  `20261016000030_notifications.sql` creates exactly arv-next's table).
- Retries: the module's default is the queue's 5 attempts; arv-next gave 15 failures. A consumer that wants more says
  `.Retry(event.Attempts(15))`. Dead letters are generic over every consumer (permissions `events.deadletter:view|retry`
  replace `notification.deadletter:*`) and the module never deletes them: arv-next's 90-day deletion of dead letters is the
  application's job if it wants one.
- The "notifies the actor" flag of a category is `.Except(actor)` at the call site.
- Not in this phase, and so not in these rules: priority, push, e-mail, preferences, quiet hours, dealer enforcement,
  location recipients, retention of old notifications (P6; arv-next's NOTIF-PREF-001, NOTIF-QUIET-001, NOTIF-DEVICE-001,
  NOTIF-DELIVERY-001, NOTIF-RECIPIENT-002 and the retention rule 5 of NOTIF-READ-001 stay there until then).
