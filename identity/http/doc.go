// Package http is identity's HTTP surface: the sign-in, session and language
// routes as typed declarations, their inputs and outputs, the cookies the
// tokens travel in, the session payload and the middleware that authenticates
// every other route. identity.Install mounts it; the declared contract is
// Routes.
//
//	POST /auth/login          public   {login, password}   the session payload, tokens set as cookies
//	POST /auth/refresh        public   {refresh_token}      the same, with rotated tokens
//	POST /auth/logout                                       ends the session, clears the cookies
//	GET  /auth/me                                           the session payload
//	POST /auth/change-locale           {locale}             sets the person's language
//	POST /auth/login-as                {user_id}            signs in as somebody, when the application allows it
//
// The session payload is {user, expires_at, access, navigation}: user is what
// the UserProjector makes of the account (it must hold the id), access is the
// authz engine's MyAccess of the person, navigation the application's menu cut
// down to what they may reach. The client reads it with vue-core's
// parseSessionPayload.
//
// Tokens travel in HttpOnly cookies (access_token for the whole site,
// refresh_token for the refresh path only) and an access token is also
// accepted as "Authorization: Bearer".
package http
