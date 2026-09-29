package authz

// Predicate selects permissions by their metadata.
type Predicate func(Permission) bool

// Selector picks the permissions a computed role holds. It is evaluated against
// the catalogue, so a permission added later is included automatically.
type Selector = Predicate

// IsSystem matches System permissions.
func IsSystem(p Permission) bool { return p.System }

// IsSensitive matches Sensitive permissions.
func IsSensitive(p Permission) bool { return p.Sensitive }

// IsOrganizationOnly matches OrganizationOnly permissions.
func IsOrganizationOnly(p Permission) bool { return p.OrganizationOnly }

// IsView matches permissions whose verb is "view".
func IsView(p Permission) bool { return p.Verb() == "view" }

// All selects every permission (a Webmaster-style role).
func All() Selector { return func(Permission) bool { return true } }

// AllExcept selects every permission that none of the predicates match. For
// example AllExcept(IsSystem) is a role holding everything but the system
// permissions.
func AllExcept(exclude ...Predicate) Selector {
	return func(p Permission) bool {
		for _, e := range exclude {
			if e(p) {
				return false
			}
		}
		return true
	}
}

// Matching selects the permissions the predicate matches, for example a
// read-only supervisor: Matching(IsView) narrowed with AllExcept(IsSystem).
func Matching(pred Predicate) Selector { return pred }

// Both selects permissions that every selector selects.
func Both(selectors ...Selector) Selector {
	return func(p Permission) bool {
		for _, s := range selectors {
			if !s(p) {
				return false
			}
		}
		return true
	}
}

// ComputedRole builds a predefined role whose grants derive from the catalogue.
func ComputedRole(key, name string, sel Selector) Role {
	return Role{Key: key, Name: name, Computed: sel}
}
