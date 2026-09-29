package authzgorm_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/authz/authzgorm"
	"github.com/wssto2/go-core/authz/authztest"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type lead struct {
	ID         int `gorm:"primaryKey"`
	DealerID   int
	LocationID int
	OwnerID    *int
	KindID     *int
}

var leadColumns = authzgorm.Columns{
	Levels:        map[string]string{"dealer": "dealer_id", "location": "location_id"},
	Owner:         "owner_id",
	OwnerLocation: "location_id",
	Attrs:         map[string]string{"vehiclekind": "CASE kind_id WHEN 1 THEN 'used' WHEN 2 THEN 'new' END"},
}

func catalogue(t *testing.T) *authz.Catalogue {
	t.Helper()
	c := authz.NewCatalogue()
	require.NoError(t, c.Define("crm.lead:view", authz.Ownable("crm.lead"), authz.UnownedIsOwn(), authz.Attributes("vehiclekind")))
	require.NoError(t, c.Define("crm.offer:view", authz.Ownable("crm.offer")))
	require.NoError(t, c.Define("crm.customer:view"))
	return c
}

func roles() []authz.Role {
	return []authz.Role{
		{Key: "own", Name: "own", Grants: authz.Grants(authz.QualifierOwn, "crm.lead:view", "crm.offer:view")},
		{Key: "ownloc", Name: "ownloc", Grants: authz.Grants(authz.QualifierOwnLocation, "crm.lead:view", "crm.offer:view")},
		{Key: "all", Name: "all", Grants: authz.Grants(authz.QualifierAll, "crm.lead:view", "crm.offer:view", "crm.customer:view")},
		{Key: "used", Name: "used", Attrs: map[string][]string{"vehiclekind": {"used"}}, Grants: authz.Grants(authz.QualifierAll, "crm.lead:view")},
		{Key: "newown", Name: "newown", Attrs: map[string][]string{"vehiclekind": {"new"}}, Grants: authz.Grants(authz.QualifierOwn, "crm.lead:view")},
	}
}

func ptr(i int) *int { return &i }

// shape is one way of laying leads out in tables. The same access set must
// select the same rows in every shape, whether the owner and the location are
// plain columns or come from other tables through trusted SQL.
type shape struct {
	name string
	// seed fills the tables and returns the ID of every lead with its resource.
	seed func(t *testing.T, db *gorm.DB) map[int]authz.Resource
	// query returns the IDs the access set selects.
	query func(db *gorm.DB, access authz.AccessSet) ([]int, error)
}

func kindAttr(kind *int) map[string]string {
	if kind == nil {
		return nil
	}
	return map[string]string{"vehiclekind": map[int]string{1: "used", 2: "new"}[*kind]}
}

// columnShape: owner, location and dealer are columns of the lead table.
func columnShape() shape {
	return shape{
		name: "columns",
		seed: func(t *testing.T, db *gorm.DB) map[int]authz.Resource {
			t.Helper()
			require.NoError(t, db.AutoMigrate(&lead{}))
			out := map[int]authz.Resource{}
			var rows []lead
			id := 0
			for _, place := range [][2]int{{1, 10}, {1, 11}, {2, 20}} {
				for _, owner := range []*int{nil, ptr(0), ptr(1), ptr(2), ptr(3)} {
					for _, kind := range []*int{nil, ptr(1), ptr(2)} {
						id++
						rows = append(rows, lead{ID: id, DealerID: place[0], LocationID: place[1], OwnerID: owner, KindID: kind})
						res := authz.Resource{Scope: authztest.Location(place[1]), Ancestors: []authz.Scope{authztest.Dealer(place[0])}, OwnerLocation: place[1], Attrs: kindAttr(kind)}
						if owner != nil {
							res.Owner = *owner
						}
						out[id] = res
					}
				}
			}
			require.NoError(t, db.Create(&rows).Error)
			return out
		},
		query: func(db *gorm.DB, access authz.AccessSet) ([]int, error) {
			got := []int{}
			err := db.Model(&lead{}).Scopes(authzgorm.Filter(access, leadColumns)).Order("id").Pluck("id", &got).Error
			return got, err
		},
	}
}

// xlead has no owner or location column: the location is its creator's (a
// person), the owner is whoever an assignment row names (or nobody).
type xlead struct {
	ID        int `gorm:"primaryKey"`
	DealerID  int
	CreatorID int
	KindID    *int
}

type person struct {
	ID         int `gorm:"primaryKey"`
	LocationID int
}

type assignment struct {
	LeadID     int
	AssignedTo *int
}

const creatorLocation = "SELECT location_id FROM people WHERE people.id = xleads.creator_id"

var xleadColumns = authzgorm.Columns{
	Levels:            map[string]string{"dealer": "xleads.dealer_id"},
	LevelExprs:        map[string]string{"location": creatorLocation},
	OwnerLocationExpr: creatorLocation,
	// visible unless another user is assigned; "unowned is own" is the legacy lead rule
	OwnerMatch: func(r authzgorm.OwnerRequest) (authzgorm.Expr, bool) {
		if r.UnownedIsOwn {
			return authzgorm.Expr{
				SQL:  "xleads.id NOT IN (SELECT lead_id FROM assignments WHERE assigned_to IS NOT NULL AND assigned_to != ?)",
				Args: []any{r.UserID},
			}, true
		}
		return authzgorm.Expr{SQL: "xleads.id IN (SELECT lead_id FROM assignments WHERE assigned_to = ?)", Args: []any{r.UserID}}, true
	},
	Attrs: map[string]string{"vehiclekind": "CASE xleads.kind_id WHEN 1 THEN 'used' WHEN 2 THEN 'new' END"},
}

// expressionShape: the same leads, but through other tables.
func expressionShape() shape {
	return shape{
		name: "expressions",
		seed: func(t *testing.T, db *gorm.DB) map[int]authz.Resource {
			t.Helper()
			require.NoError(t, db.AutoMigrate(&xlead{}, &person{}, &assignment{}))
			creators := map[int]int{10: 101, 11: 102, 20: 103}
			require.NoError(t, db.Create(&[]person{{ID: 101, LocationID: 10}, {ID: 102, LocationID: 11}, {ID: 103, LocationID: 20}}).Error)
			out := map[int]authz.Resource{}
			id := 0
			// -1: no assignment row at all; 0: a row with a NULL assignee
			for _, place := range [][2]int{{1, 10}, {1, 11}, {2, 20}} {
				for _, assignee := range []int{-1, 0, 1, 2, 3} {
					for _, kind := range []*int{nil, ptr(1), ptr(2)} {
						id++
						require.NoError(t, db.Create(&xlead{ID: id, DealerID: place[0], CreatorID: creators[place[1]], KindID: kind}).Error)
						res := authz.Resource{Scope: authztest.Location(place[1]), Ancestors: []authz.Scope{authztest.Dealer(place[0])}, OwnerLocation: place[1], Attrs: kindAttr(kind)}
						switch {
						case assignee == 0:
							require.NoError(t, db.Create(&assignment{LeadID: id}).Error)
						case assignee > 0:
							require.NoError(t, db.Create(&assignment{LeadID: id, AssignedTo: ptr(assignee)}).Error)
							res.Owner = assignee
						}
						out[id] = res
					}
				}
			}
			return out
		},
		query: func(db *gorm.DB, access authz.AccessSet) ([]int, error) {
			got := []int{}
			err := db.Model(&xlead{}).Scopes(authzgorm.Filter(access, xleadColumns)).Order("xleads.id").Pluck("xleads.id", &got).Error
			return got, err
		},
	}
}

type binding struct {
	role  string
	scope authz.Scope
}

// TestFilterAgreesWithRequireOn is the property that matters: for every
// principal and every row, the row is returned by the filtered query exactly
// when RequireOn allows it, whether owner and location are plain columns or
// trusted SQL expressions over other tables.
func TestFilterAgreesWithRequireOn(t *testing.T) {
	cases := map[string][]binding{
		"no bindings":                       nil,
		"own at dealer 1":                   {{"own", authztest.Dealer(1)}},
		"own at location 10":                {{"own", authztest.Location(10)}},
		"own location at dealer 1":          {{"ownloc", authztest.Dealer(1)}},
		"own location at location 11":       {{"ownloc", authztest.Location(11)}},
		"all at dealer 2":                   {{"all", authztest.Dealer(2)}},
		"all at location 10":                {{"all", authztest.Location(10)}},
		"all at organization":               {{"all", authztest.Org()}},
		"used at dealer 1":                  {{"used", authztest.Dealer(1)}},
		"new own at dealer 1":               {{"newown", authztest.Dealer(1)}},
		"own at dealer 1 + all at loc 20?":  {{"own", authztest.Dealer(1)}, {"all", authztest.Location(20)}},
		"own + all at location 10":          {{"own", authztest.Dealer(1)}, {"all", authztest.Location(10)}},
		"used + new own at dealer 1":        {{"used", authztest.Dealer(1)}, {"newown", authztest.Dealer(1)}},
		"own location + used at dealer 1":   {{"ownloc", authztest.Dealer(1)}, {"used", authztest.Location(11)}},
		"everything at two different sites": {{"all", authztest.Location(10)}, {"all", authztest.Location(20)}},
	}
	principals := []struct {
		user, location int
	}{{1, 10}, {2, 11}, {3, 20}, {4, 0}}

	for _, sh := range []shape{columnShape(), expressionShape()} {
		t.Run(sh.name, func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
			require.NoError(t, err)
			resources := sh.seed(t, db)

			nonEmpty, partial := 0, 0
			for name, bindings := range cases {
				for _, pr := range principals {
					t.Run(fmt.Sprintf("%s/user%d", name, pr.user), func(t *testing.T) {
						w := authztest.NewWorld(t, catalogue(t), roles()...)
						w.Places.AddDealer(1).AddDealer(2).AddLocation(10, 1).AddLocation(11, 1).AddLocation(20, 2)
						// the owner IDs in the data are 1..3; user 4 owns nothing
						principal := authz.User(pr.user, pr.location)
						for _, b := range bindings {
							w.Bind(principal.Subject, b.role, b.scope)
						}
						ctx := w.As(principal)

						access, err := w.Engine.Access(ctx, "crm.lead:view")
						require.NoError(t, err)
						got, err := sh.query(db, access)
						require.NoError(t, err)

						want := []int{}
						for id := 1; id <= len(resources); id++ {
							if w.Engine.RequireOn(ctx, "crm.lead:view", resources[id]) == nil {
								want = append(want, id)
							}
						}
						assert.Equal(t, want, got)
						if len(got) > 0 {
							nonEmpty++
						}
						if len(got) > 0 && len(got) < len(resources) {
							partial++
						}
					})
				}
			}
			// guard against a vacuous comparison: most cases return some rows, many a strict subset
			assert.Greater(t, nonEmpty, 30)
			assert.Greater(t, partial, 30)
		})
	}
}

func dryRun(t *testing.T, access authz.AccessSet, cols authzgorm.Columns) (string, []any) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{DryRun: true})
	require.NoError(t, err)
	stmt := db.Model(&lead{}).Scopes(authzgorm.Filter(access, cols)).Find(&[]lead{}).Statement
	return stmt.SQL.String(), stmt.Vars
}

func TestFilterSQL(t *testing.T) {
	w := authztest.NewWorld(t, catalogue(t), roles()...)
	w.Places.AddDealer(1).AddDealer(2).AddLocation(10, 1)
	access := func(principal authz.Principal, permission string) authz.AccessSet {
		set, err := w.Engine.Access(w.As(principal), permission)
		require.NoError(t, err)
		return set
	}

	t.Run("empty access matches nothing", func(t *testing.T) {
		sql, vars := dryRun(t, access(authz.User(1, 0), "crm.lead:view"), leadColumns)
		assert.Contains(t, sql, "WHERE 1 = 0")
		assert.Empty(t, vars)
	})
	t.Run("organization, all, unconstrained: no WHERE", func(t *testing.T) {
		w.Bind(user(2), "all", authztest.Org())
		sql, _ := dryRun(t, access(authz.User(2, 0), "crm.lead:view"), leadColumns)
		assert.NotContains(t, sql, "WHERE")
	})
	t.Run("location binding pins the dealer and the location", func(t *testing.T) {
		w.Bind(user(3), "all", authztest.Location(10))
		sql, vars := dryRun(t, access(authz.User(3, 0), "crm.lead:view"), leadColumns)
		assert.Contains(t, sql, "WHERE ((`location_id` = ? AND `dealer_id` = ?))")
		assert.Equal(t, []any{10, 1}, vars)
	})
	t.Run("own, with the unassigned pool", func(t *testing.T) {
		w.Bind(user(4), "own", authztest.Dealer(1))
		sql, vars := dryRun(t, access(authz.User(4, 0), "crm.lead:view"), leadColumns)
		assert.Contains(t, sql, "(`dealer_id` = ? AND (`owner_id` = ? OR `owner_id` IS NULL OR `owner_id` = 0))")
		assert.Equal(t, []any{1, 4}, vars)
	})
	t.Run("own for a permission without the pool", func(t *testing.T) {
		sql, _ := dryRun(t, access(authz.User(4, 0), "crm.offer:view"), leadColumns)
		assert.Contains(t, sql, "(`dealer_id` = ? AND `owner_id` = ?)")
		assert.NotContains(t, sql, "IS NULL")
	})
	t.Run("own location and attribute constraint", func(t *testing.T) {
		w.Bind(user(5), "ownloc", authztest.Dealer(2))
		sql, vars := dryRun(t, access(authz.User(5, 20), "crm.lead:view"), leadColumns)
		assert.Contains(t, sql, "`location_id` = ?")
		assert.Equal(t, []any{2, 20}, vars)

		w.Bind(user(6), "used", authztest.Dealer(1))
		sql, vars = dryRun(t, access(authz.User(6, 0), "crm.lead:view"), leadColumns)
		assert.Contains(t, sql, "(CASE kind_id WHEN 1 THEN 'used' WHEN 2 THEN 'new' END) IN (?)")
		assert.Equal(t, []any{1, "used"}, vars)
	})
	t.Run("two clauses are ORed with their arguments in order", func(t *testing.T) {
		w.Bind(user(7), "own", authztest.Dealer(1))
		w.Bind(user(7), "all", authztest.Location(10))
		sql, vars := dryRun(t, access(authz.User(7, 0), "crm.lead:view"), leadColumns)
		assert.Contains(t, sql, ") OR (")
		assert.Len(t, vars, 4)
	})
}

func TestBuildFailsClosed(t *testing.T) {
	w := authztest.NewWorld(t, catalogue(t), roles()...)
	w.Places.AddDealer(1).AddLocation(10, 1)
	w.Bind(user(1), "own", authztest.Location(10))
	w.Bind(user(2), "used", authztest.Dealer(1))
	w.Bind(user(3), "ownloc", authztest.Dealer(1))
	w.Bind(user(4), "all", authztest.Dealer(1))
	set := func(id int, perm string) authz.AccessSet {
		s, err := w.Engine.Access(w.As(authz.User(id, 0)), perm)
		require.NoError(t, err)
		return s
	}

	tests := []struct {
		name string
		set  authz.AccessSet
		cols authzgorm.Columns
		none bool
	}{
		{"no owner column: own matches nothing", set(1, "crm.lead:view"), authzgorm.Columns{Levels: leadColumns.Levels, Attrs: leadColumns.Attrs}, true},
		{"no attribute expression: constrained clause matches nothing", set(2, "crm.lead:view"), authzgorm.Columns{Levels: leadColumns.Levels, Owner: "owner_id"}, true},
		{"no location column: own location matches nothing", set(3, "crm.lead:view"), authzgorm.Columns{Levels: leadColumns.Levels, Owner: "owner_id"}, true},
		{"no level columns: a scoped clause cannot be narrowed", set(4, "crm.lead:view"), authzgorm.Columns{Owner: "owner_id"}, true},
		{"only a level the chain lacks", set(4, "crm.lead:view"), authzgorm.Columns{Levels: map[string]string{"planet": "planet_id"}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cond, err := authzgorm.Build(tt.set, tt.cols, nil)
			require.NoError(t, err)
			assert.Equal(t, tt.none, cond.None)
			assert.False(t, cond.All)
		})
	}

	t.Run("a table with only a dealer column still narrows a location binding to the dealer", func(t *testing.T) {
		cond, err := authzgorm.Build(set(1, "crm.offer:view"), authzgorm.Columns{Levels: map[string]string{"dealer": "dealer_id"}, Owner: "owner_id"}, nil)
		require.NoError(t, err)
		assert.Equal(t, "((dealer_id = ? AND owner_id = ?))", cond.SQL)
	})
	t.Run("service accounts never match own", func(t *testing.T) {
		w.Bind(authz.Subject{Kind: authz.KindServiceAccount, ID: 9}, "own", authztest.Dealer(1))
		s, err := w.Engine.Access(w.As(authz.ServiceAccount(9)), "crm.lead:view")
		require.NoError(t, err)
		cond, err := authzgorm.Build(s, leadColumns, nil)
		require.NoError(t, err)
		assert.True(t, cond.None)
	})
	t.Run("a mixed set keeps the clauses that can match", func(t *testing.T) {
		w.Bind(user(5), "own", authztest.Dealer(1))
		w.Bind(user(5), "all", authztest.Location(10))
		cond, err := authzgorm.Build(set(5, "crm.lead:view"), authzgorm.Columns{Levels: leadColumns.Levels}, nil) // no owner column
		require.NoError(t, err)
		assert.Equal(t, "((location_id = ? AND dealer_id = ?))", cond.SQL)
	})
}

func TestBuildRejectsUnsafeColumns(t *testing.T) {
	for name, cols := range map[string]authzgorm.Columns{
		"level":          {Levels: map[string]string{"dealer": "dealer_id; DROP TABLE x"}},
		"owner":          {Owner: "owner_id OR 1=1"},
		"owner location": {OwnerLocation: "a b"},
		"empty attr":     {Attrs: map[string]string{"k": " "}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := authzgorm.Build(authz.AccessSet{}, cols, nil)
			assert.Error(t, err)
		})
	}
	// through Filter the query gets an error and matches nothing
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&lead{}))
	var got []lead
	res := db.Scopes(authzgorm.Filter(authz.AccessSet{}, authzgorm.Columns{Owner: "x; y"})).Find(&got)
	assert.Error(t, res.Error)
	assert.Empty(t, got)
}

func user(id int) authz.Subject { return authz.Subject{Kind: authz.KindUser, ID: id} }

func TestExpressionColumnsSQLAndValidation(t *testing.T) {
	w := authztest.NewWorld(t, catalogue(t), roles()...)
	w.Places.AddDealer(1).AddLocation(10, 1)
	w.Bind(user(1), "own", authztest.Location(10))
	w.Bind(user(2), "ownloc", authztest.Dealer(1))
	set := func(id int) authz.AccessSet {
		s, err := w.Engine.Access(w.As(authz.User(id, 10)), "crm.lead:view")
		require.NoError(t, err)
		return s
	}

	t.Run("own through an arbitrary predicate, arguments in order", func(t *testing.T) {
		cond, err := authzgorm.Build(set(1), xleadColumns, nil)
		require.NoError(t, err)
		assert.Equal(t, "(((SELECT location_id FROM people WHERE people.id = xleads.creator_id) = ? AND xleads.dealer_id = ? AND (xleads.id NOT IN (SELECT lead_id FROM assignments WHERE assigned_to IS NOT NULL AND assigned_to != ?))))", cond.SQL)
		assert.Equal(t, []any{10, 1, 1}, cond.Args)
	})
	t.Run("own location through an expression", func(t *testing.T) {
		cond, err := authzgorm.Build(set(2), xleadColumns, nil)
		require.NoError(t, err)
		assert.Contains(t, cond.SQL, "(SELECT location_id FROM people WHERE people.id = xleads.creator_id) = ?")
		assert.Equal(t, []any{1, 10}, cond.Args)
	})
	t.Run("an owner predicate that cannot be expressed matches nothing", func(t *testing.T) {
		for name, match := range map[string]func(authzgorm.OwnerRequest) (authzgorm.Expr, bool){
			"declines": func(authzgorm.OwnerRequest) (authzgorm.Expr, bool) { return authzgorm.Expr{}, false },
			"empty":    func(authzgorm.OwnerRequest) (authzgorm.Expr, bool) { return authzgorm.Expr{SQL: " "}, true },
			"placeholder count": func(authzgorm.OwnerRequest) (authzgorm.Expr, bool) {
				return authzgorm.Expr{SQL: "a = ? AND b = ?", Args: []any{1}}, true
			},
			"too many arguments": func(authzgorm.OwnerRequest) (authzgorm.Expr, bool) {
				return authzgorm.Expr{SQL: "a = 1", Args: []any{1}}, true
			},
		} {
			cols := xleadColumns
			cols.OwnerMatch = match
			cond, err := authzgorm.Build(set(1), cols, nil)
			require.NoError(t, err, name)
			assert.True(t, cond.None, name)
		}
	})
	t.Run("the owner request carries the principal and the pool flag", func(t *testing.T) {
		var got authzgorm.OwnerRequest
		cols := xleadColumns
		cols.OwnerMatch = func(r authzgorm.OwnerRequest) (authzgorm.Expr, bool) {
			got = r
			return authzgorm.Expr{SQL: "1 = 1"}, true
		}
		_, err := authzgorm.Build(set(1), cols, nil)
		require.NoError(t, err)
		assert.Equal(t, authzgorm.OwnerRequest{UserID: 1, UnownedIsOwn: true}, got)
	})
	t.Run("a plain field and its expression together, or a placeholder in a value expression, are rejected", func(t *testing.T) {
		for name, cols := range map[string]authzgorm.Columns{
			"level twice":          {Levels: map[string]string{"dealer": "d"}, LevelExprs: map[string]string{"dealer": "SELECT 1"}},
			"owner twice":          {Owner: "o", OwnerMatch: xleadColumns.OwnerMatch},
			"location twice":       {OwnerLocation: "l", OwnerLocationExpr: "SELECT 1"},
			"placeholder in level": {LevelExprs: map[string]string{"dealer": "SELECT ?"}},
			"placeholder in loc":   {OwnerLocationExpr: "SELECT ?"},
			"empty level expr":     {LevelExprs: map[string]string{"dealer": " "}},
			"unsafe plain owner":   {Owner: "o; DROP"},
		} {
			_, err := authzgorm.Build(authz.AccessSet{}, cols, nil)
			assert.Error(t, err, name)
		}
	})
}
