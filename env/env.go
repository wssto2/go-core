// Package env reads settings from the environment with explicit calls. A value that is missing when required, or
// malformed, is recorded as a problem that names the variable, and the call returns the default so the caller can go on
// reading: the problems are reported together, once, and nothing starts half-loaded. gocore.App.Env is a Reader on the
// process environment; New gives the same reader over any lookup, for a test.
//
// A variable set to the empty string counts as unset.
package env

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Problem is one setting that cannot be used: what is wrong, and what to do about it.
type Problem struct {
	What string
	Fix  string
}

func (p Problem) String() string { return p.What + ". Fix: " + p.Fix }

// Reader reads variables through a lookup and collects the problems it meets.
type Reader struct {
	lookup   func(string) (string, bool)
	problems []Problem
}

// New returns a Reader over lookup, which has the signature of os.LookupEnv.
func New(lookup func(string) (string, bool)) *Reader { return &Reader{lookup: lookup} }

func (r *Reader) get(name string) (string, bool) {
	v, ok := r.lookup(name)

	return v, ok && v != ""
}

func (r *Reader) bad(name, v, want, fix string) {
	r.problems = append(r.problems, Problem{What: fmt.Sprintf("%s=%s is not %s", name, v, want), Fix: fix})
}

// Required returns the variable, or records a problem when it is not set.
func (r *Reader) Required(name string) string {
	v, ok := r.get(name)
	if !ok {
		r.problems = append(r.problems, Problem{What: name + " is not set", Fix: "set " + name + " in the environment"})
	}

	return v
}

// String returns the variable, or def when it is not set.
func (r *Reader) String(name, def string) string {
	if v, ok := r.get(name); ok {
		return v
	}

	return def
}

// Int returns the variable as a whole number, or def when it is not set. A value that is not a whole number is a problem.
func (r *Reader) Int(name string, def int) int {
	v, ok := r.get(name)
	if !ok {
		return def
	}

	n, err := strconv.Atoi(v)
	if err != nil {
		r.bad(name, v, "a whole number", "write "+name+" as digits, such as "+strconv.Itoa(def))

		return def
	}

	return n
}

// Bool returns the variable as a flag (true, false, 1, 0...), or def when it is not set. Anything else is a problem.
func (r *Reader) Bool(name string, def bool) bool {
	v, ok := r.get(name)
	if !ok {
		return def
	}

	b, err := strconv.ParseBool(v)
	if err != nil {
		r.bad(name, v, "true or false", "write "+name+" as true or false")

		return def
	}

	return b
}

// Duration returns the variable as a duration such as "90s" or "5m", or def when it is not set. Anything else is a problem.
func (r *Reader) Duration(name string, def time.Duration) time.Duration {
	v, ok := r.get(name)
	if !ok {
		return def
	}

	d, err := time.ParseDuration(v)
	if err != nil {
		r.bad(name, v, "a duration", "write "+name+" with a unit, such as 90s or 5m")

		return def
	}

	return d
}

// List returns the variable as a comma-separated list: spaces trimmed, empty items dropped. Unset gives nil.
func (r *Reader) List(name string) []string {
	v, ok := r.get(name)
	if !ok {
		return nil
	}

	var out []string

	for item := range strings.SplitSeq(v, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}

	return out
}

// Problems lists what the calls so far found wrong, in call order.
func (r *Reader) Problems() []Problem { return append([]Problem(nil), r.problems...) }

// Err is the problems as one error, nil when there are none.
func (r *Reader) Err() error {
	errs := make([]error, len(r.problems))
	for i, p := range r.problems {
		errs[i] = errors.New(p.String())
	}

	return errors.Join(errs...)
}
