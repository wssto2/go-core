package mailtext_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/identity/mailtext"
	"github.com/wssto2/go-core/mail"
)

func TestEveryMailRendersInEnglishAndForOtherLocales(t *testing.T) {
	for name, data := range map[string]any{
		string(mailtext.EmailCode):       mailtext.CodeData{Name: "Ana", Code: "123456", Minutes: 15},
		string(mailtext.PasswordChanged): mailtext.PasswordChangedData{Name: "Ana"},
		string(mailtext.EmailChanged):    mailtext.EmailChangedData{Name: "Ana", NewEmail: "a***@example.com"},
	} {
		for _, locale := range []string{"en", "hr", "pt-BR", ""} {
			c, err := mailtext.Defaults.Render(t.Context(), locale, mail.Name(name), data)
			require.NoError(t, err, name+" "+locale)
			require.NotEmpty(t, c.Subject, name)
			require.Contains(t, c.Text, "Ana", name)
		}
	}

	c, err := mailtext.Defaults.Render(t.Context(), "en", mailtext.EmailCode, mailtext.CodeData{Name: "<Ana>", Code: "123456", Minutes: 15})
	require.NoError(t, err)
	require.Contains(t, c.Text, "123456")
	require.Contains(t, c.HTML, "&lt;Ana&gt;", "the HTML escapes the name, the text does not")
}

func ExampleDefaults() {
	c, _ := mailtext.Defaults.Render(context.Background(), "en", mailtext.PasswordChanged, mailtext.PasswordChangedData{Name: "Ana"})
	fmt.Println(c.Subject)
	// Output: Your password was changed
}
