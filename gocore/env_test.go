package gocore_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wssto2/go-core/gocore"
)

// envCases are the forms an .env file takes; the expectations are godotenv.Load's.
var envCases = []struct {
	name, file string
	want       map[string]string
}{
	{"plain, spaces around the sign and the value", "A=1\nB = 2  \n", map[string]string{"A": "1", "B": "2"}},
	{"comments and blank lines", "# comment\n\n  # indented\nA=1\n", map[string]string{"A": "1"}},
	{"export prefix", "export A=1\nexport  B=2\n", map[string]string{"A": "1", "B": "2"}},
	{"yaml style", "A: 1\n", map[string]string{"A": "1"}},
	{"empty value", "A=\nB=x\n", map[string]string{"A": "", "B": "x"}},
	{"CRLF", "A=1\r\nB=2\r\n", map[string]string{"A": "1", "B": "2"}},
	{"no final newline", "A=1", map[string]string{"A": "1"}},
	{"trailing comment is cut after a space only", "A=plain # note\nB=a#b\n", map[string]string{"A": "plain", "B": "a#b"}},
	{"double quotes keep # and spaces", `A="a # b  "` + "\n", map[string]string{"A": "a # b  "}},
	{"double quotes: \\n is a newline, \\\" a quote", `A="x\ny"` + "\n" + `B="say \"hi\" now"` + "\n", map[string]string{"A": "x\ny", "B": `say "hi" now`}},
	{"double quotes may span lines", "A=\"one\ntwo\"\nB=3\n", map[string]string{"A": "one\ntwo", "B": "3"}},
	{"single quotes are literal", `A='x\ny # $B'` + "\n", map[string]string{"A": `x\ny # $B`}},
	{"a comment may follow a quoted value", "A=\"x\" # note\nB=2\n", map[string]string{"A": "x", "B": "2"}},
	{"a variable set earlier in the file expands", "A=1\nB=${A}x\nC=\"$A-y\"\nD='$A'\n", map[string]string{"A": "1", "B": "1x", "C": "1-y", "D": "$A"}},
	{"an escaped dollar stays", `A=\$B` + "\n", map[string]string{"A": "$B"}},
	{"the last of a repeated key wins", "A=1\nA=2\n", map[string]string{"A": "2"}},
	{"dotted and underscored names", "a.b_c=1\n", map[string]string{"a.b_c": "1"}},
}

func writeEnv(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

// unset makes sure the variable is absent for the test and restored after it.
func unset(t *testing.T, key string) {
	t.Helper()
	t.Setenv(key, "")

	if err := os.Unsetenv(key); err != nil {
		t.Fatal(err)
	}
}

func TestLoadEnvForms(t *testing.T) {
	for _, tc := range envCases {
		t.Run(tc.name, func(t *testing.T) {
			for key := range tc.want {
				unset(t, key)
			}

			if err := gocore.LoadEnv(writeEnv(t, tc.file)); err != nil {
				t.Fatal(err)
			}

			for key, want := range tc.want {
				if got, ok := os.LookupEnv(key); !ok || got != want {
					t.Errorf("%s = %q (set: %t), want %q", key, got, ok, want)
				}
			}
		})
	}
}

func TestLoadEnvKeepsWhatIsAlreadySet(t *testing.T) {
	t.Setenv("ENVTEST_KEPT", "from the shell")
	t.Setenv("ENVTEST_EMPTY", "") // set but empty counts as set
	unset(t, "ENVTEST_NEW")

	if err := gocore.LoadEnv(writeEnv(t, "ENVTEST_KEPT=file\nENVTEST_EMPTY=file\nENVTEST_NEW=file\n")); err != nil {
		t.Fatal(err)
	}

	for key, want := range map[string]string{"ENVTEST_KEPT": "from the shell", "ENVTEST_EMPTY": "", "ENVTEST_NEW": "file"} {
		if got := os.Getenv(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
}

func TestLoadEnvMissingFileIsNotExist(t *testing.T) {
	err := gocore.LoadEnv(filepath.Join(t.TempDir(), "absent.env"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("want an error that is fs.ErrNotExist, got %v", err)
	}
}

func TestLoadEnvSetsNothingFromAFileThatDoesNotParse(t *testing.T) {
	for name, file := range map[string]string{
		"unterminated":          "ENVBAD_A=1\nENVBAD_B=\"open\n",
		"character in the name": "ENVBAD_A=1\nB-C=2\n",
	} {
		t.Run(name, func(t *testing.T) {
			unset(t, "ENVBAD_A")

			err := gocore.LoadEnv(writeEnv(t, file))
			if err == nil || !strings.Contains(err.Error(), "env") {
				t.Fatalf("want a parse error naming the file, got %v", err)
			}

			if _, ok := os.LookupEnv("ENVBAD_A"); ok {
				t.Error("a half-read file must set nothing")
			}
		})
	}
}
