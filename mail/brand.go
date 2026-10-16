package mail

import (
	"fmt"
	"strings"
)

// Row is one "Label Value" line of a Page. A row with an empty Value is skipped, so
// optional fields can be passed unconditionally. Put the colon in the Label.
type Row struct {
	Label, Value string
}

// Page is the content of a branded mail, all of it plain text: Render escapes it.
type Page struct {
	// Preview is the snippet some clients show next to the subject in the inbox list. Optional.
	Preview string
	// Greeting replaces Brand.Greeting for this mail. Optional.
	Greeting string
	// Paragraphs are shown in order.
	Paragraphs []string
	// Rows are shown as a block of labels and values under the paragraphs.
	Rows []Row
	// Note is shown muted and smaller after the rows. Optional.
	Note string
	// ActionLabel and ActionURL make a button; both are needed for it to show.
	ActionLabel, ActionURL string
}

// Brand is the look of an application's HTML mails: the frame Render puts a Page in. The zero
// Brand is a plain frame without logo, greeting or footer.
type Brand struct {
	// Name is the alt text of the logo, or the header text when there is no Logo. Empty: no header.
	Name string
	// Logo is shown in the header on the Color. Render adds it to Content.Inline, so it needs a CID,
	// a ContentType and Data.
	Logo *Inline
	// Color is the colour of the header and the button, a CSS value such as "#334155". Default "#334155".
	Color string
	// Lang is the language of the HTML, such as "de". Default "en".
	Lang string
	// Greeting is the first line when the Page has none, such as "Hello,". Default: no greeting.
	Greeting string
	// Footer is the small print under the mail, such as why it was sent. Default: none.
	Footer string
}

// Render makes the HTML of the page in the brand's frame and its plain-text alternative, which
// carries the same words (a message needs both). Subject is left to the caller:
//
//	c, err := brand.Render(mail.Page{Paragraphs: []string{"Your order is ready."}, ActionLabel: "Open", ActionURL: url})
//	c.Subject = "Order 42"
//	err = sender.Send(ctx, c.To("ana@example.com"))
func (b Brand) Render(page Page) (Content, error) {
	if b.Logo != nil {
		err := b.Logo.check()
		if err != nil {
			return Content{}, fmt.Errorf("mail: Brand.Logo: %w", err)
		}
	}

	if page.Greeting == "" {
		page.Greeting = b.Greeting
	}

	if b.Color == "" {
		b.Color = "#334155"
	}

	if b.Lang == "" {
		b.Lang = "en"
	}

	html, err := execHTML("brand", frame, struct {
		Page

		Name, Color, Lang, Footer, LogoCID string
	}{page, b.Name, b.Color, b.Lang, b.Footer, b.logoCID()})
	if err != nil {
		return Content{}, err
	}

	//nolint:exhaustruct,exhaustruct_v5 // the caller sets the Subject
	content := Content{HTML: html, Text: b.text(page)}
	if b.Logo != nil {
		content.Inline = []Inline{*b.Logo}
	}

	return content, nil
}

func (b Brand) logoCID() string {
	if b.Logo == nil {
		return ""
	}

	return b.Logo.CID
}

func (b Brand) text(page Page) string {
	var out strings.Builder

	if page.Greeting != "" {
		out.WriteString(page.Greeting + "\n\n")
	}

	for _, paragraph := range page.Paragraphs {
		out.WriteString(paragraph + "\n\n")
	}

	rows := false

	for _, row := range page.Rows {
		if row.Value != "" {
			out.WriteString(row.Label + " " + row.Value + "\n")

			rows = true
		}
	}

	if rows {
		out.WriteString("\n")
	}

	if page.Note != "" {
		out.WriteString(page.Note + "\n\n")
	}

	if page.ActionLabel != "" && page.ActionURL != "" {
		out.WriteString(page.ActionLabel + ": " + page.ActionURL + "\n\n")
	}

	if b.Footer != "" {
		out.WriteString("-- \n" + b.Footer + "\n\n")
	}

	return strings.TrimRight(out.String(), "\n") + "\n"
}

// frame is an html/template, so every value is escaped for where it lands. The layout is fixed
// (560 px, Arial, grey page); the Brand sets the colour, logo, language, greeting and footer.
//
//nolint:lll // the markup is one line per element
const frame = `<!DOCTYPE html>
<html lang="{{.Lang}}">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Preview}}</title>
</head>
<body style="margin:0;padding:0;background-color:#f4f5f7;font-family:Arial,Helvetica,sans-serif;">
{{if .Preview}}<div style="display:none;max-height:0;overflow:hidden;opacity:0;">{{.Preview}}</div>{{end}}
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="background-color:#f4f5f7;padding:24px 0;">
<tr><td align="center">
<table role="presentation" width="560" cellpadding="0" cellspacing="0" style="background-color:#ffffff;border-radius:8px;overflow:hidden;max-width:560px;width:100%;">
{{if or .LogoCID .Name}}<tr>
<td style="background-color:{{.Color}};padding:20px 32px;">
{{if .LogoCID}}<img src="cid:{{.LogoCID}}" alt="{{.Name}}" height="32" style="display:block;height:32px;">{{else}}<span style="color:#ffffff;font-size:20px;font-weight:bold;">{{.Name}}</span>{{end}}
</td>
</tr>
{{end}}<tr>
<td style="padding:32px;color:#1f2937;font-size:15px;line-height:1.6;">
{{if .Greeting}}<p style="margin:0 0 16px;">{{.Greeting}}</p>
{{end}}{{range .Paragraphs}}<p style="margin:0 0 16px;">{{.}}</p>
{{end}}
{{if .Rows}}<table role="presentation" cellpadding="0" cellspacing="0" style="width:100%;margin:0 0 16px;border-collapse:collapse;">
{{range .Rows}}{{if .Value}}<tr>
<td style="padding:6px 12px 6px 0;color:#6b7280;font-size:14px;white-space:nowrap;">{{.Label}}</td>
<td style="padding:6px 0;color:#1f2937;font-size:14px;font-weight:bold;">{{.Value}}</td>
</tr>
{{end}}{{end}}</table>
{{end}}
{{if .Note}}<p style="margin:0 0 16px;color:#6b7280;font-size:13px;">{{.Note}}</p>
{{end}}
{{if and .ActionLabel .ActionURL}}<table role="presentation" cellpadding="0" cellspacing="0" style="margin:8px 0 0;">
<tr><td style="border-radius:6px;background-color:{{.Color}};">
<a href="{{.ActionURL}}" style="display:inline-block;padding:12px 24px;color:#ffffff;font-size:14px;font-weight:bold;text-decoration:none;border-radius:6px;">{{.ActionLabel}}</a>
</td></tr>
</table>
{{end}}
</td>
</tr>
{{if .Footer}}<tr>
<td style="padding:16px 32px;background-color:#f4f5f7;color:#9ca3af;font-size:12px;text-align:center;">
{{.Footer}}
</td>
</tr>
{{end}}</table>
</td></tr>
</table>
</body>
</html>
`
