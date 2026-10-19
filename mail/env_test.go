//nolint:exhaustruct,exhaustruct_v5,goconst,lll,paralleltest,varnamelen // tests in the style of the package's others
package mail_test

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/gocore"
	"github.com/wssto2/go-core/mail"
)

func newApp(opts ...gocore.Option) *gocore.App {
	return gocore.New(append([]gocore.Option{gocore.WithConfig(gocore.DefaultConfig()), gocore.WithLogger(slog.New(slog.DiscardHandler))}, opts...)...)
}

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
	t.Setenv("MAIL_LIVE", "")
	t.Setenv("APP_ENV", "")
}

func TestSMTPFromEnvReadsTheVariables(t *testing.T) {
	setSMTPEnv(t, "relay.example.com:25")
	t.Setenv("SMTP_FROM_NAME", "Shop")

	require.Equal(t, mail.SMTPConfig{
		Addr: "relay.example.com:25", Username: "u", Password: "p", From: `"Shop" <no-reply@example.com>`,
	}, mail.SMTPFromEnv(newApp(), "Default"))
}

func TestSMTPFromEnvDefaults(t *testing.T) {
	for _, k := range []string{"SMTP_HOST", "SMTP_PORT", "SMTP_USERNAME", "SMTP_PASSWORD", "SMTP_FROM_ADDRESS", "SMTP_FROM_NAME"} {
		t.Setenv(k, "")
	}

	got := mail.SMTPFromEnv(newApp(), "ARV")

	require.Equal(t, ":587", got.Addr, "port 587 without SMTP_PORT")
	require.Empty(t, got.Username)
	require.Empty(t, got.Password)
	require.Contains(t, got.From, "ARV", "the name falls back to the argument")
}

func TestSenderFromEnvSendsThroughTheRelay(t *testing.T) {
	r := startRelay(t)
	setSMTPEnv(t, r.addr)

	sender, err := mail.SenderFromEnv(newApp(), "App")
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

	sender, err := mail.SenderFromEnv(newApp(gocore.WithLogger(slog.New(slog.NewTextHandler(&logged, nil)))), "App")
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

	sender, err := mail.SenderFromEnv(newApp(), "App")
	require.ErrorContains(t, err, `MAIL_REDIRECT_TO "not an address" is not an e-mail address`)
	require.Error(t, sender.Send(context.Background(), mail.Message{To: []string{"real@example.com"}, Subject: "s", Text: "t"}))
	require.Empty(t, r.messages(), "the real recipient is never mailed")
}

func mailApp(t *testing.T, appEnv string) *gocore.App {
	t.Helper()

	setSMTPEnv(t, "relay.example.com:25")

	cfg := gocore.DefaultConfig()
	cfg.App.Env = appEnv

	return gocore.New(gocore.WithConfig(cfg), gocore.WithLogger(slog.New(slog.DiscardHandler)))
}

func TestMailOutsideProductionNeedsARedirectOrMAILLIVE(t *testing.T) {
	for _, appEnv := range []string{"staging", "development", ""} {
		app := mailApp(t, appEnv)

		_, err := mail.SenderFromEnv(app, "App")
		require.NoError(t, err)

		problem := app.Check()
		require.ErrorContains(t, problem, "MAIL_REDIRECT_TO", appEnv)
		require.ErrorContains(t, problem, "MAIL_LIVE=true", appEnv)
	}
}

func TestMailOutsideProductionPassesWithARedirectOrMAILLIVE(t *testing.T) {
	app := mailApp(t, "staging")

	t.Setenv("MAIL_REDIRECT_TO", "dev@example.com")

	_, err := mail.SenderFromEnv(app, "App")
	require.NoError(t, err)
	require.NoError(t, app.Check())

	app = mailApp(t, "staging")

	t.Setenv("MAIL_LIVE", "true")

	_, err = mail.SenderFromEnv(app, "App")
	require.NoError(t, err)
	require.NoError(t, app.Check())
}

func TestMailInProductionNeedsNeither(t *testing.T) {
	app := mailApp(t, "production")

	_, err := mail.SenderFromEnv(app, "App")
	require.NoError(t, err)
	require.NoError(t, app.Check())
}

func TestMailThatIsNotConfiguredIsNotAProblem(t *testing.T) {
	app := mailApp(t, "staging")

	t.Setenv("SMTP_HOST", "")

	_, err := mail.SenderFromEnv(app, "App")
	require.NoError(t, err)
	require.NoError(t, app.Check())
}
