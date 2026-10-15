// Package http is identity's HTTP surface: the sign-in, session and language
// routes as typed declarations, their inputs and outputs, the cookies the
// tokens travel in, the session payload and the middleware that authenticates
// every other route. identity.Install mounts it; the declared contract is
// Routes.
//
//	POST /v1/auth/login          public   {login, password}   the session payload, tokens set as cookies
//	POST /v1/auth/refresh        public   {refresh_token}      the same, with rotated tokens
//	POST /v1/auth/logout                                       ends the session, clears the cookies
//	GET  /v1/auth/me                                           the session payload
//	POST /v1/auth/change-locale           {locale}             sets the person's language
//	POST /v1/auth/login-as                {user_id}            signs in as somebody, when the application allows it
//	POST /v1/auth/login-as/return                              ends that session and signs in as oneself, no password
//
// The paths carry their version; the application's own prefix goes in front
// (gocore.WithPrefix("/api") serves /api/v1/auth/login). A v2 is new
// declarations in another group beside these.
//
// The session payload is {user, expires_at, impersonator, access, navigation}: impersonator (absent for a
// person's own session) is who really signed in, when the session was opened by login-as; user is what
// the UserProjector makes of the account (it must hold the id), access is the
// authz engine's MyAccess of the person, navigation the application's menu cut
// down to what they may reach. The client reads it with vue-core's
// parseSessionPayload.
//
// Authentication (Authenticator, which Authentication wraps) resolves the access token to a live session of an
// active account and makes the account the request's user, or the application's own type with the RequestUser
// function. A request with the X-API-Key header (Keys) is a service account's instead, authenticated by the key
// alone, and a request made with a key that is not a read is audited. The per-user rate limit of the application
// counts either.
//
// GET /embed (Embed, served by identity.WithEmbed at the site root) opens a session of 24 hours that cannot be
// refreshed for a person a host application vouches for with a signed single-use token.
//
// Tokens travel in HttpOnly cookies (access_token for the whole site,
// refresh_token for the refresh route only, wherever the application serves it) and an access token is also
// accepted as "Authorization: Bearer".
package http
