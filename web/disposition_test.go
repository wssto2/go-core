//nolint:cyclop,funlen,goconst,lll,varnamelen,wsl_v5 // tests in the style of the package's others
package web_test

import (
	"mime"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/wssto2/go-core/web"
)

const maxDispositionName = 200

func TestDisposition(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("čć", 300) + ".pdf"

	tests := []struct {
		name       string
		kind, file string
		want       string // the name a client reads back
		kindWant   string
		header     string // exact header, "" when only the round trip matters
	}{
		{"ascii", "attachment", "plan-2026-10.xlsx", "plan-2026-10.xlsx", "attachment", "attachment; filename=plan-2026-10.xlsx"},
		{"inline", "inline", "report.pdf", "report.pdf", "inline", "inline; filename=report.pdf"},
		{"unknown kind", "bogus", "a.pdf", "a.pdf", "attachment", "attachment; filename=a.pdf"},
		{"croatian", "attachment", "ugovor-čćžšđ.pdf", "ugovor-čćžšđ.pdf", "attachment", ""},
		{"croatian with space", "inline", "Račun br 1.pdf", "Račun br 1.pdf", "inline", `inline; filename="Ra_un br 1.pdf"; filename*=utf-8''Ra%C4%8Dun%20br%201.pdf`},
		{"vin with quote", "attachment", `nalog-UU1"DJF.pdf`, `nalog-UU1"DJF.pdf`, "attachment", `attachment; filename="nalog-UU1\"DJF.pdf"`},
		{"backslash", "attachment", `a\b.pdf`, "b.pdf", "attachment", "attachment; filename=b.pdf"},
		{"spaces", "attachment", "moj dokument 1.pdf", "moj dokument 1.pdf", "attachment", `attachment; filename="moj dokument 1.pdf"`},
		{"crlf", "attachment", "a.pdf\r\nSet-Cookie: x=1", "a.pdfSet-Cookie: x=1", "attachment", ""},
		{"nul and tab", "inline", "a\x00b\tc.pdf", "abc.pdf", "inline", "inline; filename=abc.pdf"},
		{"path", "attachment", "../../etc/passwd", "passwd", "attachment", "attachment; filename=passwd"},
		{"windows path", "attachment", `C:\temp\x.pdf`, "x.pdf", "attachment", "attachment; filename=x.pdf"},
		{"trailing slash", "attachment", "dir/", "", "attachment", "attachment"},
		{"dots", "attachment", "..", "", "attachment", "attachment"},
		{"empty", "inline", "", "", "inline", "inline"},
		{"invalid utf8", "attachment", "a\xffb.pdf", "ab.pdf", "attachment", "attachment; filename=ab.pdf"},
		{"long", "attachment", long, "", "attachment", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := web.Disposition(tc.kind, tc.file)

			if tc.header != "" && got != tc.header {
				t.Errorf("Disposition(%q, %q) = %q, want %q", tc.kind, tc.file, got, tc.header)
			}

			if got == "" || strings.ContainsAny(got, "\r\n") || !utf8.ValidString(got) {
				t.Fatalf("unsafe header %q", got)
			}

			kind, params, err := mime.ParseMediaType(got)
			if err != nil {
				t.Fatalf("mime.ParseMediaType(%q): %v", got, err)
			}

			if kind != tc.kindWant {
				t.Errorf("kind = %q, want %q", kind, tc.kindWant)
			}

			if tc.name == "long" {
				name := params["filename"]
				if name == "" || len(name) > maxDispositionName || !utf8.ValidString(name) || !strings.HasPrefix(long, name) {
					t.Errorf("long name read back as %d bytes %q", len(name), name)
				}

				return
			}

			if params["filename"] != tc.want {
				t.Errorf("filename = %q, want %q (header %q)", params["filename"], tc.want, got)
			}
		})
	}
}

func TestDisposition_NonASCIIHasUTF8Parameter(t *testing.T) {
	t.Parallel()

	got := web.Disposition("attachment", "ugovor-čćžšđ.pdf")
	want := "attachment; filename=ugovor-_____.pdf; filename*=utf-8''ugovor-%C4%8D%C4%87%C5%BE%C5%A1%C4%91.pdf"
	if got != want {
		t.Errorf("Disposition = %q, want %q", got, want)
	}
}
