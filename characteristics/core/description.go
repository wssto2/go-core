//nolint:cyclop,exhaustruct,exhaustruct_v5,funlen,varnamelen // the description checks are a flat list of rules
package core

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// Text is a label in the two languages the administration speaks.
type Text struct {
	HR string `json:"hr"`
	EN string `json:"en"`
}

// In returns the label in lang ("en" or anything else for Croatian), falling back to the other language when empty.
func (t Text) In(lang string) string {
	first, second := t.HR, t.EN
	if lang == "en" {
		first, second = second, first
	}

	if first != "" {
		return first
	}

	return second
}

// FieldType is what an extra column holds.
type FieldType string

// The types of an extra column. Text and Choice are strings in the API, Number a JSON number, YesNo a boolean.
const (
	TypeText   FieldType = "text"
	TypeNumber FieldType = "number"
	TypeYesNo  FieldType = "yesno"
	TypeChoice FieldType = "choice"
)

// DefaultMaxText is the longest text a Field takes unless it says otherwise.
const DefaultMaxText = 255

// Choice is one value of a Choice field.
type Choice struct {
	Value string `json:"value"`
	Label Text   `json:"label"`
}

// Under limits a Field to one branch: the rows whose ancestor on Level (1 is the root level) has this ID.
type Under struct {
	Level int `json:"level"`
	ID    int `json:"id"`
}

// Field describes one extra column of a level: what it means and who may change it. A column no Field names is never
// read into the API or written.
type Field struct {
	// Column is the column's name; it is the key of Item.Values.
	Column string
	Label  Text
	Type   FieldType
	// Choices are the allowed values of a TypeChoice field (and only of it).
	Choices []Choice
	// Max is the longest text; zero means DefaultMaxText. Text and choice only.
	Max int
	// Null stores an empty text or choice as NULL instead of an empty string; use it for a nullable column. A number
	// that is absent is NULL if Null, else 0.
	Null bool
	// InList shows the value in the screen's list.
	InList bool
	// ReadOnly shows the value but refuses a change through the API (an id owned by another system).
	ReadOnly bool
	// Under, when set, limits the field to one branch of the tree; nil is the whole level.
	Under *Under
}

// Translations says where a level keeps the name in other languages: a JSON object column ({"hr": "...", "en": "..."})
// or one column per language. Both empty means the level has none.
type Translations struct {
	JSON    string
	Columns map[string]string
}

func (t Translations) any() bool { return t.JSON != "" || len(t.Columns) > 0 }

// Audit names the columns the legacy applications fill and how they fill them. Every column is optional ("" is not
// kept). Writes: the creator and time on create; the editor and time on every update (and on create where the flag
// says so); the activator and time when a row is created active or switched on; the deactivator and time when it is
// switched off. Without an actor the "by" columns are left to the database.
type Audit struct {
	CreatedBy, UpdatedBy, ActivatedBy, DeactivatedBy string
	CreatedAt, UpdatedAt, ActivatedAt, DeactivatedAt string
	UpdatedByOnCreate, UpdatedAtOnCreate             bool
}

// Level is one table of the tree.
type Level struct {
	// Label names the level for the screen ("Marke").
	Label Text
	Table string
	// ID is the primary key column, "id" when empty.
	ID string
	// Parent is the column holding the parent row's id; empty at the root level.
	Parent string
	// Ancestors are the columns holding the ids of the ancestors above the parent, root first (level 3 of
	// site-builder keeps level1_id as well as level2_id). Empty, or exactly all of them; written on create.
	Ancestors []string
	// Name is the name column (the default language), at most NameMax characters (255 when zero).
	Name         string
	NameMax      int
	Translations Translations
	// Order is the sort column, 1-based among the siblings; empty means no manual order (sorted by name).
	Order string
	// Active is the on/off column (0/1, tinyint or enum('0','1')); empty means every row is active and cannot be
	// switched.
	Active string
	// Meta is a JSON object column.
	Meta   string
	Audit  Audit
	Fields []Field
}

// Tree is a project's description of its characteristics tables: the levels, root first.
type Tree struct {
	// Languages are the languages of the names, the first being the default of labels. Empty means hr and en.
	Languages []string
	Levels    []Level
}

// DescriptionError lists everything wrong with a Tree.
type DescriptionError struct{ Problems []string }

func (e *DescriptionError) Error() string {
	return "characteristics description is invalid: " + strings.Join(e.Problems, "; ")
}

// aboveParent is how many levels a level number is from the first ancestor: the parent and the level itself.
const aboveParent = 2

var ident = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Prepare (CHARACTERISTICS-DESC-001) checks the description and returns a copy with the defaults filled in (id,
// languages, text length). It
// returns a *DescriptionError naming every problem.
func (t Tree) Prepare() (Tree, error) {
	out := Tree{Languages: slices.Clone(t.Languages), Levels: make([]Level, len(t.Levels))}
	if len(out.Languages) == 0 {
		out.Languages = []string{"hr", "en"}
	}

	var problems []string

	for _, lang := range out.Languages {
		if !ident.MatchString(lang) {
			problems = append(problems, fmt.Sprintf("language %q is not a language code", lang))
		}
	}

	if len(t.Levels) == 0 {
		problems = append(problems, "there are no levels")
	}

	tables := map[string]bool{}

	for i, level := range t.Levels {
		level.Ancestors = slices.Clone(level.Ancestors)
		level.Fields = slices.Clone(level.Fields)

		if level.ID == "" {
			level.ID = "id"
		}

		if level.NameMax == 0 {
			level.NameMax = DefaultMaxText
		}

		for j := range level.Fields {
			if level.Fields[j].Max == 0 {
				level.Fields[j].Max = DefaultMaxText
			}
		}

		out.Levels[i] = level

		if tables[level.Table] {
			problems = append(problems, fmt.Sprintf("level %d: table %q is used by another level", i+1, level.Table))
		}

		tables[level.Table] = true
		problems = append(problems, level.problems(i+1, out.Languages)...)
	}

	if len(problems) > 0 {
		return Tree{}, &DescriptionError{Problems: problems}
	}

	return out, nil
}

func (f Field) problems(level int) []string {
	var out []string

	if !ident.MatchString(f.Column) {
		out = append(out, fmt.Sprintf("%q is not a column name", f.Column))
	}

	if f.Label.HR == "" {
		out = append(out, "the label (hr) is missing")
	}

	switch f.Type {
	case TypeText, TypeNumber, TypeYesNo:
		if len(f.Choices) > 0 {
			out = append(out, "only a choice field has choices")
		}
	case TypeChoice:
		out = append(out, f.choiceProblems()...)
	default:
		out = append(out, fmt.Sprintf("type %q is not text, number, yesno or choice", f.Type))
	}

	if f.Max < 0 {
		out = append(out, "max is negative")
	}

	if f.Under != nil && (f.Under.Level < 1 || f.Under.Level >= level || f.Under.ID < 1) {
		out = append(out, fmt.Sprintf("under must name a row (id above 0) of a level above this one (1 to %d)", level-1))
	}

	return out
}

func (f Field) choiceProblems() []string {
	if len(f.Choices) == 0 {
		return []string{"a choice field needs choices"}
	}

	var out []string

	values := map[string]bool{}

	for _, c := range f.Choices {
		if c.Label.HR == "" {
			out = append(out, fmt.Sprintf("choice %q has no label (hr)", c.Value))
		}

		if values[c.Value] {
			out = append(out, fmt.Sprintf("choice %q is listed twice", c.Value))
		}

		values[c.Value] = true
	}

	return out
}

// Level returns the level with this number (1 is the root).
func (t Tree) Level(number int) (Level, bool) {
	if number < 1 || number > len(t.Levels) {
		return Level{}, false
	}

	return t.Levels[number-1], true
}

// FieldsFor returns the fields that apply to a row of this level whose ancestors, root first, are path.
func (l Level) FieldsFor(path []int) []Field {
	var out []Field

	for _, f := range l.Fields {
		if f.Under == nil || (f.Under.Level <= len(path) && path[f.Under.Level-1] == f.Under.ID) {
			out = append(out, f)
		}
	}

	return out
}

func (l Level) problems(number int, languages []string) []string {
	var out []string

	bad := func(format string, args ...any) {
		out = append(out, fmt.Sprintf("level %d (%s): %s", number, l.Table, fmt.Sprintf(format, args...)))
	}

	seen := map[string]string{}
	use := func(column, role string) {
		if column == "" {
			return
		}

		if !ident.MatchString(column) {
			bad("%s %q is not a column name", role, column)

			return
		}

		if other, dup := seen[column]; dup {
			bad("column %q is both %s and %s", column, other, role)

			return
		}

		seen[column] = role
	}

	if !ident.MatchString(l.Table) {
		bad("table %q is not a table name", l.Table)
	}

	if l.Label.HR == "" {
		bad("the label (hr) is missing")
	}

	use(l.ID, "the id")
	use(l.Name, "the name")

	if l.Name == "" {
		bad("the name column is missing")
	}

	l.parentProblems(number, bad, use)
	use(l.Order, "the order")
	use(l.Active, "the active flag")
	use(l.Meta, "the meta")

	a := l.Audit
	for role, column := range map[string]string{
		"created by": a.CreatedBy, "updated by": a.UpdatedBy, "activated by": a.ActivatedBy,
		"deactivated by": a.DeactivatedBy, "created at": a.CreatedAt, "updated at": a.UpdatedAt,
		"activated at": a.ActivatedAt, "deactivated at": a.DeactivatedAt,
	} {
		use(column, role)
	}

	if (a.ActivatedBy != "" || a.ActivatedAt != "" || a.DeactivatedBy != "" || a.DeactivatedAt != "") && l.Active == "" {
		bad("activation columns are named but the level has no active column")
	}

	l.translationProblems(bad, use, languages)

	for i, f := range l.Fields {
		problems := f.problems(number)
		for _, p := range problems {
			bad("field %d (%s): %s", i+1, f.Column, p)
		}

		if !slices.ContainsFunc(l.Fields[:i], func(o Field) bool { return o.Column == f.Column }) {
			use(f.Column, "an extra column")
		}
	}

	out = append(out, l.sameColumnProblems(number)...)

	return out
}

func (l Level) parentProblems(number int, bad func(string, ...any), use func(string, string)) {
	switch {
	case number == 1 && (l.Parent != "" || len(l.Ancestors) > 0):
		bad("the root level has no parent column")
	case number > 1 && l.Parent == "":
		bad("the parent column is missing")
	case len(l.Ancestors) != 0 && len(l.Ancestors) != number-aboveParent:
		bad("ancestors must name all %d levels above the parent, or none", number-aboveParent)
	}

	use(l.Parent, "the parent")

	for _, column := range l.Ancestors {
		use(column, "an ancestor")
	}
}

func (l Level) translationProblems(bad func(string, ...any), use func(string, string), languages []string) {
	tr := l.Translations
	if tr.JSON != "" && len(tr.Columns) > 0 {
		bad("translations are a JSON column or one column per language, not both")
	}

	use(tr.JSON, "the translations")

	for lang, column := range tr.Columns {
		if !slices.Contains(languages, lang) {
			bad("translation column %q is for %q, which is not among the languages", column, lang)
		}

		use(column, "the "+lang+" translation")
	}
}

// sameColumnProblems checks that two fields on one column are on different branches, never one of them whole-level.
func (l Level) sameColumnProblems(number int) []string {
	var out []string

	for i, f := range l.Fields {
		for _, o := range l.Fields[:i] {
			if o.Column != f.Column {
				continue
			}

			if o.Under == nil || f.Under == nil || *o.Under == *f.Under {
				out = append(out,
					fmt.Sprintf("level %d (%s): column %q is described twice for the same rows", number, l.Table, f.Column))
			}
		}
	}

	return out
}
