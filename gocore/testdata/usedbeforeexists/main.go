// Program for TestInstallOrderIsCheckedByTheCompiler: tickets are installed
// before the users they need exist. This must not compile.
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

	installTickets(app, users)
	users := installIdentity(app)
	_ = users
}
