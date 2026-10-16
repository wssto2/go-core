// Package web holds the HTTP helpers the handlers share: path parameters, responses and file downloads.
package web

import (
	"mime"
	"strings"
	"unicode"
	"unicode/utf8"
)

// maxDispositionName bounds the file name in a header, in bytes before encoding, so a long name cannot grow a
// response header past what proxies and browsers accept.
const maxDispositionName = 200

// Disposition is the value of a Content-Disposition header: kind ("inline" or "attachment"; anything else becomes
// "attachment") and the file name the browser should offer.
//
// The name is encoded by mime.FormatMediaType: a plain ASCII name goes as filename=, a name with quotes or spaces
// is quoted and escaped, and a name with other characters gets an ASCII filename= fallback (each such character an
// underscore) and an RFC 5987 filename* parameter in UTF-8. The name is made safe first: a directory part is
// dropped, control characters (CR and LF among them) are removed, and a name longer than 200 bytes is cut at a
// character boundary. An empty name leaves the bare kind.
//
// The result is never empty and never carries a line break; if the standard library refuses the name, the header
// falls back to a plain ASCII file name, and failing that to the bare kind.
func Disposition(kind, name string) string {
	if kind != "inline" {
		kind = "attachment"
	}

	name = cleanDispositionName(name)
	if name == "" {
		return kind
	}

	header := mime.FormatMediaType(kind, dispositionParams(name))
	if header == "" {
		header = mime.FormatMediaType(kind, dispositionParams(asciiDispositionName(name)))
	} else if ascii := asciiDispositionName(name); ascii != name {
		// FormatMediaType gives a name with other characters only as filename*=; the ASCII filename= before it is
		// for a client that does not read that parameter (one that does prefers filename*).
		header = mime.FormatMediaType(kind, dispositionParams(ascii)) + header[len(kind):]
	}

	if header == "" {
		return kind
	}

	return header
}

// cleanDispositionName drops the directory part (either separator), the control characters and the edge spaces of
// name, then cuts it to maxDispositionName bytes.
func cleanDispositionName(name string) string {
	name = strings.ToValidUTF8(name, "")
	name = name[strings.LastIndexAny(name, `/\`)+1:]
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}

		return r
	}, name)

	name = strings.TrimSpace(name)
	if name == "." || name == ".." {
		return ""
	}

	if len(name) > maxDispositionName {
		cut := maxDispositionName
		for cut > 0 && !utf8.RuneStart(name[cut]) {
			cut--
		}

		name = name[:cut]
	}

	return name
}

// asciiDispositionName keeps the printable ASCII of name, with an underscore for every other character.
func asciiDispositionName(name string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r > 0x7e {
			return '_'
		}

		return r
	}, name)
}

func dispositionParams(name string) map[string]string {
	return map[string]string{"filename": name}
}
