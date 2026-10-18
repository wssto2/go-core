# Logic IDs: IAM-PREF-001

Title: A person's preferences are one JSON object, merged by patch, capped, and checked by the application

Status: Approved (owner decision 2026-10-18, M4 question 4: preferences become identity's, the application's rules are hooks)
Last updated: 2026-10-18
Module: `identity` (`identity/preferences` service, `identity/http` route, `identity/preferences/gormstore` table)

## Summary

With `identity.WithPreferences`, each person has one JSON object the client keeps between devices (dark mode,
dismissed tips). The client sends the part it changes; the server merges it into what is stored and answers the whole
object. Without the option none of this exists: no route, no table, no `preferences` in the session payload.

## IAM-PREF-001 — Merge, cap, validate, store

1. `PUT /v1/auth/preferences` takes `{"preferences": {...}}` and answers `{"preferences": {...}}`, the merged object.
   It is **person only**: a service account is refused (`403`), and so is a request that is not signed in (`401`). It
   writes the preferences of the account the request acts as (while signed in as somebody, that is the somebody).
2. The patch must be a **JSON object**; anything else (missing, `null`, an array, a string) is `422`, field
   `preferences`, rule `must_be_object`.
3. The default merge is **RFC 7396**: a key sent replaces the stored one, a `null` removes it, objects merge in
   depth, anything else (arrays included) is replaced whole. A stored value that is not a JSON object counts as `{}`.
   The application may replace the merge (`Preferences.Merge`) and refuse a patch with a validation error.
4. The merged object, and the patch, are **at most 16 KiB** (`Preferences.MaxBytes`): beyond that `422`, field
   `preferences`, rule `max` with `params.max`. Nothing is written.
5. **`Preferences.Validate`** sees the merged object before it is stored. A `*validation.ValidationError`
   (`preferences.Invalid(field, code)` makes one) is answered as it is, `422`; any other error is `422` on
   `preferences`. Nothing is written.
6. The session payload (login, refresh, me, login-as, return) carries the stored object as `preferences`, `{}` when
   nothing is stored.
7. Reading and writing are not one atomic step: two patches of one person that arrive together may overwrite each
   other's keys.

Code: `identity/preferences/preferences.go` (`Service.Update`), `identity/preferences/mergepatch.go` (`MergePatch`),
`identity/http/profile.go` (`updatePreferences`), `identity/http/handler.go` (`payload`).
