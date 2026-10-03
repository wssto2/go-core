package mailtext_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/notification/mailtext"
)

func ExampleDefaults() {
	c, _ := mailtext.Defaults.Render(context.Background(), "hr", mailtext.Email, mailtext.EmailData{
		Name: "Ana", Title: "Dodijeljen vam je Login bug", OpenURL: "https://tickets.example.com/tickets/7",
	})

	fmt.Println(c.Subject)
	// Output: Dodijeljen vam je Login bug
}

// Every language the module ships says who it is for, what happened, how to open it and why the person gets it.
func TestEveryLanguageRendersTheMail(t *testing.T) {
	for locale, want := range map[string][]string{
		"en":    {"Hello Ana,", "Open: https://x.example/t/7", "in your notification settings"},
		"hr":    {"Poštovani Ana,", "Otvori: https://x.example/t/7", "postavkama obavijesti"},
		"bs":    {"Poštovani Ana,", "Otvori: https://x.example/t/7", "postavkama obavještenja"},
		"sl":    {"Pozdravljeni Ana,", "Odpri: https://x.example/t/7", "nastavitvah obvestil"},
		"hr-HR": {"Poštovani Ana,"}, // the language of the locale
		"de":    {"Hello Ana,"},     // none: English
		"":      {"Hello Ana,"},     // none: English
	} {
		c, err := mailtext.Defaults.Render(t.Context(), locale, mailtext.Email, mailtext.EmailData{
			Name: "Ana", Title: "Title <b>", Body: "Body text", OpenURL: "https://x.example/t/7",
		})
		require.NoError(t, err, locale)
		require.Equal(t, "Title <b>", c.Subject, locale)

		for _, s := range want {
			require.Contains(t, c.Text, s, locale)
		}

		require.Contains(t, c.Text, "Body text", locale)
		require.Contains(t, c.HTML, `<a href="https://x.example/t/7">`, locale)
		require.Contains(t, c.HTML, "Title &lt;b&gt;", "the HTML is escaped: "+locale)
	}
}

func TestWithoutABodyOrALinkThereIsNeitherBodyNorButton(t *testing.T) {
	c, err := mailtext.Defaults.Render(t.Context(), "en", mailtext.Email, mailtext.EmailData{Name: "Ana", Title: "Hi"})
	require.NoError(t, err)
	require.NotContains(t, c.Text, "Open")
	require.NotContains(t, c.HTML, "<a ")
	require.NotContains(t, c.HTML, "<p></p>")
}
