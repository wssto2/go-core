//nolint:exhaustruct,exhaustruct_v5,gochecknoglobals,goconst,gosec,lll,paralleltest,varnamelen // tests in the style of the package's others
package mail_test

import (
	"encoding/base64"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/mail"
)

// logoPNG is a 1x1 PNG.
var logoPNG, _ = base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==")

// exampleBrand sets every field of a Brand to a value that is not the default.
var exampleBrand = mail.Brand{
	Name:     "Example",
	Logo:     &mail.Inline{CID: "logo@example.com", ContentType: "image/png", Name: "logo.png", Data: logoPNG},
	Color:    "#0f766e",
	Lang:     "en",
	Greeting: "Hello,",
	Footer:   "This message was sent automatically.",
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

func checkGolden(t *testing.T, name, got string) {
	t.Helper()

	if os.Getenv("UPDATE_GOLDEN") != "" {
		require.NoError(t, os.WriteFile("testdata/"+name, []byte(got), 0o600))
	}

	want, err := os.ReadFile("testdata/" + name)
	require.NoError(t, err)
	require.Equal(t, string(want), got)
}

// The frame is fixed markup: the goldens in testdata change only when the layout is meant to (write them with
// UPDATE_GOLDEN=1).
func TestBrandRendersTheFrame(t *testing.T) {
	full, err := exampleBrand.Render(fullPage)
	require.NoError(t, err)
	checkGolden(t, "layout_full.html", full.HTML)

	minimal, err := exampleBrand.Render(mail.Page{Paragraphs: []string{"Hello."}})
	require.NoError(t, err)
	checkGolden(t, "layout_minimal.html", minimal.HTML)
	require.Contains(t, minimal.HTML, "Hello,", "the brand's greeting fills in a page without one")
}

func TestBrandTextIsTheSamePageAsPlainText(t *testing.T) {
	c, err := exampleBrand.Render(fullPage)
	require.NoError(t, err)

	require.Equal(t, `Dear Ana & Bob,

Your order <42> is ready for pickup.

Bring "this" mail with you.

Location: Main & Sons
Date: 2026-10-16

Questions? Just reply.

Open order: https://app.example.com/orders/42?tab=details&lang=en

-- 
This message was sent automatically.
`, c.Text, "plain text is not HTML-escaped; an empty row is left out")

	short, err := exampleBrand.Render(mail.Page{Paragraphs: []string{"Hello."}})
	require.NoError(t, err)
	require.Equal(t, "Hello,\n\nHello.\n\n-- \nThis message was sent automatically.\n", short.Text)
}

func TestBrandLogoTravelsInTheContent(t *testing.T) {
	c, err := exampleBrand.Render(mail.Page{Paragraphs: []string{"Hi"}})
	require.NoError(t, err)
	require.Equal(t, []mail.Inline{*exampleBrand.Logo}, c.Inline)
	require.Contains(t, c.HTML, `src="cid:logo@example.com"`)

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
