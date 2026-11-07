//nolint:exhaustruct,exhaustruct_v5 // structs are built field by field; zero is the default
package core

import "strconv"

// DefaultLevels is how many levels the ready-made tables have.
const DefaultLevels = 3

// DefaultAudit is the audit columns of the default tables, written as Laravel writes them: the creator and time on
// create, the editor on every update (the time on create too), the activator and deactivator with their times.
func DefaultAudit() Audit {
	return Audit{
		CreatedBy: "created_by", UpdatedBy: "updated_by", ActivatedBy: "activated_by", DeactivatedBy: "deactivated_by",
		CreatedAt: "created_at", UpdatedAt: "updated_at", ActivatedAt: "activated_at", DeactivatedAt: "deactivated_at",
		UpdatedByOnCreate: false, UpdatedAtOnCreate: true,
	}
}

// DefaultTree describes the first n (1 to 3) default tables of characteristics/migrations: characteristics_level1 to
// _level3 with a name, its translations in a JSON column (hr and en), an order, an active flag, a meta object and the
// audit columns. A project that adds columns in its own migration appends their Fields to the levels it returns.
func DefaultTree(n int) Tree {
	n = min(max(n, 1), DefaultLevels)
	tree := Tree{Languages: []string{"hr", "en"}, Levels: make([]Level, n)}

	for index := range tree.Levels {
		number := index + 1
		level := Level{
			Label: Text{HR: "Razina " + strconv.Itoa(number), EN: "Level " + strconv.Itoa(number)},
			Table: "characteristics_level" + strconv.Itoa(number), ID: "id", Name: "name", NameMax: DefaultMaxText,
			Translations: Translations{JSON: "name_i18n"}, Order: "order", Active: "active", Meta: "meta", Audit: DefaultAudit(),
			Parent: "", Ancestors: nil, Fields: nil,
		}

		if number > 1 {
			level.Parent = "level" + strconv.Itoa(number-1) + "_id"
		}

		for above := 1; above < number-1; above++ {
			level.Ancestors = append(level.Ancestors, "level"+strconv.Itoa(above)+"_id")
		}

		tree.Levels[index] = level
	}

	return tree
}
