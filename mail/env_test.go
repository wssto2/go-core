//nolint:exhaustruct,exhaustruct_v5,goconst,lll,paralleltest,varnamelen // tests in the style of the package's others
package mail_test

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/mail"
)

func setSMTPEnv(t *testing.T, addr string) {
	t.Helper()

	host, port, err := net.SplitHostPort(addr)
	require.NoError(t, err)

	t.Setenv("SMTP_HOST", host)
	t.Setenv("SMTP_PORT", port)
	t.Setenv("SMTP_USERNAME", "u")
	t.Setenv("SMTP_PASSWORD", "p")
	t.Setenv("SMTP_FROM_ADDRESS", "no-reply@example.com")
	t.Setenv("SMTP_FROM_NAME", "")
	t.Setenv("MAIL_REDIRECT_TO", "")
}

func TestSMTPFromEnvReadsTheVariables(t *testing.T) {
	setSMTPEnv(t, "relay.example.com:25")
	t.Setenv("SMTP_FROM_NAME", "Shop")

	require.Equal(t, mail.SMTPConfig{
		Addr: "relay.example.com:25", Username: "u", Password: "p", From: `"Shop" <no-reply@example.com>`,
	}, mail.SMTPFromEnv("Default"))
}

func TestSMTPFromEnvDefaults(t *testing.T) {
	for _, k := range []string{"SMTP_HOST", "SMTP_PORT", "SMTP_USERNAME", "SMTP_PASSWORD", "SMTP_FROM_ADDRESS", "SMTP_FROM_NAME"} {
		t.Setenv(k, "")
	}

	got := mail.SMTPFromEnv("ARV")

	require.Equal(t, ":587", got.Addr, "port 587 without SMTP_PORT")
	require.Empty(t, got.Username)
	require.Empty(t, got.Password)
	require.Contains(t, got.From, "ARV", "the name falls back to the argument")
}

func TestSenderFromEnvSendsThroughTheRelay(t *testing.T) {
	r := startRelay(t)
	setSMTPEnv(t, r.addr)

	sender, err := mail.SenderFromEnv("App", slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	require.NoError(t, sender.Send(context.Background(), mail.Message{To: []string{"ana@example.com"}, Subject: "s", Text: "t"}))

	got := r.messages()
	require.Len(t, got, 1)
	require.Equal(t, []string{"ana@example.com"}, got[0].to)
}

func TestSenderFromEnvRedirects(t *testing.T) {
	r := startRelay(t)
	setSMTPEnv(t, r.addr)
	t.Setenv("MAIL_REDIRECT_TO", " dev@example.com ")

	var logged bytes.Buffer

	sender, err := mail.SenderFromEnv("App", slog.New(slog.NewTextHandler(&logged, nil)))
	require.NoError(t, err)
	require.Contains(t, logged.String(), "mail redirection is on")
	require.NoError(t, sender.Send(context.Background(), mail.Message{To: []string{"real@example.com"}, Subject: "s", Text: "t"}))

	got := r.messages()
	require.Len(t, got, 1)
	require.Equal(t, []string{"dev@example.com"}, got[0].to)
}

func TestSenderFromEnvNamesABadRedirect(t *testing.T) {
	r := startRelay(t)
	setSMTPEnv(t, r.addr)
	t.Setenv("MAIL_REDIRECT_TO", "not an address")

	sender, err := mail.SenderFromEnv("App", slog.New(slog.DiscardHandler))
	require.ErrorContains(t, err, `MAIL_REDIRECT_TO "not an address" is not an e-mail address`)
	require.Error(t, sender.Send(context.Background(), mail.Message{To: []string{"real@example.com"}, Subject: "s", Text: "t"}))
	require.Empty(t, r.messages(), "the real recipient is never mailed")
}
