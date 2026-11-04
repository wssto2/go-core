package application

import (
	domainlead "example.com/app/internal/lead"
	domainpermission "example.com/app/internal/iam/permission"
)

const local = "crm.lead"

const ghost = domainlead.Namespace

var (
	_ = "crm.lead:view"
	_ = "crm.lead:erase"
	_ = "crm.ghost:view"
	_ = "gorm:column"
	_ = "required:min"
	_ = domainpermission.Of("crm.lead", "manage")
	_ = domainpermission.Of(local, "erase")
	_ = domainpermission.Of(domainlead.Namespace, "export")
	_ = domainpermission.Of(ghost, "view")
	_ = domainpermission.Of(unknown(), "view")
)

func unknown() string { return "" }
