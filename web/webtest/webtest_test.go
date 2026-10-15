package webtest_test

import (
	"testing"

	"github.com/wssto2/go-core/web/webtest"
)

type shape struct {
	ID   int    `json:"id"`
	Name string `json:"name,omitempty"`
}

type tagged struct {
	ID   int
	Name string `json:"name"`
}

// failsOn reports whether check fails the bare testing.T it runs on (a failed Fatalf ends its goroutine with Goexit).
func failsOn(t *testing.T, check func(t *testing.T)) (failed bool) {
	t.Helper()

	ch := make(chan bool)

	go func() {
		inner := &testing.T{}
		defer func() {
			// FailNow and Fatalf on a bare testing.T end the goroutine with Goexit; recover the verdict from Failed.
			ch <- inner.Failed()
		}()

		check(inner)
	}()

	return <-ch
}

func TestAssertGolden(t *testing.T) {
	t.Parallel()

	webtest.AssertGolden(t, shape{ID: 1, Name: "a"}, `{"id":1,"name":"a"}`)
	webtest.AssertGolden(t, shape{ID: 1}, `{"id":1}`)

	if !failsOn(t, func(it *testing.T) { webtest.AssertGolden(it, shape{ID: 1}, `{"id":2}`) }) {
		t.Error("a different byte must fail")
	}

	if !failsOn(t, func(it *testing.T) { webtest.AssertGolden(it, shape{ID: 1, Name: "a"}, `{"name":"a","id":1}`) }) {
		t.Error("a reordered field must fail: the comparison is byte for byte")
	}

	if !failsOn(t, func(it *testing.T) { webtest.AssertGolden(it, make(chan int), `{}`) }) {
		t.Error("an unmarshalable value must fail")
	}
}

func TestAssertSameWire(t *testing.T) {
	t.Parallel()

	webtest.AssertSameWire(t, tagged{ID: 1, Name: "a"}, tagged{ID: 1, Name: "a"})

	if !failsOn(t, func(it *testing.T) { webtest.AssertSameWire(it, tagged{ID: 1}, tagged{ID: 2}) }) {
		t.Error("different bytes must fail")
	}
}

func TestCapture(t *testing.T) {
	t.Parallel()

	if got := webtest.Capture(t, shape{ID: 3, Name: "x"}); got != `{"id":3,"name":"x"}` {
		t.Errorf("captured %s", got)
	}

	if !failsOn(t, func(it *testing.T) { webtest.Capture(it, make(chan int)) }) {
		t.Error("an unmarshalable value must fail")
	}
}
