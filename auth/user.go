package auth

// User is the base authenticated user. T is the app-specific data payload —
// each application defines its own type and passes it here.
//
// Example (arv-next):
//
//	type AppData struct {
//	    Dealer          Dealer
//	    VehicleTypes    []VehicleType
//	    KoncesionariID  int
//	}
//	type User = auth.User[AppData]
type User[T any] struct {
	// Core identity — always present regardless of application
	ID       int      `json:"id"`
	Email    string   `json:"email"`
	Name     string   `json:"name"`
	Roles    []string `json:"roles"`
	Language string   `json:"language"`

	// App-specific payload — defined and loaded by each application
	Data T `json:"data"`
}

// GetID returns the user's ID. Satisfies the Identifiable interface.
func (u *User[T]) GetID() int {
	return u.ID
}

// GetEmail returns the user's email. Satisfies the Identifiable interface.
func (u *User[T]) GetEmail() string {
	return u.Email
}

// GetName returns the user's display name. Satisfies the Identifiable interface.
func (u *User[T]) GetName() string {
	return u.Name
}

// GetRoles returns the user's roles. Satisfies the Identifiable interface.
func (u *User[T]) GetRoles() []string {
	return u.Roles
}

// HasRole returns true if the user has the given role.
func (u *User[T]) HasRole(role string) bool {
	for _, r := range u.Roles {
		if r == role {
			return true
		}
	}
	return false
}

// HasAnyRole returns true if the user has at least one of the given roles.
func (u *User[T]) HasAnyRole(roles ...string) bool {
	for _, role := range roles {
		if u.HasRole(role) {
			return true
		}
	}
	return false
}

// HasAllRoles returns true if the user has all of the given roles.
func (u *User[T]) HasAllRoles(roles ...string) bool {
	for _, role := range roles {
		if !u.HasRole(role) {
			return false
		}
	}
	return true
}

// Identifiable is the interface guards and middleware depend on.
// Using an interface means arv-core never needs to know the concrete User[T] type.
type Identifiable interface {
	GetID() int
	GetEmail() string
	GetName() string
	GetRoles() []string
	HasRole(role string) bool
	HasAnyRole(roles ...string) bool
	HasAllRoles(roles ...string) bool
}
