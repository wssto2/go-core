package permission

// The catalogue's own package may name anything: "crm.ghost:view".
func Of(namespace, verb string) string { return namespace + ":" + verb }

func IDs() []string { return []string{"crm.lead:view", "crm.lead:manage"} }

var Ghost = "crm.ghost:view"
