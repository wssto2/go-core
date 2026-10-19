// Package env reads settings from the environment with explicit calls. A value that is missing when required, or
// malformed, is recorded as a problem that names the variable, and the call returns the default so the caller can go on
// reading: the problems are reported together, once, and nothing starts half-loaded. gocore.App.Env is a Reader on the
// process environment; New gives the same reader over any lookup, for a test.
//
// A variable set to the empty string counts as unset.
package env

import (
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
func New(lookup func(string) (string, bool)) *Reader { return &Reader{lookup: lookup, problems: nil} }

// Required returns the variable, or records a problem when it is not set.
func (r *Reader) Required(name string) string {
	raw, ok := r.get(name)
	if !ok {
		r.problems = append(r.problems, Problem{What: name + " is not set", Fix: "set " + name + " in the environment"})
	}

	return raw
}

// String returns the variable, or def when it is not set.
func (r *Reader) String(name, def string) string {
	if raw, ok := r.get(name); ok {
		return raw
	}

	return def
}

// Int returns the variable as a whole number, or def when it is not set. A value that is not one is a problem.
func (r *Reader) Int(name string, def int) int {
	raw, ok := r.get(name)
	if !ok {
		return def
	}

	number, err := strconv.Atoi(raw)
	if err != nil {
		r.bad(name, raw, "a whole number", "write "+name+" as digits, such as "+strconv.Itoa(def))

		return def
	}

	return number
}

// Bool returns the variable as a flag (true, false, 1, 0...), or def when it is not set. Anything else is a problem.
func (r *Reader) Bool(name string, def bool) bool {
	raw, ok := r.get(name)
	if !ok {
		return def
	}

	flag, err := strconv.ParseBool(raw)
	if err != nil {
		r.bad(name, raw, "true or false", "write "+name+" as true or false")

		return def
	}

	return flag
}

// Duration returns the variable as a duration such as "90s" or "5m", or def when it is not set. Anything else
// is a problem.
func (r *Reader) Duration(name string, def time.Duration) time.Duration {
	raw, ok := r.get(name)
	if !ok {
		return def
	}

	span, err := time.ParseDuration(raw)
	if err != nil {
		r.bad(name, raw, "a duration", "write "+name+" with a unit, such as 90s or 5m")

		return def
	}

	return span
}

// List returns the variable as a comma-separated list: spaces trimmed, empty items dropped. Unset gives nil.
func (r *Reader) List(name string) []string {
	raw, ok := r.get(name)
	if !ok {
		return nil
	}

	var out []string

	for item := range strings.SplitSeq(raw, ",") {
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
	if len(r.problems) == 0 {
		return nil
	}

	return &Error{Problems: r.Problems()}
}

// Error is what Err returns: every problem found, one per line.
type Error struct{ Problems []Problem }

func (e *Error) Error() string {
	lines := make([]string, len(e.Problems))
	for i, p := range e.Problems {
		lines[i] = p.String()
	}

	return strings.Join(lines, "\n")
}

func (r *Reader) get(name string) (string, bool) {
	value, ok := r.lookup(name)

	return value, ok && value != ""
}

func (r *Reader) bad(name, value, want, fix string) {
	r.problems = append(r.problems, Problem{What: fmt.Sprintf("%s=%s is not %s", name, value, want), Fix: fix})
}
