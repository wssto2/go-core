package mail

import (
	"errors"
	"fmt"
	"mime"
	"regexp"
	"strings"
	"unicode"
)

// Inline is a part the HTML of a message shows from inside the message: an <img src="cid:logo@example.com">
// is the part whose CID is "logo@example.com". Mail clients show it without fetching anything from the web.
type Inline struct {
	// CID is the Content-ID, referred to as "cid:<CID>" in the HTML: letters, digits and . _ ~ + - @,
	// for example "logo@example.com". Unique in the message.
	CID string
	// ContentType is the media type of Data, for example "image/png".
	ContentType string
	// Name is the file name a client offers when the part is saved. Optional.
	Name string
	Data []byte
}

// cidChars is what a Content-ID may hold: no space, quote, angle bracket or line break.
var cidChars = regexp.MustCompile(`^[A-Za-z0-9._~+@-]+$`)

// errInline is what every problem with an inline part wraps.
var errInline = errors.New("mail: an inline part is wrong")

func (i Inline) check() error {
	if !cidChars.MatchString(i.CID) {
		return fmt.Errorf("%w: the CID %q is not usable: use letters, digits and . _ ~ + - @, like \"logo@example.com\"",
			errInline, i.CID)
	}

	_, _, err := mime.ParseMediaType(i.ContentType)
	if err != nil || strings.ContainsAny(i.ContentType, "\r\n") {
		return fmt.Errorf("%w: %q has the content type %q: set it like \"image/png\"", errInline, i.CID, i.ContentType)
	}

	if strings.ContainsFunc(i.Name, unicode.IsControl) {
		return fmt.Errorf("%w: the name of %q has a control character: a file name is one line", errInline, i.CID)
	}

	if len(i.Data) == 0 {
		return fmt.Errorf("%w: %q has no Data", errInline, i.CID)
	}

	return nil
}

// checkInline is what Message.Check says about the inline parts: each is usable and no CID is used twice.
func checkInline(parts []Inline) error {
	seen := map[string]bool{}

	for _, part := range parts {
		err := part.check()
		if err != nil {
			return permanent{err}
		}

		if seen[part.CID] {
			return permanent{fmt.Errorf("%w: the CID %q is used twice: each part needs its own", errInline, part.CID)}
		}

		seen[part.CID] = true
	}

	return nil
}
