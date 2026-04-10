// Package main is a standalone outbox worker binary.
//
// It uses the same bootstrap.Builder as the API server but without HTTP.
// Run as a daemon (PM2/systemd) or as a cron safety net.
//
// Example systemd: ExecStart=/usr/local/bin/go-core-worker
// Example cron:    * * * * * /usr/local/bin/go-core-worker
package main

import (
	"log"

	"go-core-example/internal/domain/product"

	"github.com/wssto2/go-core/bootstrap"
)

func main() {
	cfg := loadConfig()

	app, err := bootstrap.New(cfg).
		DefaultInfrastructure().
		WithOutboxWorker(product.NewWebhookPublisher(
			bootstrap.EnvStr("NOTIFICATION_WEBHOOK_URL", ""),
			bootstrap.EnvStr("NOTIFICATION_WEBHOOK_TOKEN", ""),
			nil, // no in-process bus; image processing runs in the API server
		)).
		Build()
	if err != nil {
		log.Fatal(err)
	}

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
