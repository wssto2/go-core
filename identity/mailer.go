package identity

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/wssto2/go-core/identity/account"
	"github.com/wssto2/go-core/identity/mailtext"
	"github.com/wssto2/go-core/mail"
)

// mailer is what identity sends over a mail.Sender: the code of an e-mail change,
// and the notices of a changed password and a changed address.
type mailer struct {
	sender   mail.Sender
	render   mail.Renderer
	accounts account.Store
	clock    account.Clock
	log      *slog.Logger
}

// SendCode implements account.CodeSender. A failed mail is an error: the person cannot go on without the code.
func (m mailer) SendCode(ctx context.Context, c account.CodeMessage) error {
	minutes := max(int((c.ExpiresAt.Sub(m.clock.Now())+time.Minute-1)/time.Minute), 1)

	return m.send(ctx, c.Locale, mailtext.EmailCode, mailtext.CodeData{Name: c.Name, Code: c.Code, Minutes: minutes}, c.Recipient)
}

func (m mailer) send(ctx context.Context, locale string, name mail.Name, data any, to string) error {
	content, err := m.render.Render(ctx, locale, name, data)
	if err != nil {
		return err
	}

	return m.sender.Send(ctx, content.To(to))
}

// notices mails the person about changes to their own account, then passes every
// fact on to the application's Notices. Mailing is best effort: the change is done, so a
// mail that fails is logged, not returned.
type notices struct {
	account.Notices
	m mailer
}

func (n notices) PasswordChanged(ctx context.Context, accountID, actorID int) {
	n.Notices.PasswordChanged(ctx, accountID, actorID)

	acc, err := n.m.accounts.Find(ctx, accountID)
	if err != nil || acc.Email == "" {
		return
	}

	n.m.best(ctx, "password changed", n.m.send(ctx, acc.Locale, mailtext.PasswordChanged, mailtext.PasswordChangedData{Name: acc.Name}, acc.Email))
}

func (n notices) EmailChanged(ctx context.Context, accountID int, oldEmail, newEmail string) {
	n.Notices.EmailChanged(ctx, accountID, oldEmail, newEmail)

	if oldEmail == "" {
		return
	}

	acc, err := n.m.accounts.Find(ctx, accountID)
	if err != nil {
		return
	}

	n.m.best(ctx, "e-mail changed", n.m.send(ctx, acc.Locale, mailtext.EmailChanged, mailtext.EmailChangedData{Name: acc.Name, NewEmail: maskEmail(newEmail)}, oldEmail))
}

func (m mailer) best(ctx context.Context, what string, err error) {
	if err != nil {
		m.log.WarnContext(ctx, "identity: the notice mail could not be sent", "notice", what, "error", err)
	}
}

// maskEmail keeps the first letter of the local part and the domain: a***@example.com.
func maskEmail(email string) string {
	local, domain, ok := strings.Cut(email, "@")
	if !ok || local == "" {
		return email
	}

	return string([]rune(local)[:1]) + "***@" + domain
}
