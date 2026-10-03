package gocore_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const modulePath = "github.com/wssto2/go-core/"

// features are the modules go-core ships, each a package with one Install.
var features = []string{"identity", "access", "notification", "mail"}

// An Install builds only its own feature's services (plan section 3.1, an
// architecture rule like ARV's Rule 1): it never calls another feature's
// constructors, it takes what it needs from them as arguments. This reads the
// Install of every feature that exists and refuses a call into another
// feature's packages; types and methods of its arguments are fine.
func TestAnInstallBuildsOnlyItsOwnFeature(t *testing.T) {
	for _, feature := range features {
		dir := filepath.Join("..", feature)
		if _, err := os.Stat(dir); err != nil {
			continue
		}

		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}

		for _, file := range files {
			if strings.HasSuffix(file, "_test.go") {
				continue
			}

			parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
			if err != nil {
				t.Fatal(err)
			}

			for _, decl := range parsed.Decls {
				if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && fn.Name.Name == "Install" {
					for _, v := range foreignCalls(parsed, fn, feature) {
						t.Errorf("%s: Install of %s calls %s, a constructor of another feature: take what it builds as an argument instead", file, feature, v)
					}
				}
			}
		}
	}
}

// foreignCalls lists the calls fn makes into packages of features other than own.
func foreignCalls(file *ast.File, fn *ast.FuncDecl, own string) []string {
	imported := map[string]string{} // local name -> feature

	for _, imp := range file.Imports {
		path := strings.Trim(imp.Path.Value, `"`)

		rest, ok := strings.CutPrefix(path, modulePath)
		if !ok {
			continue
		}

		feature, _, _ := strings.Cut(rest, "/")
		if feature == own || !isFeature(feature) {
			continue
		}

		name := filepath.Base(path)
		if imp.Name != nil {
			name = imp.Name.Name
		}

		imported[name] = feature
	}

	var calls []string

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok && imported[id.Name] != "" {
				calls = append(calls, id.Name+"."+sel.Sel.Name)
			}
		}

		return true
	})

	return calls
}

func isFeature(name string) bool {
	for _, f := range features {
		if f == name {
			return true
		}
	}

	return false
}

func TestTheInstallRuleCatchesAForeignConstructor(t *testing.T) {
	src := `package access

import (
	"github.com/wssto2/go-core/identity"
	"github.com/wssto2/go-core/identity/account"
	"github.com/wssto2/go-core/gocore"
)

func Install(app *gocore.App, users *identity.Users) {
	_ = identity.Install(app)           // another feature's Install
	_ = account.New(account.Deps{}, 0)  // another feature's constructor
	_ = users.Get                       // a method of an argument is fine
}
`

	file, err := parser.ParseFile(token.NewFileSet(), "access.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}

	got := foreignCalls(file, file.Decls[1].(*ast.FuncDecl), "access")
	if len(got) != 2 || got[0] != "identity.Install" || got[1] != "account.New" {
		t.Fatalf("got %v", got)
	}
}
