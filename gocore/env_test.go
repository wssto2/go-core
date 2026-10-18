package gocore_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/wssto2/go-core/gocore"
)

type envCase struct {
	name, file string
	want       map[string]string
}

// pairs makes a map out of key, value, key, value...
func pairs(kv ...string) map[string]string {
	out := map[string]string{}
	for pair := range slices.Chunk(kv, 2) {
		out[pair[0]] = pair[1]
	}

	return out
}

// envCases are the forms an .env file takes; the expectations are godotenv.Load's.
func envCases() []envCase {
	return []envCase{
		{"plain, spaces around the sign and the value", "A=1\nB = 2  \n", pairs("A", "1", "B", "2")},
		{"comments and blank lines", "# comment\n\n  # indented\nA=1\n", pairs("A", "1")},
		{"export prefix", "export A=1\nexport  B=2\n", pairs("A", "1", "B", "2")},
		{"yaml style", "A: 1\n", pairs("A", "1")},
		{"empty value", "A=\nB=x\n", pairs("A", "", "B", "x")},
		{"CRLF", "A=1\r\nB=2\r\n", pairs("A", "1", "B", "2")},
		{"no final newline", "A=1", pairs("A", "1")},
		{"trailing comment is cut after a space only", "A=plain # note\nB=a#b\n", pairs("A", "plain", "B", "a#b")},
		{"double quotes keep # and spaces", "A=\"a # b  \"\n", pairs("A", "a # b  ")},
		{
			"double quotes read \\n and \\\"",
			"A=\"x\\ny\"\nB=\"say \\\"hi\\\" now\"\n",
			pairs("A", "x\ny", "B", `say "hi" now`),
		},
		{"double quotes may span lines", "A=\"one\ntwo\"\nB=3\n", pairs("A", "one\ntwo", "B", "3")},
		{"single quotes are literal", "A='x\\ny # $B'\n", pairs("A", `x\ny # $B`)},
		{"a comment may follow a quoted value", "A=\"x\" # note\nB=2\n", pairs("A", "x", "B", "2")},
		{
			"a variable set earlier in the file expands",
			"A=1\nB=${A}x\nC=\"$A-y\"\nD='$A'\n",
			pairs("A", "1", "B", "1x", "C", "1-y", "D", "$A"),
		},
		{"an escaped dollar stays", "A=\\$B\n", pairs("A", "$B")},
		{"the last of a repeated key wins", "A=1\nA=2\n", pairs("A", "2")},
		{"dotted and underscored names", "a.b_c=1\n", pairs("a.b_c", "1")},
	}
}

func writeEnv(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), ".env")

	err := os.WriteFile(path, []byte(content), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	return path
}

func TestLoadEnvForms(t *testing.T) {
	for _, form := range envCases() {
		t.Run(form.name, func(t *testing.T) {
			for key := range form.want {
				t.Setenv(key, "") // restored after the test
				_ = os.Unsetenv(key)
			}

			err := gocore.LoadEnv(writeEnv(t, form.file))
			if err != nil {
				t.Fatal(err)
			}

			for key, want := range form.want {
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
	t.Setenv("ENVTEST_NEW", "")

	_ = os.Unsetenv("ENVTEST_NEW")

	err := gocore.LoadEnv(writeEnv(t, "ENVTEST_KEPT=file\nENVTEST_EMPTY=file\nENVTEST_NEW=file\n"))
	if err != nil {
		t.Fatal(err)
	}

	for key, want := range pairs("ENVTEST_KEPT", "from the shell", "ENVTEST_EMPTY", "", "ENVTEST_NEW", "file") {
		if got := os.Getenv(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
}

func TestLoadEnvMissingFileIsNotExist(t *testing.T) {
	t.Parallel()

	err := gocore.LoadEnv(filepath.Join(t.TempDir(), "absent.env"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("want an error that is fs.ErrNotExist, got %v", err)
	}
}

func TestLoadEnvSetsNothingFromAFileThatDoesNotParse(t *testing.T) {
	for name, file := range pairs(
		"unterminated", "ENVBAD_A=1\nENVBAD_B=\"open\n",
		"character in the name", "ENVBAD_A=1\nB-C=2\n",
		"no sign", "ENVBAD_A=1\nB\n",
	) {
		t.Run(name, func(t *testing.T) {
			t.Setenv("ENVBAD_A", "")

			_ = os.Unsetenv("ENVBAD_A")

			err := gocore.LoadEnv(writeEnv(t, file))
			if err == nil {
				t.Fatal("want a parse error")
			}

			if _, ok := os.LookupEnv("ENVBAD_A"); ok {
				t.Error("a half-read file must set nothing")
			}
		})
	}
}
