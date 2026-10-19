package env_test

import (
	"strings"
	"testing"
	"time"

	"github.com/wssto2/go-core/env"
)

func reader(vars map[string]string) *env.Reader {
	return env.New(func(k string) (string, bool) { v, ok := vars[k]; return v, ok })
}

func TestReaderReadsValuesAndDefaults(t *testing.T) {
	t.Parallel()

	r := reader(map[string]string{"S": "x", "I": "7", "B": "true", "D": "90s", "L": " a, b ,,c ", "EMPTY": ""})

	if r.String("S", "d") != "x" || r.String("NONE", "d") != "d" || r.String("EMPTY", "d") != "d" {
		t.Fatal("String")
	}

	if r.Int("I", 1) != 7 || r.Int("NONE", 1) != 1 {
		t.Fatal("Int")
	}

	if !r.Bool("B", false) || r.Bool("NONE", true) != true {
		t.Fatal("Bool")
	}

	if r.Duration("D", time.Second) != 90*time.Second || r.Duration("NONE", time.Second) != time.Second {
		t.Fatal("Duration")
	}

	if got := strings.Join(r.List("L"), "|"); got != "a|b|c" {
		t.Fatalf("List = %q", got)
	}

	if r.List("NONE") != nil || r.Err() != nil {
		t.Fatalf("unexpected: %v", r.Err())
	}
}

func TestReaderBadValueIsAProblemNamingTheVariable(t *testing.T) {
	t.Parallel()

	r := reader(map[string]string{"APP_PORT": "80a0", "FLAG": "maybe", "WAIT": "soon"})

	if got := r.Int("APP_PORT", 8080); got != 8080 {
		t.Fatalf("a bad number returns the default so the caller goes on, got %d", got)
	}

	r.Bool("FLAG", false)
	r.Duration("WAIT", time.Second)
	r.Required("SECRET")

	problems := r.Problems()
	if len(problems) != 4 {
		t.Fatalf("problems = %v", problems)
	}

	for i, name := range []string{"APP_PORT=80a0", "FLAG=maybe", "WAIT=soon", "SECRET"} {
		if !strings.Contains(problems[i].What, name) {
			t.Errorf("problem %d %q does not name %s", i, problems[i].What, name)
		}

		if problems[i].Fix == "" {
			t.Errorf("problem %d has no fix", i)
		}
	}

	if err := r.Err(); err == nil || !strings.Contains(err.Error(), "APP_PORT=80a0") || !strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("Err = %v", err)
	}
}

func TestRequiredReturnsTheValue(t *testing.T) {
	t.Parallel()

	r := reader(map[string]string{"SECRET": "s3"})
	if r.Required("SECRET") != "s3" || r.Err() != nil {
		t.Fatal("Required")
	}
}
