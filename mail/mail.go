// Package mail sends e-mail: the Sender port, an SMTP sender over the standard
// library, a Sink that records messages for tests and a first run, and a
// Renderer that makes a message's text in the recipient's language.
//
//	sender := mail.SMTP(mail.SMTPConfig{Addr: "smtp.example.com:587", Username: "u", Password: "p", From: "App <no-reply@example.com>"})
//	err := sender.Send(ctx, mail.Message{To: []string{"ana@example.com"}, Subject: "Hello", Text: "Hi Ana"})
//
// A Message with Text only is sent as plain text; with HTML as well it is
// multipart/alternative, text first, so a client that cannot show HTML shows
// the text. Subjects and names are encoded, so non-ASCII text is safe.
//
// In a test, or on a development machine without a relay, use a Sink:
//
//	sink := mail.NewSink()
//	users := identity.Install(app, identity.WithMail(sink))
//	// ... the code a person was sent is in sink.Last().Text
package mail

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
)

// Message is one e-mail. Text is required; HTML is optional.
type Message struct {
	// To is the recipients, plain addresses ("ana@example.com") or with a name
	// ("Ana <ana@example.com>").
	To      []string
	Subject string
	Text    string
	HTML    string
}

// Sender delivers a Message. Implementations are safe for concurrent use.
type Sender interface {
	Send(ctx context.Context, m Message) error
}

// SenderFunc is a Sender made of a function.
type SenderFunc func(ctx context.Context, m Message) error

// Send calls f.
func (f SenderFunc) Send(ctx context.Context, m Message) error { return f(ctx, m) }

// Check says what is wrong with the message, or nil. The senders call it, so
// a bad message never reaches a relay: a recipient that is not an address, a
// line break in the subject (it would add headers), no recipient, no text.
func (m Message) Check() error {
	if len(m.To) == 0 {
		return errors.New("mail: the message has no recipient: set Message.To")
	}

	for _, to := range m.To {
		if _, err := mail.ParseAddress(to); err != nil || strings.ContainsAny(to, "\r\n") {
			return fmt.Errorf("mail: %q is not an e-mail address: use \"ana@example.com\" or \"Ana <ana@example.com>\"", to)
		}
	}

	if strings.ContainsAny(m.Subject, "\r\n") {
		return errors.New("mail: the subject has a line break: a subject is one line")
	}

	if m.Text == "" {
		return errors.New("mail: the message has no Text: every message carries a plain-text body, HTML is the extra")
	}

	return nil
}
