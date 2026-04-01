package auth

import "slices"

type User struct {
	ID           int      `json:"id" gorm:"primaryKey"`
	Username     string   `json:"username" gorm:"size:255;not null"`
	PasswordHash string   `json:"-" gorm:"size:255;not null"`
	Policies     []string `json:"policies"   gorm:"type:json;serializer:json"`
}

func (u User) GetID() int {
	return u.ID
}

func (u User) GetEmail() string {
	return u.Username
}

func (u User) GetPolicies() []string {
	return u.Policies
}

func (u User) HasPolicy(policy string) bool {
	return slices.Contains(u.Policies, policy)
}

func (u User) HasAnyPolicy(policy ...string) bool {
	return slices.ContainsFunc(policy, u.HasPolicy)
}

func (u User) HasAllPolicies(policy ...string) bool {
	for _, p := range policy {
		if !u.HasPolicy(p) {
			return false
		}
	}
	return true
}
