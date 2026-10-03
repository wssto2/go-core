package mail

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"strings"
	texttemplate "text/template"
)

// Name identifies a mail an application sends, such as "identity.email-code".
type Name string

// Content is the text of a mail, made for one recipient.
type Content struct {
	Subject string
	Text    string
	HTML    string
}

// To makes the Message of the content for the recipients.
func (c Content) To(to ...string) Message {
	return Message{To: to, Subject: c.Subject, Text: c.Text, HTML: c.HTML}
}

// Renderer makes the text of a mail in the recipient's language. Modules ship
// neutral English defaults (Templates over an embedded directory); an
// application that writes its own copy, in its own languages and with its own
// look, gives the module a Renderer of its own, which may also wrap another to
// override only some mails.
type Renderer interface {
	// Render returns the content of the named mail for a BCP-47 locale such as
	// "hr" or "pt-BR". data is what the mail's templates read; each mail's
	// documentation names its type.
	Render(ctx context.Context, locale string, name Name, data any) (Content, error)
}

// RendererFunc is a Renderer made of a function.
type RendererFunc func(ctx context.Context, locale string, name Name, data any) (Content, error)

// Render calls f.
func (f RendererFunc) Render(ctx context.Context, locale string, name Name, data any) (Content, error) {
	return f(ctx, locale, name, data)
}

// ErrNoTemplate is what a Renderer returns for a mail it has no text for in
// any language; test with errors.Is.
var ErrNoTemplate = errors.New("mail: no template")

// Templates is a Renderer over a directory of templates, one folder per
// language:
//
//	en/identity.email-code.subject.txt   one line, text/template
//	en/identity.email-code.txt           the plain-text body, text/template
//	en/identity.email-code.html          the HTML body, html/template (optional)
//	hr/identity.email-code.txt           a Croatian version
//
// A locale is looked up as given ("pt-BR"), then by its language ("pt"), then
// in the fallback language "en". The subject and the text are required; a mail
// without an .html file is plain text.
func Templates(files fs.FS) Renderer { return templates{files: files} }

type templates struct{ files fs.FS }

func (t templates) Render(_ context.Context, locale string, name Name, data any) (Content, error) {
	for _, dir := range candidates(locale) {
		subject, err := fs.ReadFile(t.files, dir+"/"+string(name)+".subject.txt")
		if err != nil {
			continue
		}

		text, err := fs.ReadFile(t.files, dir+"/"+string(name)+".txt")
		if err != nil {
			return Content{}, fmt.Errorf("mail: %s has a subject but no %s/%s.txt: add the plain-text body", name, dir, name)
		}

		var c Content

		if c.Subject, err = execText(string(name)+" subject", string(subject), data); err != nil {
			return Content{}, err
		}

		c.Subject = strings.TrimSpace(c.Subject)

		if c.Text, err = execText(string(name), string(text), data); err != nil {
			return Content{}, err
		}

		if page, err := fs.ReadFile(t.files, dir+"/"+string(name)+".html"); err == nil {
			if c.HTML, err = execHTML(string(name)+" html", string(page), data); err != nil {
				return Content{}, err
			}
		}

		return c, nil
	}

	return Content{}, fmt.Errorf("%w: %q for locale %q (add en/%s.subject.txt and en/%s.txt)", ErrNoTemplate, name, locale, name, name)
}

func candidates(locale string) []string {
	out := []string{}

	if locale != "" {
		out = append(out, locale)

		if base, _, ok := strings.Cut(locale, "-"); ok {
			out = append(out, base)
		}
	}

	if locale != "en" {
		out = append(out, "en")
	}

	return out
}

func execText(name, src string, data any) (string, error) {
	tpl, err := texttemplate.New(name).Option("missingkey=error").Parse(src)
	if err != nil {
		return "", fmt.Errorf("mail: template %s: %w", name, err)
	}

	var b bytes.Buffer
	if err := tpl.Execute(&b, data); err != nil {
		return "", fmt.Errorf("mail: template %s: %w", name, err)
	}

	return b.String(), nil
}

func execHTML(name, src string, data any) (string, error) {
	tpl, err := template.New(name).Option("missingkey=error").Parse(src)
	if err != nil {
		return "", fmt.Errorf("mail: template %s: %w", name, err)
	}

	var b bytes.Buffer
	if err := tpl.Execute(&b, data); err != nil {
		return "", fmt.Errorf("mail: template %s: %w", name, err)
	}

	return b.String(), nil
}
