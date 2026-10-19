// Command tool shows an application command: go run ./cmd/tool hello -name Ana
//
// Besides serving, an App has a command line. migrate and help are built in;
// app.Command adds the application's own, each declaring its flags on the
// *gocore.Cmd it is given. "go run ./cmd/tool help" lists them.
package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"log/slog"

	"github.com/wssto2/go-core/gocore"
)

func main() {
	// A .env is optional here; the real environment wins over it. Load it
	// before the configuration is read.
	if err := gocore.LoadEnv(".env"); err != nil && !errors.Is(err, fs.ErrNotExist) {
		log.Fatal(err)
	}

	app := gocore.New(gocore.WithLogger(slog.Default()))
	app.Command("hello", "greet someone", hello)

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

// hello declares its flags on cmd; "hello -h" prints them and exits 0, a stray argument is an error.
func hello(_ context.Context, cmd *gocore.Cmd) error {
	name := cmd.String("name", "world", "who to greet")
	shout := cmd.Bool("shout", false, "greet loudly")

	if err := cmd.Parse(); err != nil {
		return err
	}

	greeting := "Hello, " + *name
	if *shout {
		greeting += "!"
	}

	_, err := fmt.Fprintln(cmd.Out, greeting)

	return err
}
