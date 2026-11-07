//nolint:cyclop,varnamelen // validation is a flat list of rules
package core

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/wssto2/go-core/apperr"
)

// Input is what a person writes of an item.
type Input struct {
	Name string
	// Translations by language; nil keeps them on update, and an empty text clears a language.
	Translations map[string]string
	// Meta replaces the meta object; nil keeps it on update.
	Meta map[string]any
	// Values are the extra columns to set, by column; the other columns keep their value.
	Values map[string]any
	// Active is whether a new item starts switched on; ignored on update (SetActive switches).
	Active bool
}

// fill checks the input against the level and puts what is to be written into item. current is the stored row on an
// update, nil on a create.
func (s *Service) fill(def Level, item *Item, in Input, current *Item) error {
	name := strings.TrimSpace(in.Name)

	switch {
	case name == "":
		return apperr.BadRequest("name is required")
	case utf8.RuneCountInString(name) > def.NameMax:
		return apperr.BadRequest(fmt.Sprintf("name is longer than %d characters", def.NameMax))
	}

	item.Name = name

	if in.Translations != nil {
		translations, err := s.translations(def, in.Translations)
		if err != nil {
			return err
		}

		item.Translations = translations
	}

	if in.Meta != nil {
		if def.Meta == "" {
			return apperr.BadRequest("this level has no meta column")
		}

		item.Meta = in.Meta
	}

	values, err := fieldValues(def.FieldsFor(item.Path), in.Values, current)
	if err != nil {
		return err
	}

	item.Values = values

	return nil
}

func (s *Service) translations(def Level, given map[string]string) (map[string]string, error) {
	if len(given) > 0 && !def.Translations.any() {
		return nil, apperr.BadRequest("this level has no translations")
	}

	out := make(map[string]string, len(given))

	for lang, text := range given {
		if !slices.Contains(s.tree.Languages, lang) {
			return nil, apperr.BadRequest(
				fmt.Sprintf("language %q is not one of %s", lang, strings.Join(s.tree.Languages, ", ")))
		}

		text = strings.TrimSpace(text)
		if utf8.RuneCountInString(text) > def.NameMax {
			return nil, apperr.BadRequest(fmt.Sprintf("the %s name is longer than %d characters", lang, def.NameMax))
		}

		out[lang] = text
	}

	return out, nil
}

// fieldValues (CHARACTERISTICS-DESC-001) validates the given values against the fields that apply to the row.
func fieldValues(fields []Field, given map[string]any, current *Item) (map[string]any, error) {
	out := make(map[string]any, len(given))

	for column, raw := range given {
		i := slices.IndexFunc(fields, func(f Field) bool { return f.Column == column })
		if i < 0 {
			return nil, apperr.BadRequest(fmt.Sprintf("%q is not a field of this item", column))
		}

		field := fields[i]

		value, err := field.normalize(raw)
		if err != nil {
			return nil, err
		}

		if field.ReadOnly {
			var was any
			if current != nil {
				was = current.Values[column]
			}

			if format(value) != format(was) && (was != nil || !isZero(value)) {
				return nil, apperr.BadRequest(fmt.Sprintf("%q is read-only", column))
			}

			continue
		}

		out[column] = value
	}

	return out, nil
}

func isZero(v any) bool {
	switch typed := v.(type) {
	case nil:
		return true
	case string:
		return typed == ""
	case int64:
		return typed == 0
	case bool:
		return !typed
	default:
		return false
	}
}

// normalize turns what JSON decoded into the value the column takes: a string, int64, float64, bool or nil.
func (f Field) normalize(raw any) (any, error) {
	bad := func(what string) error { return apperr.BadRequest(fmt.Sprintf("%s: %s", f.Column, what)) }

	switch f.Type {
	case TypeText, TypeChoice:
		text, ok := raw.(string)
		if raw != nil && !ok {
			return nil, bad("a text is expected")
		}

		text = strings.TrimSpace(text)

		switch {
		case utf8.RuneCountInString(text) > f.Max:
			return nil, bad(fmt.Sprintf("longer than %d characters", f.Max))
		case text == "" && f.Null:
			return nil, nil //nolint:nilnil // NULL
		case text != "" && f.Type == TypeChoice && !f.hasChoice(text):
			return nil, bad("not one of the choices")
		}

		return text, nil
	case TypeNumber:
		return f.number(raw, bad)
	case TypeYesNo:
		switch typed := raw.(type) {
		case nil:
			return false, nil
		case bool:
			return typed, nil
		default:
			return nil, bad("yes or no is expected")
		}
	}

	return nil, bad("unknown type")
}

func (f Field) number(raw any, bad func(string) error) (any, error) {
	var n float64

	switch typed := raw.(type) {
	case nil:
		if f.Null {
			return nil, nil //nolint:nilnil // NULL
		}

		return int64(0), nil
	case float64:
		n = typed
	case int:
		n = float64(typed)
	case int64:
		return typed, nil
	case json.Number:
		parsed, err := typed.Float64()
		if err != nil {
			return nil, bad("a number is expected")
		}

		n = parsed
	default:
		return nil, bad("a number is expected")
	}

	if math.IsNaN(n) || math.IsInf(n, 0) {
		return nil, bad("a number is expected")
	}

	if n == math.Trunc(n) && math.Abs(n) < 1<<53 {
		return int64(n), nil
	}

	return n, nil
}

func (f Field) hasChoice(value string) bool {
	return slices.ContainsFunc(f.Choices, func(c Choice) bool { return c.Value == value })
}
