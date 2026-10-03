// Package identitytest is what a test of identity, or of a feature built on
// it, needs: memory stores that pass the same conformance suite as the SQL
// one, a clock the test moves, and a Users over them.
//
//	users := identitytest.Users(t, identitytest.Account(1, "ana", "secret"))
//	ana, _ := users.Get(ctx, 1)
//
// Kit gives the sign-in service as well:
//
//	kit := identitytest.New(t, []account.Account{identitytest.Account(1, "ana", "secret")})
//	signed, err := kit.SignIn.Login(ctx, account.LoginInput{Login: "ana", Password: "secret"})
//	kit.Clock.Advance(16 * time.Minute)
package identitytest
