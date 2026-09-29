package authztest

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/wssto2/go-core/authz"
)

//go:embed testdata/dealership.json
var dealershipJSON []byte

// Attribute keys and values of the Dealership sample.
const (
	AttrVehicleKind = "vehiclekind"
	VehicleUsed     = "used"
	VehicleNew      = "new"
)

// Sample is a realistic catalogue with predefined roles.
type Sample struct {
	Catalogue *authz.Catalogue
	// Roles are the ten predefined job roles plus two computed ones,
	// "webmaster" (everything) and "importer" (everything but System).
	Roles []authz.Role
	// Modules maps a permission to the module label it was declared under.
	Modules map[string]string
}

type dealershipFile struct {
	// Catalogue rows: module, screen, permission, label, flags. Flags: s
	// sensitive, y system, g organization-only, o ownable.
	Catalogue [][]string `json:"catalogue"`
	Roles     map[string]struct {
		Name     string            `json:"name"`
		Scope    string            `json:"scope"`
		Vehicles string            `json:"vehicles"`
		Grants   map[string]string `json:"grants"`
	} `json:"roles"`
}

// Dealership loads the sample: 162 permissions of a car-dealer group, and roles
// for its jobs (a salesperson for used cars, one for new cars, a manager, a
// back office, ...). Every non-view permission requires its screen's view; the
// ownable ones carry Own / OwnLocation / All; roles for used or new vehicles
// carry a vehiclekind constraint. The Sample is built fresh on each call.
func Dealership() (*Sample, error) {
	var f dealershipFile
	if err := json.Unmarshal(dealershipJSON, &f); err != nil {
		return nil, fmt.Errorf("authztest: dealership fixture: %w", err)
	}
	present := map[string]bool{}
	for _, row := range f.Catalogue {
		present[row[2]] = true
	}
	cat := authz.NewCatalogue()
	modules := map[string]string{}
	for _, row := range f.Catalogue {
		module, id, label, flags := row[0], row[2], row[3], row[4]
		ns, verb, _ := strings.Cut(id, ":")
		opts := []authz.DefineOption{authz.Label(id + ".label"), authz.Description(label)}
		if view := ns + ":view"; verb != "view" && present[view] {
			opts = append(opts, authz.Requires(view))
		}
		if strings.Contains(flags, "s") {
			opts = append(opts, authz.Sensitive())
		}
		if strings.Contains(flags, "y") {
			opts = append(opts, authz.System())
		}
		if strings.Contains(flags, "g") {
			opts = append(opts, authz.OrganizationOnly())
		}
		if strings.Contains(flags, "o") {
			opts = append(opts, authz.Ownable(ns))
			if ns == "crm.lead" {
				opts = append(opts, authz.UnownedIsOwn())
			}
			if ns == "crm.lead" || ns == "crm.offer" || ns == "crm.contract" { // record types the vehicle kind narrows
				opts = append(opts, authz.Attributes(AttrVehicleKind))
			}
		}
		if err := cat.Define(id, opts...); err != nil {
			return nil, err
		}
		modules[id] = module
	}
	if err := cat.Validate(); err != nil {
		return nil, err
	}

	qualifiers := map[string]authz.Qualifier{"A": authz.QualifierAll, "L": authz.QualifierOwnLocation, "S": authz.QualifierOwn}
	roles := []authz.Role{
		authz.ComputedRole("webmaster", "Webmaster", authz.All()),
		authz.ComputedRole("importer", "Importer", authz.AllExcept(authz.IsSystem)),
	}
	for _, key := range slices.Sorted(maps.Keys(f.Roles)) {
		r := f.Roles[key]
		role := authz.Role{Key: key, Name: r.Name}
		for _, perm := range slices.Sorted(maps.Keys(r.Grants)) {
			role.Grants = append(role.Grants, authz.Grant{Permission: perm, Qualifier: qualifiers[r.Grants[perm]]})
		}
		switch r.Vehicles {
		case "Rabljena":
			role.Attrs = map[string][]string{AttrVehicleKind: {VehicleUsed}}
		case "Nova":
			role.Attrs = map[string][]string{AttrVehicleKind: {VehicleNew}}
		}
		roles = append(roles, role)
	}
	return &Sample{Catalogue: cat, Roles: roles, Modules: modules}, nil
}

// MustDealership is Dealership for tests: it panics on error.
func MustDealership() *Sample {
	s, err := Dealership()
	if err != nil {
		panic(err)
	}
	return s
}
