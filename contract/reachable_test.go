package contract

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/wssto2/go-core/route"
)

func names(types []reflect.Type) []string {
	out := make([]string, len(types))
	for i, t := range types {
		out[i] = t.Name()
	}

	return out
}

func TestReachableListsWhatGenerateWrites(t *testing.T) {
	fixedVersion(t)

	got, err := Reachable(tickets, tickets)
	if err != nil {
		t.Fatal(err)
	}

	if want := []string{"ListInput", "ShowInput", "Draft"}; !reflect.DeepEqual(names(got.Inputs), want) {
		t.Errorf("inputs: want %v, got %v", want, names(got.Inputs))
	}

	if want := []string{"Row", "Ticket", "Stats"}; !reflect.DeepEqual(names(got.Outputs), want) {
		t.Errorf("outputs: want %v, got %v", want, names(got.Outputs))
	}

	// Every listed type is a type Generate writes (the nested Person is written too, but is not listed).
	dir := t.TempDir()
	if err := Generate(dir, tickets); err != nil {
		t.Fatal(err)
	}

	for file, types := range map[string][]reflect.Type{"entities.ts": got.Outputs, "schemas.ts": got.Inputs} {
		src, err := os.ReadFile(filepath.Join(dir, "tickets", file)) //nolint:gosec // temp dir
		if err != nil {
			t.Fatal(err)
		}

		for _, typ := range types {
			if !strings.Contains(string(src), "export type "+typ.Name()+" ") {
				t.Errorf("%s does not export %s", file, typ.Name())
			}
		}
	}
}

type Mood string

type MoodInput struct {
	Mood Mood `json:"mood" validation:"required|max:8"`
}

type MoodOut struct {
	Mood Mood `json:"mood"`
}

func TestReachableListsEnumsAndFailsLikeGenerate(t *testing.T) {
	set := route.Post[MoodInput, MoodOut]("/priority").Name("mood.set")
	enum := route.Enum[Mood]("calm")
	g := route.Group("mood", set).Types(enum)

	got, err := Reachable(g)
	if err != nil {
		t.Fatal(err)
	}

	if len(got.Enums) != 1 || got.Enums[0].Type() != reflect.TypeFor[Mood]() {
		t.Errorf("enums: got %v", got.Enums)
	}

	if len(got.Inputs) != 1 || len(got.Outputs) != 1 {
		t.Errorf("an enum is not an input or an output: %v %v", got.Inputs, got.Outputs)
	}

	bad := route.Group("bad", route.Get[route.None, map[int]string]("/n").Name("bad.n"))
	if _, err := Reachable(bad); err == nil {
		t.Error("want the error Generate gives for an output with no TypeScript form")
	}
}
