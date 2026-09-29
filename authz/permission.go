package authz

import (
	"fmt"
	"regexp"
	"strings"
)

// word is one segment of a permission identifier: a single lowercase word.
var word = regexp.MustCompile(`^[a-z][a-z0-9]*$`)

// maxResourceSegments is the most segments allowed before the colon:
// module.resource.section.
const maxResourceSegments = 3

// Permission is one entry of the catalogue. Build it with Catalogue.Define.
type Permission struct {
	// ID is "module.resource[.section]:verb"; every segment is one lowercase word.
	ID string `json:"id"`
	// LabelKey and DescriptionKey are i18n keys; go-core never holds the texts.
	LabelKey       string `json:"label_key,omitempty"`
	DescriptionKey string `json:"description_key,omitempty"`
	// Sensitive marks permissions an editor should flag (delete, billed calls, ...).
	Sensitive bool `json:"sensitive"`
	// System marks permissions that run the system rather than the business.
	// Computed roles built with AllExcept(IsSystem) leave them out, so delegation
	// keeps them from being given away by anyone who does not hold them.
	System bool `json:"system"`
	// OrganizationOnly permissions only take effect in a binding at the
	// hierarchy's root, and a role holding them cannot be bound below it.
	OrganizationOnly bool `json:"organization_only"`
	// Requires lists permissions a role must also grant (at least as widely,
	// where both are ownable).
	Requires []string `json:"requires,omitempty"`
	// RecordType is set for ownable permissions: the record type whose owner the
	// Own and OwnLocation qualifiers refer to. Empty means qualifiers are not
	// allowed.
	RecordType string `json:"record_type,omitempty"`
	// UnownedIsOwn makes records without an owner count as the user's own (a
	// lead pool anyone may pick from). Only meaningful with RecordType.
	UnownedIsOwn bool `json:"unowned_is_own,omitempty"`
	// Feature, when set, gates the permission on the tenant having the feature.
	Feature string `json:"feature,omitempty"`
	// Attributes lists the role attribute keys that constrain this permission
	// (for example "vehiclekind"). A role attribute not listed here does not
	// apply to the permission.
	Attributes []string `json:"attributes,omitempty"`
}

// Module is the first segment of the identifier.
func (p Permission) Module() string {
	module, _, _ := strings.Cut(p.Namespace(), ".")
	return module
}

// Ownable reports whether the Own and OwnLocation qualifiers apply.
func (p Permission) Ownable() bool { return p.RecordType != "" }

// Verb is the segment after the colon.
func (p Permission) Verb() string { return p.ID[strings.IndexByte(p.ID, ':')+1:] }

// Namespace is everything before the colon: "module.resource[.section]".
func (p Permission) Namespace() string { return p.ID[:strings.IndexByte(p.ID, ':')] }

// DefineOption configures a Permission in Catalogue.Define.
type DefineOption func(*Permission)

// Label sets the i18n key of the permission's label.
func Label(key string) DefineOption { return func(p *Permission) { p.LabelKey = key } }

// Description sets the i18n key of the permission's description.
func Description(key string) DefineOption { return func(p *Permission) { p.DescriptionKey = key } }

// Sensitive flags the permission for editors and audit reviews.
func Sensitive() DefineOption { return func(p *Permission) { p.Sensitive = true } }

// System marks a permission only the people running the system should hold.
func System() DefineOption { return func(p *Permission) { p.System = true } }

// OrganizationOnly restricts the permission to bindings at the hierarchy root.
func OrganizationOnly() DefineOption { return func(p *Permission) { p.OrganizationOnly = true } }

// Requires names permissions a role holding this one must also grant.
func Requires(ids ...string) DefineOption {
	return func(p *Permission) { p.Requires = append(p.Requires, ids...) }
}

// Ownable makes the permission apply to records of recordType that have an
// owner, so a grant may carry the Own or OwnLocation qualifier.
func Ownable(recordType string) DefineOption {
	return func(p *Permission) { p.RecordType = recordType }
}

// UnownedIsOwn makes records with no owner count as the user's own for this
// (ownable) permission.
func UnownedIsOwn() DefineOption { return func(p *Permission) { p.UnownedIsOwn = true } }

// Feature gates the permission on a tenant feature (see FeatureResolver).
func Feature(name string) DefineOption { return func(p *Permission) { p.Feature = name } }

// Attributes lists the role attribute keys that constrain this permission.
func Attributes(keys ...string) DefineOption {
	return func(p *Permission) { p.Attributes = append(p.Attributes, keys...) }
}

// checkIdentifier validates "module.resource[.section]:verb".
func checkIdentifier(id string) error {
	ns, verb, ok := strings.Cut(id, ":")
	if !ok || strings.Contains(verb, ":") {
		return fmt.Errorf("identifier %q must look like module.resource:verb", id)
	}
	if !word.MatchString(verb) {
		return fmt.Errorf("identifier %q: verb %q must be a single lowercase word", id, verb)
	}
	segments := strings.Split(ns, ".")
	if len(segments) > maxResourceSegments {
		return fmt.Errorf("identifier %q: at most %d segments before the colon", id, maxResourceSegments)
	}
	for _, s := range segments {
		if !word.MatchString(s) {
			return fmt.Errorf("identifier %q: segment %q must be a single lowercase word", id, s)
		}
	}
	return nil
}

// IsWord reports whether s is a single lowercase word (letters and digits,
// starting with a letter). Attribute keys, feature names and levels use it.
func IsWord(s string) bool { return word.MatchString(s) }
