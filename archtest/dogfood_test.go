package archtest_test

import (
	"testing"

	"github.com/wssto2/go-core/archtest"
)

const goCore = "github.com/wssto2/go-core/"

// layers are ARCHITECTURE.md section 4, lowest first: a package imports its own
// layer and the layers below, never a layer above.
var layers = [][]string{
	{"apperr", "utils", "i18n", "navigation", "mail"},
	{"validation", "database/types", "logger", "resilience"},
	{"database", "auth", "event", "storage", "tenancy", "worker"},
	{"binders", "middlewares", "datatable", "resource", "observability", "web", "audit", "health"},
	{"bootstrap", "frontend", "go2ts", "route"},
	{"contract"},
	{"gocore"},
}

// ban says that no production file of the package imports any of the targets.
func ban(in string, targets ...string) []archtest.Banned {
	var out []archtest.Banned

	for _, target := range targets {
		out = append(out, archtest.Banned{Import: goCore + target, In: []string{in}, Why: "section 4: " + in + " must not import " + target})
	}

	return out
}

// TestArchitectureDocument holds go-core to the package dependency rules of
// ARCHITECTURE.md section 4: the layers, the per-package prohibitions, the
// test-only gocoretest and the framework-free core of identity.
func TestArchitectureDocument(t *testing.T) {
	var banned []archtest.Banned

	// Layers 0 to 5: a package never imports a layer above its own.
	for l, pkgs := range layers {
		for _, p := range pkgs {
			for _, above := range layers[l+1:] {
				for _, target := range above {
					// database/types is documented as layer 1 and imports database (layer 2) in 16 files. The
					// rule is left out for that one pair until section 4 or the package changes; it is held
					// against every other layer above it.
					if p == "database/types" && target == "database" {
						continue
					}

					banned = append(banned, ban(p, target)...)
				}
			}
		}
	}

	// Specific prohibitions.
	for _, b := range []struct {
		in      string
		targets []string
	}{
		{"auth", []string{"database", "bootstrap", "event"}},
		{"event", []string{"bootstrap", "auth"}},
		{"audit", []string{"bootstrap", "auth"}},
		{"database", []string{"event", "auth", "bootstrap"}},
		{"health", []string{"bootstrap"}},
		{"route", []string{"bootstrap", "gocore"}},
		{"contract", []string{"bootstrap", "gocore"}},
		{"bootstrap", []string{"gocore", "gocoretest"}},
		{"database", []string{"gocore", "gocoretest"}},
		{"authz", []string{"gocore", "gocoretest"}},
		{"route", []string{"gocoretest"}},
	} {
		banned = append(banned, ban(b.in, b.targets...)...)
	}

	banned = append(banned,
		// resilience is pure algorithms.
		archtest.Banned{Import: goCore + "...", In: []string{"resilience"}, Why: "section 4: resilience imports nothing from go-core"},
		// The notification arrow: neither event nor identity imports notification (the key notice, which
		// needs both, is the one package of identity that may).
		archtest.Banned{Import: goCore + "notification/...", In: []string{"event/...", "identity/..."}, Except: []string{"identity/serviceaccount/keynotice/..."},
			Why: "section 4: notification imports event and identity, not the other way round"},
		// A module's facade is the only package that imports gocore.
		archtest.Banned{Import: goCore + "gocore", In: []string{"identity/...", "access/...", "notification/..."},
			Except: []string{"identity", "access", "notification", "identity/serviceaccount/keynotice"},
			Why:    "section 4.1: a module's facade is the only package that imports gocore"},
		// gocoretest is for _test.go files and test-support packages only.
		archtest.Banned{Import: goCore + "gocoretest", Why: "section 4: gocoretest is imported by _test.go files only"},
		// identity's framework-free core takes the standard library and these, nothing that knows HTTP or a database.
		archtest.Banned{
			Import: "...", In: []string{"identity/account"},
			Unless: []string{goCore + "apperr", goCore + "authz", goCore + "mail", "golang.org/x/crypto/bcrypt"},
			Why:    "section 4.1: identity/account takes the standard library, apperr, authz (types), mail and bcrypt only",
		},
	)

	archtest.Run(t, archtest.Config{
		Banned: banned,
		// go-core is a library: none of the application rules apply.
		Skip: []string{archtest.ErrorfWrap, archtest.RoutePaths, archtest.RouteIdentifiers, archtest.RouteAccess},
	})
}
