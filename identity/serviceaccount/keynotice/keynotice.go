// Package keynotice tells the owner of a service account, in the app's notifications, about a key that expires
// soon or that nobody uses. identity.WithServiceAccounts runs the daily check (serviceaccount.Notices) and queues
// one event per key and condition; this package is the consumer that turns the event into the owner's notification,
// in the owner's language:
//
//	notices := notification.Install(app, users, keynotice.Expiring, keynotice.Unused, notification.AppURL(url))
//	keynotice.Install(app, notices, nil) // English words; pass your own function for your languages
//
// It is a package of its own because notification imports identity: identity cannot import notification, so the
// pieces that need both live here. Until Install is called the daily run sends nothing.
package keynotice

import (
	"context"
	"fmt"
	"strconv"

	"github.com/wssto2/go-core/event"
	"github.com/wssto2/go-core/gocore"
	"github.com/wssto2/go-core/identity/serviceaccount"
	"github.com/wssto2/go-core/notification"
)

// The categories of the notices; register them with notification.Install. A person chooses their e-mail and
// push for each. The codes are the ones arv-next used.
//
//nolint:gochecknoglobals // a category is declared once, as a value
var (
	// Expiring: a key of an account the person answers for expires within 14 days. Its push is urgent.
	Expiring = notification.Category("iam.serviceaccount.keyexpiring").PushByDefault().HighPriority()
	// Unused: a key of an account the person answers for has not been used for 60 days.
	Unused = notification.Category("iam.serviceaccount.keyunused").PushByDefault()
)

// Text words the notice for one recipient, in the language they use (Recipient.Locale), like
// notification.TestMessage. The Link, when empty, is the account's page, "/iam/service-accounts/<id>".
type Text func(recipient notification.Recipient, notice serviceaccount.KeyNotice) notification.Message

// Install makes the owner's notification of every notice the daily check queues. text is nil for the English
// default, or yours.
func Install(app *gocore.App, notices *notification.Notices, text Text) {
	if text == nil {
		text = English
	}

	app.Events(consumer(notices, text))
}

func consumer(notices *notification.Notices, text Text) event.Consumer {
	handle := func(ctx context.Context, notice serviceaccount.KeyNotice) error {
		kind := Unused
		if notice.Kind == serviceaccount.NoticeExpiring {
			kind = Expiring
		}

		// Nobody is the actor of a notice about a key: the owner is told whoever they are.
		recipient := notification.To(notice.OwnerID)

		return notices.Send(ctx, kind, recipient, func(person notification.Recipient) notification.Message {
			msg := text(person, notice)
			if msg.Link == "" {
				msg.Link = "/iam/service-accounts/" + strconv.Itoa(notice.AccountID)
			}

			if msg.Data == nil {
				msg.Data = map[string]string{
					"account_id": strconv.Itoa(notice.AccountID), "key_id": strconv.Itoa(notice.KeyID),
				}
			}

			return msg
		})
	}

	return serviceaccount.KeyNoticed.To(serviceaccount.NoticeConsumer, handle)
}

// English is the default Text.
func English(_ notification.Recipient, notice serviceaccount.KeyNotice) notification.Message {
	if notice.Kind == serviceaccount.NoticeExpiring {
		return notification.Message{
			Title: "An API key is about to expire",
			Body: fmt.Sprintf("Key “%s” of “%s” expires on %s (in %d days). Add a new key and revoke this one.",
				notice.Key, notice.Account, notice.Date, notice.Days),
			Link: "", Data: nil,
		}
	}

	return notification.Message{
		Title: "An API key has not been used",
		Body: fmt.Sprintf("Key “%s” of “%s” has not been used for %d days (since %s). Revoke it if it is no longer needed.",
			notice.Key, notice.Account, notice.Days, notice.Date),
		Link: "", Data: nil,
	}
}
