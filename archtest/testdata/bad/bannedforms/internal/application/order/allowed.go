package order

import domainpermission "example.com/app/internal/permission"

var _ = domainpermission.Permission{Verb: "read"}
