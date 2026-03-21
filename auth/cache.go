package auth

type CacheProvider interface {
	GetUser(id int) (Identifiable, bool)
	SetUser(id int, user Identifiable)
	InvalidateUser(id int)
	GetAccountRole(id int) (*AccountRole, bool)
	SetAccountRole(id int, role *AccountRole)
	InvalidateAccountRole(id int)
	GetAccountType(id int) (*AccountType, bool)
	SetAccountType(id int, at *AccountType)
	InvalidateAccountType(id int)
}
