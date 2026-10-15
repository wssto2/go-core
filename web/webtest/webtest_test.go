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

// failsOn reports whether check fails the bare testing.T it runs on. A failed Fatalf ends the goroutine with Goexit,
// so the verdict is read in a deferred send.
func failsOn(check func(t *testing.T)) bool {
	verdict := make(chan bool)

	go func() {
		bare := &testing.T{}

		defer func() { verdict <- bare.Failed() }()

		check(bare)
	}()

	return <-verdict
}

func TestAssertGolden(t *testing.T) {
	t.Parallel()

	webtest.AssertGolden(t, shape{ID: 1, Name: "a"}, `{"id":1,"name":"a"}`)
	webtest.AssertGolden(t, shape{ID: 1, Name: ""}, `{"id":1}`)

	if !failsOn(func(t *testing.T) {
		t.Helper()
		webtest.AssertGolden(t, shape{ID: 1, Name: ""}, `{"id":2}`)
	}) {
		t.Error("a different byte must fail")
	}

	if !failsOn(func(t *testing.T) {
		t.Helper()
		webtest.AssertGolden(t, shape{ID: 1, Name: "a"}, `{"name":"a","id":1}`)
	}) {
		t.Error("a reordered field must fail: the comparison is byte for byte")
	}

	if !failsOn(func(t *testing.T) {
		t.Helper()
		webtest.AssertGolden(t, make(chan int), `{}`)
	}) {
		t.Error("an unmarshalable value must fail")
	}
}

func TestAssertSameWire(t *testing.T) {
	t.Parallel()

	webtest.AssertSameWire(t, tagged{ID: 1, Name: "a"}, tagged{ID: 1, Name: "a"})

	if !failsOn(func(t *testing.T) {
		t.Helper()
		webtest.AssertSameWire(t, tagged{ID: 1, Name: ""}, tagged{ID: 2, Name: ""})
	}) {
		t.Error("different bytes must fail")
	}
}

func TestCapture(t *testing.T) {
	t.Parallel()

	if got := webtest.Capture(t, shape{ID: 3, Name: "x"}); got != `{"id":3,"name":"x"}` {
		t.Errorf("captured %s", got)
	}

	if !failsOn(func(t *testing.T) {
		t.Helper()
		webtest.Capture(t, make(chan int))
	}) {
		t.Error("an unmarshalable value must fail")
	}
}
