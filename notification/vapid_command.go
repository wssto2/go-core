package notification

import (
	"context"
	"fmt"

	"github.com/wssto2/go-core/gocore"
	"github.com/wssto2/go-core/notification/push"
)

// VapidKeysCommand registers notification:vapid-keys on app: it prints a new key pair as the .env lines
// push.ConfigFromEnv reads. subjectExample is the VAPID_SUBJECT it suggests, such as "mailto:it@example.com": the
// operator's own contact. Generate once per environment and keep the pair; a new one invalidates every device
// subscription. It is the facade's, not the push package's, because only a facade imports gocore.
func VapidKeysCommand(app *gocore.App, subjectExample string) {
	run := func(ctx context.Context, cmd *gocore.Cmd) error {
		err := cmd.Parse()
		if err != nil {
			return fmt.Errorf(vapidName+": %w", err)
		}

		err = ctx.Err()
		if err != nil {
			return fmt.Errorf(vapidName+": %w", err)
		}

		publicKey, privateKey, err := push.GenerateKeys()
		if err != nil {
			return fmt.Errorf("generate VAPID keys: %w", err)
		}

		err = ctx.Err() // the pair is never printed once cancelled
		if err != nil {
			return fmt.Errorf(vapidName+": %w", err)
		}

		_, err = fmt.Fprintf(cmd.Out, "VAPID_PUBLIC_KEY=%s\nVAPID_PRIVATE_KEY=%s\nVAPID_SUBJECT=%s\n",
			publicKey, privateKey, subjectExample)
		if err != nil {
			return fmt.Errorf(vapidName+": %w", err)
		}

		return nil
	}

	app.Command(vapidName, "generate a VAPID key pair for Web Push", run, gocore.Long(vapidHelp))
}

const (
	vapidName = "notification:vapid-keys"
	vapidHelp = "Generate a VAPID key pair for Web Push.\n\n" +
		"Prints a new VAPID key pair as .env lines. Generate once per environment and keep it:\n" +
		"a new pair invalidates every device subscription (NOTIF-DELIVERY-001)."
)
