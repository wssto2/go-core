// Package mailtext is the text of the mails identity sends, in English: the
// defaults for the development sink and a first run. An application writes its
// own copy, in its own languages and with its own look, as a mail.Renderer it
// gives identity.WithMailContent; that renderer need only know the mails it
// changes, identity falls back to these for the rest.
//
// Each mail has a name and the data its templates read:
//
//	identity.email-code        CodeData             a code to confirm a new address
//	identity.password-changed  PasswordChangedData  told after a password changed
//	identity.email-changed     EmailChangedData     told to the previous address
//
// A template is a file per locale: en/identity.email-code.subject.txt (one line),
// en/identity.email-code.txt (the body) and, optionally, .html.
package mailtext

import (
	"embed"

	"github.com/wssto2/go-core/mail"
)

// The mails identity sends.
const (
	// EmailCode is the code that confirms a new e-mail address. Data: CodeData.
	EmailCode mail.Name = "identity.email-code"
	// PasswordChanged tells a person their password was changed. Data: PasswordChangedData.
	PasswordChanged mail.Name = "identity.password-changed"
	// EmailChanged tells the previous address that the account moved on. Data: EmailChangedData.
	EmailChanged mail.Name = "identity.email-changed"
)

// CodeData is the data of EmailCode.
type CodeData struct {
	// Name is the person's name.
	Name string
	// Code is the digits to type.
	Code string
	// Minutes is how long the code lasts, from now.
	Minutes int
}

// PasswordChangedData is the data of PasswordChanged.
type PasswordChangedData struct {
	Name string
}

// EmailChangedData is the data of EmailChanged: the address the account moved to is masked ("a***@example.com").
type EmailChangedData struct {
	Name     string
	NewEmail string
}

//go:embed en
var files embed.FS

// Defaults renders the English text in this package.
var Defaults = mail.Templates(files)
