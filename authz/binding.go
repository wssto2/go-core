package authz

import "time"

// Binding gives a subject a role at a scope. A subject may have several; the
// effective access is their union, the widest winning.
type Binding struct {
	ID        int       `json:"id"`
	Subject   Subject   `json:"subject"`
	Role      RoleRef   `json:"role"`
	Scope     Scope     `json:"scope"`
	CreatedBy int       `json:"created_by,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}
