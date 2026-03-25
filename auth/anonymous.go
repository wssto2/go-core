package auth

type Anonymous struct{}

func (p Anonymous) GetID() int                           { return 0 }
func (p Anonymous) GetEmail() string                     { return "" }
func (p Anonymous) GetPolicies() []string                { return nil }
func (p Anonymous) HasPolicy(policy string) bool         { return false }
func (p Anonymous) HasAnyPolicy(policy ...string) bool   { return false }
func (p Anonymous) HasAllPolicies(policy ...string) bool { return false }
