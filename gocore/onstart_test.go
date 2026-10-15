//nolint:testpackage,paralleltest // uses the package's runnable helper; serving sets gin's global mode
package gocore

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

var errNoConfigRow = errors.New("no config row")

func TestOnStartRunsBeforeServingAndItsErrorStopsStartUp(t *testing.T) {
	app, _ := runnable(t)

	ran := false

	app.OnStart(func(context.Context) error {
		ran = true

		return errNoConfigRow
	})

	// Without the check the App would serve; the timeout keeps a broken check from hanging the test.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := app.RunContext(ctx)

	var se *StartupError
	if !errors.As(err, &se) || len(se.Problems) != 1 || !strings.Contains(se.Problems[0].What, "no config row") {
		t.Fatalf("want one start-up problem naming the error, got %v", err)
	}

	if !ran {
		t.Fatal("the start-up check did not run")
	}
}

func TestOnStartPassesAndServingGoesOn(t *testing.T) {
	app, _ := runnable(t)

	ran := make(chan struct{}, 1)

	app.OnStart(func(context.Context) error {
		ran <- struct{}{}

		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan error, 1)

	go func() { done <- app.RunContext(ctx) }()

	select {
	case <-ran:
	case <-ctx.Done():
		t.Fatal("the start-up check did not run")
	}

	cancel()

	err := <-done
	if err != nil {
		t.Fatalf("serving stopped with %v", err)
	}
}

func TestCheckDoesNotRunOnStart(t *testing.T) {
	app, _ := runnable(t)

	app.OnStart(func(context.Context) error {
		t.Error("Check ran an OnStart check")

		return nil
	})

	err := app.Check()
	if err != nil {
		t.Fatal(err)
	}
}
