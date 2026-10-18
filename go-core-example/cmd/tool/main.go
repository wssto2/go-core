// Command tool shows an application command: go run ./cmd/tool hello -name Ana
//
// Besides serving, an App has a command line. migrate and help are built in;
// app.Command adds the application's own, each parsing its arguments with the
// standard flag package. "go run ./cmd/tool help" lists them.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"log/slog"

	"github.com/wssto2/go-core/bootstrap"
	"github.com/wssto2/go-core/gocore"
)

func main() {
	// A .env is optional here; the real environment wins over it. Load it
	// before the configuration is read.
	if err := gocore.LoadEnv(".env"); err != nil && !errors.Is(err, fs.ErrNotExist) {
		log.Fatal(err)
	}

	app := gocore.New(bootstrap.DefaultConfig(), gocore.WithLogger(slog.Default()))
	app.Command("hello", "greet someone", hello)

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

// hello parses its own flags; "hello -h" prints them and exits 0.
func hello(_ context.Context, _ *gocore.App, args []string, out io.Writer) error {
	flags := flag.NewFlagSet("hello", flag.ContinueOnError)
	flags.SetOutput(out)

	name := flags.String("name", "world", "who to greet")
	shout := flags.Bool("shout", false, "greet loudly")

	if err := flags.Parse(args); err != nil {
		return err
	}

	greeting := "Hello, " + *name
	if *shout {
		greeting += "!"
	}

	_, err := fmt.Fprintln(out, greeting)

	return err
}
