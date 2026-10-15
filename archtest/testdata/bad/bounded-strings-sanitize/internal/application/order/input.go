package order

type Both struct{ First string }

func (Both) Validate() error { return nil }
func (Both) Sanitize()       {}

type One struct{ Second string }

func (One) Validate() error { return nil }
