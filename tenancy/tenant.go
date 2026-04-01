// go-core/tenancy/tenant.go
package tenancy

// Tenant represents any organization-level entity that owns/scopes resources.
// In arv-next this is a Dealer. In a SaaS product it might be a Company or Workspace.
// Applications implement this interface on their own concrete type.
type Tenant interface {
	GetTenantID() int
	GetTenantName() string
}

// TenantAware is implemented by users who belong to a specific tenant.
// This is separate from Identifiable — a user can be authenticated (Identifiable)
// without necessarily belonging to a tenant (system admins, for example).
type TenantAware interface {
	GetTenantID() int // returns 0 if the user is a super-admin with no tenant
	HasTenant() bool  // true if this user is scoped to a specific tenant
}
