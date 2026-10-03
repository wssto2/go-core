package account_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/identity/account"
	"github.com/wssto2/go-core/identity/identitytest"
)

// seededWithEmail gives ana an address, which the seed does not.
func seededWithEmail(t *testing.T, opts ...identitytest.Option) identitytest.Kit {
	t.Helper()

	k := seeded(t, opts...)
	require.NoError(t, k.Accounts.Update(t.Context(), 1, account.Changes{Email: ptr("ana@old.test")}))

	return k
}

func TestProfileGetAndUpdateDetails(t *testing.T) {
	k := seededWithEmail(t)

	view, err := k.Profile.Get(t.Context(), 1)
	require.NoError(t, err)
	require.Equal(t, "ana", view.Login)
	require.Nil(t, view.PendingEmail)

	view, err = k.Profile.UpdateDetails(t.Context(), account.ProfileDetails{AccountID: 1, Name: "  Ana Anić ", Phone: " 099 111 "})
	require.NoError(t, err)
	require.Equal(t, "Ana Anić", view.Name)
	require.Equal(t, "099 111", view.Phone)
	require.Equal(t, "ana@old.test", view.Email, "the address is not part of the details")
	require.Equal(t, "ana", view.Login, "nor is the login (IAM-PROFILE-002)")

	pg1, _ := k.Admin.Changes(t.Context(), 1, account.ChangesAll, account.Paging{})
	changes := pg1.Rows
	require.Equal(t, account.ChangeProfile, changes[0].Action)
	require.Equal(t, []string{"name", "phone"}, changes[0].Fields)
	require.Equal(t, 0, changes[0].ActorID, "the person themselves")

	_, err = k.Profile.UpdateDetails(t.Context(), account.ProfileDetails{AccountID: 1, Name: "Ana Anić", Phone: "099 111"})
	require.NoError(t, err)

	pg2, _ := k.Admin.Changes(t.Context(), 1, account.ChangesAll, account.Paging{})
	total := pg2.Total
	require.Equal(t, 1, total, "saving what is there records nothing")

	_, err = k.Profile.UpdateDetails(t.Context(), account.ProfileDetails{AccountID: 1, Name: " "})
	require.True(t, apperr.HasReason(err, account.ReasonNameInvalid))

	_, err = k.Profile.UpdateDetails(t.Context(), account.ProfileDetails{AccountID: 1, Name: "Ana", Phone: "1234567890123456789012345678901"})
	require.True(t, apperr.HasReason(err, account.ReasonPhoneInvalid))

	_, err = k.Profile.UpdateDetails(t.Context(), account.ProfileDetails{AccountID: 99, Name: "Ana"})
	require.True(t, apperr.HasReason(err, account.ReasonAccountNotFound))
}

func change(current, next string) account.PasswordChange {
	return account.PasswordChange{AccountID: 2, CurrentPassword: current, NewPassword: next, Confirmation: next}
}

// IAM-PROFILE-003: a good change signs out every other session and keeps this one.
func TestChangePasswordSignsOutEverywhereElse(t *testing.T) {
	n := &notices{}
	k := seeded(t, identitytest.WithNotices(n))

	here, err := k.SignIn.Login(t.Context(), account.LoginInput{Login: "boris", Password: "hunter2", Device: "here"})
	require.NoError(t, err)

	there, err := k.SignIn.Login(t.Context(), account.LoginInput{Login: "boris", Password: "hunter2", Device: "there"})
	require.NoError(t, err)

	in := change("hunter2", "a better one")
	in.KeepToken = here.Credentials.Access
	require.NoError(t, k.Profile.ChangePassword(t.Context(), in))

	_, err = k.SignIn.Authenticate(t.Context(), here.Credentials.Access)
	require.NoError(t, err, "the session that made the change stays")

	_, err = k.SignIn.Authenticate(t.Context(), there.Credentials.Access)
	require.True(t, apperr.HasReason(err, account.ReasonSessionInvalid), "the others are gone")

	_, err = k.SignIn.Login(t.Context(), account.LoginInput{Login: "boris", Password: "a better one"})
	require.NoError(t, err)

	pg3, _ := k.Admin.Changes(t.Context(), 2, account.ChangesAll, account.Paging{})
	changes := pg3.Rows
	require.Equal(t, account.ChangePassword, changes[0].Action)
	require.Empty(t, changes[0].After, "never the value")
	require.Contains(t, n.events, "password 2 by 0")

	pg4, _ := k.Profile.SignIns(t.Context(), 2, account.SignInsAll, account.Paging{})
	rows := pg4.Rows
	require.Equal(t, account.SignedIn, rows[0].Event)
}

// A refused change leaves everything as it was, sessions included.
func TestARefusedPasswordChangeLeavesTheSessionsIntact(t *testing.T) {
	k := seeded(t, identitytest.WithPasswordPolicy(account.Passwords{MinLength: 4}))

	other, err := k.SignIn.Login(t.Context(), account.LoginInput{Login: "boris", Password: "hunter2"})
	require.NoError(t, err)

	for name, tc := range map[string]struct {
		in     account.PasswordChange
		reason apperr.Reason
		field  string
	}{
		"a wrong current password":    {change("nope", "a better one"), account.ReasonPasswordWrong, "current_password"},
		"a weak new password":         {change("hunter2", "abc"), account.ReasonPasswordWeak, "new_password"},
		"a confirmation that differs": {account.PasswordChange{AccountID: 2, CurrentPassword: "hunter2", NewPassword: "a better one", Confirmation: "another one"}, account.ReasonPasswordMismatch, "new_password_confirmation"},
		"the same password":           {change("hunter2", "hunter2"), account.ReasonPasswordSame, "new_password"},
	} {
		err := k.Profile.ChangePassword(t.Context(), tc.in)
		reason, field := fieldOf(t, err)
		require.Equal(t, tc.reason, reason, name)
		require.Equal(t, tc.field, field, name)
	}

	_, err = k.SignIn.Authenticate(t.Context(), other.Credentials.Access)
	require.NoError(t, err, "no refusal ended a session")

	_, err = k.SignIn.Login(t.Context(), account.LoginInput{Login: "boris", Password: "hunter2"})
	require.NoError(t, err, "the password is unchanged")

	pg5, _ := k.Admin.Changes(t.Context(), 2, account.ChangesAll, account.Paging{})
	total := pg5.Total
	require.Zero(t, total)
}

// A change that fails after the checks (here the history cannot be written) ends no session either:
// the history is written first, and a real database rolls the rest back (gormstore's users_test.go).
func TestAFailedPasswordTransactionLeavesTheSessionsIntact(t *testing.T) {
	k := seeded(t)

	profile, err := account.NewProfile(account.ProfileDeps{
		Users: k.Users, Reauth: k.Reauth, History: k.SignIns, Changes: failingChanges{k.Changes}, Transact: identitytest.Transactor{},
	})
	require.NoError(t, err)

	signed, err := k.SignIn.Login(t.Context(), account.LoginInput{Login: "boris", Password: "hunter2"})
	require.NoError(t, err)

	require.Error(t, profile.ChangePassword(t.Context(), change("hunter2", "a better one")))

	_, err = k.SignIn.Authenticate(t.Context(), signed.Credentials.Access)
	require.NoError(t, err, "the session lives")

	_, err = k.SignIn.Login(t.Context(), account.LoginInput{Login: "boris", Password: "hunter2"})
	require.NoError(t, err, "and so does the old password")
}

type failingChanges struct{ account.ChangeLog }

func (failingChanges) Record(context.Context, account.Change) error {
	return errors.New("audit is down")
}

// IAM-REAUTH-001: password and e-mail change share the lock.
func TestReconfirmationLocksAcrossPasswordAndEmailChange(t *testing.T) {
	k := seededWithEmail(t)

	for range 3 {
		err := k.Profile.ChangePassword(t.Context(), account.PasswordChange{AccountID: 1, CurrentPassword: "nope", NewPassword: "a better one", Confirmation: "a better one"})
		require.True(t, apperr.HasReason(err, account.ReasonPasswordWrong))
	}

	_, err := k.Profile.RequestEmailChange(t.Context(), account.RequestEmail{AccountID: 1, Email: "new@example.test", CurrentPassword: "nope"})
	require.True(t, apperr.HasReason(err, account.ReasonPasswordWrong), "the fourth")

	_, err = k.Profile.RequestEmailChange(t.Context(), account.RequestEmail{AccountID: 1, Email: "new@example.test", CurrentPassword: "nope"})
	require.True(t, apperr.HasReason(err, account.ReasonReauthLocked), "the fifth locks both")

	err = k.Profile.ChangePassword(t.Context(), account.PasswordChange{AccountID: 1, CurrentPassword: "secret", NewPassword: "a better one", Confirmation: "a better one"})
	require.True(t, apperr.HasReason(err, account.ReasonReauthLocked), "even the right password is not checked")
}

// IAM-PROFILE-004: request, resend, confirm.
func TestEmailChangeByCode(t *testing.T) {
	n := &notices{}
	k := seededWithEmail(t, identitytest.WithNotices(n))
	ctx := t.Context()

	pending, err := k.Profile.RequestEmailChange(ctx, account.RequestEmail{AccountID: 1, Email: " New@Example.test ", CurrentPassword: "secret", IP: "10.0.0.1"})
	require.NoError(t, err)
	require.Equal(t, "new@example.test", pending.Target)

	msg := k.Mailbox.Last()
	require.Equal(t, "new@example.test", msg.Recipient, "the code goes to the new address")
	require.Equal(t, "en", msg.Locale)

	view, err := k.Profile.Get(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, "ana@old.test", view.Email, "nothing changes yet")
	require.NotNil(t, view.PendingEmail)
	require.Equal(t, "new@example.test", view.PendingEmail.Target)
	require.Equal(t, 5, view.PendingEmail.AttemptsLeft)

	k.Clock.Advance(time.Minute)
	_, err = k.Profile.ResendEmailChange(ctx, 1, "10.0.0.1")
	require.NoError(t, err)
	require.Len(t, k.Mailbox.Sent(), 2)
	require.Equal(t, "new@example.test", k.Mailbox.Last().Recipient, "the stored target, never one from the request")

	_, err = k.Profile.ConfirmEmailChange(ctx, account.ConfirmEmail{AccountID: 1, Code: "12"})
	require.True(t, apperr.HasReason(err, account.ReasonCodeInvalid))

	code := k.Mailbox.Last().Code
	view, err = k.Profile.ConfirmEmailChange(ctx, account.ConfirmEmail{AccountID: 1, Code: code[:3] + " - " + code[3:]})
	require.NoError(t, err, "a pasted code with separators works")
	require.Equal(t, "new@example.test", view.Email)
	require.Nil(t, view.PendingEmail)

	pg6, _ := k.Admin.Changes(ctx, 1, account.ChangesAll, account.Paging{})
	changes := pg6.Rows
	require.Equal(t, account.ChangeEmail, changes[0].Action)
	require.Equal(t, map[string]string{"email": "ana@old.test"}, changes[0].Before)
	require.Equal(t, map[string]string{"email": "new@example.test"}, changes[0].After)
	require.Contains(t, n.events, "email 1 ana@old.test>new@example.test")

	_, err = k.Profile.ConfirmEmailChange(ctx, account.ConfirmEmail{AccountID: 1, Code: code})
	require.True(t, apperr.HasReason(err, account.ReasonCodeExpired), "a code is used once")
}

func TestEmailChangeRefusals(t *testing.T) {
	k := seededWithEmail(t)
	ctx := t.Context()

	require.NoError(t, k.Accounts.Update(ctx, 2, account.Changes{Email: ptr("boris@example.test")}))

	for name, tc := range map[string]struct {
		email  string
		reason apperr.Reason
	}{
		"not an address":        {"nope", account.ReasonEmailInvalid},
		"the current address":   {" ANA@old.test", account.ReasonEmailUnchanged},
		"another account's one": {"Boris@example.test", account.ReasonEmailTaken},
	} {
		_, err := k.Profile.RequestEmailChange(ctx, account.RequestEmail{AccountID: 1, Email: tc.email, CurrentPassword: "secret"})
		reason, field := fieldOf(t, err)
		require.Equal(t, tc.reason, reason, name)
		require.Equal(t, "email", field, name)
	}

	require.Empty(t, k.Mailbox.Sent(), "no code for a refused address")

	_, err := k.Profile.RequestEmailChange(ctx, account.RequestEmail{AccountID: 1, Email: "new@example.test", CurrentPassword: "nope"})
	require.True(t, apperr.HasReason(err, account.ReasonPasswordWrong))

	_, err = k.Profile.ResendEmailChange(ctx, 1, "")
	require.True(t, apperr.HasReason(err, account.ReasonCodeNoPending), "nothing to resend")
}

// IAM-PROFILE-004.4: the address may be taken between the request and the confirmation.
func TestAnAddressTakenMeanwhileIsRefusedAtConfirmation(t *testing.T) {
	k := seededWithEmail(t)
	ctx := t.Context()

	_, err := k.Profile.RequestEmailChange(ctx, account.RequestEmail{AccountID: 1, Email: "new@example.test", CurrentPassword: "secret"})
	require.NoError(t, err)

	require.NoError(t, k.Accounts.Update(ctx, 2, account.Changes{Email: ptr("new@example.test")}))

	_, err = k.Profile.ConfirmEmailChange(ctx, account.ConfirmEmail{AccountID: 1, Code: k.Mailbox.Last().Code})
	require.True(t, apperr.HasReason(err, account.ReasonEmailTaken))

	view, _ := k.Profile.Get(ctx, 1)
	require.Equal(t, "ana@old.test", view.Email)
	require.Nil(t, view.PendingEmail, "the code is used up: start over")
}

func TestCancelEmailChange(t *testing.T) {
	k := seededWithEmail(t)

	_, err := k.Profile.RequestEmailChange(t.Context(), account.RequestEmail{AccountID: 1, Email: "new@example.test", CurrentPassword: "secret"})
	require.NoError(t, err)

	require.NoError(t, k.Profile.CancelEmailChange(t.Context(), 1))
	require.NoError(t, k.Profile.CancelEmailChange(t.Context(), 1))

	view, _ := k.Profile.Get(t.Context(), 1)
	require.Nil(t, view.PendingEmail)

	_, err = k.Profile.ConfirmEmailChange(t.Context(), account.ConfirmEmail{AccountID: 1, Code: k.Mailbox.Last().Code})
	require.True(t, apperr.HasReason(err, account.ReasonCodeExpired))
}

// Without a mail sender the address cannot change by code, and the rest of the profile works.
func TestWithoutMailTheAddressCannotChange(t *testing.T) {
	k := seededWithEmail(t, identitytest.WithoutMail())
	ctx := t.Context()

	_, err := k.Profile.RequestEmailChange(ctx, account.RequestEmail{AccountID: 1, Email: "new@example.test", CurrentPassword: "secret"})
	require.True(t, apperr.HasReason(err, account.ReasonEmailDisabled))

	_, err = k.Profile.ResendEmailChange(ctx, 1, "")
	require.True(t, apperr.HasReason(err, account.ReasonEmailDisabled))

	_, err = k.Profile.ConfirmEmailChange(ctx, account.ConfirmEmail{AccountID: 1, Code: "123456"})
	require.True(t, apperr.HasReason(err, account.ReasonEmailDisabled))

	require.NoError(t, k.Profile.CancelEmailChange(ctx, 1))

	view, err := k.Profile.Get(ctx, 1)
	require.NoError(t, err)
	require.Nil(t, view.PendingEmail)

	require.NoError(t, k.Profile.ChangePassword(ctx, account.PasswordChange{AccountID: 1, CurrentPassword: "secret", NewPassword: "a better one", Confirmation: "a better one"}))
}

// IAM-USER-004: a person's own sessions; the one the request came with is signing out.
func TestOwnSessions(t *testing.T) {
	k := seeded(t)

	first, _ := k.SignIn.Login(t.Context(), account.LoginInput{Login: "boris", Password: "hunter2", Device: "Firefox"})
	_, _ = k.SignIn.Login(t.Context(), account.LoginInput{Login: "boris", Password: "hunter2", Device: "Chrome"})

	sessions, err := k.Profile.Sessions(t.Context(), 2)
	require.NoError(t, err)
	require.Len(t, sessions, 2)

	current, err := k.SignIn.Authenticate(t.Context(), first.Credentials.Access)
	require.NoError(t, err)

	err = k.Profile.RevokeSession(t.Context(), 2, current.Session.ID, current.Session.ID)
	require.True(t, apperr.HasReason(err, account.ReasonSessionCurrent))

	for _, s := range sessions {
		if s.ID != current.Session.ID {
			require.NoError(t, k.Profile.RevokeSession(t.Context(), 2, s.ID, current.Session.ID))
		}
	}

	sessions, _ = k.Profile.Sessions(t.Context(), 2)
	require.Len(t, sessions, 1)

	err = k.Profile.RevokeSession(t.Context(), 1, sessions[0].ID, 0)
	require.True(t, apperr.HasReason(err, account.ReasonSessionNotFound), "somebody else's session is not found")
}

func TestNewProfileNamesWhatIsMissing(t *testing.T) {
	k := seeded(t)
	full := account.ProfileDeps{Users: k.Users, Reauth: k.Reauth, History: k.SignIns, Changes: k.Changes, Transact: identitytest.Transactor{}}

	for want, mutate := range map[string]func(*account.ProfileDeps){
		"ProfileDeps.Users":    func(d *account.ProfileDeps) { d.Users = nil },
		"ProfileDeps.Reauth":   func(d *account.ProfileDeps) { d.Reauth = nil },
		"ProfileDeps.History":  func(d *account.ProfileDeps) { d.History = nil },
		"ProfileDeps.Changes":  func(d *account.ProfileDeps) { d.Changes = nil },
		"ProfileDeps.Transact": func(d *account.ProfileDeps) { d.Transact = nil },
	} {
		d := full
		mutate(&d)

		_, err := account.NewProfile(d)
		require.ErrorContains(t, err, want)
	}
}
