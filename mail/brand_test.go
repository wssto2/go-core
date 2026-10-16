//nolint:exhaustruct,exhaustruct_v5,gochecknoglobals,goconst,gosec,lll,paralleltest,varnamelen // tests in the style of the package's others
package mail_test

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/mail"
)

// arvBrand is the brand of the application whose mail frame this one was made from. Its values
// live here only: the golden files in testdata are what that application rendered with its own template.
var arvBrand = mail.Brand{
	Name:     "ARV",
	Logo:     &mail.Inline{CID: "arv-logo@arv-next", ContentType: "image/png", Name: "arv-logo.png", Data: []byte("png")},
	Color:    "#1e293b",
	Lang:     "hr",
	Greeting: "Poštovani,",
	Footer:   "Ova poruka je automatski generirana iz ARV sustava.",
}

var fullPage = mail.Page{
	Preview:     "Order 42 is ready",
	Greeting:    "Dear Ana & Bob,",
	Paragraphs:  []string{"Your order <42> is ready for pickup.", `Bring "this" mail with you.`},
	Rows:        []mail.Row{{Label: "Location:", Value: "Main & Sons"}, {Label: "Empty:"}, {Label: "Date:", Value: "2026-10-16"}},
	Note:        "Questions? Just reply.",
	ActionLabel: "Open order",
	ActionURL:   "https://app.example.com/orders/42?tab=details&lang=en",
}

func golden(t *testing.T, name string) string {
	t.Helper()

	b, err := os.ReadFile("testdata/" + name)
	require.NoError(t, err)

	return string(b)
}

// The same page in the same brand is the same HTML the application rendered before: byte for byte.
func TestBrandRendersTheMarkupItWasMadeFrom(t *testing.T) {
	full, err := arvBrand.Render(fullPage)
	require.NoError(t, err)
	require.Equal(t, golden(t, "layout_full.html"), full.HTML)

	minimal, err := arvBrand.Render(mail.Page{Paragraphs: []string{"Hello."}})
	require.NoError(t, err)
	require.Equal(t, golden(t, "layout_minimal.html"), minimal.HTML, "the brand's greeting fills in a page without one")
}

func TestBrandTextIsTheSamePageAsPlainText(t *testing.T) {
	c, err := arvBrand.Render(fullPage)
	require.NoError(t, err)

	require.Equal(t, `Dear Ana & Bob,

Your order <42> is ready for pickup.

Bring "this" mail with you.

Location: Main & Sons
Date: 2026-10-16

Questions? Just reply.

Open order: https://app.example.com/orders/42?tab=details&lang=en

-- 
Ova poruka je automatski generirana iz ARV sustava.
`, c.Text, "plain text is not HTML-escaped; an empty row is left out")

	short, err := arvBrand.Render(mail.Page{Paragraphs: []string{"Hello."}})
	require.NoError(t, err)
	require.Equal(t, "Poštovani,\n\nHello.\n\n-- \nOva poruka je automatski generirana iz ARV sustava.\n", short.Text)
}

func TestBrandLogoTravelsInTheContent(t *testing.T) {
	c, err := arvBrand.Render(mail.Page{Paragraphs: []string{"Hi"}})
	require.NoError(t, err)
	require.Equal(t, []mail.Inline{*arvBrand.Logo}, c.Inline)
	require.Contains(t, c.HTML, `src="cid:arv-logo@arv-next"`)

	c.Subject = "Hello"
	m := c.To("ana@example.com")
	require.Equal(t, c.Inline, m.Inline, "Content.To keeps the logo, so identity and notification mails carry it")
	require.NoError(t, m.Check())
}

func TestZeroBrandIsAPlainFrame(t *testing.T) {
	c, err := mail.Brand{}.Render(mail.Page{Paragraphs: []string{"Hello."}})
	require.NoError(t, err)

	require.Empty(t, c.Inline)
	require.Equal(t, "Hello.\n", c.Text)
	require.Contains(t, c.HTML, `<html lang="en">`)
	require.Contains(t, c.HTML, "Hello.")
	require.NotContains(t, c.HTML, "<img")
	require.NotContains(t, c.HTML, "cid:")
	require.Equal(t, 2, strings.Count(c.HTML, "<tr>"), "the outer row and the body row: no header, no footer")
}

func TestBrandNameAloneIsTheHeader(t *testing.T) {
	c, err := mail.Brand{Name: "Acme"}.Render(mail.Page{Paragraphs: []string{"Hello."}})
	require.NoError(t, err)
	require.Contains(t, c.HTML, ">Acme</span>")
	require.Empty(t, c.Inline)
}

func TestBrandEscapesWhatItPlacesInTheHTML(t *testing.T) {
	c, err := mail.Brand{Name: `"><script>x()</script>`, Footer: "<b>f</b>", Color: `red;"><script>y()</script>`}.Render(mail.Page{
		Preview: "<i>", Paragraphs: []string{"<script>z()</script>"}, ActionLabel: "Go", ActionURL: "javascript:alert(1)",
	})
	require.NoError(t, err)
	require.NotContains(t, c.HTML, "<script>")
	require.NotContains(t, c.HTML, "javascript:", "an unsafe link is not made a button")
}

func TestBrandRefusesALogoNoMessageCouldCarry(t *testing.T) {
	_, err := mail.Brand{Logo: &mail.Inline{CID: "logo", ContentType: "image/png"}}.Render(mail.Page{Paragraphs: []string{"Hi"}})
	require.ErrorContains(t, err, "Brand.Logo")

	_, err = mail.Brand{Logo: &mail.Inline{CID: "a\r\nBcc: x", ContentType: "image/png", Data: []byte("x")}}.Render(mail.Page{Paragraphs: []string{"Hi"}})
	require.Error(t, err)
}
