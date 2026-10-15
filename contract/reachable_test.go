package contract_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/wssto2/go-core/contract"
	"github.com/wssto2/go-core/route"
)

type ReachQuery struct {
	Status string `query:"status" validation:"max:20"`
}

type ReachID struct {
	ID int `path:"id"`
}

type ReachDraft struct {
	Title string `json:"title" validation:"required|max:100"`
}

type ReachOwner struct {
	Name string `json:"name"`
}

type ReachRow struct {
	ID    int        `json:"id"`
	Owner ReachOwner `json:"owner"`
}

type ReachDetail struct {
	ID int `json:"id"`
}

type ReachStats struct {
	Open int `json:"open"`
}

type Mood string

type MoodInput struct {
	Mood Mood `json:"mood" validation:"required|max:8"`
}

type MoodOut struct {
	Mood Mood `json:"mood"`
}

func typeNames(types []reflect.Type) []string {
	out := make([]string, len(types))
	for idx, typ := range types {
		out[idx] = typ.Name()
	}

	return out
}

func TestReachableListsWhatGenerateWrites(t *testing.T) {
	t.Parallel()

	list := route.Get[ReachQuery, []ReachRow]("/rows").Name("reach.list")
	show := route.Get[ReachID, ReachDetail]("/rows/:id").Name("reach.show")
	create := route.Post[ReachDraft, ReachDetail]("/rows").Name("reach.create")
	again := route.Get[ReachQuery, []ReachRow]("/rows/again").Name("reach.again")
	group := route.Group("reach", list, show, create, again).Types(ReachStats{Open: 0})

	got, err := contract.Reachable(group, group)
	if err != nil {
		t.Fatal(err)
	}

	if want := []string{"ReachQuery", "ReachID", "ReachDraft"}; !reflect.DeepEqual(typeNames(got.Inputs), want) {
		t.Errorf("inputs: want %v, got %v", want, typeNames(got.Inputs))
	}

	if want := []string{"ReachRow", "ReachDetail", "ReachStats"}; !reflect.DeepEqual(typeNames(got.Outputs), want) {
		t.Errorf("outputs: want %v, got %v (ReachOwner is written but nested, so not listed)", want, typeNames(got.Outputs))
	}

	// Every listed type is one Generate writes.
	dir := t.TempDir()

	err = contract.Generate(dir, group)
	if err != nil {
		t.Fatal(err)
	}

	written, err := os.OpenRoot(filepath.Join(dir, "reach"))
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = written.Close() }()

	for file, types := range map[string][]reflect.Type{"entities.ts": got.Outputs, "schemas.ts": got.Inputs} {
		src, err := written.ReadFile(file)
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

func TestReachableListsEnumsAndFailsLikeGenerate(t *testing.T) {
	t.Parallel()

	set := route.Post[MoodInput, MoodOut]("/mood").Name("mood.set")
	group := route.Group("mood", set).Types(route.Enum[Mood]("calm"))

	got, err := contract.Reachable(group)
	if err != nil {
		t.Fatal(err)
	}

	if len(got.Enums) != 1 || got.Enums[0].Type() != reflect.TypeFor[Mood]() {
		t.Errorf("enums: got %v", got.Enums)
	}

	if len(got.Inputs) != 1 || len(got.Outputs) != 1 {
		t.Errorf("an enum is neither an input nor an output: %v %v", got.Inputs, got.Outputs)
	}

	bad := route.Group("bad", route.Get[route.None, map[int]string]("/n").Name("bad.n"))

	_, err = contract.Reachable(bad)
	if err == nil {
		t.Error("want the error Generate gives for an output with no TypeScript form")
	}
}
