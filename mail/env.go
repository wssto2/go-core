package mail

import (
	"fmt"
	"log/slog"
	"net"
	"net/mail"
	"os"
	"strings"
)

// SMTPFromEnv reads the relay every mail goes through from SMTP_HOST, SMTP_PORT (587), SMTP_USERNAME, SMTP_PASSWORD,
// SMTP_FROM_ADDRESS and SMTP_FROM_NAME (defaultFromName when it is not set). The connection starts in the clear and is
// upgraded with STARTTLS when the relay offers it, which is what port 25 and 587 do. Without SMTP_HOST or
// SMTP_FROM_ADDRESS every send fails naming what to set (see SMTP).
func SMTPFromEnv(defaultFromName string) SMTPConfig {
	from := mail.Address{Name: env("SMTP_FROM_NAME", defaultFromName), Address: env("SMTP_FROM_ADDRESS", "")}

	return SMTPConfig{
		Addr:     net.JoinHostPort(env("SMTP_HOST", ""), env("SMTP_PORT", "587")),
		Username: env("SMTP_USERNAME", ""),
		Password: env("SMTP_PASSWORD", ""),
		From:     from.String(),
	}
}

// SenderFromEnv is the one sender of every mail an application sends: the SMTP relay of SMTPFromEnv, or, with
// MAIL_REDIRECT_TO set, the same relay delivering every message to that one address (see Redirect). The redirection is
// for development and staging, whose databases are copies of production with real people's addresses; production
// leaves it unset. A value that is not an e-mail address is an error naming the variable, so start-up stops instead
// of every mail failing later; the sender returned with that error refuses every Send, it never reaches the relay.
func SenderFromEnv(defaultFromName string, logger *slog.Logger) (Sender, error) { //nolint:ireturn // Sender is the port every sender here implements
	next := SMTP(SMTPFromEnv(defaultFromName))

	to := strings.TrimSpace(env("MAIL_REDIRECT_TO", ""))
	if to == "" {
		return next, nil
	}

	if _, err := mail.ParseAddress(to); err != nil {
		return Redirect(next, to), fmt.Errorf("MAIL_REDIRECT_TO %q is not an e-mail address: %w", to, err)
	}

	logger.Warn("mail redirection is on: every mail goes to one address", "MAIL_REDIRECT_TO", to)

	return Redirect(next, to), nil
}

// env is bootstrap.EnvStr; mail sits below bootstrap and cannot import it.
func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}

	return def
}
