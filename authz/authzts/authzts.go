// Package authzts writes a catalogue as TypeScript: an exact string-literal
// union of the permission identifiers, a metadata object keyed by them, and the
// types of the /me/access payload. The frontend's can() and scopeOf() are
// typed by it, so a permission that does not exist fails to compile.
package authzts

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/go2ts"
)

// Options tunes the output.
type Options struct {
	// Hierarchy, when set, adds a ScopeLevel union of its levels.
	Hierarchy *authz.Hierarchy
}

// Render returns the TypeScript source for the catalogue. The output is
// deterministic (permissions sorted by identifier), so a regenerate-and-diff
// check in CI is stable.
func Render(cat *authz.Catalogue, opts Options) string {
	perms := cat.All()
	slices.SortFunc(perms, func(a, b authz.Permission) int { return strings.Compare(a.ID, b.ID) })
	ids := make([]string, len(perms))
	for i, p := range perms {
		ids[i] = p.ID
	}

	var b strings.Builder
	b.WriteString(go2ts.GeneratedHeader)
	b.WriteString("\n")
	b.WriteString(go2ts.StringUnion("Permission", ids))
	b.WriteString("\n")
	b.WriteString(go2ts.StringUnion("Qualifier", []string{
		authz.QualifierOwn.String(), authz.QualifierOwnLocation.String(), authz.QualifierAll.String(),
	}))
	if opts.Hierarchy != nil {
		b.WriteString("\n")
		b.WriteString(go2ts.StringUnion("ScopeLevel", opts.Hierarchy.Levels()))
	}
	b.WriteString(metaTypes)
	b.WriteString("\nexport const permissions: Record<Permission, PermissionMeta> = {\n")
	for _, p := range perms {
		b.WriteString(metaEntry(p))
	}
	b.WriteString("};\n")
	b.WriteString(payloadTypes(opts.Hierarchy != nil))
	return b.String()
}

// WriteFile renders the catalogue into path, creating directories as needed.
func WriteFile(cat *authz.Catalogue, path string, opts Options) error {
	// Generated source files are meant to be read by the frontend build, like go2ts output.
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec // see above
		return fmt.Errorf("authzts: create directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(Render(cat, opts)), 0o644); err != nil { //nolint:gosec // see above
		return fmt.Errorf("authzts: write %s: %w", path, err)
	}
	return nil
}

const metaTypes = `
export interface PermissionMeta {
  readonly module: string;
  readonly resource: string;
  readonly verb: string;
  /** i18n keys; the texts live in the application. */
  readonly labelKey: string;
  readonly descriptionKey: string;
  readonly sensitive: boolean;
  readonly system: boolean;
  readonly organizationOnly: boolean;
  /** The record type the Own / OwnLocation qualifiers refer to, if any. */
  readonly ownable: string | null;
  readonly unownedIsOwn: boolean;
  readonly feature: string | null;
  /** Role attribute keys that constrain this permission. */
  readonly attributes: readonly string[];
  readonly requires: readonly Permission[];
}
`

func metaEntry(p authz.Permission) string {
	resource := strings.TrimPrefix(strings.TrimPrefix(p.Namespace(), p.Module()), ".")
	return fmt.Sprintf("  %s: { module: %s, resource: %s, verb: %s, labelKey: %s, descriptionKey: %s, sensitive: %t, system: %t, organizationOnly: %t, ownable: %s, unownedIsOwn: %t, feature: %s, attributes: %s, requires: %s },\n",
		go2ts.Quote(p.ID), go2ts.Quote(p.Module()), go2ts.Quote(resource), go2ts.Quote(p.Verb()),
		go2ts.Quote(p.LabelKey), go2ts.Quote(p.DescriptionKey),
		p.Sensitive, p.System, p.OrganizationOnly,
		nullable(p.RecordType), p.UnownedIsOwn, nullable(p.Feature),
		list(p.Attributes), list(sorted(p.Requires)))
}

func nullable(s string) string {
	if s == "" {
		return "null"
	}
	return go2ts.Quote(s)
}

func list(values []string) string {
	quoted := make([]string, len(values))
	for i, v := range values {
		quoted[i] = go2ts.Quote(v)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

func sorted(values []string) []string {
	out := slices.Clone(values)
	slices.Sort(out)
	return out
}

// payloadTypes mirrors authz.MyAccess for the /me/access response.
func payloadTypes(hasLevels bool) string {
	level := "string"
	if hasLevels {
		level = "ScopeLevel"
	}
	return fmt.Sprintf(`
export interface Scope {
  level: %s;
  /** Absent for the root scope. */
  id?: number;
}

export interface ClauseInfo {
  scope: Scope;
  qualifier: Qualifier;
  attrs?: Record<string, string[]>;
  role?: string;
  binding_id?: number;
}

export interface PermissionAccess {
  /** The widest scope the permission is held at. */
  scope: Scope;
  /** The widest qualifier. It can come from a different clause than scope. */
  qualifier: Qualifier;
  clauses: ClauseInfo[];
}

/** The /me/access payload. */
export interface MyAccess {
  subject: { kind: 'user' | 'service'; id: number };
  /** True when some binding is at the root scope (crosses tenants). */
  root: boolean;
  permissions: Partial<Record<Permission, PermissionAccess>>;
  /** Granted by a role but switched off for the tenant (feature gate). */
  unavailable?: Permission[];
}
`, level)
}
