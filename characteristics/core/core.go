// Package core is the framework-free part of the characteristics module: the description a project writes of its
// tables, the items they hold, the ports a store and an application implement, and the service that applies the rules.
// It takes the standard library and apperr only.
//
//nolint:cyclop,exhaustruct,exhaustruct_v5,varnamelen // structs are built field by field; zero is the default
package core

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/wssto2/go-core/apperr"
)

// ErrNotFound is an item that does not exist.
var ErrNotFound = errors.New("characteristic not found")

// Item is one row of one level.
type Item struct {
	// Level is 1 for the root level.
	Level int
	ID    int
	// ParentID is 0 at the root level.
	ParentID int
	// Path holds the ids of the ancestors, root first (one fewer than Level). The service fills it; a store writes the
	// ancestor columns from it.
	Path []int
	Name string
	// Translations are the names by language, only the languages that have one.
	Translations map[string]string
	// Order is the position among the siblings, 1-based; 0 when the level has no order column.
	Order int
	// Active is true for every row of a level without an active column.
	Active bool
	// Meta is the JSON object of the meta column; nil when empty or when the level has none.
	Meta map[string]any
	// Values are the described extra columns that apply to the row, by column: string, int64, float64, bool or nil.
	Values map[string]any
}

// Stamp is who changed a row and when: what the audit columns are written from.
type Stamp struct {
	// Actor is the person's id; zero leaves the "by" columns to the database.
	Actor int
	At    time.Time
}

// Store reads and writes the tables the Tree describes. gormstore is the SQL implementation and storetest the suite
// every store passes. Update writes only the extra columns present in item.Values, the translations and meta only
// when they are not nil, and never a column the description does not name. A method joins the transaction in ctx.
type Store interface {
	// Rows returns every row of the level, by order (else name) and id; the Values hold every described column.
	Rows(ctx context.Context, level int) ([]Item, error)
	// Row returns one row, else ErrNotFound.
	Row(ctx context.Context, level, id int) (Item, error)
	// Create inserts the item, setting its ID and Order (last among its siblings), and writes the audit columns.
	Create(ctx context.Context, item *Item, stamp Stamp) error
	// Update changes the name, translations, meta and values, and the audit columns of an update.
	Update(ctx context.Context, item Item, stamp Stamp) error
	// SetActive switches the row, writing the activation columns; a row already in that state is not touched.
	SetActive(ctx context.Context, level, id int, active bool, stamp Stamp) error
	// SetOrders writes the order column of the given rows (id to position), and no audit column.
	SetOrders(ctx context.Context, level int, orders map[int]int) error
	// Delete removes the rows.
	Delete(ctx context.Context, level int, ids []int) error
	// Transaction runs fn with a context that carries one transaction for every Store call.
	Transaction(ctx context.Context, fn func(ctx context.Context) error) error
}

// Use is one kind of place an item is used: "2 pages".
type Use struct {
	What  string `json:"what"`
	Count int    `json:"count"`
}

// Usage is what an application implements to say where an item is used. It is optional: the application chooses
// WithUsage or WithoutUsage at Install, and without a port the answers say the usage is unknown.
type Usage interface {
	// Uses returns the places the item is used, empty when nowhere.
	Uses(ctx context.Context, level, id int) ([]Use, error)
}

// Clock tells the time.
type Clock interface{ Now() time.Time }

// Config is what New takes besides the tree and the store.
type Config struct {
	// Usage is nil when the application cannot tell where items are used.
	Usage Usage
	Clock Clock
	// CacheTTL is how long a read tree is kept; zero keeps nothing. A write through the service drops it at once, a
	// write by another process (the legacy application) is seen after the TTL.
	CacheTTL time.Duration
}

// Service reads and changes the tree. It has no permission: the routes guard it.
type Service struct {
	tree  Tree
	store Store
	cfg   Config

	mu    sync.Mutex
	snap  *snapshot
	loads int
}

type snapshot struct {
	at     time.Time
	levels [][]Item
	byID   []map[int]Item
}

// New returns the service over the store. The tree must be prepared (Tree.Prepare).
func New(tree Tree, store Store, cfg Config) *Service {
	return &Service{tree: tree, store: store, cfg: cfg}
}

// Description is the prepared tree.
func (s *Service) Description() Tree { return s.tree }

// Loads counts the times the service read the store to fill its cache: what a test checks the cache with.
func (s *Service) Loads() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.loads
}

// Get returns one item.
func (s *Service) Get(ctx context.Context, level, id int) (Item, error) {
	item, _, err := s.find(ctx, level, id)

	return item, err
}

// Node is an item with the items below it.
type Node struct {
	Item     Item
	Children []Node
}

// Tree returns the roots with everything below them.
func (s *Service) Tree(ctx context.Context) ([]Node, error) {
	snap, err := s.snapshot(ctx)
	if err != nil {
		return nil, err
	}

	return snap.nodes(0, 0), nil
}

// Branch returns an item with everything below it.
func (s *Service) Branch(ctx context.Context, level, id int) (Node, error) {
	item, snap, err := s.find(ctx, level, id)
	if err != nil {
		return Node{}, err
	}

	return snap.node(item), nil
}

func (sn *snapshot) nodes(index, parent int) []Node {
	out := []Node{}

	for _, row := range sn.levels[index] {
		if row.ParentID == parent {
			out = append(out, sn.node(row))
		}
	}

	return out
}

func (sn *snapshot) node(item Item) Node {
	n := Node{Item: item, Children: []Node{}}
	if item.Level < len(sn.levels) {
		n.Children = sn.nodes(item.Level, item.ID)
	}

	return n
}

// below returns everything under the item, shallowest level first.
func (sn *snapshot) below(item Item) []Item {
	var out []Item

	parents := map[int]bool{item.ID: true}

	for index := item.Level; index < len(sn.levels); index++ {
		next := map[int]bool{}

		for _, row := range sn.levels[index] {
			if parents[row.ParentID] {
				out = append(out, row)
				next[row.ID] = true
			}
		}

		parents = next
	}

	return out
}

// Option is one choice of a form field.
type Option struct {
	ID    int
	Value string
	Label string
}

// OptionsQuery asks for the choices of one branch: the active children of Parent on Level (Parent is 0 at the root
// level), the value being the id, or the described column Value; labelled in Lang.
type OptionsQuery struct {
	Level, Parent int
	Value, Lang   string
}

// Options (CHARACTERISTICS-OPTIONS-001) returns the active items of one branch for a form field. Items with an empty
// value are left out.
func (s *Service) Options(ctx context.Context, q OptionsQuery) ([]Option, error) {
	level, err := s.level(q.Level)
	if err != nil {
		return nil, err
	}

	if (q.Level == 1) != (q.Parent == 0) {
		return nil, apperr.BadRequest("parent is required below the root level and not allowed at it")
	}

	if q.Value != "" && !slices.ContainsFunc(level.Fields, func(f Field) bool { return f.Column == q.Value }) {
		return nil, apperr.BadRequest("value must be empty (the id) or a described column of the level")
	}

	lang := q.Lang
	if lang == "" {
		lang = s.tree.Languages[0]
	}

	snap, err := s.snapshot(ctx)
	if err != nil {
		return nil, err
	}

	out := []Option{}

	for _, row := range snap.levels[q.Level-1] {
		if row.ParentID != q.Parent || !row.Active {
			continue
		}

		value := strconv.Itoa(row.ID)
		if q.Value != "" {
			value = format(row.Values[q.Value])
		}

		if value == "" {
			continue
		}

		label := row.Translations[lang]
		if label == "" {
			label = row.Name
		}

		out = append(out, Option{ID: row.ID, Value: value, Label: label})
	}

	return out, nil
}

func format(v any) string {
	switch typed := v.(type) {
	case nil:
		return ""
	case bool:
		if typed {
			return "1"
		}

		return "0"
	default:
		return fmt.Sprint(v)
	}
}

// Create adds an item below the parent (0 at the root level), last among its siblings.
func (s *Service) Create(ctx context.Context, actor, level, parent int, in Input) (Item, error) {
	def, err := s.level(level)
	if err != nil {
		return Item{}, err
	}

	var path []int

	if level > 1 {
		up, _, upErr := s.find(ctx, level-1, parent)
		if upErr != nil {
			return Item{}, apperr.BadRequest("the parent does not exist")
		}

		path = append(slices.Clone(up.Path), up.ID)
	} else if parent != 0 {
		return Item{}, apperr.BadRequest("the root level has no parent")
	}

	item := Item{Level: level, ParentID: parent, Path: path, Active: in.Active && def.Active != ""}

	err = s.fill(def, &item, in, nil)
	if err != nil {
		return Item{}, err
	}

	err = s.store.Create(ctx, &item, s.stamp(actor))
	s.drop()

	if err != nil {
		return Item{}, apperr.Internal(fmt.Errorf("create characteristic: %w", err))
	}

	return s.Get(ctx, level, item.ID)
}

// Update changes the name, translations, meta and values of an item.
func (s *Service) Update(ctx context.Context, actor, level, id int, in Input) (Item, error) {
	def, err := s.level(level)
	if err != nil {
		return Item{}, err
	}

	current, _, err := s.find(ctx, level, id)
	if err != nil {
		return Item{}, err
	}

	item := Item{Level: level, ID: id, ParentID: current.ParentID, Path: current.Path}

	err = s.fill(def, &item, in, &current)
	if err != nil {
		return Item{}, err
	}

	err = s.store.Update(ctx, item, s.stamp(actor))
	s.drop()

	if err != nil {
		return Item{}, apperr.Internal(fmt.Errorf("update characteristic: %w", err))
	}

	return s.Get(ctx, level, id)
}

// SetActive switches an item on or off, and writes the activation columns when that changes it.
func (s *Service) SetActive(ctx context.Context, actor, level, id int, active bool) (Item, error) {
	def, err := s.level(level)
	if err != nil {
		return Item{}, err
	}

	if def.Active == "" {
		return Item{}, apperr.BadRequest("this level has no active flag")
	}

	_, _, err = s.find(ctx, level, id)
	if err != nil {
		return Item{}, err
	}

	err = s.store.SetActive(ctx, level, id, active, s.stamp(actor))
	s.drop()

	if err != nil {
		return Item{}, apperr.Internal(fmt.Errorf("switch characteristic: %w", err))
	}

	return s.Get(ctx, level, id)
}

// Move (CHARACTERISTICS-STORE-001) puts the item at a position (1-based) among its siblings and renumbers the ones it
// passes.
func (s *Service) Move(ctx context.Context, level, id, position int) error {
	def, err := s.level(level)
	if err != nil {
		return err
	}

	if def.Order == "" {
		return apperr.BadRequest("this level has no order column")
	}

	item, snap, err := s.find(ctx, level, id)
	if err != nil {
		return err
	}

	siblings := []Item{}

	for _, row := range snap.levels[level-1] {
		if row.ParentID == item.ParentID && row.ID != id {
			siblings = append(siblings, row)
		}
	}

	sort.SliceStable(siblings, func(i, j int) bool { return siblings[i].Order < siblings[j].Order })

	at := min(max(position, 1), len(siblings)+1) - 1
	siblings = slices.Insert(siblings, at, item)
	changes := map[int]int{}

	for i, row := range siblings {
		if row.Order != i+1 {
			changes[row.ID] = i + 1
		}
	}

	if len(changes) == 0 {
		return nil
	}

	err = s.store.SetOrders(ctx, level, changes)
	s.drop()

	if err != nil {
		return apperr.Internal(fmt.Errorf("reorder characteristics: %w", err))
	}

	return nil
}

// Report is where an item (and what deleting it takes with it) is used.
type Report struct {
	// Known is false when the application gave no Usage: Uses is then empty and means nothing.
	Known bool
	Uses  []Use
	// Children counts the items below it.
	Children int
}

// Usage says where the item, and the items below it, are used.
func (s *Service) Usage(ctx context.Context, level, id int) (Report, error) {
	item, snap, err := s.find(ctx, level, id)
	if err != nil {
		return Report{}, err
	}

	return s.report(ctx, snap, item)
}

// DeleteResult is what a delete did.
type DeleteResult struct {
	// Deleted counts the rows removed, the item and what was below it.
	Deleted int
	// UsageKnown is false when the application cannot say where items are used, so the delete was not checked.
	UsageKnown bool
}

// The reasons a delete is refused (409).
const (
	ReasonInUse       apperr.Reason = "characteristics.in_use"
	ReasonHasChildren apperr.Reason = "characteristics.has_children"
)

// Delete (CHARACTERISTICS-DELETE-001) removes an item. It is refused (409) while the application says the item, or one
// below it, is used, and while it has items below it, unless force: then those go too. Deactivating (SetActive) is
// always possible.
func (s *Service) Delete(ctx context.Context, level, id int, force bool) (DeleteResult, error) {
	item, snap, err := s.find(ctx, level, id)
	if err != nil {
		return DeleteResult{}, err
	}

	report, err := s.report(ctx, snap, item)
	if err != nil {
		return DeleteResult{}, err
	}

	if !force && len(report.Uses) > 0 {
		return DeleteResult{}, apperr.Conflict("the characteristic is in use").
			WithReason(ReasonInUse, map[string]any{"uses": report.Uses})
	}

	if !force && report.Children > 0 {
		return DeleteResult{}, apperr.Conflict("the characteristic has items below it").
			WithReason(ReasonHasChildren, map[string]any{"children": report.Children})
	}

	below := snap.below(item)
	byLevel := map[int][]int{level: {id}}

	for _, row := range below {
		byLevel[row.Level] = append(byLevel[row.Level], row.ID)
	}

	err = s.store.Transaction(ctx, func(ctx context.Context) error {
		for l := len(s.tree.Levels); l >= level; l-- { // children before parents
			if ids := byLevel[l]; len(ids) > 0 {
				err := s.store.Delete(ctx, l, ids)
				if err != nil {
					return err //nolint:wrapcheck // named by the caller
				}
			}
		}

		return nil
	})
	s.drop()

	if err != nil {
		return DeleteResult{}, apperr.Internal(fmt.Errorf("delete characteristic: %w", err))
	}

	return DeleteResult{Deleted: 1 + len(below), UsageKnown: report.Known}, nil
}

func (s *Service) snapshot(ctx context.Context) (*snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.snap != nil && s.cfg.CacheTTL > 0 && s.cfg.Clock.Now().Sub(s.snap.at) < s.cfg.CacheTTL {
		return s.snap, nil
	}

	count := len(s.tree.Levels)
	snap := &snapshot{at: s.cfg.Clock.Now(), levels: make([][]Item, count), byID: make([]map[int]Item, count)}

	for i := range s.tree.Levels {
		rows, err := s.store.Rows(ctx, i+1)
		if err != nil {
			return nil, apperr.Internal(fmt.Errorf("read characteristics level %d: %w", i+1, err))
		}

		snap.levels[i] = rows
		snap.byID[i] = make(map[int]Item, len(rows))
	}

	// A path needs the parent's, so the levels are filled root first.
	for i := range snap.levels {
		level := s.tree.Levels[i]

		for j, row := range snap.levels[i] {
			row.Level = i + 1
			if i > 0 {
				row.Path = append(slices.Clone(snap.byID[i-1][row.ParentID].Path), row.ParentID)
			}

			fields := level.FieldsFor(row.Path)
			values := make(map[string]any, len(fields))

			for _, f := range fields {
				values[f.Column] = row.Values[f.Column]
			}

			row.Values = values
			snap.levels[i][j] = row
			snap.byID[i][row.ID] = row
		}
	}

	s.snap, s.loads = snap, s.loads+1

	return snap, nil
}

// drop forgets the cached tree: called after every write.
func (s *Service) drop() {
	s.mu.Lock()
	s.snap = nil
	s.mu.Unlock()
}

func (s *Service) level(number int) (Level, error) {
	level, ok := s.tree.Level(number)
	if !ok {
		return Level{}, apperr.NotFound("characteristic level not found")
	}

	return level, nil
}

func (s *Service) find(ctx context.Context, level, id int) (Item, *snapshot, error) {
	_, err := s.level(level)
	if err != nil {
		return Item{}, nil, err
	}

	snap, err := s.snapshot(ctx)
	if err != nil {
		return Item{}, nil, err
	}

	item, ok := snap.byID[level-1][id]
	if !ok {
		return Item{}, nil, apperr.NotFound("characteristic not found")
	}

	return item, snap, nil
}

func (s *Service) stamp(actor int) Stamp {
	return Stamp{Actor: actor, At: s.cfg.Clock.Now().Truncate(time.Second)}
}

func (s *Service) report(ctx context.Context, snap *snapshot, item Item) (Report, error) {
	below := snap.below(item)
	report := Report{Known: s.cfg.Usage != nil, Uses: []Use{}, Children: len(below)}

	if !report.Known {
		return report, nil
	}

	counts := map[string]int{}

	var order []string

	for _, row := range append([]Item{item}, below...) {
		uses, err := s.cfg.Usage.Uses(ctx, row.Level, row.ID)
		if err != nil {
			return Report{}, apperr.Internal(fmt.Errorf("characteristic usage: %w", err))
		}

		for _, use := range uses {
			if use.Count <= 0 {
				continue
			}

			if _, seen := counts[use.What]; !seen {
				order = append(order, use.What)
			}

			counts[use.What] += use.Count
		}
	}

	for _, what := range order {
		report.Uses = append(report.Uses, Use{What: what, Count: counts[what]})
	}

	return report, nil
}
