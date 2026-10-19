package archtest_test

import (
	"testing"

	"github.com/wssto2/go-core/archtest"
)

const (
	goCore        = "github.com/wssto2/go-core/"
	pkgAuth       = "auth"
	pkgBootstrap  = "bootstrap"
	pkgDatabase   = "database"
	pkgEvent      = "event"
	pkgGocore     = "gocore"
	pkgGocoreTest = "gocoretest"
	pkgNotify     = "notification/..."
	pkgRoute      = "route"
)

// layers are ARCHITECTURE.md section 4, lowest first: a package imports its own
// layer and the layers below, never a layer above.
func layers() [][]string {
	return [][]string{
		{"apperr", "i18n", "navigation", "mail", "pagination"},
		{"validation", "logger", "httpclient/internal/resilience", "archtest"},
		{pkgDatabase, "database/types", pkgAuth, pkgEvent, "storage", "tenancy", "worker"},
		{"internal/binders", "middlewares", "datatable", "resource", "observability", "web", "audit", "health"},
		{pkgBootstrap, "frontend", "go2ts", pkgRoute, "web/typed"},
		{"contract"},
		{pkgGocore},
	}
}

// ban says that no production file of the package imports the target.
func ban(pkg, target string) archtest.Banned {
	ban := banned(goCore+target, "", "section 4: "+pkg+" must not import "+target)
	ban.In = []string{pkg}

	return ban
}

// layerBans: a package never imports a layer above its own.
func layerBans() []archtest.Banned {
	var out []archtest.Banned

	all := layers()
	for level, pkgs := range all {
		for _, pkg := range pkgs {
			for _, above := range all[level+1:] {
				for _, target := range above {
					out = append(out, ban(pkg, target))
				}
			}
		}
	}

	return out
}

// prohibitions are the "MUST NOT import" lines of section 4.
func prohibitions() []archtest.Banned {
	var out []archtest.Banned

	for pkg, targets := range map[string][]string{
		pkgAuth:      {pkgDatabase, pkgBootstrap, pkgEvent},
		pkgEvent:     {pkgBootstrap, pkgAuth},
		"audit":      {pkgBootstrap, pkgAuth},
		pkgDatabase:  {pkgEvent, pkgAuth, pkgBootstrap, pkgGocore, pkgGocoreTest},
		"health":     {pkgBootstrap},
		pkgRoute:     {pkgBootstrap, pkgGocore, pkgGocoreTest},
		"contract":   {pkgBootstrap, pkgGocore},
		pkgBootstrap: {pkgGocore, pkgGocoreTest},
		"authz":      {pkgGocore, pkgGocoreTest},
	} {
		for _, target := range targets {
			out = append(out, ban(pkg, target))
		}
	}

	return out
}

// modules are the arrows of sections 4 and 4.1 that are not between two layers.
func modules() []archtest.Banned {
	pure := banned(goCore+"...", "", "section 4: resilience imports nothing from go-core")
	pure.In = []string{"httpclient/internal/resilience"}

	// neither event nor identity imports notification; the key notice, which needs both, is the one package that may.
	arrow := banned(goCore+pkgNotify, "", "section 4: notification imports event and identity, not the other way round")
	arrow.In = []string{"event/...", "identity/..."}
	arrow.Except = []string{"identity/serviceaccount/keynotice/..."}

	// a module's facade is the only package that imports gocore.
	facade := banned(goCore+pkgGocore, "", "section 4.1: a module's facade is the only package that imports gocore")
	facade.In = []string{"identity/...", "access/...", "notification/...", "incident/...", "savedview/..."}
	facade.Except = []string{
		"identity", "access", "notification", "incident", "savedview", "identity/serviceaccount/keynotice",
	}

	// gocoretest is for _test.go files and test-support packages only.
	testOnly := banned(goCore+pkgGocoreTest, "", "section 4: gocoretest is imported by _test.go files only")

	// identity's framework-free core takes the standard library and these, nothing that knows HTTP or a database.
	core := banned("...", "",
		"section 4.1: identity/account takes the standard library, apperr, authz (types), mail and bcrypt only")
	core.In = []string{"identity/account"}
	core.Unless = []string{goCore + "apperr", goCore + "authz", goCore + "mail", "golang.org/x/crypto/bcrypt"}

	// incident's core is framework-free too: the standard library, apperr and pagination.
	incidentCore := banned("...", "", "section 4.1: incident/core takes the standard library, apperr and pagination only")
	incidentCore.In = []string{"incident/core"}
	incidentCore.Unless = []string{goCore + "apperr", goCore + "pagination"}

	// savedview's core is framework-free too: the standard library and apperr.
	savedviewCore := banned("...", "", "section 4.1: savedview/core takes the standard library and apperr only")
	savedviewCore.In = []string{"savedview/core"}
	savedviewCore.Unless = []string{goCore + "apperr"}

	return []archtest.Banned{pure, arrow, facade, testOnly, core, incidentCore, savedviewCore}
}

// TestArchitectureDocument holds go-core to the package dependency rules of
// ARCHITECTURE.md section 4: the layers, the per-package prohibitions, the
// test-only gocoretest and the framework-free core of identity.
func TestArchitectureDocument(t *testing.T) {
	t.Parallel()

	var cfg archtest.Config

	cfg.Banned = append(append(layerBans(), prohibitions()...), modules()...)
	// The route naming rules are an application's (modules build some paths at run time); ErrorfWrap waits on
	// datatable.go:34 and resource.go:54, whose error may be nil.
	cfg.Skip = []string{archtest.ErrorfWrap, archtest.RoutePaths, archtest.RouteIdentifiers}

	archtest.Run(t, cfg)
}
