// Command modulets writes the TypeScript contract of every go-core module.
//
//	go run github.com/wssto2/go-core/cmd/modulets <dir>
//
// writes <dir>/identity/{entities,schemas,routes}.ts, <dir>/access/…, <dir>/notification/… (the inbox, and a person's e-mail preferences and quiet hours) and
// <dir>/events/… (the dead letters of the event queue), <dir>/serviceaccount/… (service accounts, their keys
// and their access) and <dir>/incident/… (the incident log), each
// file starting with the go-core version that wrote it. vue-core runs it to
// commit the module types its screens use; an app generates only its own
// features' types (contract.Generate).
package main

import (
	"fmt"
	"os"

	"github.com/wssto2/go-core/access"
	"github.com/wssto2/go-core/contract"
	"github.com/wssto2/go-core/identity"
	sahttp "github.com/wssto2/go-core/identity/serviceaccount/http"
	"github.com/wssto2/go-core/incident"
	"github.com/wssto2/go-core/notification"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: go run github.com/wssto2/go-core/cmd/modulets <dir>")
		os.Exit(2)
	}

	if err := generate(os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// generate writes every module's contract into dir, one folder per module.
func generate(dir string) error {
	err := contract.Generate(dir,
		identity.Routes, access.Routes, notification.Routes, notification.DeadLetterRoutes, sahttp.Contract, incident.Routes)
	if err != nil {
		return fmt.Errorf("modulets: %w", err)
	}

	return nil
}
