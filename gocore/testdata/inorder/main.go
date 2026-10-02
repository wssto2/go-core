// Program for TestInstallOrderIsCheckedByTheCompiler: features installed in
// data-flow order build.
package main

import (
	"github.com/wssto2/go-core/bootstrap"
	"github.com/wssto2/go-core/gocore"
)

type Users interface{ Name(id int) string }

type memoryUsers struct{}

func (memoryUsers) Name(int) string { return "ada" }

func installIdentity(*gocore.App) Users { return memoryUsers{} }

func installTickets(*gocore.App, Users) {}

func main() {
	app := gocore.New(bootstrap.DefaultConfig())

	users := installIdentity(app)
	installTickets(app, users)
}
