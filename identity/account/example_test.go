package account_test

import (
	"context"
	"fmt"
	"time"

	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/identity/account"
	"github.com/wssto2/go-core/identity/identitytest"
)

// kit builds the services over memory stores with one account, ana, whose
// password is "secret".
func kit() identitytest.Kit {
	return identitytest.New(exampleT{}, []account.Account{identitytest.Account(1, "ana", "secret")},
		identitytest.WithImpersonation(permitAll{}))
}

func ExampleNew() {
	accounts, signIns, sessions := identitytest.NewAccounts(), identitytest.NewSignIns(), identitytest.NewSessions()

	svc, err := account.New(account.Deps{
		Accounts: accounts, SignIns: signIns, Sessions: sessions, Clock: identitytest.NewClock(identitytest.Epoch),
	}, account.Config{TokenTTL: 8 * time.Hour})

	fmt.Println(svc.SignIn != nil, svc.Users != nil, err)

	_, err = account.New(account.Deps{}, account.Config{})
	fmt.Println(err)
	// Output:
	// true true <nil>
	// identity: Deps.Accounts is missing: pass a Store, for example gormstore.New(db).Accounts
}

func ExampleSignIn_Login() {
	k := kit()
	ctx := context.Background()

	signed, err := k.SignIn.Login(ctx, account.LoginInput{Login: "Ana", Password: "secret", Device: "curl", IP: "10.0.0.1"})
	fmt.Println(signed.Account.Login, signed.Credentials.Access != "", err)

	// A wrong password and an unknown login are refused alike.
	_, err = k.SignIn.Login(ctx, account.LoginInput{Login: "ana", Password: "nope"})
	fmt.Println(apperr.HasReason(err, account.ReasonSignInFailed))
	_, err = k.SignIn.Login(ctx, account.LoginInput{Login: "nobody", Password: "nope"})
	fmt.Println(apperr.HasReason(err, account.ReasonSignInFailed))
	// Output:
	// ana true <nil>
	// true
	// true
}

func ExampleSignIn_Refresh() {
	k := kit()
	ctx := context.Background()

	first, _ := k.SignIn.Login(ctx, account.LoginInput{Login: "ana", Password: "secret"})
	second, err := k.SignIn.Refresh(ctx, account.RefreshInput{Token: first.Credentials.Refresh})
	fmt.Println(second.Credentials.Access != first.Credentials.Access, err)

	// A refresh token works once.
	_, err = k.SignIn.Refresh(ctx, account.RefreshInput{Token: first.Credentials.Refresh})
	fmt.Println(apperr.HasReason(err, account.ReasonSessionInvalid))
	// Output:
	// true <nil>
	// true
}

func ExampleSignIn_Logout() {
	k := kit()
	ctx := context.Background()

	signed, _ := k.SignIn.Login(ctx, account.LoginInput{Login: "ana", Password: "secret"})
	fmt.Println(k.SignIn.Logout(ctx, signed.Credentials.Access))

	_, err := k.SignIn.Authenticate(ctx, signed.Credentials.Access)
	fmt.Println(apperr.HasReason(err, account.ReasonSessionInvalid))
	// Output:
	// <nil>
	// true
}

func ExampleSignIn_LoginAs() {
	k := identitytest.New(exampleT{}, []account.Account{
		identitytest.Account(1, "ana", "secret"), identitytest.Account(2, "boris", "hunter2"),
	}, identitytest.WithImpersonation(permitAll{}))
	ctx := context.Background()

	signed, err := k.SignIn.LoginAs(ctx, account.LoginAsInput{ActorID: 1, TargetID: 2})
	fmt.Println(signed.Account.Login, err)

	got, _ := k.SignIn.Authenticate(ctx, signed.Credentials.Access)
	fmt.Println("opened by account", got.Session.ActorID)
	// Output:
	// boris <nil>
	// opened by account 1
}

func ExampleSignIn_Authenticate() {
	k := kit()
	ctx := context.Background()

	signed, _ := k.SignIn.Login(ctx, account.LoginInput{Login: "ana", Password: "secret"})
	got, err := k.SignIn.Authenticate(ctx, signed.Credentials.Access)
	fmt.Println(got.Account.Login, err)

	_, err = k.SignIn.Authenticate(ctx, "unknown")
	fmt.Println(apperr.HasReason(err, account.ReasonSessionInvalid))
	// Output:
	// ana <nil>
	// true
}

func ExampleUsers_Get() {
	users := identitytest.Users(exampleT{}, identitytest.Account(1, "ana", "secret"))

	acc, err := users.Get(context.Background(), 1)
	fmt.Println(acc.Login, acc.Locale, err)

	_, err = users.Get(context.Background(), 2)
	fmt.Println(apperr.HasReason(err, account.ReasonAccountNotFound))
	// Output:
	// ana en <nil>
	// true
}

func ExampleUsers_ChangeLocale() {
	users := identitytest.Users(exampleT{}, identitytest.Account(1, "ana", "secret"))
	ctx := context.Background()

	fmt.Println(users.ChangeLocale(ctx, account.ChangeLocaleInput{AccountID: 1, Locale: "hr"}))

	acc, _ := users.Get(ctx, 1)
	fmt.Println(acc.Locale)
	// Output:
	// <nil>
	// hr
}

func ExampleUsers_Sessions() {
	k := kit()
	ctx := context.Background()

	_, _ = k.SignIn.Login(ctx, account.LoginInput{Login: "ana", Password: "secret", Device: "Mozilla/5.0 (X11; Linux x86_64; rv:130.0) Firefox/130.0"})

	sessions, _ := k.Users.Sessions(ctx, 1)
	fmt.Println(len(sessions), sessions[0].Label())
	// Output: 1 Firefox · Linux
}

func ExampleUsers_RevokeSession() {
	k := kit()
	ctx := context.Background()

	_, _ = k.SignIn.Login(ctx, account.LoginInput{Login: "ana", Password: "secret"})
	sessions, _ := k.Users.Sessions(ctx, 1)

	fmt.Println(k.Users.RevokeSession(ctx, account.RevokeSessionInput{AccountID: 1, SessionID: sessions[0].ID}))

	left, _ := k.Users.Sessions(ctx, 1)
	fmt.Println(len(left))
	// Output:
	// <nil>
	// 0
}

func ExampleUsers_RevokeSessions() {
	k := kit()
	ctx := context.Background()

	here, _ := k.SignIn.Login(ctx, account.LoginInput{Login: "ana", Password: "secret"})
	_, _ = k.SignIn.Login(ctx, account.LoginInput{Login: "ana", Password: "secret"})

	// Everywhere but the session the request came with.
	fmt.Println(k.Users.RevokeSessions(ctx, account.RevokeSessionsInput{AccountID: 1, KeepToken: here.Credentials.Access}))

	left, _ := k.Users.Sessions(ctx, 1)
	fmt.Println(len(left))
	// Output:
	// <nil>
	// 1
}

func ExampleLock_LockedUntil() {
	now := identitytest.Epoch
	wrong := account.SignInEntry{Event: account.WrongPassword, CreatedAt: now.Add(-time.Minute)}

	until, locked := account.Lock{After: 2, For: 15 * time.Minute}.LockedUntil([]account.SignInEntry{wrong, wrong}, now)
	fmt.Println(locked, until.Sub(now))
	// Output: true 14m0s
}

func ExampleLockEvent() {
	fmt.Println(account.LockEvent(account.WrongPassword), account.LockEvent(account.LockedOut))
	// Output: true false
}

func ExampleDeviceLabel() {
	fmt.Println(account.DeviceLabel("Mozilla/5.0 (Windows NT 10.0) Firefox/130.0"))
	fmt.Println(account.DeviceLabel("curl/8.4.0"))
	// Output:
	// Firefox · Windows
	// curl/8.4.0
}

func ExampleSession_Label() {
	s := account.Session{Device: "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0) Mobile/15E148"}
	fmt.Println(s.Label())
	// Output: Safari · iPhone
}

func ExampleNormalizeLogin() {
	fmt.Println(account.NormalizeLogin("  Ana.Horvat "))
	// Output: ana.horvat
}

func ExampleValidLocale() {
	fmt.Println(account.ValidLocale("hr"), account.ValidLocale("pt-BR"), account.ValidLocale("Croatian"))
	// Output: true true false
}

func ExampleBcrypt() {
	hasher := account.Bcrypt{Cost: 4}
	hash, _ := hasher.Hash("secret")
	fmt.Println(hasher.Matches(hash, "secret"), hasher.Matches(hash, "other"))
	// Output: true false
}

func ExampleNewToken() {
	token, err := account.NewToken()
	fmt.Println(len(token), err)
	// Output: 43 <nil>
}

func ExampleNoNotices() {
	// The default does nothing with the facts it hears.
	account.NoNotices.SignedInAs(context.Background(), 1, 2)
	fmt.Println("ok")
	// Output: ok
}

// *Users names people for access.SubjectDirectory, which asks for the display
// name of the subjects it is about to show.
func ExampleUsers_SubjectNames() {
	users := identitytest.Users(exampleT{}, identitytest.Account(1, "ana", "secret"))

	names, _ := users.SubjectNames(context.Background(), []authz.Subject{
		{Kind: authz.KindUser, ID: 1}, {Kind: authz.KindUser, ID: 2}, {Kind: authz.KindServiceAccount, ID: 1},
	})
	fmt.Println(names)
	// Output: map[user:1:ana]
}

func ExampleCodes() {
	k := kit()
	ctx := context.Background()

	// Ask for a code for a new address. The test Mailbox holds what the person would be mailed.
	_, err := k.Codes.Issue(ctx, account.IssueCode{
		AccountID: 1, Purpose: account.PurposeEmailChange, Target: "ana@new.example", Recipient: "ana@new.example", Name: "Ana", Locale: "en",
	})
	fmt.Println(err)

	code := k.Mailbox.Last().Code
	fmt.Println(len(code), "digits")

	// A wrong code costs an attempt; the right one confirms the target, once.
	wrong := "000000"
	if code == wrong {
		wrong = "000001"
	}

	_, err = k.Codes.Verify(ctx, 1, account.PurposeEmailChange, wrong)
	fmt.Println(apperr.HasReason(err, account.ReasonCodeMismatch))

	target, err := k.Codes.Verify(ctx, 1, account.PurposeEmailChange, code)
	fmt.Println(target, err)

	_, err = k.Codes.Verify(ctx, 1, account.PurposeEmailChange, code)
	fmt.Println(apperr.HasReason(err, account.ReasonCodeExpired))
	// Output:
	// <nil>
	// 6 digits
	// true
	// ana@new.example <nil>
	// true
}

func ExampleNewCodes() {
	_, err := account.NewCodes(account.CodesDeps{
		Store: identitytest.NewCodes(), Sender: &identitytest.Mailbox{}, Clock: identitytest.NewClock(identitytest.Epoch), Secret: "too short",
	}, account.CodeRules{})
	fmt.Println(err)
	// Output: identity: CodesDeps.Secret needs at least 32 characters: set a long random secret, for example from an environment variable
}

func ExampleReauth_Confirm() {
	k := kit()
	ctx := context.Background()

	ana, _ := k.Users.Get(ctx, 1)

	fmt.Println(k.Reauth.Confirm(ctx, 1, ana.PasswordHash, "secret"))
	fmt.Println(k.Reauth.Confirm(ctx, 1, ana.PasswordHash, "nope") == account.ErrWrongPassword)

	// Five wrong passwords lock re-confirmation: from then on even the right one is not checked.
	for range 4 {
		_ = k.Reauth.Confirm(ctx, 1, ana.PasswordHash, "nope")
	}

	err := k.Reauth.Confirm(ctx, 1, ana.PasswordHash, "secret")
	fmt.Println(apperr.HasReason(err, account.ReasonReauthLocked))
	// Output:
	// <nil>
	// true
	// true
}

func ExampleNewReauth() {
	_, err := account.NewReauth(account.ReauthDeps{}, account.Lock{})
	fmt.Println(err)
	// Output: identity: ReauthDeps.Store is missing: pass a ReauthStore, for example gormstore.New(db).Reauth
}

func ExampleAdmin_Create() {
	k := kit()
	ctx := context.Background()

	dora, err := k.Admin.Create(ctx, account.CreateAccount{
		Login: "Dora", Name: "Dora Horvat", Email: "Dora@Example.com", Locale: "hr", Password: "a long password", ActorID: 1,
	})
	fmt.Println(dora.Login, dora.Email, dora.Active, err)

	_, err = k.Admin.Create(ctx, account.CreateAccount{Login: "dora", Name: "Another", Email: "other@example.com", Locale: "hr", Password: "a long password"})
	fmt.Println(apperr.HasReason(err, account.ReasonLoginTaken))

	_, err = k.Admin.Create(ctx, account.CreateAccount{Login: "eva", Name: "Eva", Email: "eva@example.com", Locale: "hr", Password: "short"})
	fmt.Println(apperr.HasReason(err, account.ReasonPasswordWeak))
	// Output:
	// dora dora@example.com true <nil>
	// true
	// true
}

func ExampleAdmin_Deactivate() {
	// The application's hook refuses while the person still owns something.
	owns := account.DeactivationHookFunc(func(_ context.Context, a account.Account, _ int) error {
		if a.Login == "boris" {
			return apperr.BadRequest("owns leads").WithReason("crm.owns_leads")
		}

		return nil
	})

	k := identitytest.New(exampleT{}, []account.Account{
		identitytest.Account(1, "ana", "secret"), identitytest.Account(2, "boris", "secret"), identitytest.Account(3, "cvita", "secret"),
	}, identitytest.WithDeactivationHooks(owns))
	ctx := context.Background()

	err := k.Admin.Deactivate(ctx, account.DeactivateInput{ID: 2, ActorID: 1})
	fmt.Println(apperr.HasReason(err, "crm.owns_leads"))

	fmt.Println(k.Admin.Deactivate(ctx, account.DeactivateInput{ID: 3, ActorID: 1}))

	cvita, _ := k.Users.Get(ctx, 3)
	fmt.Println(cvita.Active)
	// Output:
	// true
	// <nil>
	// false
}

func ExampleAdmin_List() {
	k := kit()
	ctx := context.Background()

	for _, login := range []string{"dora", "eva"} {
		_, _ = k.Admin.Create(ctx, account.CreateAccount{Login: login, Name: login, Email: login + "@example.com", Locale: "en", Password: "a long password"})
	}

	page, _ := k.Admin.List(ctx, account.ListInput{View: account.ViewAll, Search: "d", Paging: account.Paging{PerPage: 10}})

	for _, row := range page.Rows {
		fmt.Println(row.Login, row.LastSignIn.IsZero())
	}

	fmt.Println(page.Total, page.LastPage)
	// Output:
	// dora true
	// 1 1
}

func ExampleNewAdmin() {
	_, err := account.NewAdmin(account.AdminDeps{})
	fmt.Println(err)
	// Output: identity: AdminDeps.Users is missing: pass the Users service account.New built
}

func ExamplePasswords() {
	policy := account.Passwords{MinLength: 10}

	fmt.Println(policy.Violations("short"))
	fmt.Println(policy.Violations("long enough password"))
	// Output:
	// [min_length]
	// []
}

func ExampleDiscardNotices() {
	// Embed it to hear only some of the facts.
	var n account.Notices = deactivations{}

	n.AccountCreated(context.Background(), 1, 2) // not heard
	n.AccountDeactivated(context.Background(), 1, 2)
	// Output: account 1 deactivated by 2
}

type deactivations struct{ account.DiscardNotices }

func (deactivations) AccountDeactivated(_ context.Context, id, actor int) {
	fmt.Println("account", id, "deactivated by", actor)
}

func ExampleProfile_RequestEmailChange() {
	k := kit()
	ctx := context.Background()

	// Ana asks to move to a new address: a code goes to the new address, nothing changes yet.
	_, err := k.Profile.RequestEmailChange(ctx, account.RequestEmail{AccountID: 1, Email: "ana@new.example", CurrentPassword: "secret"})
	fmt.Println(err, k.Mailbox.Last().Recipient)

	view, _ := k.Profile.Get(ctx, 1)
	fmt.Println(view.Email == "", view.PendingEmail.Target)

	// She types the code that was mailed; now the address is hers.
	view, err = k.Profile.ConfirmEmailChange(ctx, account.ConfirmEmail{AccountID: 1, Code: k.Mailbox.Last().Code})
	fmt.Println(view.Email, view.PendingEmail == nil, err)
	// Output:
	// <nil> ana@new.example
	// true ana@new.example
	// ana@new.example true <nil>
}

func ExampleProfile_ChangePassword() {
	k := kit()
	ctx := context.Background()

	err := k.Profile.ChangePassword(ctx, account.PasswordChange{AccountID: 1, CurrentPassword: "nope", NewPassword: "a better one", Confirmation: "a better one"})
	fmt.Println(apperr.HasReason(err, account.ReasonPasswordWrong))

	err = k.Profile.ChangePassword(ctx, account.PasswordChange{AccountID: 1, CurrentPassword: "secret", NewPassword: "a better one", Confirmation: "a better one"})
	fmt.Println(err)

	_, err = k.SignIn.Login(ctx, account.LoginInput{Login: "ana", Password: "a better one"})
	fmt.Println(err)
	// Output:
	// true
	// <nil>
	// <nil>
}

func ExampleProfile_UpdateDetails() {
	k := kit()

	view, err := k.Profile.UpdateDetails(context.Background(), account.ProfileDetails{AccountID: 1, Name: " Ana Anić ", Phone: "099 123"})
	fmt.Println(view.Name, view.Phone, err)
	// Output: Ana Anić 099 123 <nil>
}

func ExampleNewProfile() {
	_, err := account.NewProfile(account.ProfileDeps{})
	fmt.Println(err)
	// Output: identity: ProfileDeps.Users is missing: pass the Users service account.New built
}

func ExampleActivityActionOf() {
	for _, audit := range []string{"create", "updated", "password", "delete"} {
		fmt.Println(audit, "->", account.ActivityActionOf(audit))
	}
	// Output:
	// create -> created
	// updated -> changed
	// password -> changed
	// delete -> deleted
}

func ExampleRecordSet_Has() {
	contracts := account.RecordSet{Types: []string{"offers"}, Prefixes: []string{"contracts."}}

	fmt.Println(contracts.Has("offers"), contracts.Has("contracts.line"), contracts.Has("customers"))
	// Output: true true false
}

func ExampleAdmin_Activity() {
	k := kit()
	ctx := context.Background()

	_, _ = k.Admin.Create(ctx, account.CreateAccount{Login: "dora", Name: "Dora", Email: "dora@example.com", Locale: "en", Password: "a long password", ActorID: 1})

	rows, total, _ := k.Admin.Activity(ctx, 1, account.ActivityFilter{Area: account.IdentityArea}, account.Paging{})
	for _, r := range rows {
		fmt.Println(r.Area, r.RecordType, r.Action, r.SignedInAs)
	}

	fmt.Println(total)
	// Output:
	// identity account created 0
	// 1
}

func ExampleActivityAreas_AreaOf() {
	areas, _ := account.NewActivityAreas(
		account.Area("crm").Types("customers", "offers").Prefix("contracts."),
		account.Area("vehicles").Types("vehicles"),
	)

	fmt.Println(areas.AreaOf("contracts.line"), areas.AreaOf("vehicles"), areas.AreaOf("account"), areas.AreaOf("settings"))
	// Output: crm vehicles identity other
}

func ExampleNewActivityAreas() {
	_, err := account.NewActivityAreas(account.Area("crm"))
	fmt.Println(err)
	// Output: identity: activity area "crm" covers no record type: give it .Types(...) or .Prefix(...), none of them empty
}

func ExampleActivityAreas_Keys() {
	areas, _ := account.NewActivityAreas(account.Area("crm").Types("customers"))

	fmt.Println(areas.Keys())
	// Output: [crm identity other]
}

func ExampleArea() {
	fmt.Println(account.Area("crm").Key())
	// Output: crm
}

func ExampleActivityArea_Types() {
	area := account.Area("crm").Types("customers", "offers")

	fmt.Println(area.Key())
	// Output: crm
}

func ExampleActivityArea_Prefix() {
	areas, _ := account.NewActivityAreas(account.Area("crm").Prefix("contracts."))

	fmt.Println(areas.AreaOf("contracts.line"), areas.AreaOf("contract"))
	// Output: crm other
}

func ExampleActivityArea_Key() {
	fmt.Println(account.Area("vehicles").Key())
	// Output: vehicles
}

func ExampleRecordSet_Empty() {
	fmt.Println(account.RecordSet{}.Empty(), account.RecordSet{Prefixes: []string{"a."}}.Empty())
	// Output: true false
}

func ExampleAdmin_ActivityCounts() {
	k := kit()
	ctx := context.Background()

	_, _ = k.Admin.Create(ctx, account.CreateAccount{Login: "dora", Name: "Dora", Email: "dora@example.com", Locale: "en", Password: "a long password", ActorID: 1})

	counts, _ := k.Admin.ActivityCounts(ctx, 1, account.ActivityFilter{})
	for _, c := range counts {
		fmt.Println(c.Area, c.Count)
	}
	// Output:
	// all 1
	// identity 1
	// other 0
}
