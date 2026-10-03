// Package mailtext is the text of the e-mail a notification is sent as, in English, Croatian, Bosnian and
// Slovenian: the defaults of notification, the same kind of files identity/mailtext holds for identity's mails. An
// application that writes its own copy, with its own look, gives identity.WithMailContent a mail.Renderer that knows
// Email; notification asks that renderer first and falls back to these.
//
//	notification.email   EmailData   a notification, by e-mail
//
// The subject is the notification's title; the body is the notification's body, a link that opens it, and a footer
// that says why the person gets the mail and where to change it. The language is the person's, then the language
// of a locale such as "hr-HR", then English.
package mailtext

import (
	"embed"

	"github.com/wssto2/go-core/mail"
)

// Email is the mail a notification is sent as. Data: EmailData.
const Email mail.Name = "notification.email"

// EmailData is the data of Email.
type EmailData struct {
	// Name is the person's name, or their login when they have none.
	Name string
	// Title is the notification's title: one line, the mail's subject.
	Title string
	// Body is the notification's body, empty when it has none.
	Body string
	// OpenURL is the absolute address that opens the notification in the application, empty when it opens nothing.
	OpenURL string
}

//go:embed en hr bs sl
var files embed.FS

// Defaults renders Email in the languages of this package.
var Defaults = mail.Templates(files)
