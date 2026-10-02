package contract_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wssto2/go-core/contract"
	"github.com/wssto2/go-core/route"
)

type ShowInput struct {
	ID int `path:"id"`
}

type Ticket struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
}

var (
	Show   = route.Get[ShowInput, Ticket]("/tickets/:id").Name("tickets.show").Requires("tickets.ticket:view")
	Events = route.Raw("GET", "/events").Public()
	Routes = route.Group("tickets", Show, Events)
)

// Generate writes a folder per group, from the declarations alone.
func ExampleGenerate() {
	dir, _ := os.MkdirTemp("", "contract")
	defer func() { _ = os.RemoveAll(dir) }()

	if err := contract.Generate(dir, Routes); err != nil {
		fmt.Println(err)
		return
	}

	src, _ := os.ReadFile(filepath.Join(dir, "tickets", "routes.ts"))
	_, table, _ := strings.Cut(string(src), "\n") // the first line names the go-core version
	fmt.Print(table)
	// Output:
	// import { route } from "@wssto2/vue-core";
	// import type { ShowInput } from "./schemas";
	// import type { Ticket } from "./entities";
	//
	// export const ticketsRoutes = {
	//   show: route<ShowInput, Ticket>("GET", "/tickets/:id", { permission: "tickets.ticket:view" }),
	//   getEvents: route.raw("GET", "/events", { public: true }),
	// } as const;
}
