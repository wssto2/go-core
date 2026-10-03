// Command modulets writes the TypeScript contract of every go-core module.
//
//	go run github.com/wssto2/go-core/cmd/modulets <dir>
//
// writes <dir>/identity/{entities,schemas,routes}.ts and <dir>/access/…, each
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
	return contract.Generate(dir, identity.Routes, access.Routes)
}
