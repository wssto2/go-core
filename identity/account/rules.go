package account

import (
	"net/mail"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/wssto2/go-core/apperr"
)

// Reasons of the refusals of the users module and the profile. A refused field
// is also in the error's Fields, named as the input names it.
const (
	ReasonLoginInvalid    apperr.Reason = "identity.login.invalid"
	ReasonLoginTaken      apperr.Reason = "identity.login.taken"
	ReasonNameInvalid     apperr.Reason = "identity.name.invalid"
	ReasonEmailInvalid    apperr.Reason = "identity.email.invalid"
	ReasonEmailTaken      apperr.Reason = "identity.email.taken"
	ReasonEmailUnchanged  apperr.Reason = "identity.email.unchanged"
	ReasonEmailDisabled   apperr.Reason = "identity.email.disabled"
	ReasonPhoneInvalid    apperr.Reason = "identity.phone.invalid"
	ReasonPasswordWeak    apperr.Reason = "identity.password.weak"
	ReasonPasswordWrong   apperr.Reason = "identity.password.wrong"
	ReasonPasswordSame    apperr.Reason = "identity.password.unchanged"
	ReasonSelfDeactivate  apperr.Reason = "identity.account.self_deactivation"
	ReasonAlreadyInactive apperr.Reason = "identity.account.already_inactive"
	ReasonAlreadyActive   apperr.Reason = "identity.account.already_active"
	// ReasonListViewInvalid and ReasonListOrderInvalid refuse a list's view or sort column that does not exist.
	ReasonListViewInvalid  apperr.Reason = "identity.list.view_invalid"
	ReasonListOrderInvalid apperr.Reason = "identity.list.order_invalid"
	// ReasonActivityAreaUnknown refuses an area the application did not name; ReasonActivityRangeInvalid
	// a range that ends before it starts or a date that is not YYYY-MM-DD.
	ReasonActivityAreaUnknown  apperr.Reason = "identity.activity.area_unknown"
	ReasonActivityRangeInvalid apperr.Reason = "identity.activity.range_invalid"
)

// invalid is a refused field: a validation error with the reason, naming the field.
func invalid(field string, reason apperr.Reason, params ...map[string]any) error {
	return apperr.New(nil, string(reason), apperr.CodeValidationError).
		WithReason(reason, params...).WithFields(map[string]string{field: string(reason)}).WithLog(apperr.LevelInfo)
}

func conflict(field string, reason apperr.Reason) error {
	return apperr.New(nil, string(reason), apperr.CodeAlreadyExists).
		WithReason(reason).WithFields(map[string]string{field: string(reason)}).WithLog(apperr.LevelInfo)
}

// NormalizeEmail is how an address is compared and stored: trimmed and lower-case.
func NormalizeEmail(email string) string { return strings.ToLower(strings.TrimSpace(email)) }

// checkLogin refuses a login that is empty, too long or has white space or control characters in it.
func checkLogin(login string) error {
	if login == "" || utf8.RuneCountInString(login) > LoginMax || strings.IndexFunc(login, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return invalid("login", ReasonLoginInvalid, map[string]any{"max": LoginMax})
	}

	return nil
}

func checkName(name string) error {
	if name == "" || utf8.RuneCountInString(name) > NameMax {
		return invalid("name", ReasonNameInvalid, map[string]any{"max": NameMax})
	}

	return nil
}

func checkPhone(phone string) error {
	if utf8.RuneCountInString(phone) > PhoneMax {
		return invalid("phone", ReasonPhoneInvalid, map[string]any{"max": PhoneMax})
	}

	return nil
}

// checkEmail refuses an address that is not one ("ana@example.com", without a
// display name) or is too long. email is already normalised.
func checkEmail(field, email string) error {
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email || utf8.RuneCountInString(email) > EmailMax {
		return invalid(field, ReasonEmailInvalid, map[string]any{"max": EmailMax})
	}

	return nil
}

// PasswordPolicy names the rules a new password breaks, none when it is fine.
// The rule names reach the client as params.rules of identity.password.weak, to
// translate.
type PasswordPolicy interface {
	Violations(password string) []string
}

// Passwords is the default PasswordPolicy: at least MinLength characters
// (default 8) and at most 72 bytes, which is as much of a password as bcrypt
// reads. An application that keeps older rules, such as digits and symbols,
// passes its own policy to identity.WithPasswordPolicy.
type Passwords struct{ MinLength int }

// Violations implements PasswordPolicy: "min_length" and "max_length".
func (p Passwords) Violations(password string) []string {
	shortest := p.MinLength
	if shortest <= 0 {
		shortest = 8
	}

	var broken []string

	if utf8.RuneCountInString(password) < shortest {
		broken = append(broken, "min_length")
	}

	if len(password) > 72 {
		broken = append(broken, "max_length")
	}

	return broken
}

// checkPassword refuses a password that breaks the policy, naming every rule.
func checkPassword(policy PasswordPolicy, field, password string) error {
	if broken := policy.Violations(password); len(broken) > 0 {
		return invalid(field, ReasonPasswordWeak, map[string]any{"rules": broken})
	}

	return nil
}
