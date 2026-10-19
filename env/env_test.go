package env_test

import (
	"strings"
	"testing"
	"time"

	"github.com/wssto2/go-core/env"
)

func reader(vars map[string]string) *env.Reader {
	return env.New(func(key string) (string, bool) {
		value, ok := vars[key]

		return value, ok
	})
}

func TestReaderReadsScalarsAndDefaults(t *testing.T) {
	t.Parallel()

	rdr := reader(map[string]string{"S": "x", "I": "7", "B": "true", "D": "90s", "EMPTY": ""})

	if rdr.String("S", "d") != "x" || rdr.String("NONE", "d") != "d" || rdr.String("EMPTY", "d") != "d" {
		t.Fatal("String")
	}

	if rdr.Int("I", 1) != 7 || rdr.Int("NONE", 1) != 1 {
		t.Fatal("Int")
	}

	if !rdr.Bool("B", false) || !rdr.Bool("NONE", true) {
		t.Fatal("Bool")
	}

	if rdr.Err() != nil {
		t.Fatalf("unexpected: %v", rdr.Err())
	}
}

func TestReaderReadsDurations(t *testing.T) {
	t.Parallel()

	rdr := reader(map[string]string{"D": "90s"})

	if rdr.Duration("D", time.Second) != 90*time.Second || rdr.Duration("NONE", time.Second) != time.Second {
		t.Fatal("Duration")
	}
}

func TestReaderListTrimsAndDropsEmptyItems(t *testing.T) {
	t.Parallel()

	rdr := reader(map[string]string{"L": " a, b ,,c "})

	if got := strings.Join(rdr.List("L"), "|"); got != "a|b|c" {
		t.Fatalf("List = %q", got)
	}

	if rdr.List("NONE") != nil {
		t.Fatal("an unset list is nil")
	}
}

func TestReaderBadValueIsAProblemNamingTheVariable(t *testing.T) {
	t.Parallel()

	rdr := reader(map[string]string{"APP_PORT": "80a0", "FLAG": "maybe", "WAIT": "soon"})

	if got := rdr.Int("APP_PORT", 8080); got != 8080 {
		t.Fatalf("a bad number returns the default so the caller goes on, got %d", got)
	}

	rdr.Bool("FLAG", false)
	rdr.Duration("WAIT", time.Second)
	rdr.Required("SECRET")

	problems := rdr.Problems()
	if len(problems) != 4 {
		t.Fatalf("problems = %v", problems)
	}

	for idx, name := range []string{"APP_PORT=80a0", "FLAG=maybe", "WAIT=soon", "SECRET"} {
		if !strings.Contains(problems[idx].What, name) {
			t.Errorf("problem %d %q does not name %s", idx, problems[idx].What, name)
		}

		if problems[idx].Fix == "" {
			t.Errorf("problem %d has no fix", idx)
		}
	}

	err := rdr.Err()
	if err == nil || !strings.Contains(err.Error(), "APP_PORT=80a0") || !strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("Err = %v", err)
	}
}

func TestRequiredReturnsTheValue(t *testing.T) {
	t.Parallel()

	rdr := reader(map[string]string{"SECRET": "s3"})
	if rdr.Required("SECRET") != "s3" || rdr.Err() != nil {
		t.Fatal("Required")
	}
}
