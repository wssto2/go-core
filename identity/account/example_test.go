package account_test

import (
	"context"
	"fmt"
	"time"

	"github.com/wssto2/go-core/apperr"
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
