# Logic IDs: CHARACTERISTICS-DESC-001, CHARACTERISTICS-STORE-001, CHARACTERISTICS-PERM-001, CHARACTERISTICS-OPTIONS-001, CHARACTERISTICS-DELETE-001

Title: Characteristics are a tree in the project's own tables, described in code, edited by the webmaster, picked by everyone

Status: Approved (owner decisions 2026-11-07, ADR 0003 of site-builder)
Last updated: 2026-11-07
Module: `characteristics` (`characteristics/core` description and service, `characteristics/gormstore` store, `characteristics` routes)

## Summary

Reference data that editors maintain (news categories, brands, delivery countries) lives in a few tables, one per level,
with a parent column to the level above. The module does not own those tables: the project describes them in code
(`core.Tree`), the module reads and writes them as they are, and `characteristics.Default(n)` plus
`WithMigrations(n)` give a new project ready-made ones.

## CHARACTERISTICS-DESC-001 — The description is the whole contract with the tables

1. A level names its table, id, parent, name, and optionally the translations (a JSON column or one column per
   language), order, active flag, meta and the audit columns. An extra column is a `Field`: label (hr/en), type (text,
   number, yesno, choice), whether it is shown in the list, whether it is read-only, and optionally the branch it is
   for (`Under`: the rows below one row of a higher level).
2. A column the description does not name is never selected, inserted or updated. A value put there survives every
   write.
3. An invalid description (no levels, a bad name, a column used twice, a parent missing, a translation language that is
   not listed, a choice field without choices, a branch above no level...) stops start-up and lists every problem.
   Once the migrations ran, a described column the database lacks stops it too.
4. A field for a branch is read, written and shown only for the rows below that branch. A value for it on another row
   is `400`. A read-only field cannot be changed through the API (`400`), on create or update.
5. `Level.Ancestors` names the columns that hold the ids above the parent (site-builder's level 3 keeps `level1_id`
   as well as `level2_id`); they are written on create from the parent's path.

Code: `characteristics/core/description.go` (`Tree.Prepare`), `characteristics/core/input.go` (`fieldValues`),
`characteristics/gormstore/store.go` (`Store.Check`, `columns`).

## CHARACTERISTICS-STORE-001 — Rows are written as the legacy applications write them

1. **Create:** last among its siblings (`MAX(order)+1` under the parent; 1 for the first), the creator and time, the
   editor and time where the description says they are set on create (`UpdatedByOnCreate`, `UpdatedAtOnCreate`), the
   activator and time when the row starts active. Every described extra column is written, the ones not given as the
   empty value of their type (NULL for a `Null` field), because a NOT NULL column cannot be left out.
2. **Update:** the name, the given translations, the given meta and the given extra columns; the editor and time. The
   JSON translations column keeps the languages the description does not list; an empty text clears a language; nil
   keeps all.
3. **Switching on:** the activator and time, and the editor and time; the deactivation pair keeps its value. **Off:**
   the deactivator and time, and the editor and time; the activation pair keeps its value. A row already in that state
   is not touched.
4. **Order:** moving renumbers the siblings 1..n and writes only the order column of the rows whose number changed, no
   audit column, as Laravel's reorder does. It counts per parent.
5. Without an actor (0) the "by" columns are left to the database. Times are the application's clock, to the second,
   in the connection's time zone. A flag is written as the text `0` or `1`, which a `tinyint` and an `enum('0','1')`
   both read.
6. Reads are kept per tree for a minute (`WithCache`) and dropped on every write through the module.

Code: `characteristics/gormstore/store.go` (`Store.Create`, `Store.Update`, `Store.SetActive`, `Store.SetOrders`),
`characteristics/core/core.go` (`Service.Move`, `Service.snapshot`).

## CHARACTERISTICS-PERM-001 — Only the webmaster edits; any signed-in caller picks

1. `characteristics:view` reads the tree, an item, the description and the usage; `characteristics:edit` creates,
   changes, switches, orders and deletes, and needs `view`. Both have hr/en labels (`characteristics.Labels`).
2. The module defines them in the catalogue; it grants them to nobody. The application gives `characteristics.Grants()`
   to its webmaster role, or has a computed role that holds the whole catalogue.
3. `GET /v1/characteristics/options` needs a signed-in caller and no permission: editors pick characteristics in forms.

Code: `characteristics/characteristics.go` (`definePermissions`, `Grants`), `characteristics/routes.go` (`Declare`).

## CHARACTERISTICS-OPTIONS-001 — The choices of a form field

1. `GET /v1/characteristics/options?level=&parent=&value=&lang=` answers the **active** children of one parent on one
   level (no `parent` at level 1, required below it) as `{value, label}`, in the order of the tree.
2. The label is the name in `lang` (the first listed language when omitted), else the name.
3. The value is the id, or the described column `value` names (a brand code). Items whose value is empty are left out.
   A `value` that is not a described column of the level is `400`.

Code: `characteristics/core/core.go` (`Service.Options`).

## CHARACTERISTICS-DELETE-001 — Deleting what is used

1. The application may say where an item is used (`core.Usage`: "2 pages, 1 host"). `Install` requires
   `WithUsage(port)` or `WithoutUsage()`; neither stops start-up.
2. A delete is refused with `409` (reason `characteristics.in_use`, the uses in the params) while the item, or an item
   below it, is used, and with `409` (`characteristics.has_children`) while it has items below it, unless `force=true`:
   then the whole branch goes, deepest first, in one transaction.
3. Deactivating is always possible.
4. Without a port the usage answer has `known: false` and the delete answer `usage_known: false`: unused is never
   claimed.

Code: `characteristics/core/core.go` (`Service.Delete`, `Service.Usage`).
