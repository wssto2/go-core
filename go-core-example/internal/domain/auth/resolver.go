package auth

// IdentityResolver is implemented as Module.IdentityResolver, which delegates
// to Service.ResolveIdentity. This keeps the resolver DB-backed without
// exposing the module's internal fields.
//
// Usage in main.go:
//
//	authMod := auth.NewModule(tokenCfg)
//	bootstrap.New(cfg).WithJWTAuth(authMod.IdentityResolver).WithModules(authMod, ...)
