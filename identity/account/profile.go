package account

import (
	"context"
	"errors"
	"strings"

	"github.com/wssto2/go-core/apperr"
)

// Reasons of the profile's refusals beside those in rules.go.
const (
	// ReasonPasswordMismatch is a new password and its confirmation that differ.
	ReasonPasswordMismatch apperr.Reason = "identity.password.mismatch"
	// ReasonCodeInvalid is a code that is not CodeLength digits.
	ReasonCodeInvalid apperr.Reason = "identity.code.invalid"
)

// ProfileDeps are what Profile is built on. Users, Reauth, History, Changes and
// Transact are required. Policy defaults to Passwords{}. Codes is optional:
// without it, changing the e-mail address is refused (identity.email.disabled),
// which is how an application without a mail sender runs.
type ProfileDeps struct {
	Users    *Users
	Reauth   *Reauth
	History  SignInHistory
	Changes  ChangeLog
	Transact Transactor
	Policy   PasswordPolicy
	Codes    *Codes
}

// Profile is what a signed-in person does with their own account: the details,
// the password, the e-mail address (confirmed with a code), their sessions and
// their sign-in history. Every method acts on the account id it is given, which
// the route takes from the session: there is nothing to authorize beyond being
// signed in (IAM-PROFILE-001).
type Profile struct {
	d ProfileDeps
}

// NewProfile builds the service, or says which dependency is missing.
func NewProfile(d ProfileDeps) (*Profile, error) {
	switch {
	case d.Users == nil:
		return nil, errors.New("identity: ProfileDeps.Users is missing: pass the Users service account.New built")
	case d.Reauth == nil:
		return nil, errors.New("identity: ProfileDeps.Reauth is missing: pass the service account.NewReauth built")
	case d.History == nil:
		return nil, errors.New("identity: ProfileDeps.History is missing: pass a SignInHistory, for example gormstore.New(db).SignIns")
	case d.Changes == nil:
		return nil, errors.New("identity: ProfileDeps.Changes is missing: pass a ChangeLog, for example gormstore.NewChangeLog(db)")
	case d.Transact == nil:
		return nil, errors.New("identity: ProfileDeps.Transact is missing: pass a Transactor, for example database.NewTransactor(db)")
	}

	if d.Policy == nil {
		d.Policy = Passwords{}
	}

	p := &Profile{d: d}
	d.Users.profile = p

	return p, nil
}

func (p *Profile) deps() Deps { return p.d.Users.deps }

func (p *Profile) within(ctx context.Context, fn func(ctx context.Context) error) error {
	err := p.d.Transact.WithinTransaction(ctx, fn)

	var ae *apperr.AppError
	if err == nil || errors.As(err, &ae) {
		return err
	}

	return apperr.Internal(err)
}

// ProfileView is the account with the e-mail change that waits for its code, if any.
type ProfileView struct {
	Account
	// PendingEmail is the live e-mail change (its Target is the new address), nil when none.
	PendingEmail *PendingCode
}

// Get returns the profile of the account (the active one the session is for).
func (p *Profile) Get(ctx context.Context, accountID int) (ProfileView, error) {
	acc, err := p.d.Users.Get(ctx, accountID)
	if err != nil {
		return ProfileView{}, err
	}

	view := ProfileView{Account: acc}

	if p.d.Codes != nil {
		pending, found, err := p.d.Codes.Pending(ctx, accountID, PurposeEmailChange)
		if err != nil {
			return ProfileView{}, err
		}

		if found {
			view.PendingEmail = &pending
		}
	}

	return view, nil
}

// ProfileDetails is a person's own details: the login is not among them (IAM-PROFILE-002).
type ProfileDetails struct {
	AccountID int
	Name      string
	Phone     string
}

// UpdateDetails writes the name (required) and the phone (IAM-PROFILE-001): only
// those, so a stale page cannot overwrite what an administrator changed meanwhile,
// and what changed is on the account's history. The login is the person's to
// read, not to change (IAM-PROFILE-002), and the address changes by code.
func (p *Profile) UpdateDetails(ctx context.Context, in ProfileDetails) (ProfileView, error) {
	name, phone := strings.TrimSpace(in.Name), strings.TrimSpace(in.Phone)

	if err := checkAll(checkName(name), checkPhone(phone)); err != nil {
		return ProfileView{}, err
	}

	current, err := p.d.Users.Get(ctx, in.AccountID)
	if err != nil {
		return ProfileView{}, err
	}

	if current.Name != name || current.Phone != phone {
		var fields []string

		before, after := map[string]string{}, map[string]string{}

		for _, f := range []struct{ field, was, now string }{{"name", current.Name, name}, {"phone", current.Phone, phone}} {
			if f.was != f.now {
				fields = append(fields, f.field)
				before[f.field], after[f.field] = f.was, f.now
			}
		}

		err = p.within(ctx, func(ctx context.Context) error {
			if err := p.deps().Accounts.Update(ctx, in.AccountID, Changes{Name: &name, Phone: &phone}); err != nil {
				return stored(err)
			}

			return p.d.Changes.Record(ctx, Change{AccountID: in.AccountID, Action: ChangeProfile, Fields: fields, Before: before, After: after})
		})
		if err != nil {
			return ProfileView{}, err
		}
	}

	return p.Get(ctx, in.AccountID)
}

// PasswordChange is a person changing their own password. KeepToken is the
// access token of the session that makes the change, which stays signed in.
type PasswordChange struct {
	AccountID       int
	CurrentPassword string
	NewPassword     string
	Confirmation    string
	KeepToken       string
}

// ChangePassword re-confirms the current password under the lock of
// IAM-REAUTH-001 (a wrong one is the field error current_password), applies the
// password policy, requires the confirmation to match and the new password to
// differ (IAM-PROFILE-003). The new hash, the history row and the ending of every
// other session are one transaction: a change that fails leaves the sessions
// intact, and one that succeeds signs out everywhere else, including the sessions
// the person opened by signing in as somebody. Passwords are never trimmed.
func (p *Profile) ChangePassword(ctx context.Context, in PasswordChange) error {
	acc, err := p.d.Users.Get(ctx, in.AccountID)
	if err != nil {
		return err
	}

	if err := p.confirm(ctx, acc, in.CurrentPassword); err != nil {
		return err
	}

	if err := checkPassword(p.d.Policy, "new_password", in.NewPassword); err != nil {
		return err
	}

	if in.NewPassword != in.Confirmation {
		return invalid("new_password_confirmation", ReasonPasswordMismatch)
	}

	if p.deps().Hasher.Matches(acc.PasswordHash, in.NewPassword) {
		return invalid("new_password", ReasonPasswordSame)
	}

	hash, err := p.deps().Hasher.Hash(in.NewPassword)
	if err != nil {
		return apperr.Internal(err)
	}

	err = p.within(ctx, func(ctx context.Context) error {
		// The history first: what is most likely to fail is written before anything that matters,
		// and a database rolls the rest back. The hash is never recorded, only the fact of the change.
		if err := p.d.Changes.Record(ctx, Change{AccountID: acc.ID, Action: ChangePassword, Fields: []string{"password"}}); err != nil {
			return err
		}

		if err := p.deps().Accounts.SetPasswordHash(ctx, acc.ID, hash); err != nil {
			return stored(err)
		}

		return p.d.Users.RevokeSessions(ctx, RevokeSessionsInput{AccountID: acc.ID, KeepToken: in.KeepToken})
	})
	if err != nil {
		return err
	}

	p.deps().Notices.PasswordChanged(ctx, acc.ID, 0)

	return nil
}

// confirm is the re-confirmation of the current password: a wrong one is a field
// error, a locked one passes through with its reason.
func (p *Profile) confirm(ctx context.Context, acc Account, password string) error {
	err := p.d.Reauth.Confirm(ctx, acc.ID, acc.PasswordHash, password)
	if errors.Is(err, ErrWrongPassword) {
		return invalid("current_password", ReasonPasswordWrong)
	}

	return err
}

// RequestEmail is a person asking to change their e-mail address.
type RequestEmail struct {
	AccountID       int
	Email           string
	CurrentPassword string
	IP              string
}

// RequestEmailChange re-confirms the password (the same lock), checks the new
// address (valid, not the current one, not another account's) and mails a code to
// it, which is what confirms the person has the mailbox (IAM-PROFILE-004).
// Nothing about the account changes yet; Get shows the change as pending. Without
// a mail sender it is refused (identity.email.disabled).
func (p *Profile) RequestEmailChange(ctx context.Context, in RequestEmail) (PendingCode, error) {
	if p.d.Codes == nil {
		return PendingCode{}, emailDisabled()
	}

	acc, err := p.d.Users.Get(ctx, in.AccountID)
	if err != nil {
		return PendingCode{}, err
	}

	if err := p.confirm(ctx, acc, in.CurrentPassword); err != nil {
		return PendingCode{}, err
	}

	email := NormalizeEmail(in.Email)
	if err := p.checkNewEmail(ctx, acc, email); err != nil {
		return PendingCode{}, err
	}

	return p.issue(ctx, acc, email, in.IP)
}

// ResendEmailChange mails a fresh code to the pending address: always the
// stored one, never an address from the request (IAM-PROFILE-004), subject to
// the cooldown and the cap of IAM-OTP-003.
func (p *Profile) ResendEmailChange(ctx context.Context, accountID int, ip string) (PendingCode, error) {
	if p.d.Codes == nil {
		return PendingCode{}, emailDisabled()
	}

	acc, err := p.d.Users.Get(ctx, accountID)
	if err != nil {
		return PendingCode{}, err
	}

	pending, found, err := p.d.Codes.Pending(ctx, accountID, PurposeEmailChange)
	if err != nil {
		return PendingCode{}, err
	}

	if !found {
		return PendingCode{}, apperr.New(nil, string(ReasonCodeNoPending), apperr.CodeInvalidArgument).WithReason(ReasonCodeNoPending).WithLog(apperr.LevelInfo)
	}

	return p.issue(ctx, acc, pending.Target, ip)
}

// ConfirmEmail is the code a person typed.
type ConfirmEmail struct {
	AccountID int
	Code      string
}

// ConfirmEmailChange verifies the code (IAM-OTP-002; spaces and dashes in it are
// ignored, so a pasted "123 456" works), checks again that the address is still
// free (it may have been taken since the request; the code is then used up and the
// person starts over), writes it and puts the change on the history. The previous
// address is told through Notices.EmailChanged.
func (p *Profile) ConfirmEmailChange(ctx context.Context, in ConfirmEmail) (ProfileView, error) {
	if p.d.Codes == nil {
		return ProfileView{}, emailDisabled()
	}

	code := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}

		return -1
	}, in.Code)
	if len(code) != CodeLength {
		return ProfileView{}, invalid("code", ReasonCodeInvalid)
	}

	acc, err := p.d.Users.Get(ctx, in.AccountID)
	if err != nil {
		return ProfileView{}, err
	}

	email, err := p.d.Codes.Verify(ctx, acc.ID, PurposeEmailChange, code)
	if err != nil {
		return ProfileView{}, err
	}

	if err := p.checkNewEmail(ctx, acc, email); err != nil {
		return ProfileView{}, err
	}

	err = p.within(ctx, func(ctx context.Context) error {
		if err := p.deps().Accounts.Update(ctx, acc.ID, Changes{Email: &email}); err != nil {
			return stored(err)
		}

		return p.d.Changes.Record(ctx, Change{
			AccountID: acc.ID, Action: ChangeEmail, Fields: []string{"email"},
			Before: map[string]string{"email": acc.Email}, After: map[string]string{"email": email},
		})
	})
	if err != nil {
		return ProfileView{}, err
	}

	p.deps().Notices.EmailChanged(ctx, acc.ID, acc.Email, email)

	return p.Get(ctx, acc.ID)
}

// CancelEmailChange drops the pending e-mail change; having none is fine.
func (p *Profile) CancelEmailChange(ctx context.Context, accountID int) error {
	if p.d.Codes == nil {
		return nil
	}

	return p.d.Codes.Cancel(ctx, accountID, PurposeEmailChange)
}

func (p *Profile) issue(ctx context.Context, acc Account, email, ip string) (PendingCode, error) {
	return p.d.Codes.Issue(ctx, IssueCode{
		AccountID: acc.ID, Purpose: PurposeEmailChange, Target: email, Recipient: email, Name: acc.Name, Locale: acc.Locale, IP: ip,
	})
}

func (p *Profile) checkNewEmail(ctx context.Context, acc Account, email string) error {
	if err := checkEmail("email", email); err != nil {
		return err
	}

	if email == NormalizeEmail(acc.Email) {
		return invalid("email", ReasonEmailUnchanged)
	}

	return emailFree(ctx, p.deps().Accounts, email, acc.ID)
}

func emailDisabled() error {
	return apperr.BadRequest(string(ReasonEmailDisabled)).WithReason(ReasonEmailDisabled)
}

// Sessions lists the person's live sessions, the latest used first (IAM-USER-004).
func (p *Profile) Sessions(ctx context.Context, accountID int) ([]Session, error) {
	return p.d.Users.Sessions(ctx, accountID)
}

// RevokeSession ends one of the person's own sessions. current is the id of the
// session the request came with: ending it is signing out, not this
// (identity.session.current).
func (p *Profile) RevokeSession(ctx context.Context, accountID, sessionID, current int) error {
	return p.d.Users.RevokeSession(ctx, RevokeSessionInput{AccountID: accountID, SessionID: sessionID, Keep: current})
}

// SignIns lists the person's own sign-in history, newest first, with how many
// rows there are (IAM-USER-003).
func (p *Profile) SignIns(ctx context.Context, accountID int, page Paging) ([]SignInEntry, int, error) {
	rows, total, err := p.d.History.Entries(ctx, accountID, page.offset(), page.resolved().PerPage)
	if err != nil {
		return nil, 0, apperr.Internal(err)
	}

	return rows, total, nil
}
