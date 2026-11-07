//nolint:exhaustruct,exhaustruct_v5,varnamelen,goconst,funlen,unqueryvet,thelper,lll // tests in the style of the package's others
package gormstore_test

import (
	"fmt"
	"io/fs"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/characteristics/core"
	"github.com/wssto2/go-core/characteristics/gormstore"
	"github.com/wssto2/go-core/characteristics/migrations"
	"github.com/wssto2/go-core/characteristics/storetest"
	"gorm.io/gorm"
)

// A shape is a set of tables as a project has them, created from DDL in a scratch database, with the description
// the project would write of them.
type shape struct {
	name        string
	tree        core.Tree
	tables      []string // drop order: children first
	create      func(t *testing.T, db *gorm.DB)
	undescribed map[int]map[string]any
	unset       string
}

func (s shape) harness(t *testing.T, db *gorm.DB) storetest.Harness {
	tree, err := s.tree.Prepare()
	require.NoError(t, err)

	table := func(level int) string { return tree.Levels[level-1].Table }
	idColumn := func(level int) string { return tree.Levels[level-1].ID }

	return storetest.Harness{
		Tree: tree,
		New: func(t *testing.T) core.Store {
			for _, name := range s.tables {
				require.NoError(t, db.Exec("DROP TABLE IF EXISTS `"+name+"`").Error)
			}

			s.create(t, db)

			store := gormstore.New(db, tree)
			require.NoError(t, store.Check(t.Context()), "the description names columns the DDL lacks")

			return store
		},
		Raw: func(t *testing.T, level, id int) map[string]string {
			var rows []map[string]any

			require.NoError(t, db.Raw("SELECT * FROM `"+table(level)+"` WHERE `"+idColumn(level)+"` = ?", id).Scan(&rows).Error)
			require.Len(t, rows, 1)

			out := map[string]string{}

			for column, v := range rows[0] {
				switch typed := unwrapRaw(v).(type) {
				case nil:
					out[column] = "NULL"
				case []byte:
					out[column] = string(typed)
				case time.Time:
					out[column] = typed.Format("2006-01-02 15:04:05")
				default:
					out[column] = fmt.Sprint(typed)
				}
			}

			return out
		},
		Poke: func(t *testing.T, level, id int, column string, value any) {
			require.NoError(t, db.Exec("UPDATE `"+table(level)+"` SET `"+column+"` = ? WHERE `"+idColumn(level)+"` = ?", value, id).Error)
		},
		Undescribed: s.undescribed,
		Unset:       s.unset,
	}
}

// goCore: the default tables of the module, plus two columns of the project's own: one described for a branch, one not
// described at all.
func goCore() shape {
	tree := core.DefaultTree(3)
	tree.Levels[2].Fields = []core.Field{{
		Column: "code", Label: core.Text{HR: "Kod", EN: "Code"}, Type: core.TypeText, Max: 20, Null: true,
		Under: &core.Under{Level: 2, ID: 2},
	}}
	tree.Levels[1].Fields = []core.Field{{Column: "slug", Label: core.Text{HR: "Slug", EN: "Slug"}, Type: core.TypeText, Max: 30, Null: true, InList: true}}

	return shape{
		name:   "gocore",
		tree:   tree,
		tables: []string{"characteristics_level3", "characteristics_level2", "characteristics_level1"},
		create: func(t *testing.T, db *gorm.DB) {
			if db.Name() == "sqlite" {
				require.NoError(t, gormstore.Migrate(db, 3))
			} else {
				entries, err := fs.ReadDir(migrations.Files, ".")
				require.NoError(t, err)

				for _, entry := range entries {
					raw, err := fs.ReadFile(migrations.Files, entry.Name())
					require.NoError(t, err)
					require.NoError(t, db.Exec(string(raw)).Error)
				}
			}

			require.NoError(t, db.Exec("ALTER TABLE characteristics_level3 ADD COLUMN code varchar(20) NULL").Error)
			require.NoError(t, db.Exec("ALTER TABLE characteristics_level3 ADD COLUMN legacy_note varchar(50) NULL").Error)
			require.NoError(t, db.Exec("ALTER TABLE characteristics_level2 ADD COLUMN slug varchar(30) NULL").Error)
		},
		undescribed: map[int]map[string]any{3: {"legacy_note": "keep-me"}},
	}
}

const laravelTail = `
  name_i18n longtext CHARACTER SET utf8mb4 COLLATE utf8mb4_bin,
  meta longtext CHARACTER SET utf8mb4 COLLATE utf8mb4_bin,
  ` + "`order`" + ` int unsigned NOT NULL DEFAULT '1',
  active tinyint(1) NOT NULL DEFAULT '0',
  created_by int unsigned DEFAULT NULL,
  updated_by int unsigned DEFAULT NULL,
  activated_by int unsigned DEFAULT NULL,
  deactivated_by int unsigned DEFAULT NULL,
  created_at timestamp NULL DEFAULT NULL,
  updated_at timestamp NULL DEFAULT NULL,
  activated_at timestamp NULL DEFAULT NULL,
  deactivated_at timestamp NULL DEFAULT NULL,
  PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`

// siteBuilder: characteristics_level1/2/3 as site-builder's Laravel migrations made them (DDL read from a dump's
// SHOW CREATE TABLE; no data). Level 3 keeps level1_id as well as level2_id, and the DWS brand code, additional_3, is
// described for the branch under level 2's second row, read-only. additional_4 is left out of the description.
func siteBuilder() shape {
	laravel := core.DefaultAudit()
	levelOf := func(n int, parent string, ancestors []string, fields []core.Field) core.Level {
		return core.Level{
			Label: core.Text{HR: fmt.Sprintf("Razina %d", n)}, Table: fmt.Sprintf("characteristics_level%d", n), Parent: parent, Ancestors: ancestors,
			Name: "name", NameMax: 50, Translations: core.Translations{JSON: "name_i18n"}, Order: "order", Active: "active", Meta: "meta",
			Audit: laravel, Fields: fields,
		}
	}

	level3 := []core.Field{
		{Column: "country", Label: core.Text{HR: "Zemlja"}, Type: core.TypeText, Max: 4, Null: true, InList: true},
		{Column: "additional_1", Label: core.Text{HR: "ARV ID"}, Type: core.TypeNumber, Null: true},
		{Column: "additional_2", Label: core.Text{HR: "Vrsta države"}, Type: core.TypeChoice, Null: true,
			Choices: []core.Choice{{Value: "0", Label: core.Text{HR: "Zemlje aplikacije"}}, {Value: "1", Label: core.Text{HR: "Samo prebivalište"}}}},
		{Column: "additional_3", Label: core.Text{HR: "Kod marke"}, Type: core.TypeText, Max: 20, Null: true, ReadOnly: true, InList: true, Under: &core.Under{Level: 2, ID: 2}},
	}

	return shape{
		name: "sitebuilder",
		tree: core.Tree{Levels: []core.Level{
			levelOf(1, "", nil, nil),
			levelOf(2, "level1_id", nil, []core.Field{{Column: "additional_1", Label: core.Text{HR: "Oznaka kategorije"}, Type: core.TypeText, Null: true, InList: true}}),
			levelOf(3, "level2_id", []string{"level1_id"}, level3),
		}},
		tables: []string{"characteristics_level3", "characteristics_level2", "characteristics_level1"},
		create: func(t *testing.T, db *gorm.DB) {
			ddl := []string{
				"CREATE TABLE characteristics_level1 (id bigint unsigned NOT NULL AUTO_INCREMENT, name varchar(50) COLLATE utf8mb4_unicode_ci NOT NULL," + laravelTail,
				"CREATE TABLE characteristics_level2 (id bigint unsigned NOT NULL AUTO_INCREMENT, level1_id smallint unsigned NOT NULL, name varchar(50) COLLATE utf8mb4_unicode_ci NOT NULL," +
					" additional_1 varchar(255) COLLATE utf8mb4_unicode_ci DEFAULT NULL," + laravelTail,
				"CREATE TABLE characteristics_level3 (id bigint unsigned NOT NULL AUTO_INCREMENT, level1_id smallint unsigned NOT NULL, level2_id smallint unsigned NOT NULL," +
					" country varchar(4) COLLATE utf8mb4_unicode_ci DEFAULT NULL, name varchar(50) COLLATE utf8mb4_unicode_ci NOT NULL," +
					" additional_1 int unsigned DEFAULT NULL, additional_2 smallint unsigned DEFAULT NULL," +
					" additional_3 varchar(20) COLLATE utf8mb4_unicode_ci DEFAULT NULL, additional_4 varchar(50) COLLATE utf8mb4_unicode_ci DEFAULT NULL," + laravelTail,
			}
			for _, statement := range ddl {
				require.NoError(t, db.Exec(statement).Error)
			}
		},
		undescribed: map[int]map[string]any{3: {"additional_4": "keep-me"}},
	}
}

const arvAudit = `
  unio smallint unsigned NOT NULL,
  edit smallint unsigned NOT NULL,
  datum_unos datetime NOT NULL DEFAULT '1000-01-01 00:00:00',
  datum_edit datetime NOT NULL DEFAULT '1000-01-01 00:00:00',
  datum_akt datetime NOT NULL DEFAULT '1000-01-01 00:00:00',
  datum_deakt datetime NOT NULL DEFAULT '1000-01-01 00:00:00',
  PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3`

// arvNext: karakteristike_marka, _kat, karakteristike and _det with the columns of arv-next's GORM models
// (internal/infrastructure/db/gen). Their zero-date defaults ('0000-00-00') cannot be created on MySQL 9, so a date
// that was never set is 1000-01-01 here. Names are Croatian, translations are one column per language, and level 4 has
// no active flag.
func arvNext() shape {
	legacy := core.Audit{
		CreatedBy: "unio", UpdatedBy: "edit", CreatedAt: "datum_unos", UpdatedAt: "datum_edit", ActivatedAt: "datum_akt", DeactivatedAt: "datum_deakt",
		UpdatedByOnCreate: true,
	}
	text := func(column string, size int, null bool) core.Field {
		return core.Field{Column: column, Label: core.Text{HR: column}, Type: core.TypeText, Max: size, Null: null}
	}
	yesno := func(column string) core.Field {
		return core.Field{Column: column, Label: core.Text{HR: column}, Type: core.TypeYesNo}
	}

	level3 := []core.Field{text("code", 50, true)}

	for i := 1; i <= 10; i++ {
		size := 50
		if i == 2 {
			size = 255
		}

		f := text(fmt.Sprintf("oznaka_%d", i), size, false)
		if i == 4 { // colours: the hex code, for the branch under category 2
			f.Under = &core.Under{Level: 2, ID: 2}
		}

		level3 = append(level3, f)
	}

	level3 = append(level3, text("opis", 1000, false), text("opis_2", 1000, false), yesno("disabled"))

	return shape{
		name: "arvnext",
		tree: core.Tree{Languages: []string{"hr", "en", "si", "rs"}, Levels: []core.Level{
			{Label: core.Text{HR: "Marka"}, Table: "karakteristike_marka", Name: "naziv", NameMax: 50, Order: "rbr", Active: "active", Audit: legacy, Fields: []core.Field{text("code", 50, true)}},
			{Label: core.Text{HR: "Kategorija"}, Table: "karakteristike_kat", Parent: "marka_id", Name: "naziv", NameMax: 100, Order: "rbr", Active: "active", Audit: legacy,
				Fields: []core.Field{text("code", 50, true), text("oznaka_1", 50, true), text("oznaka_2", 30, true), yesno("oznaka_3")}},
			{Label: core.Text{HR: "Karakteristika"}, Table: "karakteristike", Parent: "kat_id", Ancestors: []string{"marka_id"}, Name: "naziv", NameMax: 100,
				Translations: core.Translations{Columns: map[string]string{"hr": "prijevod_hr", "en": "prijevod_en", "si": "prijevod_si", "rs": "prijevod_rs"}},
				Order:        "rbr", Active: "active", Audit: legacy, Fields: level3},
			{Label: core.Text{HR: "Detalj"}, Table: "karakteristike_det", Parent: "kar_id", Ancestors: []string{"marka_id", "kat_id"}, Name: "naziv", NameMax: 50, Order: "rbr",
				Audit: core.Audit{CreatedBy: "unio", UpdatedBy: "edit", CreatedAt: "datum_unos", UpdatedAt: "datum_edit", UpdatedByOnCreate: true},
				Fields: []core.Field{text("code", 50, true), text("naziv_renault", 50, true), text("naziv_dacia", 50, true), text("naziv_nissan", 50, true),
					yesno("naziv_po_marki"), {Column: "code_additional", Label: core.Text{HR: "Dodatni kod"}, Type: core.TypeNumber, Null: true}}},
		}},
		tables: []string{"karakteristike_det", "karakteristike", "karakteristike_kat", "karakteristike_marka"},
		create: func(t *testing.T, db *gorm.DB) {
			var oznake []string

			for i := 1; i <= 10; i++ {
				size := 50
				if i == 2 {
					size = 255
				}

				oznake = append(oznake, fmt.Sprintf("oznaka_%d varchar(%d) NOT NULL", i, size))
			}

			ddl := []string{
				"CREATE TABLE karakteristike_marka (id int NOT NULL AUTO_INCREMENT, code varchar(50) DEFAULT NULL, naziv varchar(50) NOT NULL, rbr tinyint unsigned NOT NULL," +
					" active enum('0','1') NOT NULL DEFAULT '0'," + arvAudit,
				"CREATE TABLE karakteristike_kat (id int NOT NULL AUTO_INCREMENT, marka_id tinyint unsigned NOT NULL, code varchar(50) DEFAULT NULL, naziv varchar(100) NOT NULL," +
					" oznaka_1 varchar(50) DEFAULT NULL, oznaka_2 varchar(30) DEFAULT NULL, oznaka_3 tinyint unsigned NOT NULL, rbr int unsigned NOT NULL," +
					" active enum('0','1') NOT NULL DEFAULT '0'," + arvAudit,
				"CREATE TABLE karakteristike (id int NOT NULL AUTO_INCREMENT, marka_id tinyint unsigned NOT NULL, kat_id int unsigned NOT NULL, code varchar(50) DEFAULT NULL," +
					" naziv varchar(100) NOT NULL, " + strings.Join(oznake, ", ") +
					", opis text NOT NULL, opis_2 text NOT NULL, meta text, prijevod_en varchar(250) DEFAULT NULL, prijevod_si varchar(250) DEFAULT NULL," +
					" prijevod_rs varchar(250) DEFAULT NULL, prijevod_hr varchar(250) DEFAULT NULL, active enum('0','1') NOT NULL DEFAULT '0'," +
					" disabled tinyint unsigned NOT NULL, rbr int unsigned NOT NULL," + arvAudit,
				"CREATE TABLE karakteristike_det (id int NOT NULL AUTO_INCREMENT, marka_id tinyint unsigned NOT NULL, kat_id int unsigned NOT NULL, kar_id int unsigned NOT NULL," +
					" naziv varchar(50) NOT NULL, naziv_po_marki tinyint unsigned NOT NULL, naziv_renault varchar(50) DEFAULT NULL, naziv_dacia varchar(50) DEFAULT NULL," +
					" naziv_nissan varchar(50) DEFAULT NULL, code varchar(50) DEFAULT NULL, code_additional int unsigned DEFAULT NULL, vendor_code varchar(10) DEFAULT NULL," +
					" oznaka_1 tinyint unsigned DEFAULT NULL, marke_vozila_id int unsigned DEFAULT NULL, rbr smallint unsigned NOT NULL, unio smallint unsigned DEFAULT NULL," +
					" edit smallint unsigned DEFAULT NULL, datum_unos datetime DEFAULT NULL, datum_edit datetime DEFAULT NULL, PRIMARY KEY (id)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3",
			}
			for _, statement := range ddl {
				require.NoError(t, db.Exec(statement).Error)
			}
		},
		undescribed: map[int]map[string]any{3: {"meta": `{"keep":1}`}, 4: {"marke_vozila_id": 77, "vendor_code": "VC"}},
		unset:       "1000-01-01 00:00:00",
	}
}

// unwrapRaw reads through the pointers GORM scans a column into.
func unwrapRaw(v any) any {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil
		}

		return unwrapRaw(rv.Elem().Interface())
	}

	return v
}
