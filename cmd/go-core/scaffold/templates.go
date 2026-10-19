package scaffold

// eventTpl generates a standalone event file (for go-core new event).
const eventTpl = `package {{.Package}}

import "github.com/wssto2/go-core/event"

// {{.Pascal}}Event carries the payload for the {{.Name}} domain event.
type {{.Pascal}}Event struct {
	// TODO: add event fields
}

func ({{.Pascal}}Event) EventName() string { return "{{.Package}}.{{actionName .Package .Pascal}}" }

// Compile-time check.
var _ event.Event = {{.Pascal}}Event{}
`
