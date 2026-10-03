package route

import (
	"reflect"
	"strings"
)

// PathProblems says where a typed route's path and its input disagree, one
// sentence each, naming the route, the parameter and the fix. Every ":name"
// (or "*name") of the path needs a field tagged path:"name" in the input, and
// every path:"name" field needs its parameter in the path; the generated
// TypeScript cannot check this, so Go does, when the application starts
// (gocore Check) and when contract.Generate runs. A raw route has no input and
// is not checked.
func (s Spec) PathProblems() []string {
	if s.Untyped || (s.In != nil && s.In.Kind() != reflect.Struct) {
		return nil // a non-struct input is reported where it is bound
	}

	inPath := map[string]bool{}

	var params []string

	for _, seg := range strings.Split(s.Path, "/") {
		if len(seg) > 1 && (seg[0] == ':' || seg[0] == '*') {
			inPath[seg[1:]] = true
			params = append(params, seg[1:])
		}
	}

	tagged := map[string]bool{}

	var fields []string

	if s.In != nil && s.In != reflect.TypeFor[None]() {
		fields = pathFields(s.In, tagged)
	}

	var problems []string

	for _, p := range params {
		if tagged[p] {
			continue
		}

		if s.In == nil || s.In == reflect.TypeFor[None]() {
			problems = append(problems, "route "+s.String()+": the path has :"+p+" but the route has no input (route.None): declare an input struct with a field tagged path:\""+p+"\", or fix the path")

			continue
		}

		problems = append(problems, "route "+s.String()+": the path has :"+p+" but "+s.In.Name()+" has no field tagged path:\""+p+"\": add one or fix the path")
	}

	for _, f := range fields {
		if !inPath[f] {
			problems = append(problems, "route "+s.String()+": "+s.In.Name()+" has a field tagged path:\""+f+"\" but the path has no :"+f+": add the parameter to the path or remove the tag")
		}
	}

	return problems
}

// pathFields lists the names of the path:"…" tags of t, through embedded structs.
func pathFields(t reflect.Type, tagged map[string]bool) []string {
	var names []string

	for i := range t.NumField() {
		f := t.Field(i)

		if f.Anonymous && f.Type.Kind() == reflect.Struct {
			names = append(names, pathFields(f.Type, tagged)...)
			continue
		}

		if name, ok := f.Tag.Lookup("path"); ok && name != "" && name != "-" {
			tagged[name] = true
			names = append(names, name)
		}
	}

	return names
}
