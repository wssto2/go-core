package account_test

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The core is framework-free (ARCHITECTURE.md section 4.1): the standard
// library, apperr, authz (its types), mail (the sender port: it imports nothing) and x/crypto, nothing that knows HTTP or a database.
func TestCoreIsFrameworkFree(t *testing.T) {
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)

	allowed := func(path string) bool {
		return !strings.Contains(path, ".") || // the standard library
			path == "github.com/wssto2/go-core/apperr" ||
			path == "github.com/wssto2/go-core/authz" || // types only: it imports apperr and nothing else
			path == "github.com/wssto2/go-core/mail" || // Layer 0, standard library only: the port of Users.Mail
			path == "golang.org/x/crypto/bcrypt"
	}

	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}

		parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
		require.NoError(t, err)

		for _, imp := range parsed.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			require.Truef(t, allowed(path), "%s imports %s: the core takes the standard library, apperr and bcrypt only", file, path)
		}
	}
}
