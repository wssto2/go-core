package order

import (
	"example.com/app/internal/migrations"
	domainpermission "example.com/app/internal/permission"
	"example.com/framework"
)

func build(app *framework.App) {
	app.Modules()
	app.WithModules()
	_ = migrations.Modules
	_ = domainpermission.Permission{Namespace: "x"}
	_ = domainpermission.Permission{}
}
