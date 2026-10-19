// go-core/tenancy/tenant.go
package tenancy

// TenantAware is implemented by users who belong to a specific tenant.
// This is separate from Identifiable — a user can be authenticated (Identifiable)
// without necessarily belonging to a tenant (system admins, for example).
type TenantAware interface {
	GetTenantID() int // returns 0 if the user is a super-admin with no tenant
	HasTenant() bool  // true if scoped to a tenant; false marks a super-admin that crosses tenants
}
