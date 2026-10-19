// go-core/tenancy/tenant.go
package tenancy

// TenantAware is implemented by users who belong to a specific tenant.
// This is separate from Identifiable — a user can be authenticated (Identifiable)
// without necessarily belonging to a tenant (system admins, for example).
type TenantAware interface {
	// GetTenantID returns the tenant the user is scoped to; 0 marks a super-admin with no tenant, who crosses tenants.
	GetTenantID() int
}
