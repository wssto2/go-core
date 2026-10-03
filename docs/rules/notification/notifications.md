# Logic IDs: NOTIF-EVENT-001, NOTIF-CATEGORY-001, NOTIF-RECIPIENT-001, NOTIF-CONTENT-001, NOTIF-READ-001, NOTIF-PREF-001, NOTIF-QUIET-001, NOTIF-DELIVERY-001

Title: Notifications come from events, to active people, in their own language; they are read everywhere at once, and e-mailed as each person chose

Status: Approved (the rules moved from arv-next, `documentation/business-rules/notification/notifications.md`; owner decisions 2026-10-03 for the API)
Last updated: 2026-10-04
Module: `notification` (inbox, live stream, e-mail, settings, routes), over the event queue in `event`; dead-letter routes are `notification`'s too, over `event.DeadLetters`

## Summary

A feature never writes a notification. It publishes an event, in the transaction of the write it reports, and a
consumer of that event calls `Notices.Send` with the category, the recipients and a function that renders the
message for each of them. The module keeps the inbox (read state per notification, so it is the same on every
device) and tells the apps a person has open. When identity has mail, each person is also e-mailed the categories whose
e-mail is on for them (NOTIF-PREF-001), never in their quiet hours (NOTIF-QUIET-001), by a worker that retries and gives up
(NOTIF-DELIVERY-001). Web push and devices are not part of these rules yet (see "Not yet").

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
4. A category carries its default: `Category("tickets.assigned").EmailByDefault()` is e-mailed until the person turns it off;
   without it e-mail is off until the person turns it on. The in-app notification is always made. `system.test` is internal:
   not configurable, never e-mailed, never held.

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
5. **Retention.** A notification is deleted 90 days after it was made, read or not, and a finished delivery 30 days after its
   last change (`NotificationRetention`, `DeliveryRetention`), by a worker `Install` starts (every hour, in batches by primary
   key). A delivery that is still owed (pending or held) is never deleted, and a notification only once none of its deliveries is
   left, so none is orphaned.

## NOTIF-PREF-001 — The e-mail of a category is what is enforced, else what the person chose, else the default

1. For a person and a category the setting is the first that applies: **enforced** (the function given to
   `notification.Enforce`, such as a dealer's policy: it says on or off and that the person may not change it), the **person's
   own choice**, the category's **default** (`.EmailByDefault()` or off). The setting says which of the three decided
   (`enforced` / `person` / `default`).
2. Only e-mail is configurable: the in-app notification is always on. The module's internal `system.test` category is not
   listed, not configurable and never e-mailed; an unknown category is not found (404).
3. Only a choice that **differs from the default** is stored, so a person who never touched a setting follows the default if it
   changes; choosing the default again deletes the row.
4. An **enforced** setting cannot be changed: `PUT /v1/notifications/preferences/:category` answers 422 with the reason
   `notification.setting.enforced`, and nothing is stored. An error of the enforcer is an error (500), never a guess.
5. **E-mail is available only when identity has mail.** With `identity.WithoutMail()` (or people that are not identity's), the
   settings say `email_available: false`, every category reads off, turning one on answers 422
   (`notification.email.unavailable`), and no delivery row is ever made: nothing is reported as sent. With mail but no
   `notification.AppURL(...)`, start-up stops and says so.
6. A person with **no e-mail address** is never e-mailed, whatever the setting; an inactive account is not notified at all
   (NOTIF-RECIPIENT-001).
7. Routes, for a signed-in person and no permission, acting on that person only: `GET /v1/notifications/preferences` (each
   configurable category with its e-mail `{enabled, source}`, `email_available`, the quiet hours and the time zone they are
   read in), `PUT /v1/notifications/preferences/:category` (`{"email": true|false}`, required), `PUT /v1/notifications/quiet-hours`.

## NOTIF-QUIET-001 — E-mail waits for the end of the person's quiet hours

1. **Default for everyone: on, 21:00 to 07:00**, in the zone given to `notification.TimeZone` (default `time.Local`), read on the
   wall clock, so a night with a daylight-saving change still ends at 07:00. A person may change them (start and end as minutes
   after midnight, `0` to `1439`, start different from end; a window may run over midnight) or switch them off. Invalid hours are
   refused with 422 (`notification.quiet_hours.invalid`).
2. During quiet hours the in-app notification is made at once and the **e-mail is held**: its delivery is `held` and due when the
   window ends. A held delivery's TTL starts at the release, not at creation, so a night does not use it up. The internal
   `system.test` notification is never held.
3. When the quiet hours end, a held e-mail is sent **only if the notification is still unread**; reading it on any device in the
   meantime cancels it.
4. E-mails are never bundled.

## NOTIF-DELIVERY-001 — An e-mail is made with the notification, sent once, retried, then given up on

1. One **delivery** row per notification and address (`channel` `email`, `device_id` 0), with the status `pending | held | sent |
   failed | cancelled`, attempts, `next_attempt_at`, `expires_at` and the last error. It is written **in the transaction that writes
   the notification**, for each recipient whose setting is on and who has an address; a notification that already exists (a retried
   event) gets no second delivery, so one event makes one e-mail per person.
2. **Content.** The subject is the title (on one line), the body is the notification's body, a button **Open** to `AppURL` plus the
   in-app link (none without a link), and a footer that says why the person gets the mail and where to change it, in the person's
   language (`en`, `hr`, `bs`, `sl`; their language, else the language of the locale, else English). It is rendered with the same
   templates mechanism as identity's mails; the application's renderer given to `identity.WithMailContent` may write
   `notification.email` itself.
3. **Retries.** A failed send is retried 30 seconds later, then after twice as long each time up to one hour, for as long as the
   delivery's TTL of 24 hours lasts; when the next attempt would fall after it, the delivery is `failed` (`expired: ...`). A
   **permanent** error (`mail.ErrPermanent`: a message the checks refuse, an address the relay refuses with a 5xx) fails at once
   (`permanent: ...`) with no retry. A delivery whose notification was deleted or read before it was sent is `cancelled`.
4. **Each delivery is sent once**, however many instances run the worker: a worker claims the due rows it is about to send (a
   candidate read, then the rows locked `FOR UPDATE` by primary key and re-checked as still due, then `next_attempt_at` moves to the
   end of a **5-minute lease**, in one transaction). Another worker claiming meanwhile skips them until the lease ends. A worker
   starts no send in the last minute of its lease; saving the result ends the claim, unless the delivery was cancelled meanwhile,
   which then stands. (No `SKIP LOCKED`, `FOR SHARE` or `FOR UPDATE OF`: MariaDB 10.3.)
5. The worker runs as background work of `Install` every second (it goes again at once while batches are full), only when e-mail
   is available. `Notices.DeliverDue` does what one tick does, for tests and commands. Metrics by category and channel are not part
   of this phase: the worker's own run errors and restarts are counted by the worker manager as `notification.delivery`.

## Not yet

These stay in arv-next until web push comes (P6b, only if the application becomes installable): **NOTIF-DEVICE-001** (devices and
push subscriptions, VAPID), the **push** parts of NOTIF-DELIVERY-001 (the push TTL and urgency headers, rule 2; push retries on
401/403/429/5xx and the device clean-up, rules 3 and 5 of NOTIF-DEVICE-001; **bundling** of bursts, rule 4; the VAPID key pair, rule 5), the **push** channel of
NOTIF-PREF-001 and the bundling of held pushes of NOTIF-QUIET-001 (rule 4). `notification_policies` (a dealer's enforced settings)
stays in arv-next behind `notification.Enforce`; location recipients (NOTIF-RECIPIENT-002) stay in arv-next.

## Differences from arv-next

- The dedupe reference is the queue's event id (`<event id>:<user>:<category>`); arv-next's was `outbox:<id>`. A
  notification row arv-next already has keeps its key; its table is adopted as it is (migration
  `20261016000030_notifications.sql` creates exactly arv-next's table).
- Retries: the module's default is the queue's 5 attempts; arv-next gave 15 failures. A consumer that wants more says
  `.Retry(event.Attempts(15))`. Dead letters are generic over every consumer (permissions `events.deadletter:view|retry`
  replace `notification.deadletter:*`) and the module never deletes them: arv-next's 90-day deletion of dead letters is the
  application's job if it wants one.
- The "notifies the actor" flag of a category is `.Except(actor)` at the call site.
- E-mail (P6): the tables `notification_deliveries`, `notification_preferences` and `notification_quiet_hours` are arv-next's as
  they are (migrations `20261016000031` and `20261016000032`; the delivery table includes arv-next's retention index
  `notification_deliveries_finished`). The in-app notification stays always on; only e-mail is configurable. A dealer's enforced
  setting is the function given to `notification.Enforce`; `notification_policies` and the dealer screens stay in arv-next.
- The category's priority is not in the module: it only decided the push TTL and urgency (4 hours or 24 hours). An e-mail's TTL is
  always 24 hours, as arv-next's normal priority was; a category that was high priority had 4 hours for e-mail too.
- The e-mail's footer names the place to change the setting ("in your notification settings"), without arv-next's "My profile >
  Notifications" path, which is the application's screen.
- Not in this phase, and so not in these rules: push, devices, bundling, location recipients (see "Not yet").
