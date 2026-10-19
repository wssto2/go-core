package auth

// Identifiable is the interface for any entity that can be authenticated.
type Identifiable interface {
	GetID() int
}
