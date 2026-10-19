package mail

import (
	"fmt"
	"net"
	"net/mail"
	"strings"

	"github.com/wssto2/go-core/gocore"
)

// SMTPFromEnv reads the relay every mail goes through from SMTP_HOST, SMTP_PORT (587), SMTP_USERNAME, SMTP_PASSWORD,
// SMTP_FROM_ADDRESS and SMTP_FROM_NAME (defaultFromName when it is not set). The connection starts in the clear and is
// upgraded with STARTTLS when the relay offers it, which is what port 25 and 587 do. Without SMTP_HOST or
// SMTP_FROM_ADDRESS every send fails naming what to set (see SMTP).
func SMTPFromEnv(app *gocore.App, defaultFromName string) SMTPConfig {
	vars := app.Env()
	from := mail.Address{Name: vars.String("SMTP_FROM_NAME", defaultFromName), Address: vars.String("SMTP_FROM_ADDRESS", "")}

	//nolint:exhaustruct,exhaustruct_v5 // zero by design: STARTTLS when offered, the default timeout
	return SMTPConfig{
		Addr:     net.JoinHostPort(vars.String("SMTP_HOST", ""), vars.String("SMTP_PORT", "587")),
		Username: vars.String("SMTP_USERNAME", ""),
		Password: vars.String("SMTP_PASSWORD", ""),
		From:     from.String(),
	}
}

// SenderFromEnv is the one sender of every mail an application sends: the SMTP relay of SMTPFromEnv, or, with
// MAIL_REDIRECT_TO set, the same relay delivering every message to that one address (see Redirect). The redirection is
// for development and staging, whose databases are copies of production with real people's addresses; production
// leaves it unset. Outside production a configured relay (SMTP_HOST) therefore needs MAIL_REDIRECT_TO or an explicit
// MAIL_LIVE=true, else start-up stops naming both; with no relay configured there is nothing to guard. A value that is not an e-mail address is an error naming the variable, so start-up stops instead
// of every mail failing later; the sender returned with that error refuses every Send, it never reaches the relay.
//
//nolint:ireturn // Sender is the port every sender here implements
func SenderFromEnv(app *gocore.App, defaultFromName string) (Sender, error) {
	next := SMTP(SMTPFromEnv(app, defaultFromName))

	address := strings.TrimSpace(app.Env().String("MAIL_REDIRECT_TO", ""))
	live := app.Env().Bool("MAIL_LIVE", false)

	if address == "" {
		if app.Config().App.Env != gocore.EnvProduction && !live && app.Env().String("SMTP_HOST", "") != "" {
			app.Fail("mail is on in "+envName(app)+" with neither MAIL_REDIRECT_TO nor MAIL_LIVE=true: it would reach real people",
				"set MAIL_REDIRECT_TO to one address that receives every mail, or MAIL_LIVE=true to send for real")
		}

		return next, nil
	}

	_, err := mail.ParseAddress(address)
	if err != nil {
		return Redirect(next, address), fmt.Errorf("MAIL_REDIRECT_TO %q is not an e-mail address: %w", address, err)
	}

	app.Logger().Warn("mail redirection is on: every mail goes to one address", "MAIL_REDIRECT_TO", address)

	return Redirect(next, address), nil
}

func envName(app *gocore.App) string {
	if name := app.Config().App.Env; name != "" {
		return name
	}

	return gocore.EnvDevelopment
}
