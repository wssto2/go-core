package gocore

import (
	"errors"
	"fmt"
	"reflect"
	"sync/atomic"

	"github.com/wssto2/go-core/bootstrap"
)

// Deferred holds a value that exists only after the features that need each
// other are all installed. Create one with Later.
type Deferred[T any] struct {
	value atomic.Pointer[T]
	// fromContainer is set by FromContainer: Run keeps the promise from the container, so Check does not ask for it.
	fromContainer bool
}

// unsetter is what Run asks of every Deferred without knowing its type.
type unsetter interface {
	typeName() string
	isSet() bool
	// keptByContainer is true for a FromContainer promise.
	keptByContainer() bool
	// fill keeps a FromContainer promise from the container.
	fill(container *bootstrap.Container) error
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

// FromContainer makes a promise for a T that an old bootstrap.Module (see App.Modules) binds into the container, for a
// feature that already moved to Install while its collaborators are still built by the modules it replaces: the
// repositories, a cached config, a service. Run keeps the promise itself, after every module registered and before the
// routes serve, and refuses to start when no module bound a T, naming the type.
//
//	vehicles := gocore.FromContainer[vehicles.Service](app)
//	pricerule.Install(app, vehicles) // a handler calls vehicles.MustGet() at request time
//
// A Deferred is read at request time, never in Install, because the container does not exist yet. When a
// feature needs the value to build itself, hoist the collaborator out of its module first. It is a bridge for the
// migration: delete it with the module. In a test through Handler there is no container: call Set.
func FromContainer[T any](app *App) *Deferred[T] {
	d := &Deferred[T]{value: atomic.Pointer[T]{}, fromContainer: true}
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

func (d *Deferred[T]) keptByContainer() bool { return d.fromContainer }

var errNotBound = errors.New("no module bound it: Bind it in a module passed to App.Modules, or hoist it")

// fill resolves T from container. A strict container panics on a type nobody bound; that is reported as the error.
func (d *Deferred[T]) fill(container *bootstrap.Container) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("gocore: FromContainer[%s]: %w (%v)", d.typeName(), errNotBound, r)
		}
	}()

	v, err := bootstrap.Resolve[T](container)
	if err != nil {
		return fmt.Errorf("gocore: FromContainer[%s]: %w (%w)", d.typeName(), errNotBound, err)
	}

	d.Set(v)

	return nil
}

func (d *Deferred[T]) unsetError() error {
	return fmt.Errorf("gocore: %s was used before it was set: call Set once every feature that needs it is installed", d.typeName())
}
