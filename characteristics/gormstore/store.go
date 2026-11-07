// Package gormstore is the SQL store of the characteristics module: it reads and writes the tables a core.Tree
// describes, with raw, quoted SQL built from the description, and nothing else. MariaDB 10.3 and MySQL safe. It passes
// storetest. A column the description does not name is never selected, inserted or updated.
//
//nolint:cyclop,exhaustruct,exhaustruct_v5,funlen,varnamelen // structs are built field by field; zero is the default
package gormstore

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/wssto2/go-core/characteristics/core"
	"github.com/wssto2/go-core/database"
	"gorm.io/gorm"
)

// Store is the SQL store of one described tree.
type Store struct {
	db   *gorm.DB
	tree core.Tree
}

var _ core.Store = (*Store)(nil)

var (
	// ErrMissing is a table or column the description names and the database lacks.
	ErrMissing = errors.New("the characteristics description names what the database lacks")
	// ErrNoOrder is a reorder of a level that has no order column.
	ErrNoOrder = errors.New("the level has no order column")
	// ErrNotDescribed is a level number the description does not have.
	ErrNotDescribed = errors.New("the characteristics level is not described")
)

// New returns the store over db for a prepared tree (core.Tree.Prepare).
func New(db *gorm.DB, tree core.Tree) *Store { return &Store{db: db, tree: tree} }

// Check (CHARACTERISTICS-DESC-001) reports every described column the database lacks: the description names columns,
// and a typo would otherwise
// show at the first query. Run it once the migrations have been applied.
func (s *Store) Check(ctx context.Context) error {
	var missing []string

	migrator := s.db.WithContext(ctx).Migrator()

	for i, level := range s.tree.Levels {
		if !migrator.HasTable(level.Table) {
			missing = append(missing, fmt.Sprintf("level %d: table %s", i+1, level.Table))

			continue
		}

		for _, column := range columns(level) {
			if !migrator.HasColumn(level.Table, column) {
				missing = append(missing, fmt.Sprintf("level %d: %s.%s", i+1, level.Table, column))
			}
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("%w: %s", ErrMissing, strings.Join(missing, ", "))
	}

	return nil
}

// Transaction implements core.Store.
func (s *Store) Transaction(ctx context.Context, fn func(ctx context.Context) error) error {
	return database.NewTransactor(s.db).WithinTransaction(ctx, fn) //nolint:wrapcheck // the caller names the step
}

// selectFrom is SELECT of the described columns FROM the level's table.
func selectFrom(level core.Level) string {
	cols := columns(level)
	quoted := make([]string, len(cols))

	for index, column := range cols {
		quoted[index] = q(column)
	}

	return "SELECT " + strings.Join(quoted, ", ") + " FROM " + q(level.Table)
}

// q quotes an identifier; the description allows letters, digits and underscores only.
func q(name string) string { return "`" + name + "`" }

// columns are the columns the description names for a level, each once, that a read selects.
func columns(l core.Level) []string {
	var out []string

	add := func(c string) {
		if c == "" {
			return
		}

		if slices.Contains(out, c) {
			return
		}

		out = append(out, c)
	}

	add(l.ID)
	add(l.Parent)
	add(l.Name)
	add(l.Translations.JSON)

	for _, c := range sortedColumns(l.Translations.Columns) {
		add(c)
	}

	add(l.Order)
	add(l.Active)
	add(l.Meta)

	for _, f := range l.Fields {
		add(f.Column)
	}

	return out
}

func sortedColumns(m map[string]string) []string {
	out := make([]string, 0, len(m))

	for _, lang := range slices.Sorted(maps.Keys(m)) { // a fixed order keeps the SQL the same from run to run
		out = append(out, m[lang])
	}

	return out
}

// Rows implements core.Store.
func (s *Store) Rows(ctx context.Context, number int) ([]core.Item, error) {
	level, err := s.level(number)
	if err != nil {
		return nil, err
	}

	sort := q(level.Name)
	if level.Order != "" {
		sort = q(level.Order)
	}

	var raw []map[string]any

	err = s.conn(ctx).Raw(selectFrom(level) + " ORDER BY " + sort + ", " + q(level.ID)).Scan(&raw).Error
	if err != nil {
		return nil, err
	}

	items := make([]core.Item, len(raw))
	for i, row := range raw {
		items[i] = s.item(number, level, row)
	}

	return items, nil
}

// Row implements core.Store.
func (s *Store) Row(ctx context.Context, number, id int) (core.Item, error) {
	level, err := s.level(number)
	if err != nil {
		return core.Item{}, err
	}

	var raw []map[string]any

	err = s.conn(ctx).Raw(selectFrom(level)+" WHERE "+q(level.ID)+" = ?", id).Scan(&raw).Error
	if err != nil {
		return core.Item{}, err
	}

	if len(raw) == 0 {
		return core.Item{}, core.ErrNotFound
	}

	return s.item(number, level, raw[0]), nil
}

// unwrap reads through the pointers and sql.Null types a driver may scan a column into.
func unwrap(v any) any {
	if valuer, ok := v.(driver.Valuer); ok && v != nil {
		inner, err := valuer.Value()
		if err == nil {
			return inner
		}
	}

	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil
		}

		return unwrap(rv.Elem().Interface())
	}

	return v
}

func toString(v any) string {
	switch typed := unwrap(v).(type) {
	case nil:
		return ""
	case []byte:
		return string(typed)
	case string:
		return typed
	case time.Time:
		return typed.Format("2006-01-02 15:04:05")
	default:
		return fmt.Sprint(typed)
	}
}

func toInt(v any) int {
	n, _ := strconv.Atoi(toString(v))

	return n
}

func toBool(v any) bool {
	switch typed := unwrap(v).(type) {
	case bool:
		return typed
	default:
		text := toString(v)

		return text != "" && text != "0" && text != "false"
	}
}

func fromDB(f core.Field, v any) any {
	if unwrap(v) == nil {
		return nil
	}

	switch f.Type {
	case core.TypeYesNo:
		return toBool(v)
	case core.TypeNumber:
		text := toString(v)

		whole, err := strconv.ParseInt(text, 10, 64)
		if err == nil {
			return whole
		}

		fraction, err := strconv.ParseFloat(text, 64)
		if err == nil {
			return fraction
		}

		return nil
	case core.TypeText, core.TypeChoice:
		return toString(v)
	}

	return nil
}

// toDB is a value as the driver takes it. A boolean is written as the text 0 or 1, which a tinyint and an
// enum('0','1') both read as the flag (an integer would pick the enum member by position).
func toDB(v any) any {
	if b, ok := v.(bool); ok {
		if b {
			return "1"
		}

		return "0"
	}

	return v
}

// writer collects the column assignments of one statement.
type writer struct {
	cols []string
	args []any
}

// set adds an assignment; the first one for a column wins (two described fields may share a column).
func (w *writer) set(column string, value any) {
	if column == "" || slices.Contains(w.cols, column) {
		return
	}

	w.cols = append(w.cols, column)
	w.args = append(w.args, toDB(value))
}

func (w *writer) setActor(column string, actor int) {
	if actor > 0 {
		w.set(column, actor)
	}
}

// fields adds the name, translations (merged into the current JSON), meta and extra values of the item.
func (w *writer) fields(level core.Level, item core.Item, currentJSON string) {
	w.set(level.Name, item.Name)

	if item.Translations != nil {
		for lang, column := range level.Translations.Columns {
			if text, given := item.Translations[lang]; given {
				w.set(column, text)
			}
		}

		if level.Translations.JSON != "" {
			w.set(level.Translations.JSON, mergeJSON(currentJSON, item.Translations))
		}
	}

	if item.Meta != nil && level.Meta != "" {
		w.set(level.Meta, jsonOrNil(item.Meta))
	}

	for _, f := range level.Fields {
		if v, ok := item.Values[f.Column]; ok {
			w.set(f.Column, v)
		}
	}
}

// empty is what a field holds when nothing was given.
func empty(f core.Field) any {
	switch {
	case f.Null:
		return nil
	case f.Type == core.TypeText || f.Type == core.TypeChoice:
		return ""
	case f.Type == core.TypeYesNo:
		return false
	default:
		return int64(0)
	}
}

// mergeJSON sets the given languages in the stored object, keeping the keys it does not name; an empty text removes a
// language. An empty result is NULL.
func mergeJSON(current string, given map[string]string) any {
	object := map[string]any{}
	_ = json.Unmarshal([]byte(current), &object)

	if object == nil {
		object = map[string]any{}
	}

	for lang, text := range given {
		if text == "" {
			delete(object, lang)
		} else {
			object[lang] = text
		}
	}

	return jsonOrNil(object)
}

func jsonOrNil(object map[string]any) any {
	if len(object) == 0 {
		return nil
	}

	raw, err := json.Marshal(object)
	if err != nil {
		return nil
	}

	return string(raw)
}

// Create implements core.Store (CHARACTERISTICS-STORE-001). The audit columns are written as Laravel's creating hook
// and arv-next's service do: the creator and time; the editor and time where the description says they are set on
// create; the activator and time when the row starts active.
func (s *Store) Create(ctx context.Context, item *core.Item, stamp core.Stamp) error {
	level, err := s.level(item.Level)
	if err != nil {
		return err
	}

	conn := s.conn(ctx)
	w := &writer{}

	// Every described field is written, the ones not given as the empty value of their type: a NOT NULL column
	// without a default cannot be left out, and a new row has nothing to lose.
	values := map[string]any{}
	for _, f := range level.Fields {
		values[f.Column] = empty(f)
	}

	maps.Copy(values, item.Values)

	filled := *item
	filled.Values = values
	w.fields(level, filled, "")

	if level.Parent != "" {
		w.set(level.Parent, item.ParentID)
	}

	for i, column := range level.Ancestors {
		w.set(column, item.Path[i])
	}

	if level.Order != "" {
		var last int

		query := "SELECT COALESCE(MAX(" + q(level.Order) + "), 0) FROM " + q(level.Table)

		args := []any{}

		if level.Parent != "" {
			query += " WHERE " + q(level.Parent) + " = ?"

			args = append(args, item.ParentID)
		}

		err = conn.Raw(query, args...).Scan(&last).Error
		if err != nil {
			return err
		}

		item.Order = last + 1
		w.set(level.Order, item.Order)
	}

	a := level.Audit
	w.setActor(a.CreatedBy, stamp.Actor)

	if a.CreatedAt != "" {
		w.set(a.CreatedAt, stamp.At)
	}

	if a.UpdatedByOnCreate {
		w.setActor(a.UpdatedBy, stamp.Actor)
	}

	if a.UpdatedAtOnCreate && a.UpdatedAt != "" {
		w.set(a.UpdatedAt, stamp.At)
	}

	if level.Active != "" {
		w.set(level.Active, item.Active)

		if item.Active {
			w.setActor(a.ActivatedBy, stamp.Actor)
			w.set(a.ActivatedAt, stamp.At)
		}
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?, ", len(w.cols)), ", ")
	names := make([]string, len(w.cols))

	for i, c := range w.cols {
		names[i] = q(c)
	}

	result, err := conn.Statement.ConnPool.ExecContext(ctx,
		"INSERT INTO "+q(level.Table)+" ("+strings.Join(names, ", ")+") VALUES ("+placeholders+")", w.args...)
	if err != nil {
		return err //nolint:wrapcheck // the caller names the step
	}

	id, err := result.LastInsertId()
	if err != nil {
		return err //nolint:wrapcheck // the caller names the step
	}

	item.ID = int(id)

	return nil
}

// Update implements core.Store. The editor and time are written on every update, as Laravel's updating hook does.
func (s *Store) Update(ctx context.Context, item core.Item, stamp core.Stamp) error {
	level, err := s.level(item.Level)
	if err != nil {
		return err
	}

	return s.Transaction(ctx, func(ctx context.Context) error {
		current := ""

		if item.Translations != nil && level.Translations.JSON != "" {
			var raw []map[string]any

			err := s.conn(ctx).Raw("SELECT "+q(level.Translations.JSON)+" FROM "+q(level.Table)+
				" WHERE "+q(level.ID)+" = ?", item.ID).Scan(&raw).Error
			if err != nil {
				return err
			}

			if len(raw) == 0 {
				return core.ErrNotFound
			}

			current = toString(raw[0][level.Translations.JSON])
		}

		w := &writer{}
		w.fields(level, item, current)
		w.setActor(level.Audit.UpdatedBy, stamp.Actor)

		if level.Audit.UpdatedAt != "" {
			w.set(level.Audit.UpdatedAt, stamp.At)
		}

		return s.update(ctx, item.Level, level, item.ID, w)
	})
}

// SetActive implements core.Store. Switching on writes the activator and time, switching off the deactivator and time
// (the other pair keeps its value), and the editor and time as every update does; a row already in that state is left
// as it is.
func (s *Store) SetActive(ctx context.Context, number, id int, active bool, stamp core.Stamp) error {
	level, err := s.level(number)
	if err != nil {
		return err
	}

	return s.Transaction(ctx, func(ctx context.Context) error {
		row, err := s.Row(ctx, number, id)
		if err != nil {
			return err
		}

		if row.Active == active {
			return nil
		}

		a := level.Audit
		w := &writer{}
		w.set(level.Active, active)

		if active {
			w.setActor(a.ActivatedBy, stamp.Actor)
			w.set(a.ActivatedAt, stamp.At)
		} else {
			w.setActor(a.DeactivatedBy, stamp.Actor)
			w.set(a.DeactivatedAt, stamp.At)
		}

		w.setActor(a.UpdatedBy, stamp.Actor)
		w.set(a.UpdatedAt, stamp.At)

		return s.update(ctx, number, level, id, w)
	})
}

// SetOrders implements core.Store. Like Laravel's reorder it writes the order column only.
func (s *Store) SetOrders(ctx context.Context, number int, orders map[int]int) error {
	level, err := s.level(number)
	if err != nil {
		return err
	}

	if level.Order == "" {
		return ErrNoOrder
	}

	return s.Transaction(ctx, func(ctx context.Context) error {
		for id, order := range orders {
			err := s.conn(ctx).Exec("UPDATE "+q(level.Table)+" SET "+q(level.Order)+" = ? WHERE "+q(level.ID)+" = ?",
				order, id).Error
			if err != nil {
				return err
			}
		}

		return nil
	})
}

// Delete implements core.Store.
func (s *Store) Delete(ctx context.Context, number int, ids []int) error {
	level, err := s.level(number)
	if err != nil {
		return err
	}

	return s.conn(ctx).Exec("DELETE FROM "+q(level.Table)+" WHERE "+q(level.ID)+" IN ?", ids).
		Error
}

func (s *Store) conn(ctx context.Context) *gorm.DB {
	if tx, ok := database.TxFor(ctx, s.db); ok {
		return tx.WithContext(ctx)
	}

	return s.db.WithContext(ctx)
}

func (s *Store) level(number int) (core.Level, error) {
	level, ok := s.tree.Level(number)
	if !ok {
		return core.Level{}, fmt.Errorf("%w: %d", ErrNotDescribed, number)
	}

	return level, nil
}

func (s *Store) item(number int, level core.Level, row map[string]any) core.Item {
	item := core.Item{
		Level: number, ID: toInt(row[level.ID]), Name: toString(row[level.Name]), Active: level.Active == "",
		Values: make(map[string]any, len(level.Fields)),
	}

	if level.Parent != "" {
		item.ParentID = toInt(row[level.Parent])
	}

	if level.Order != "" {
		item.Order = toInt(row[level.Order])
	}

	if level.Active != "" {
		item.Active = toBool(row[level.Active])
	}

	if level.Meta != "" {
		_ = json.Unmarshal([]byte(toString(row[level.Meta])), &item.Meta) // NULL or broken JSON read as no meta
		if len(item.Meta) == 0 {
			item.Meta = nil
		}
	}

	item.Translations = s.translationsOf(level, row)

	for _, f := range level.Fields {
		item.Values[f.Column] = fromDB(f, row[f.Column])
	}

	return item
}

// translationsOf reads the listed languages only; a stored language the description does not list is not shown.
func (s *Store) translationsOf(level core.Level, row map[string]any) map[string]string {
	out := map[string]string{}

	if level.Translations.JSON != "" {
		var object map[string]any

		_ = json.Unmarshal([]byte(toString(row[level.Translations.JSON])), &object)

		for lang, v := range object {
			if text, ok := v.(string); ok && text != "" && slices.Contains(s.tree.Languages, lang) {
				out[lang] = text
			}
		}
	}

	for lang, column := range level.Translations.Columns {
		if text := toString(row[column]); text != "" {
			out[lang] = text
		}
	}

	return out
}

// update runs UPDATE ... SET the collected columns WHERE id.
func (s *Store) update(ctx context.Context, number int, level core.Level, id int, w *writer) error {
	sets := make([]string, len(w.cols))
	for i, c := range w.cols {
		sets[i] = q(c) + " = ?"
	}

	conn := s.conn(ctx)

	result := conn.Exec("UPDATE "+q(level.Table)+" SET "+strings.Join(sets, ", ")+" WHERE "+q(level.ID)+" = ?",
		append(w.args, id)...)
	if result.Error != nil {
		return result.Error
	}

	if result.RowsAffected == 0 { // MySQL reports 0 for an update that wrote what was already there
		_, err := s.Row(ctx, number, id)

		return err
	}

	return nil
}
