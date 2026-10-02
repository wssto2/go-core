package gocore

import (
	"fmt"
	"reflect"
	"sync/atomic"
)

// Deferred holds a value that exists only after the features that need each
// other are all installed. Create one with Later.
type Deferred[T any] struct {
	value atomic.Pointer[T]
}

// unsetter is what Run asks of every Deferred without knowing its type.
type unsetter interface {
	typeName() string
	isSet() bool
}

// Later makes a promise for a value of type T that is kept after two features
// that truly depend on each other are both installed. Run refuses to start
// while it is unset and names it; use before Set fails with the same message.
//
//	reads := gocore.Later[contracts.VehicleReads](app)
//	vehicles := vehicles.Install(app, reads)
//	crm := crm.Install(app, vehicles)
//	reads.Set(crm.VehicleReads)
//
// Later is only for real cycles: every one in main marks a tangle worth
// untangling.
func Later[T any](app *App) *Deferred[T] {
	d := &Deferred[T]{}
	app.laters = append(app.laters, d)

	return d
}

// Set keeps the promise.
func (d *Deferred[T]) Set(value T) {
	d.value.Store(&value)
}

// Get returns the value, or an error naming the promise when Set has not been
// called yet.
func (d *Deferred[T]) Get() (T, error) {
	if v := d.value.Load(); v != nil {
		return *v, nil
	}

	var zero T

	return zero, d.unsetError()
}

// MustGet is Get that panics when the value is not set yet. Use it only where
// a call before start-up is a programming error.
func (d *Deferred[T]) MustGet() T {
	v, err := d.Get()
	if err != nil {
		panic(err)
	}

	return v
}

func (d *Deferred[T]) typeName() string { return reflect.TypeFor[T]().String() }

func (d *Deferred[T]) isSet() bool { return d.value.Load() != nil }

func (d *Deferred[T]) unsetError() error {
	return fmt.Errorf("gocore: %s was used before it was set: call Set once every feature that needs it is installed", d.typeName())
}
